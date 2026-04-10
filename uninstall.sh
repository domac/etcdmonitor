#!/bin/bash
set -e

echo "========================================="
echo "  etcdmonitor - Uninstall Script"
echo "========================================="

# 必须以 root 运行
if [ "$EUID" -ne 0 ]; then
    echo "[ERROR] Please run as root: sudo ./uninstall.sh"
    exit 1
fi

INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_NAME="etcdmonitor"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# 停止并移除服务
if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    echo "[INFO] Stopping $SERVICE_NAME service..."
    systemctl stop "$SERVICE_NAME"
fi

if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
    echo "[INFO] Disabling $SERVICE_NAME service..."
    systemctl disable "$SERVICE_NAME"
fi

if [ -f "$SERVICE_FILE" ]; then
    echo "[INFO] Removing systemd service file..."
    rm -f "$SERVICE_FILE"
    systemctl daemon-reload
fi

echo ""

# 询问是否删除运行时数据
read -p "[?] Delete monitoring data ($INSTALL_DIR/data/)? [y/N]: " DELETE_DATA
if [[ "$DELETE_DATA" =~ ^[yY]$ ]]; then
    echo "[INFO] Removing data directory..."
    rm -rf "$INSTALL_DIR/data"
    echo "[OK] Data deleted."
else
    echo "[INFO] Data preserved in $INSTALL_DIR/data/"
fi

# 询问是否删除日志
read -p "[?] Delete log files ($INSTALL_DIR/logs/)? [y/N]: " DELETE_LOGS
if [[ "$DELETE_LOGS" =~ ^[yY]$ ]]; then
    echo "[INFO] Removing logs directory..."
    rm -rf "$INSTALL_DIR/logs"
    echo "[OK] Logs deleted."
else
    echo "[INFO] Logs preserved in $INSTALL_DIR/logs/"
fi

echo "========================================="
echo "[OK] etcdmonitor uninstalled successfully!"
echo ""
echo "  Service removed, binary and config preserved."
echo "  You can re-install anytime: sudo ./install.sh"
echo ""
echo "  To completely remove everything:"
echo "    rm -rf $INSTALL_DIR"
echo "========================================="
