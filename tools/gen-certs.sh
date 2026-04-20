#!/bin/bash
# Generate a self-signed TLS certificate for etcdmonitor on the local machine.
#
# Usage:
#   ./tools/gen-certs.sh [--host <hostname>]... [--ip <address>]... [--days <N>] [--force]
#
# Examples:
#   ./tools/gen-certs.sh
#   ./tools/gen-certs.sh --host monitor.corp.local
#   ./tools/gen-certs.sh --host monitor.corp.local --ip 10.0.1.5 --days 730
#   ./tools/gen-certs.sh --force  # overwrite existing cert
#
# Defaults:
#   SAN  = DNS:localhost, IP:127.0.0.1, IP:0.0.0.0
#   Days = 365
#
# Output (in <repo>/certs/):
#   server.key  (mode 0600)
#   server.crt  (mode 0644)

set -e

echo "========================================="
echo "  etcdmonitor - Generate TLS Certificates"
echo "========================================="

cd "$(dirname "$0")"
CERT_DIR="../certs"

# === defaults ===
DAYS=365
FORCE="false"
EXTRA_HOSTS=()
EXTRA_IPS=()

# === parse flags ===
while [ $# -gt 0 ]; do
    case "$1" in
        --host)
            if [ -z "${2:-}" ]; then
                echo "[ERROR] --host requires a value" >&2
                exit 2
            fi
            EXTRA_HOSTS+=("$2")
            shift 2
            ;;
        --ip)
            if [ -z "${2:-}" ]; then
                echo "[ERROR] --ip requires a value" >&2
                exit 2
            fi
            EXTRA_IPS+=("$2")
            shift 2
            ;;
        --days)
            if [ -z "${2:-}" ]; then
                echo "[ERROR] --days requires a value" >&2
                exit 2
            fi
            DAYS="$2"
            shift 2
            ;;
        --force)
            FORCE="true"
            shift
            ;;
        -h|--help)
            sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "[ERROR] Unknown argument: $1" >&2
            echo "Run $0 --help for usage." >&2
            exit 2
            ;;
    esac
done

mkdir -p "$CERT_DIR"

# === 防止覆盖已有证书 ===
if [ -f "$CERT_DIR/server.key" ] && [ "$FORCE" != "true" ]; then
    echo "[ERROR] Certificate already exists: $CERT_DIR/server.key"
    echo "        Use --force to overwrite (this invalidates any existing sessions)."
    exit 1
fi

# === 组装 SAN ===
SAN="DNS:localhost,IP:127.0.0.1,IP:0.0.0.0"
for h in "${EXTRA_HOSTS[@]}"; do
    SAN="${SAN},DNS:${h}"
done
for ip in "${EXTRA_IPS[@]}"; do
    SAN="${SAN},IP:${ip}"
done

echo "[INFO] Generating self-signed TLS certificate (valid for ${DAYS} days)..."
echo "[INFO] SAN: ${SAN}"

openssl req -x509 -newkey rsa:2048 \
    -keyout "$CERT_DIR/server.key" \
    -out "$CERT_DIR/server.crt" \
    -days "$DAYS" \
    -nodes \
    -subj "/CN=etcdmonitor" \
    -addext "subjectAltName=${SAN}" 2>/dev/null

chmod 600 "$CERT_DIR/server.key"
chmod 644 "$CERT_DIR/server.crt"

echo ""
echo "[INFO] Certificate details:"
openssl x509 -in "$CERT_DIR/server.crt" -noout -dates -subject
openssl x509 -in "$CERT_DIR/server.crt" -noout -ext subjectAltName 2>/dev/null \
    || echo "(SAN extension not displayable on this openssl version)"

echo ""
echo "========================================="
echo "[OK] Certificates generated:"
echo "  Certificate: $CERT_DIR/server.crt"
echo "  Private Key: $CERT_DIR/server.key"
echo "  Valid for:   ${DAYS} days"
echo ""
echo "  To use, set in config.yaml:"
echo "    server:"
echo "      tls_enable: true"
echo "      tls_cert: \"certs/server.crt\""
echo "      tls_key:  \"certs/server.key\""
echo "========================================="
