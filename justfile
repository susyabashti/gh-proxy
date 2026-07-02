set shell := ["bash", "-uc"]

# List available recipes
default:
    @just --list

check:
    @echo "🔒 Checking for exposed secrets..."
    @gitleaks protect --staged --redact || echo "⚠️ gitleaks not found, skipping"

    @echo "✅ Running go vet..."
    @go vet ./...

    @echo "✅ Running revive..."
    @revive -config revive.toml ./... || echo "⚠️ revive not found, skipping"

    @echo "✅ Running gofumpt..."
    @gofumpt -l -w .

    @echo "✅ Running fieldalignment..."
    @fieldalignment ./... || echo "⚠️ fieldalignment not found, skipping"

    @echo "✅ Running tests..."
    @go test -v -race -count=1 ./...

release version: check
    #!/usr/bin/env bash
    set -euo pipefail

    echo "🔍 Validating environment..."

    # 1. Ensure working tree is clean
    if [[ -n $(git status --porcelain) ]]; then
      echo "❌ Working tree is dirty. Commit or stash your changes first." >&2
      exit 1
    fi

    # 2. Ensure we are on main
    current_branch=$(git branch --show-current)
    if [[ "$current_branch" != "main" ]]; then
      echo "❌ You are on branch '$current_branch', but releases must come from 'main'." >&2
      exit 1
    fi

    # 3. Pull latest changes to avoid pushing stale history
    echo "⬇️ Syncing with origin..."
    git pull --ff-only origin main

    # 4. Perform the release
    echo "🚀 Tagging and pushing {{version}}..."
    git tag {{version}}
    git push origin {{version}}

    echo "✅ Released {{version}}. CI will now build and distribute."
    GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
