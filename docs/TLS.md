← Back to [README](../README.md)

# TLS / HTTPS Guide

This document covers both sides of TLS in etcdmonitor:

1. **Dashboard HTTPS** — serving the Dashboard over TLS
2. **etcd client TLS (mTLS)** — connecting to a TLS-secured etcd cluster

For production deployments, also walk through the
[Security checklist](./SECURITY_CHECKLIST.md).

---

## Dashboard HTTPS

The release package **does not** ship with bundled certificates — you
generate a local self-signed key on each target machine. Use the included
helper:

```bash
# Simplest form (SAN: localhost, 127.0.0.1, 0.0.0.0)
./tools/gen-certs.sh

# Production: include the hostnames / IPs users will access the dashboard through.
# --host and --ip are BOTH repeatable (see ./tools/gen-certs.sh --help).
./tools/gen-certs.sh \
    --host monitor.corp.local \
    --host etcd-dashboard.internal \
    --ip 10.0.1.5 \
    --ip 10.0.1.6 \
    --days 730

# Overwrite an existing cert (invalidates current sessions)
./tools/gen-certs.sh --force
```

This writes `certs/server.key` (mode `0600`) and `certs/server.crt`
(mode `0644`) in the project directory.

Enable HTTPS in `config.yaml`:

```yaml
server:
  tls_enable: true
  tls_cert: "certs/server.crt"
  tls_key:  "certs/server.key"
```

Restart the service and access via `https://<server-ip>:9090`.

> `install.sh` refuses to start when `tls_enable: true` but the cert files
> are missing — it prints a one-line pointer back to `./tools/gen-certs.sh`.

### Using a CA-signed certificate

Replace the files in the `certs/` directory (or symlink them to a central
cert-management directory — `install.sh` respects symlinks and will not
`chmod` their targets):

```bash
cp /path/to/your/cert.crt certs/server.crt
cp /path/to/your/cert.key certs/server.key
sudo systemctl restart etcdmonitor
```

> Self-signed certificates will trigger a browser warning. Click
> "Advanced" → "Proceed" to continue, or use a certificate from a trusted
> CA for production.

---

## etcd Client TLS (mTLS)

If your etcd cluster is deployed with
`client-transport-security.client-cert-auth: true`, etcdmonitor supports
connecting via client certificates.

### Configuration

```yaml
etcd:
  endpoint: "https://10.0.1.1:2379,https://10.0.1.2:2379,https://10.0.1.3:2379"
  tls_enable: true
  tls_cert: "certs/etcd-client.pem"         # Client certificate
  tls_key: "certs/etcd-client-key.pem"      # Client private key
  tls_ca_cert: "certs/ca.pem"               # CA certificate
```

### Setup steps

```bash
# Copy etcd client certificates to the etcdmonitor certs directory
cp /etc/etcd/ssl/etcd-client.pem     certs/etcd-client.pem
cp /etc/etcd/ssl/etcd-client-key.pem certs/etcd-client-key.pem
cp /etc/etcd/ssl/ca.pem              certs/ca.pem

# Edit config.yaml (set tls_enable: true and certificate paths)
vim config.yaml

# Restart the service
systemctl restart etcdmonitor
```

### Supported scenarios

| Scenario | `tls_enable` | `tls_cert` / `tls_key` | `tls_ca_cert` | `username` / `password` |
|---|---|---|---|---|
| Plain HTTP (no auth) | `false` | — | — | — |
| Plain HTTP + password auth | `false` | — | — | ✅ |
| HTTPS + CA only (server verification) | `true` | — | ✅ | Optional |
| HTTPS + mTLS (client cert) | `true` | ✅ | ✅ | Optional |
| HTTPS + mTLS + password auth | `true` | ✅ | ✅ | ✅ |

> **Note:** When `tls_enable: true`, the endpoint must use `https://`.
> TLS is applied to all etcd connections: health probes, metrics collection,
> member discovery, KV management, and authentication.

---

## Related documents

- [Configuration reference](./CONFIGURATION.md)
- [API reference](./API.md)
- [Security checklist](./SECURITY_CHECKLIST.md)
- [Back to README](../README.md)

<!-- Keep this file in sync with the main README; any change to TLS setup must be reflected here. -->
