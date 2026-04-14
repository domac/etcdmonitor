#!/bin/bash
# ============================================================================
#  etcd TLS 证书一键生成脚本
#
#  生成 etcd 集群所需的全套 TLS 证书（CA + Server + Client + Peer）
#  适用于 etcd 3.4+ 版本
#
#  用法:
#    ./generate.sh                              # 单节点，默认 IP 127.0.0.1
#    ./generate.sh -n 10.0.1.1,10.0.1.2,10.0.1.3   # 多节点集群
#    ./generate.sh -d 730                       # 自定义有效期（天）
#    ./generate.sh -o /etc/etcd/ssl             # 自定义输出目录
#
#  依赖: openssl (>= 1.1.1)
# ============================================================================
set -euo pipefail

# ===== 默认参数 =====
NODES="127.0.0.1"
DAYS=365
OUTPUT_DIR=""
CA_CN="etcd-ca"
KEY_SIZE=2048

# ===== 颜色输出 =====
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# ===== 帮助信息 =====
usage() {
    cat <<'USAGE'
用法: ./generate.sh [选项]

选项:
  -n, --nodes <IPs>     etcd 节点 IP 列表，逗号分隔（默认: 127.0.0.1）
  -d, --days <天数>     证书有效期天数（默认: 365）
  -o, --output <目录>   证书输出目录（默认: 脚本同目录下的 output/）
  -h, --help            显示帮助信息

示例:
  # 单节点开发环境
  ./generate.sh

  # 三节点生产集群
  ./generate.sh -n 10.0.1.1,10.0.1.2,10.0.1.3

  # 自定义有效期和输出目录
  ./generate.sh -n 10.0.1.1,10.0.1.2 -d 730 -o /etc/etcd/ssl
USAGE
    exit 0
}

# ===== 参数解析 =====
while [[ $# -gt 0 ]]; do
    case "$1" in
        -n|--nodes)  NODES="$2"; shift 2 ;;
        -d|--days)   DAYS="$2"; shift 2 ;;
        -o|--output) OUTPUT_DIR="$2"; shift 2 ;;
        -h|--help)   usage ;;
        *) error "未知参数: $1"; usage ;;
    esac
done

# 默认输出目录
if [ -z "$OUTPUT_DIR" ]; then
    OUTPUT_DIR="$(cd "$(dirname "$0")" && pwd)/output"
fi

# ===== 前置检查 =====
if ! command -v openssl &>/dev/null; then
    error "openssl 未安装。请先安装: yum install -y openssl  或  apt install -y openssl"
    exit 1
fi

OPENSSL_VERSION=$(openssl version 2>/dev/null || true)
info "OpenSSL: $OPENSSL_VERSION"

# 解析节点列表
IFS=',' read -ra NODE_LIST <<< "$NODES"
if [ ${#NODE_LIST[@]} -eq 0 ]; then
    error "节点列表为空"
    exit 1
fi

# ===== 开始 =====
echo ""
echo "========================================="
echo "  etcd TLS 证书生成"
echo "========================================="
echo ""
info "节点列表: ${NODE_LIST[*]}"
info "有效期:   ${DAYS} 天"
info "输出目录: ${OUTPUT_DIR}"
echo ""

# 创建输出目录
mkdir -p "$OUTPUT_DIR"

# ===== 构建 SAN（Subject Alternative Names）=====
# 包含所有节点 IP + localhost + 127.0.0.1
build_san() {
    local san="DNS:localhost"
    local ip_index=1

    # 始终包含 127.0.0.1
    san="${san},IP.${ip_index}:127.0.0.1"
    ip_index=$((ip_index + 1))

    for node in "${NODE_LIST[@]}"; do
        node=$(echo "$node" | tr -d '[:space:]')
        if [ "$node" != "127.0.0.1" ] && [ -n "$node" ]; then
            san="${san},IP.${ip_index}:${node}"
            ip_index=$((ip_index + 1))
        fi
    done

    echo "$san"
}

SAN=$(build_san)
info "SAN: ${SAN}"
echo ""

# ===== 1. 生成 CA 证书 =====
info "生成 CA 证书..."

openssl genrsa -out "${OUTPUT_DIR}/ca-key.pem" ${KEY_SIZE} 2>/dev/null

openssl req -new -x509 \
    -key "${OUTPUT_DIR}/ca-key.pem" \
    -out "${OUTPUT_DIR}/ca.pem" \
    -days "${DAYS}" \
    -subj "/CN=${CA_CN}" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null

info "  ca.pem       (CA 证书)"
info "  ca-key.pem   (CA 私钥)"

# ===== 辅助函数：生成证书 =====
generate_cert() {
    local name="$1"
    local cn="$2"
    local san="$3"
    local key_usage="$4"
    local ext_key_usage="$5"

    # 生成私钥
    openssl genrsa -out "${OUTPUT_DIR}/${name}-key.pem" ${KEY_SIZE} 2>/dev/null

    # 创建临时扩展配置文件
    local ext_file
    ext_file=$(mktemp)
    cat > "$ext_file" <<EXTEOF
[req]
req_extensions = v3_req
distinguished_name = dn

[dn]

[v3_req]
basicConstraints = CA:FALSE
keyUsage = critical,${key_usage}
extendedKeyUsage = ${ext_key_usage}
subjectAltName = ${san}
EXTEOF

    # 生成 CSR
    openssl req -new \
        -key "${OUTPUT_DIR}/${name}-key.pem" \
        -out "${OUTPUT_DIR}/${name}.csr" \
        -subj "/CN=${cn}" \
        -config "$ext_file" 2>/dev/null

    # 用 CA 签发证书
    openssl x509 -req \
        -in "${OUTPUT_DIR}/${name}.csr" \
        -CA "${OUTPUT_DIR}/ca.pem" \
        -CAkey "${OUTPUT_DIR}/ca-key.pem" \
        -CAcreateserial \
        -out "${OUTPUT_DIR}/${name}.pem" \
        -days "${DAYS}" \
        -extensions v3_req \
        -extfile "$ext_file" 2>/dev/null

    # 清理临时文件
    rm -f "$ext_file" "${OUTPUT_DIR}/${name}.csr"
}

# ===== 2. 生成 Server 证书 =====
info "生成 Server 证书..."
generate_cert "server" "etcd-server" "$SAN" \
    "digitalSignature,keyEncipherment" \
    "serverAuth,clientAuth"
info "  server.pem       (Server 证书)"
info "  server-key.pem   (Server 私钥)"

# ===== 3. 生成 Peer 证书 =====
info "生成 Peer 证书..."
generate_cert "peer" "etcd-peer" "$SAN" \
    "digitalSignature,keyEncipherment" \
    "serverAuth,clientAuth"
info "  peer.pem         (Peer 证书)"
info "  peer-key.pem     (Peer 私钥)"

# ===== 4. 生成 Client 证书 =====
info "生成 Client 证书..."
generate_cert "client" "etcd-client" "$SAN" \
    "digitalSignature,keyEncipherment" \
    "clientAuth"
info "  client.pem       (Client 证书)"
info "  client-key.pem   (Client 私钥)"

# ===== 5. 设置文件权限 =====
info "设置文件权限..."
chmod 644 "${OUTPUT_DIR}/ca.pem"
chmod 644 "${OUTPUT_DIR}/server.pem"
chmod 644 "${OUTPUT_DIR}/peer.pem"
chmod 644 "${OUTPUT_DIR}/client.pem"
chmod 600 "${OUTPUT_DIR}/ca-key.pem"
chmod 600 "${OUTPUT_DIR}/server-key.pem"
chmod 600 "${OUTPUT_DIR}/peer-key.pem"
chmod 600 "${OUTPUT_DIR}/client-key.pem"

# 清理序列号文件
rm -f "${OUTPUT_DIR}/ca.srl"

# ===== 6. 验证证书链 =====
info "验证证书链..."
VERIFY_OK=true

for cert in server peer client; do
    if openssl verify -CAfile "${OUTPUT_DIR}/ca.pem" "${OUTPUT_DIR}/${cert}.pem" &>/dev/null; then
        info "  ${cert}.pem  ✓"
    else
        error "  ${cert}.pem  ✗ 验证失败！"
        VERIFY_OK=false
    fi
done

if [ "$VERIFY_OK" = false ]; then
    error "证书验证未全部通过，请检查。"
    exit 1
fi

# ===== 完成 =====
echo ""
echo "========================================="
echo -e "${GREEN}[OK] 证书生成完成！${NC}"
echo ""
echo "  输出目录: ${OUTPUT_DIR}"
echo "  有效期:   ${DAYS} 天"
echo ""
echo "  文件列表:"
echo "    ca.pem / ca-key.pem         CA 根证书"
echo "    server.pem / server-key.pem etcd 服务端证书"
echo "    peer.pem / peer-key.pem     etcd 节点间通信证书"
echo "    client.pem / client-key.pem etcd 客户端证书"
echo ""
echo "  下一步:"
echo "    1. 将证书分发到各 etcd 节点"
echo "    2. 配置 etcd 启用 TLS（参考 README.md）"
echo "    3. 配置 etcdmonitor 连接（config.yaml）"
echo "========================================="
