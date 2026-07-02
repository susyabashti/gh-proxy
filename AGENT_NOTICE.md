### ℹ️ ENVIRONMENT NOTICE: GitHub CLI Proxy Active

This system utilizes a transparent **gh-proxy** wrapper to automatically manage repository and organization access.

#### 🤖 For the AI Agent:
- **Standard Syntax Supported:** You do **not** need to change how you write commands. Treat this binary exactly like the official GitHub CLI (`gh`).
- **Automating Authentication:** Do **not** attempt to run `gh auth login` or configure local git credentials. Authentication tokens are dynamically generated via GitHub App credentials and injected into your execution scope automatically.
- **Underlying Capabilities:** All native flags, extensions, and subcommands (like `gh repo`, `gh pr`, `gh api`) are fully functional.

---
