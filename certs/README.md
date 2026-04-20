# certs/

This directory is intentionally empty. TLS certificates are **never** committed to this repository.

## Local setup

Generate a self-signed certificate on the target deployment machine:

```bash
./tools/gen-certs.sh
# With custom hostname / IP SANs:
./tools/gen-certs.sh --host monitor.corp.local --ip 10.0.1.5 --days 730
```

This produces:
- `certs/server.key` (mode `0600`, private key)
- `certs/server.crt` (mode `0644`, self-signed certificate)

## Using a CA-signed certificate

Place your organization's signed files as `certs/server.crt` and `certs/server.key`
(or create symbolic links). `install.sh` respects existing symlinks and will not
`chmod` their targets.

## Never

- Never commit `*.key`, `*.crt`, `*.pem`, `*.p12`, `*.pfx` files. `.gitignore` and
  `gitleaks` enforce this.
- Never reuse example certificates across machines. Regenerate per host.

See `SECURITY.md` and `docs/SECURITY_CHECKLIST.md` for the full policy.
