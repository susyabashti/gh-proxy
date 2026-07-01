# gh-proxy

`gh-proxy` is a lightweight, zero-dependency Go proxy that acts as a secure, automated wrapper for the GitHub CLI (`gh`).

It is designed to manage the lifecycle of **GitHub App Installation Tokens**. It automatically fetches, caches, and rotates tokens using your GitHub App's private key, and injects them into your environment so that `gh` (or any other CLI tool) can operate as your App without manual authentication.

---

## Features

* **Seamless Integration**: Acts as a transparent proxy. If no command is provided, it defaults to running `gh`.
* **Smart Command Routing**: Automatically detects whether to run a command via `gh` or execute a system binary (like `git` or `ls`).
* **Automatic Lifecycle Management**: Automatically requests a new token before the current one expires.
* **Secure At-Rest Caching**: Stores tokens in an encrypted format (`AES-GCM`) in a dedicated cache directory.
* **Process Transparency**: Uses `syscall.Exec` to become the target process, ensuring signal handling (like `Ctrl+C`) and process identity are perfectly preserved.
* **GitHub App Ready**: Perfect for Agents, MCP servers, and automated pipelines.

---

## Setup

### 1. Installation

You can build the binary directly from source:

```bash
go build -o gh-proxy main.go
# Move it to a directory in your PATH
sudo mv gh-proxy /usr/local/bin/

```

### 2. Configuration (Environment Variables)

The proxy requires the following variables to manage your GitHub App:

* `GITHUB_APP_ID`: The numeric ID of your GitHub App.
* `GITHUB_INSTALLATION_ID`: The ID of the specific installation.
* `GITHUB_PRIVATE_KEY`: Either the raw PEM string or a path to the file (e.g., `./app.private-key.pem`).

---

## Use Cases

### Case 1: Transparent `gh` Alias

You can alias `gh` to `gh-proxy`. This makes every `gh` command automatically authenticated as your GitHub App.

**Add to your `.bashrc` or `.zshrc`:**

```bash
alias gh='GITHUB_APP_ID="123" GITHUB_INSTALLATION_ID="456" GITHUB_PRIVATE_KEY="/path/to/key.pem" gh-proxy'

```

### Case 2: Git Credential Helper

Use `gh-proxy` to securely provide credentials to Git for automated repository operations.

**Update your `.gitconfig`:**

```ini
[credential "https://github.com"]
    helper = !/usr/local/bin/gh-proxy gh auth git-credential
[credential "https://gist.github.com"]
    helper = !/usr/local/bin/gh-proxy gh auth git-credential

```

### Case 3: The `--` Separator

If you have a local binary or script that shares a name with a `gh` command, or you want to force `gh-proxy` to interpret arguments as `gh` commands, use the `--` separator:

```bash
# Forces the following arguments to be treated as 'gh' commands
./gh-proxy -- pr list

```

### Case 4: GitHub MCP Server Wrapper

For MCP servers, `gh-proxy` ensures the server always has valid credentials. The proxy replaces itself with the MCP process, ensuring a clean and efficient execution.

**Example:**

```bash
GITHUB_APP_ID="123" ... gh-proxy github-mcp-server stdio

```

---

## How it works

### Command Routing Logic

`gh-proxy` intelligently routes commands:

1. **No arguments?** Defaults to `gh`.
2. **Starts with `--`?** Forces `gh` execution for all subsequent arguments.
3. **Is the first argument an executable?** (e.g., `git`, `grep`): Executes that binary directly with the injected App Token.
4. **Fallback:** If not an executable, assumes it is a `gh` subcommand (e.g., `auth`, `pr`) and executes via `gh`.

### Transparency & Security

* **Token Rotation**: `gh-proxy` monitors `ExpiresAt`. If the token is near expiration (default 15m buffer), it refreshes the token before executing your command.
* **Process Replacement**: Using `syscall.Exec`, the proxy replaces its own process with the target command. This prevents shell alias recursion and ensures your agent's process tree remains clean and signals (like `SIGTERM`) reach the target directly.

---

## License

MIT. Built for automation, security, and simplicity.
