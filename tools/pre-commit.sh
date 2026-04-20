#!/bin/bash
# pre-commit hook for etcdmonitor
# Install:
#   ln -sf ../../tools/pre-commit.sh .git/hooks/pre-commit
#   chmod +x .git/hooks/pre-commit
#
# Behavior:
#   1. gofmt -l .          (fails on any non-formatted .go file)
#   2. go vet ./...        (static analysis)
#   3. gitleaks protect    (secret scan on staged changes, if installed)
#
# Any failure aborts the commit.

set -e

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

echo "[pre-commit] gofmt -l ..."
UNFORMATTED=$(gofmt -l . 2>/dev/null | grep -v '^web/vendor/' | grep -v '^openspec/' || true)
if [ -n "$UNFORMATTED" ]; then
    echo "[pre-commit] The following files are not gofmt-clean:"
    echo "$UNFORMATTED" | sed 's/^/    /'
    echo "[pre-commit] Run: gofmt -w <file> and re-stage"
    exit 1
fi

echo "[pre-commit] go vet ./..."
if ! go vet ./... 2>&1; then
    echo "[pre-commit] go vet reported issues, aborting commit"
    exit 1
fi

if command -v gitleaks >/dev/null 2>&1; then
    echo "[pre-commit] gitleaks protect --staged ..."
    if ! gitleaks protect --staged --no-banner --redact; then
        echo "[pre-commit] gitleaks detected potential secrets in staged changes"
        echo "[pre-commit] Review the output above. If it is a false positive,"
        echo "[pre-commit] update .gitleaks.toml allowlist with a comment explaining why."
        exit 1
    fi
else
    echo "[pre-commit] gitleaks not installed, skipping secret scan"
    echo "[pre-commit]   brew install gitleaks  # macOS"
    echo "[pre-commit]   # or see https://github.com/gitleaks/gitleaks"
fi

echo "[pre-commit] OK"
