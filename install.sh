#!/bin/bash
set -e

echo "========================================="
echo "  etcdmonitor - Install Script"
echo "========================================="

# === CLI flags ===
# Default: run as root (matches the historical 0.8.x behavior).
# Operators are strongly encouraged to use a dedicated non-root user instead:
#     sudo useradd -r -s /sbin/nologin -d <INSTALL_DIR> etcdmonitor
#     sudo ./install.sh --run-user etcdmonitor
# See docs/SECURITY_CHECKLIST.md for the full production recommendation.
RUN_USER="root"
for arg in "$@"; do
    case "$arg" in
        --run-user=*)
            RUN_USER="${arg#--run-user=}"
            ;;
        --run-user)
            # Next argument is the user (handled below)
            ;;
        -h|--help)
            cat <<EOF
Usage: sudo ./install.sh [--run-user <name>]

  --run-user <name>   Run etcdmonitor as the given system user (must exist).
                      Default: root.
                      Production recommendation: --run-user etcdmonitor
                      (create with: useradd -r -s /sbin/nologin -d <dir> etcdmonitor)
EOF
            exit 0
            ;;
    esac
done
# Handle `--run-user <value>` form
prev=""
for arg in "$@"; do
    if [ "$prev" = "--run-user" ]; then
        RUN_USER="$arg"
    fi
    prev="$arg"
done

# 必须以 root 运行（脚本本身需要 root 才能创建 systemd unit、chown 等）
if [ "$EUID" -ne 0 ]; then
    echo "[ERROR] Please run as root: sudo ./install.sh"
    exit 1
fi

INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG="$INSTALL_DIR/config.yaml"
SERVICE_NAME="etcdmonitor"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

echo "[INFO] Install directory: $INSTALL_DIR"

# 检查二进制文件（优先 etcdmonitor，兼容 etcdmonitor-linux-amd64）
if [ -f "$INSTALL_DIR/etcdmonitor" ]; then
    BINARY="$INSTALL_DIR/etcdmonitor"
elif [ -f "$INSTALL_DIR/etcdmonitor-linux-amd64" ]; then
    BINARY="$INSTALL_DIR/etcdmonitor-linux-amd64"
else
    echo "[WARN] Binary not found, trying to build..."
    if command -v go &> /dev/null; then
        echo "[INFO] Building..."
        cd "$INSTALL_DIR"
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o etcdmonitor ./cmd/etcdmonitor
        BINARY="$INSTALL_DIR/etcdmonitor"
    else
        echo "[ERROR] Binary not found and Go is not installed."
        echo "        Please run build.sh on a machine with Go, or use package.sh to create an install package."
        exit 1
    fi
fi

echo "[INFO] Binary: $BINARY"

# 检查配置文件
if [ ! -f "$CONFIG" ]; then
    echo "[ERROR] config.yaml not found in $INSTALL_DIR"
    exit 1
fi

# ===== 运行用户配置 =====
# 默认以 root 运行（与 0.8.x 保持一致）。生产环境强烈建议使用非 root 专用用户。
RUN_USER="${RUN_USER:-root}"
if [ "$RUN_USER" != "root" ]; then
    WARN_MSG="[INFO] Running etcdmonitor as dedicated user: $RUN_USER"
else
    WARN_MSG="[WARN] Running etcdmonitor as root. For production, prefer: --run-user etcdmonitor (create via useradd -r -s /sbin/nologin -d $INSTALL_DIR etcdmonitor)"
    echo "$WARN_MSG"
    if command -v logger >/dev/null 2>&1; then
        logger -t etcdmonitor "$WARN_MSG"
    fi
fi

# 校验用户是否存在
if ! id "$RUN_USER" &>/dev/null; then
    echo "[ERROR] User '$RUN_USER' does not exist."
    echo ""
    echo "  Create it first with:"
    echo "      useradd -r -s /sbin/nologin -d $INSTALL_DIR $RUN_USER"
    echo ""
    echo "  Or drop --run-user to use root (default)."
    exit 1
fi

# 自动推导用户组
RUN_GROUP=$(id -gn "$RUN_USER")
echo "[INFO] Run as user: $RUN_USER (group: $RUN_GROUP)"

# ===== TLS 证书预检查 =====
TLS_ENABLED_LINE=$(awk '
    /^server:/         { in_server=1; next }
    /^[a-zA-Z]/ && in_server { in_server=0 }
    in_server && /^[[:space:]]+tls_enable:/ { print $2; exit }
' "$CONFIG" || true)
if [ "$TLS_ENABLED_LINE" = "true" ]; then
    if [ ! -f "$INSTALL_DIR/certs/server.key" ] || [ ! -f "$INSTALL_DIR/certs/server.crt" ]; then
        echo ""
        echo "[ERROR] TLS enabled but cert files missing."
        echo "        Run: ./tools/gen-certs.sh"
        echo "        (add --host <hostname> --ip <addr> for production access)"
        exit 1
    fi
fi

# 创建数据目录
mkdir -p "$INSTALL_DIR/data"

# 创建日志目录
mkdir -p "$INSTALL_DIR/logs"

# 设置权限
chmod +x "$BINARY"
chown -R "$RUN_USER:$RUN_GROUP" "$INSTALL_DIR/data"
chown -R "$RUN_USER:$RUN_GROUP" "$INSTALL_DIR/logs"
chown "$RUN_USER:$RUN_GROUP" "$CONFIG"
chown "$RUN_USER:$RUN_GROUP" "$BINARY"

# 收紧数据目录权限：包含密码哈希、会话数据、初始密码文件，禁止同组/其他用户读取
chmod 0700 "$INSTALL_DIR/data"
# 若已有 DB 文件，同步收紧到 0600
if [ -f "$INSTALL_DIR/data/etcdmonitor.db" ]; then
    chmod 0600 "$INSTALL_DIR/data/etcdmonitor.db"
fi
if [ -f "$INSTALL_DIR/data/initial-admin-password" ]; then
    chmod 0600 "$INSTALL_DIR/data/initial-admin-password"
fi

# 证书目录权限（私钥仅运行用户可读）
# 尊重 symlink：企业运维可能把 server.key 链接到集中证书管理目录，
# 无脑 chmod 会修改目标文件权限；因此对 symlink 跳过 chmod。
if [ -d "$INSTALL_DIR/certs" ]; then
    chown -R "$RUN_USER:$RUN_GROUP" "$INSTALL_DIR/certs" 2>/dev/null || true
    for f in "$INSTALL_DIR/certs"/*.key; do
        [ -e "$f" ] || continue
        if [ -L "$f" ]; then
            echo "[INFO] Skip chmod for symlink $f (managed externally)"
        else
            chmod 600 "$f"
        fi
    done
    for f in "$INSTALL_DIR/certs"/*.crt; do
        [ -e "$f" ] || continue
        if [ -L "$f" ]; then
            echo "[INFO] Skip chmod for symlink $f (managed externally)"
        else
            chmod 644 "$f"
        fi
    done
fi

# 创建 systemd 服务
echo "[INFO] Creating systemd service..."
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=etcd Monitor Dashboard
After=network.target

[Service]
Type=simple
User=$RUN_USER
Group=$RUN_GROUP
WorkingDirectory=$INSTALL_DIR
ExecStart=$BINARY -config $CONFIG
Restart=always
RestartSec=5
LimitNOFILE=65536
UMask=0077

# Sandbox hardening (widely supported on systemd >= 232).
# See docs/SECURITY_CHECKLIST.md for optional additional lockdowns.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
RestrictSUIDSGID=true
RestrictNamespaces=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths=$INSTALL_DIR/data $INSTALL_DIR/logs

# 日志同时写入文件和 journal
StandardOutput=journal
StandardError=journal
SyslogIdentifier=$SERVICE_NAME

[Install]
WantedBy=multi-user.target
EOF

# 重新加载 systemd
systemctl daemon-reload

# 启动服务
echo "[INFO] Starting $SERVICE_NAME service..."
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"

# 等待启动
sleep 2

# 检查状态
if systemctl is-active --quiet "$SERVICE_NAME"; then
    # 获取监听端口和 TLS 状态
    LISTEN_PORT=$(grep -A1 "^server:" "$CONFIG" | grep "listen" | grep -oP ':\K[0-9]+' || echo "9090")
    TLS_ENABLED=$(grep "tls_enable" "$CONFIG" | grep -i "true" || true)
    if [ -n "$TLS_ENABLED" ]; then
        PROTOCOL="https"
    else
        PROTOCOL="http"
    fi

    echo "========================================="
    echo "[OK] etcdmonitor installed successfully!"
    echo ""
    echo "  Service status: running"
    echo "  Run as user:    $RUN_USER"
    echo "  Dashboard URL:  ${PROTOCOL}://<server-ip>:${LISTEN_PORT}"
    echo ""
    echo "  Manage commands:"
    echo "    systemctl status  $SERVICE_NAME"
    echo "    systemctl stop    $SERVICE_NAME"
    echo "    systemctl restart $SERVICE_NAME"
    echo "    journalctl -u $SERVICE_NAME -f"
    echo ""
    echo "  Verify sandbox hardening:"
    echo "    systemd-analyze security $SERVICE_NAME"
    echo ""
    echo "  Log files:  $INSTALL_DIR/logs/"
    echo "  Data files: $INSTALL_DIR/data/"
    echo "========================================="
else
    echo "[ERROR] Service failed to start!"
    echo "  Check logs: journalctl -u $SERVICE_NAME -n 50"
    echo "  Check log file: $INSTALL_DIR/logs/etcdmonitor.log"
    exit 1
fi
