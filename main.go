package main

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// TokenCache represents the structure saved to the encrypted cache file
type TokenCache struct {
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token"`
}

// GitHubTokenResponse represents the JSON response from the GitHub API
type GitHubTokenResponse struct {
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token"`
}

var (
	version = "main"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("gh-proxy version %s (commit: %s, built at: %s)\n", version, commit, date)
		os.Exit(0)
	}

	for _, e := range os.Environ() {
		fmt.Fprintf(os.Stderr, "PROXY_ENV: %s\n", e)
	}

	os.Exit(run())
}

func run() int {
	appID := os.Getenv("GITHUB_APP_ID")
	privateKeyRaw := os.Getenv("GITHUB_PRIVATE_KEY")
	installID := os.Getenv("GITHUB_INSTALLATION_ID")

	if appID == "" || privateKeyRaw == "" || installID == "" {
		fmt.Fprintf(os.Stderr, "❌ Missing required environment variables: GITHUB_APP_ID, GITHUB_PRIVATE_KEY, GITHUB_INSTALLATION_ID\n")
		return 1
	}

	args := os.Args[1:]
	ghPath, err := getGhPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: 'gh' binary not found. Set GH_CLI_PATH or ensure 'gh' is in PATH.\n")
		return 1
	}

	var command string
	var commandArgs []string

	if len(args) == 0 {
		// Default: no args, run 'gh'
		command = ghPath
		commandArgs = []string{}
	} else if args[0] == "--" {
		// Explicit override: everything after '--' is for 'gh'
		command = ghPath
		commandArgs = args[1:]
	} else if path, err := exec.LookPath(args[0]); err == nil {
		// If the first argument is an actual binary (e.g., 'git'), run it
		command = path
		commandArgs = args[1:]
	} else {
		// Assume it's a 'gh' command (e.g., 'auth', 'pr')
		command = ghPath
		commandArgs = args
	}

	// 1. Resolve Private Key (File or Raw string)
	privateKeyPEM, err := resolvePrivateKey(privateKeyRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to resolve private key: %v\n", err)
		return 1
	}

	// 2. Generate cache filenames and encryption keys based on inputs
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".gh-proxy-cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		os.MkdirAll(cacheDir, 0o700)
	}

	configHash := sha256.Sum256([]byte(appID + installID))
	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("token-%x.enc", configHash[:12]))

	// Derive a 32-byte AES key for at-rest encryption based on the private key and IDs
	encryptionKeyHash := sha256.Sum256([]byte(privateKeyPEM + appID + installID + "gh-proxy-salt"))
	encryptionKey := encryptionKeyHash[:]

	// 3. Attempt to load valid token from cache
	var validToken string
	cached, err := loadEncryptedCache(cacheFile, encryptionKey)

	// Buffer of X minutes to ensure token doesn't expire during command execution
	// where X is determined by the GITHUB_ROTATE_BUFFER_MINUTES environment variable
	rotateBuffer := getRotationBuffer()
	if err == nil && time.Now().Add(rotateBuffer).Before(cached.ExpiresAt) {
		validToken = cached.Token
	} else {
		// 4. Cache missed or token expired: Fetch a new one
		jwtStr, err := buildAppJWT(appID, privateKeyPEM)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to build JWT: %v\n", err)
			return 1
		}

		newTokenResp, err := fetchInstallationToken(jwtStr, installID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to fetch installation token: %v\n", err)
			return 1
		}

		validToken = newTokenResp.Token

		// Save the new token to the encrypted cache
		err = saveEncryptedCache(cacheFile, encryptionKey, TokenCache{
			Token:     validToken,
			ExpiresAt: newTokenResp.ExpiresAt,
		})
		if err != nil {
			// Non-fatal, but we should log it
			fmt.Fprintf(os.Stderr, "⚠️ Warning: Failed to cache token: %v\n", err)
		}
	}

	// 5. Execute the underlying command
	return executeCommand(append([]string{command}, commandArgs...), validToken)
}

func resolvePrivateKey(input string) (string, error) {
	input = strings.TrimSpace(input)

	// Attempt to resolve as file path
	path := input
	if strings.HasPrefix(input, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, input[2:])
	}

	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		b, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(b)), nil
		}
	}

	// Fallback: Treat as raw string
	formatted := strings.ReplaceAll(input, "\\n", "\n")
	if !strings.Contains(formatted, "-----BEGIN") {
		formatted = "-----BEGIN RSA PRIVATE KEY-----\n" + formatted + "\n-----END RSA PRIVATE KEY-----"
	}
	return formatted, nil
}

func buildAppJWT(appID, pemKey string) (string, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return "", errors.New("failed to decode PEM block containing private key")
	}

	var parsedKey any
	var err error
	if parsedKey, err = x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		if parsedKey, err = x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
			return "", errors.New("failed to parse private key as PKCS1 or PKCS8")
		}
	}

	rsaKey, ok := parsedKey.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("private key is not an RSA key")
	}

	now := time.Now().Unix()
	header := `{"alg":"RS256","typ":"JWT"}`
	payload := fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":"%s"}`, now-60, now+300, appID)

	b64Header := base64URLEncode([]byte(header))
	b64Payload := base64URLEncode([]byte(payload))
	signingInput := b64Header + "." + b64Payload

	hashed := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}

	return signingInput + "." + base64URLEncode(signature), nil
}

func fetchInstallationToken(jwtStr, installID string) (*GitHubTokenResponse, error) {
	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", installID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "github-mcp-connector-go")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp GitHubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	if tokenResp.Token == "" {
		return nil, errors.New("github response did not include a token")
	}

	return &tokenResp, nil
}

// executeCommand runs the CLI tool, sanitizes the env vars, and injects the tokens
func executeCommand(args []string, token string) int {
	binary, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Binary not found: %s\n", args[0])
		return 1
	}

	var env []string
	for _, e := range os.Environ() {
		// Filter out sensitive App config AND existing token variations
		// to prevent duplicate or conflicting environment variables
		if strings.HasPrefix(e, "GITHUB_APP_ID=") ||
			strings.HasPrefix(e, "GITHUB_PRIVATE_KEY=") ||
			strings.HasPrefix(e, "GITHUB_INSTALLATION_ID=") ||
			strings.HasPrefix(e, "GITHUB_TOKEN=") ||
			strings.HasPrefix(e, "GH_TOKEN=") ||
			strings.HasPrefix(e, "GITHUB_PERSONAL_ACCESS_TOKEN=") {
			continue
		}
		env = append(env, e)
	}

	// Append only the fresh, clean tokens
	env = append(env,
		"GITHUB_TOKEN="+token,
		"GH_TOKEN="+token,
		"GITHUB_PERSONAL_ACCESS_TOKEN="+token,
	)

	err = syscall.Exec(binary, args, env)
	fmt.Fprintf(os.Stderr, "❌ Failed to exec command: %v\n", err)
	return 1
}

// --- Encryption and Cache Helpers ---

func saveEncryptedCache(path string, key []byte, cache TokenCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return os.WriteFile(path, ciphertext, 0o600)
}

func loadEncryptedCache(path string, key []byte) (*TokenCache, error) {
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	data, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	var cache TokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// base64URLEncode encodes data to base64url format without padding as required by JWT standard
func base64URLEncode(b []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

func getRotationBuffer() time.Duration {
	// Default to 15 minutes
	const defaultBuffer = 15 * time.Minute

	val := os.Getenv("GITHUB_ROTATE_BUFFER_MINUTES")
	if val == "" {
		return defaultBuffer
	}

	if intVal, err := strconv.Atoi(val); err == nil {
		return time.Duration(intVal) * time.Minute
	}

	// Fallback to default on error
	return defaultBuffer
}

func getGhPath() (string, error) {
	// 1. Check for the user-defined path via environment variable
	if path := os.Getenv("GH_CLI_PATH"); path != "" {
		return path, nil
	}
	// 2. Fallback to looking it up in PATH
	return exec.LookPath("gh")
}
