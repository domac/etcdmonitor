# Security Policy

Thank you for caring about the security of etcdmonitor. This document describes
how to report vulnerabilities and what to expect in response.

## Supported Versions

Only the latest minor release receives security fixes. Older releases are
considered end-of-life and will not be patched.

| Version | Supported |
|---------|-----------|
| latest  | ✅        |
| older   | ❌        |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Use one of the following private channels:

1. **GitHub Private Security Advisories** (preferred):
   [Report a vulnerability](../../security/advisories/new)
2. **Email**: `security@example.com` *(maintainers: replace with a real address
   before publishing this document)*

When reporting, please include:

- A clear description of the issue and potential impact
- Steps to reproduce (minimal test case preferred)
- etcdmonitor version, etcd version, OS / kernel
- Any proof-of-concept code or exploit (encrypted if sensitive)

If you wish to encrypt your report, request our PGP key in your first message.

## Response SLA

- **Acknowledgement**: within **3 business days**
- **Triage & severity assessment**: within **7 business days**
- **Fix or mitigation**: within **30 days** for High/Critical, best effort for
  Medium/Low

We will keep you informed of progress and coordinate public disclosure.

## Disclosure Policy

1. Reporter contacts the maintainers privately.
2. Maintainers confirm the vulnerability and develop a fix.
3. A coordinated disclosure date is agreed.
4. Fix is released; advisory is published with credit to the reporter (unless
   anonymity is requested).
5. CVE is requested when applicable.

## Historical Advisories

### 2026-04 — Self-signed example TLS certificate exposed in Git history

Commits prior to the `security-audit-hardening` release shipped an example
`certs/server.key` / `certs/server.crt` pair (self-signed, `CN=etcdmonitor`).
**This key is revoked and MUST NOT be used.** All deployments must run
`./tools/gen-certs.sh` on the target machine to generate a local keypair. The
build (`install.sh`) refuses to start when `tls_enable: true` and the key files
are absent.

Git history has not been rewritten (to preserve stable history for forks and
stargazers). Since the keypair is self-signed with no CA trust, the only
realistic attack surface is operators who blindly deploy example files. That
path is now blocked by `install.sh` behavior and by this advisory.

## Scope

This policy covers:

- The `etcdmonitor` binary and its modules under `internal/`, `cmd/`
- The web dashboard in `web/` (including `web/vendor/*` bundled assets)
- Installation scripts (`install.sh`, `uninstall.sh`, `tools/*`)
- Deployment configuration (`config.yaml` semantics)

Out of scope:

- Vulnerabilities in unrelated 3rd-party software even if used alongside
  etcdmonitor (please report those upstream)
- Social engineering, physical access, or denial-of-service via resource
  exhaustion beyond documented limits
- Misconfiguration by the deploying operator (e.g. running as root without
  following the `--run-user etcdmonitor` recommendation in `SECURITY_CHECKLIST.md`)

## Safe Harbor

We will not take legal action against researchers who:

- Make a good-faith effort to avoid privacy violations and service disruption
- Report vulnerabilities privately per this policy
- Do not exploit or access more data than necessary to demonstrate the issue

Thank you for helping keep etcdmonitor and its users safe.
