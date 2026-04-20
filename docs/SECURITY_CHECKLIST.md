# Production Security Checklist

> **Scope**: This checklist targets **bare-metal / VM deployments managed by
> `systemd`** (what `install.sh` produces). Container / Kubernetes deployments
> require a separate hardening review — most items below do not apply there.

Work top-to-bottom before exposing the dashboard outside your host. Each item is
independently verifiable.

---

## 1. TLS Certificates

- [ ] Ran `./tools/gen-certs.sh` **on the target machine** (never reused from a
      package / shared drive).
- [ ] SAN includes every hostname / IP users will access the dashboard through.
      Example: `./tools/gen-certs.sh --host monitor.corp.local --ip 10.0.1.5`.
- [ ] `certs/server.key` has mode `0600` and is owned by the service run user.
- [ ] `certs/server.crt` has mode `0644` and is owned by the service run user.
- [ ] If using an organization CA cert, files are placed (or symlinked) as
      `certs/server.crt` / `certs/server.key`. `install.sh` respects symlinks
      and will NOT `chmod` their targets.
- [ ] Certificate expiry date is tracked (e.g. in a calendar reminder). Verify
      with `openssl x509 -in certs/server.crt -noout -dates`.

## 2. File System Permissions

- [ ] `$INSTALL_DIR/data/` is mode `0700` and owned by `etcdmonitor:etcdmonitor`.
- [ ] `$INSTALL_DIR/data/etcdmonitor.db` is mode `0600`.
- [ ] `$INSTALL_DIR/data/initial-admin-password` (if still present) is mode
      `0600`. If present, change the admin password to make this file
      auto-destruct.
- [ ] `$INSTALL_DIR/logs/` is owned by the run user; log files are readable
      only by the run user.
- [ ] No world-writable files anywhere under `$INSTALL_DIR`:
      `find $INSTALL_DIR -type f -perm -0002 -print` returns nothing.

## 3. systemd Hardening

- [ ] `systemctl status etcdmonitor` shows `active (running)`.
- [ ] Service is NOT running as root. The default `install.sh` keeps root for
      upgrade continuity, so for production you must explicitly run:
      ```
      sudo useradd -r -s /sbin/nologin -d <INSTALL_DIR> etcdmonitor
      sudo ./install.sh --run-user etcdmonitor
      ```
      Verify with: `systemctl show -p User etcdmonitor` reports
      `User=etcdmonitor`.
- [ ] `journalctl -u etcdmonitor | grep -i 'running.*as root'` is empty —
      confirming no WARN about root mode was emitted during the last install.
- [ ] `systemd-analyze security etcdmonitor` Exposure value is ≤ `3.0 OK`.
      Review any red entries and consider adding:
      `ProtectKernelTunables=true`, `ProtectKernelModules=true`,
      `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`.
- [ ] Unit file (`/etc/systemd/system/etcdmonitor.service`) contains all of:
      `NoNewPrivileges=true`, `ProtectSystem=strict`, `ProtectHome=true`,
      `PrivateTmp=true`, `PrivateDevices=true`, `RestrictSUIDSGID=true`,
      `RestrictNamespaces=true`, `LockPersonality=true`,
      `MemoryDenyWriteExecute=true`, `CapabilityBoundingSet=`,
      `AmbientCapabilities=`, `ReadWritePaths=$INSTALL_DIR/data $INSTALL_DIR/logs`.

## 4. Local User & Password

- [ ] On first start, initial admin password was retrieved via
      `cat $INSTALL_DIR/data/initial-admin-password` and then **changed
      immediately** via the dashboard login flow.
- [ ] `$INSTALL_DIR/data/initial-admin-password` no longer exists (auto-deleted
      after first password change).
- [ ] `config.yaml` `auth.bcrypt_cost` is within [8, 14]. Default 10 is fine.
- [ ] `config.yaml` `auth.lockout_threshold` and `lockout_duration_seconds` are
      appropriate for your ops team's tolerance.
- [ ] Emergency recovery procedure is documented: operators know to run
      `etcdmonitor reset-password --username admin` or `etcdmonitor unlock
      --username admin` from the service host.

## 5. Logs & Audit

- [ ] Application logs rotate correctly (check `config.yaml` `log.max_size_mb` /
      `max_files` / `compress`).
- [ ] `ops_audit_log` table is being written (query `SELECT COUNT(*) FROM
      ops_audit_log` after one login attempt).
- [ ] Audit retention (`config.yaml` `ops.audit_retention_days`) matches your
      compliance requirements.
- [ ] journald integration works: `journalctl -u etcdmonitor -n 50` shows
      recent entries.
- [ ] No plaintext passwords in logs: `grep -i password $INSTALL_DIR/logs/*.log`
      returns nothing except redaction notices.

## 6. Frontend & Network

- [ ] `curl -sI https://<host>:<port>/ | grep Content-Security-Policy` shows
      the strict CSP (no `'unsafe-eval'`, no `cdn.jsdelivr.net`).
- [ ] Dashboard loaded successfully **with no network connectivity to
      cdn.jsdelivr.net / unpkg** (verifies echarts vendoring works offline).
- [ ] `grep -rn -E 'src="https?://|href="https?://' web/*.html` returns no
      results (only SVG `xmlns` namespaces allowed).
- [ ] Firewall (`iptables` / `firewalld` / cloud SG) restricts dashboard port
      to the trusted operator network — NOT `0.0.0.0/0`.
- [ ] If placing behind a reverse proxy, verify `X-Forwarded-For` is forwarded
      (for audit log client IPs).

## 7. Upgrade & Rollback

- [ ] Before upgrade: backup `$INSTALL_DIR/data/etcdmonitor.db` and
      `/etc/systemd/system/etcdmonitor.service`.
- [ ] Upgrade procedure documented for operators:
      `systemctl stop etcdmonitor` → replace binary / configs → `sudo
      ./install.sh` → `systemctl status etcdmonitor`.
- [ ] Rollback procedure tested at least once in a staging environment.
- [ ] SHA256 of deployed binary recorded (`sha256sum $INSTALL_DIR/etcdmonitor`
      in a deployment notebook / CMDB).

---

If any item is unchecked, the deployment is **not** ready for production. Fix
the item, do not add an exception.

## Known Limitations

This release does **not** fully eliminate the following exposures. They are
tracked as follow-up work; operators should be aware of them.

- **CSP `script-src 'unsafe-inline'` retained.** The login page, change-password
  page, and dashboard main page currently rely on inline `<script>` blocks and
  inline `on*` event handler attributes (~59 in `index.html`). Removing
  `'unsafe-inline'` would break the dashboard immediately. The eventual fix
  (extract inline scripts to `.js` files, migrate handlers to
  `addEventListener`, adopt per-request nonces) is tracked for a future
  OpenSpec change. In the meantime, the primary XSS defense is `escapeHTML()`
  on all backend-data interpolation (see `web/util.js` and `web/ops.js`).

- **CSP `style-src 'unsafe-inline'` retained.** The frontend uses many dynamic
  `style=""` attributes for theming and panel sizing. Removing it requires a
  styling-system refactor, out of scope here.

- **Git history still contains the revoked example TLS key.** See the
  Historical Advisories section in `SECURITY.md`. The key is self-signed with
  no CA trust and cannot impersonate a CA-signed certificate, but operators
  must not reuse it. `install.sh` enforces this by refusing to start if
  `tls_enable: true` without a freshly generated local cert.

- **Default `install.sh` still runs the service as `root`.** This keeps the
  upgrade path continuous with 0.8.x. For production deployments, switch to
  a dedicated user: see Section 3 above.

Each of these is a conscious tradeoff, not an oversight — review them against
your threat model before deployment.
