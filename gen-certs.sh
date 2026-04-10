#!/bin/bash
set -e

echo "========================================="
echo "  etcdmonitor - Generate TLS Certificates"
echo "========================================="

cd "$(dirname "$0")"
CERT_DIR="certs"

mkdir -p "$CERT_DIR"

# 证书有效期（天）
DAYS="${1:-3650}"

echo "[INFO] Generating self-signed TLS certificate (valid for ${DAYS} days)..."

# 生成私钥 + 自签名证书
openssl req -x509 -newkey rsa:2048 \
    -keyout "$CERT_DIR/server.key" \
    -out "$CERT_DIR/server.crt" \
    -days "$DAYS" \
    -nodes \
    -subj "/CN=etcdmonitor" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:0.0.0.0"

chmod 600 "$CERT_DIR/server.key"
chmod 644 "$CERT_DIR/server.crt"

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
echo "      tls_key: \"certs/server.key\""
echo "========================================="
