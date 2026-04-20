# Contributing to etcdmonitor

Thanks for your interest in contributing! This guide describes how to set up
your environment, submit changes, and follow project conventions.

## Development Environment

- **Go** 1.25+
- **openssl** (for generating TLS certificates locally via `tools/gen-certs.sh`)
- **git** 2.30+
- *(optional)* `gitleaks` (for pre-commit secret scanning;
  `brew install gitleaks` on macOS)

Clone and build:

```bash
git clone https://github.com/<org>/etcdmonitor.git
cd etcdmonitor
go build ./...
go test ./... -race -timeout 5m
```

## Pre-commit Hook

We ship a pre-commit hook that runs `gofmt`, `go vet`, and (if installed)
`gitleaks protect`. Enable it once:

```bash
ln -sf ../../tools/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

The hook aborts commits with formatting / vet / secret issues. If gitleaks
reports a false positive, update `.gitleaks.toml` allowlist with a comment
explaining why the match is safe.

## Code Style

- All Go code **must** be `gofmt`-clean (enforced by CI and pre-commit).
- Run `go vet ./...` before submitting.
- Keep functions short and comments close to the code they describe.
- Prefer small, focused PRs over sweeping refactors.

## Commit Messages

Use short, imperative subject lines (under 72 chars) prefixed with one of:

- `feat:` — new user-visible functionality
- `fix:` — bug fix
- `docs:` — documentation-only changes
- `refactor:` — code change that neither fixes a bug nor adds a feature
- `test:` — adding or fixing tests
- `chore:` — build, dependency, tooling changes
- `security:` — security-relevant changes (also requires a `Security` line in
  CHANGELOG `[Unreleased]`)

### AI co-authorship

Per the project's `CLAUDE.md`, commits authored with AI assistance must include
a `Co-Authored-By` trailer, e.g.:

```
Co-Authored-By: Claude <noreply@anthropic.com>
```

## Testing

- Add unit tests alongside the package you modify (`*_test.go`).
- For HTTP handlers, extend `internal/api/auth_integration_test.go` or create a
  focused integration test.
- All PRs must pass `go test ./... -race -timeout 5m` locally and in CI.

## Pull Request Checklist

Before requesting review:

- [ ] `gofmt -l .` is empty (or limited to `web/vendor/`, `openspec/`)
- [ ] `go vet ./...` is clean
- [ ] `go test ./... -race` passes
- [ ] `CHANGELOG.md` `[Unreleased]` has a new entry under the appropriate group
- [ ] README / `config.yaml` / docs updated if user-visible behavior changed
- [ ] If the change modifies specs or capabilities, an OpenSpec change is
      created under `openspec/changes/<name>/` (see `openspec/AGENTS.md`)

## Vendor Dependency Management

The `web/vendor/` directory contains third-party frontend assets bundled with
the binary via `go:embed`. Do not add new external CDN links in `web/*.html`;
vendor the asset locally instead.

**Upgrading echarts** (or adding similar vendored JS libraries):

1. Edit `web/vendor/echarts/VERSION` to the new version number.
2. Run `./tools/vendor-echarts.sh`. This downloads the bundle, updates
   `SHA256SUMS`, and runs a self-check.
3. Manually smoke-test the dashboard (charts render, no console errors).
4. `git add web/vendor/echarts/{VERSION,echarts.min.js,SHA256SUMS}`
5. Update `CHANGELOG.md` under `[Unreleased] > Changed` (or `> Security` if the
   upgrade addresses a CVE).

**Constraint**: do not add files starting with `.` or `_` in `web/` — Go's
`//go:embed web/*` directive skips them by default, so they will not be
shipped in the binary.

## Reporting Security Issues

Never open a public issue for security vulnerabilities. See `SECURITY.md` for
the private reporting process.

## Code of Conduct

This project follows the [Contributor Covenant](./CODE_OF_CONDUCT.md).
By participating, you agree to abide by its terms.
