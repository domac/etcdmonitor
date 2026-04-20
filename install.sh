#!/bin/bash
set -e

echo "========================================="
echo "  etcdmonitor - Install Script"
echo "========================================="

# 必须以 root 运行
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
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o etcdmonitor .
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
# 修改此处指定服务运行用户（默认 root）
# 例如: RUN_USER="etcdmonitor" 或 RUN_USER="ops"
RUN_USER="root"

# 校验用户是否存在
if ! id "$RUN_USER" &>/dev/null; then
    echo "[ERROR] User '$RUN_USER' does not exist."
    echo "        Create it first:  useradd -r -s /sbin/nologin $RUN_USER"
    echo "        Or change RUN_USER at the top of this script."
    exit 1
fi

# 自动推导用户组
RUN_GROUP=$(id -gn "$RUN_USER")
echo "[INFO] Run as user: $RUN_USER (group: $RUN_GROUP)"

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
if [ -d "$INSTALL_DIR/certs" ]; then
    chown -R "$RUN_USER:$RUN_GROUP" "$INSTALL_DIR/certs"
    chmod 600 "$INSTALL_DIR/certs"/*.key 2>/dev/null || true
    chmod 644 "$INSTALL_DIR/certs"/*.crt 2>/dev/null || true
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
systemctl start "$SERVICE_NAME"

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
    echo "  Log files:  $INSTALL_DIR/logs/"
    echo "  Data files: $INSTALL_DIR/data/"
    echo "========================================="
else
    echo "[ERROR] Service failed to start!"
    echo "  Check logs: journalctl -u $SERVICE_NAME -n 50"
    echo "  Check log file: $INSTALL_DIR/logs/etcdmonitor.log"
    exit 1
fi
