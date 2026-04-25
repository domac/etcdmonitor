#!/bin/bash
set -e

echo "========================================="
echo "  etcdmonitor - Install Script"
echo "========================================="

# === CLI flags ===
# Run-user priority: CLI --run-user > config.yaml service.run_user > install-meta > $(whoami)
# Operators are strongly encouraged to use a dedicated non-root user:
#     sudo useradd -r -s /sbin/nologin -d <INSTALL_DIR> etcdmonitor
#     sudo ./install.sh --run-user etcdmonitor
# Or set `service.run_user: etcdmonitor` in config.yaml.
# See docs/SECURITY_CHECKLIST.md for the full production recommendation.
RUN_USER=""
CLI_RUN_USER=""
CLI_WORK_DIR=""
FORCE=false
for arg in "$@"; do
    case "$arg" in
        --run-user=*|-run-user=*)
            RUN_USER="${arg#*=}"
            CLI_RUN_USER="$RUN_USER"
            ;;
        --run-user|-run-user)
            # Next argument is the user (handled below)
            ;;
        --work-dir=*|-work-dir=*)
            CLI_WORK_DIR="${arg#*=}"
            ;;
        --work-dir|-work-dir)
            # Next argument is the path (handled below)
            ;;
        --force|-force)
            FORCE=true
            ;;
        -h|--help|-help)
            cat <<EOF
Usage: sudo ./install.sh [--run-user <name>] [--work-dir <path>] [--force]

  --run-user <name>   Run etcdmonitor as the given system user (must exist).
                      Default: \$(whoami) (root when using sudo).
                      Can also be set via config.yaml: service.run_user
                      Priority: CLI --run-user > config.yaml > install-meta > \$(whoami)
                      Production recommendation: --run-user etcdmonitor
                      (create with: useradd -r -s /sbin/nologin -d <dir> etcdmonitor)

  --work-dir <path>   Deploy runtime files (binary, config, data, logs) to the
                      specified directory instead of the extraction directory.
                      Default: the directory containing this script.
                      On upgrade, automatically read from /etc/etcdmonitor/install-meta.

  --force             Force installation when work directory has changed.
                      Required when --work-dir differs from a previous installation.
EOF
            exit 0
            ;;
    esac
done
# Handle `--run-user <value>` and `--work-dir <value>` forms (both single and double dash)
prev=""
for arg in "$@"; do
    if [ "$prev" = "--run-user" ] || [ "$prev" = "-run-user" ]; then
        RUN_USER="$arg"
        CLI_RUN_USER="$arg"
    fi
    if [ "$prev" = "--work-dir" ] || [ "$prev" = "-work-dir" ]; then
        CLI_WORK_DIR="$arg"
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
META_DIR="/etc/etcdmonitor"
META_FILE="$META_DIR/install-meta"

echo "[INFO] Install directory: $INSTALL_DIR"

# ===== Read install-meta for upgrade auto-completion =====
META_WORK_DIR=""
META_RUN_USER=""
if [ -f "$META_FILE" ]; then
    # shellcheck disable=SC1090
    . "$META_FILE" 2>/dev/null || true
    META_WORK_DIR="${WORK_DIR:-}"
    META_RUN_USER="${RUN_USER:-}"
    # Reset WORK_DIR/RUN_USER sourced from meta (will re-derive below)
    unset WORK_DIR
    RUN_USER=""
fi

# ===== Read service.run_user from config.yaml =====
CONFIG_RUN_USER=""
SRC_CONFIG_EARLY="$INSTALL_DIR/config.yaml"
if [ -f "$SRC_CONFIG_EARLY" ]; then
    CONFIG_RUN_USER=$(awk '
        /^service:/            { in_service=1; next }
        /^[a-zA-Z]/ && in_service { in_service=0 }
        in_service && /^[[:space:]]+run_user:/ {
            val=$2
            gsub(/^["'\''"]|["'\''"]$/, "", val)
            print val
            exit
        }
    ' "$SRC_CONFIG_EARLY" || true)
fi

# ===== Resolve RUN_USER (priority: CLI > config.yaml > install-meta > whoami) =====
if [ -n "$CLI_RUN_USER" ]; then
    RUN_USER="$CLI_RUN_USER"
elif [ -n "$CONFIG_RUN_USER" ]; then
    RUN_USER="$CONFIG_RUN_USER"
    echo "[INFO] Using run_user=$RUN_USER from config.yaml"
elif [ -n "$META_RUN_USER" ]; then
    RUN_USER="$META_RUN_USER"
    echo "[INFO] Using RUN_USER=$RUN_USER from previous installation record"
else
    RUN_USER="$(whoami)"
fi

# ===== Resolve WORK_DIR =====
if [ -n "$CLI_WORK_DIR" ]; then
    # CLI explicitly specified --work-dir
    mkdir -p "$CLI_WORK_DIR"
    WORK_DIR="$(cd "$CLI_WORK_DIR" && pwd)"
elif [ -n "$META_WORK_DIR" ]; then
    # Auto-complete from install-meta
    WORK_DIR="$META_WORK_DIR"
    echo "[INFO] Using WORK_DIR=$WORK_DIR from previous installation record"
else
    # Default: extraction directory
    WORK_DIR="$INSTALL_DIR"
fi

# Normalize both paths for reliable comparison
WORK_DIR_REAL="$(realpath "$WORK_DIR" 2>/dev/null || echo "$WORK_DIR")"
INSTALL_DIR_REAL="$(realpath "$INSTALL_DIR" 2>/dev/null || echo "$INSTALL_DIR")"

echo "[INFO] Work directory: $WORK_DIR"

# ===== Migration detection =====
if [ -n "$META_WORK_DIR" ]; then
    META_WORK_DIR_REAL="$(realpath "$META_WORK_DIR" 2>/dev/null || echo "$META_WORK_DIR")"
    if [ "$WORK_DIR_REAL" != "$META_WORK_DIR_REAL" ]; then
        if [ "$FORCE" = false ]; then
            echo ""
            echo "[ERROR] etcdmonitor is already installed at $META_WORK_DIR"
            echo "        You are attempting to install to $WORK_DIR"
            echo ""
            echo "  To migrate to a new directory:"
            echo "    1. sudo $META_WORK_DIR/uninstall.sh"
            echo "    2. cp -a $META_WORK_DIR/data $WORK_DIR/data"
            echo "    3. sudo ./install.sh --work-dir $WORK_DIR"
            echo ""
            echo "  To force overwrite (old directory becomes orphan):"
            echo "    sudo ./install.sh --work-dir $WORK_DIR --force"
            exit 1
        else
            echo "[WARN] Work directory changed: $META_WORK_DIR -> $WORK_DIR (--force)"
            # Disable old uninstall.sh to prevent accidental removal of new service
            if [ -f "$META_WORK_DIR/uninstall.sh" ]; then
                mv "$META_WORK_DIR/uninstall.sh" "$META_WORK_DIR/uninstall.sh.disabled" 2>/dev/null || true
                echo "[WARN] Renamed $META_WORK_DIR/uninstall.sh to uninstall.sh.disabled"
            fi
            echo "[WARN] Data in $META_WORK_DIR/data/ must be migrated manually if needed"
        fi
    fi
fi

# 检测当前系统架构并映射为 Go 命名
detect_arch() {
    local machine
    machine=$(uname -m)
    case "$machine" in
        x86_64)       echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)            echo "$machine" ;;
    esac
}
CURRENT_ARCH=$(detect_arch)

# 检查二进制文件（优先 etcdmonitor，回退到 etcdmonitor-linux-<arch>）
if [ -f "$INSTALL_DIR/etcdmonitor" ]; then
    SRC_BINARY="$INSTALL_DIR/etcdmonitor"
    BINARY_NAME="etcdmonitor"
elif [ -f "$INSTALL_DIR/etcdmonitor-linux-${CURRENT_ARCH}" ]; then
    SRC_BINARY="$INSTALL_DIR/etcdmonitor-linux-${CURRENT_ARCH}"
    BINARY_NAME="etcdmonitor-linux-${CURRENT_ARCH}"
else
    echo "[WARN] Binary not found, trying to build..."
    if command -v go &> /dev/null; then
        echo "[INFO] Building for linux/${CURRENT_ARCH}..."
        cd "$INSTALL_DIR"
        CGO_ENABLED=0 GOOS=linux GOARCH="$CURRENT_ARCH" go build -ldflags="-s -w" -o etcdmonitor ./cmd/etcdmonitor
        SRC_BINARY="$INSTALL_DIR/etcdmonitor"
        BINARY_NAME="etcdmonitor"
    else
        echo "[ERROR] Binary not found and Go is not installed."
        echo "        Please run build.sh on a machine with Go, or use package.sh to create an install package."
        exit 1
    fi
fi

echo "[INFO] Source binary: $SRC_BINARY"

# 检查配置文件（安装源中必须有 config.yaml）
SRC_CONFIG="$INSTALL_DIR/config.yaml"
if [ ! -f "$SRC_CONFIG" ]; then
    echo "[ERROR] config.yaml not found in $INSTALL_DIR"
    exit 1
fi

# ===== 文件复制（当 WORK_DIR != INSTALL_DIR）=====
if [ "$WORK_DIR_REAL" != "$INSTALL_DIR_REAL" ]; then
    echo "[INFO] Deploying files to work directory: $WORK_DIR"
    mkdir -p "$WORK_DIR"

    # Copy binary
    cp -f "$SRC_BINARY" "$WORK_DIR/$BINARY_NAME"
    echo "[INFO] Copied binary to $WORK_DIR/$BINARY_NAME"

    # Copy config.yaml (prompt if exists to preserve customized config)
    if [ -f "$WORK_DIR/config.yaml" ]; then
        echo "[WARN] config.yaml already exists in $WORK_DIR"
        read -p "[?] Overwrite with new config? [y/N]: " OVERWRITE_CONFIG
        if [[ "$OVERWRITE_CONFIG" =~ ^[yY]$ ]]; then
            cp -f "$SRC_CONFIG" "$WORK_DIR/config.yaml"
            echo "[INFO] config.yaml overwritten"
        else
            echo "[INFO] Using existing config.yaml"
        fi
    else
        cp "$SRC_CONFIG" "$WORK_DIR/config.yaml"
        echo "[INFO] Copied config.yaml to $WORK_DIR/"
    fi

    # Copy uninstall.sh (always overwrite to keep in sync with install version)
    if [ -f "$INSTALL_DIR/uninstall.sh" ]; then
        cp "$INSTALL_DIR/uninstall.sh" "$WORK_DIR/uninstall.sh"
        chmod +x "$WORK_DIR/uninstall.sh"
        echo "[INFO] Copied uninstall.sh to $WORK_DIR/"
    fi

    # Copy certs/ directory (preserve symlinks with cp -a)
    if [ -d "$INSTALL_DIR/certs" ]; then
        cp -a "$INSTALL_DIR/certs" "$WORK_DIR/"
        echo "[INFO] Copied certs/ to $WORK_DIR/"
    fi

    # Copy tools/ directory
    if [ -d "$INSTALL_DIR/tools" ]; then
        cp -a "$INSTALL_DIR/tools" "$WORK_DIR/"
        echo "[INFO] Copied tools/ to $WORK_DIR/"
    fi

    # NOTE: install.sh is NOT copied to WORK_DIR (by design)
fi

# ===== Set runtime paths to WORK_DIR =====
BINARY="$WORK_DIR/$BINARY_NAME"
CONFIG="$WORK_DIR/config.yaml"

# ===== 运行用户配置 =====
# 优先级：CLI --run-user > config.yaml service.run_user > install-meta > $(whoami)
if [ "$RUN_USER" = "root" ]; then
    WARN_MSG="[WARN] Running etcdmonitor as root. For production, prefer: --run-user etcdmonitor (create via useradd -r -s /sbin/nologin -d $WORK_DIR etcdmonitor) or set service.run_user in config.yaml"
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
    echo "      useradd -r -s /sbin/nologin -d $WORK_DIR $RUN_USER"
    echo ""
    echo "  Or remove --run-user / service.run_user to use \$(whoami) default."
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
    if [ ! -f "$WORK_DIR/certs/server.key" ] || [ ! -f "$WORK_DIR/certs/server.crt" ]; then
        echo ""
        echo "[ERROR] TLS enabled but cert files missing."
        echo "        Run: $WORK_DIR/tools/gen-certs.sh"
        echo "        (add --host <hostname> --ip <addr> for production access)"
        exit 1
    fi
fi

# 创建数据目录
mkdir -p "$WORK_DIR/data"

# 创建日志目录
mkdir -p "$WORK_DIR/logs"

# 设置权限
chmod +x "$BINARY"
chown -R "$RUN_USER:$RUN_GROUP" "$WORK_DIR/data"
chown -R "$RUN_USER:$RUN_GROUP" "$WORK_DIR/logs"
chown "$RUN_USER:$RUN_GROUP" "$CONFIG"
chown "$RUN_USER:$RUN_GROUP" "$BINARY"

# 收紧数据目录权限：包含密码哈希、会话数据、初始密码文件，禁止同组/其他用户读取
chmod 0700 "$WORK_DIR/data"
# 若已有 DB 文件，同步收紧到 0600
if [ -f "$WORK_DIR/data/etcdmonitor.db" ]; then
    chmod 0600 "$WORK_DIR/data/etcdmonitor.db"
fi
if [ -f "$WORK_DIR/data/initial-admin-password" ]; then
    chmod 0600 "$WORK_DIR/data/initial-admin-password"
fi

# 证书目录权限（私钥仅运行用户可读）
# 尊重 symlink：企业运维可能把 server.key 链接到集中证书管理目录，
# 无脑 chmod 会修改目标文件权限；因此对 symlink 跳过 chmod。
if [ -d "$WORK_DIR/certs" ]; then
    chown -R "$RUN_USER:$RUN_GROUP" "$WORK_DIR/certs" 2>/dev/null || true
    for f in "$WORK_DIR/certs"/*.key; do
        [ -e "$f" ] || continue
        if [ -L "$f" ]; then
            echo "[INFO] Skip chmod for symlink $f (managed externally)"
        else
            chmod 600 "$f"
        fi
    done
    for f in "$WORK_DIR/certs"/*.crt; do
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
WorkingDirectory=$WORK_DIR
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
ReadWritePaths=$WORK_DIR/data $WORK_DIR/logs

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
    LISTEN_PORT=$(grep -A1 "^server:" "$CONFIG" | grep "listen" | grep -Eo '[0-9]+' || echo "9090")
    TLS_ENABLED=$(grep "tls_enable" "$CONFIG" | grep -i "true" || true)
    if [ -n "$TLS_ENABLED" ]; then
        PROTOCOL="https"
    else
        PROTOCOL="http"
    fi

    # ===== Write install-meta =====
    mkdir -p "$META_DIR"
    chmod 0755 "$META_DIR"
    # Read version from version file if available
    ETCD_MON_VERSION=""
    if [ -f "$INSTALL_DIR/version" ]; then
        ETCD_MON_VERSION=$(head -1 "$INSTALL_DIR/version" | tr -d '[:space:]')
    fi
    cat > "$META_FILE" <<METAEOF
WORK_DIR=$WORK_DIR
RUN_USER=$RUN_USER
INSTALL_SOURCE=$INSTALL_DIR
INSTALLED_AT=$(date -u +"%Y-%m-%dT%H:%M:%S")
VERSION=$ETCD_MON_VERSION
METAEOF
    chmod 0644 "$META_FILE"

    echo "========================================="
    echo "[OK] etcdmonitor installed successfully!"
    echo ""
    echo "  Service status: running"
    echo "  Run as user:    $RUN_USER"
    echo "  Work directory: $WORK_DIR"
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
    echo "  Log files:  $WORK_DIR/logs/"
    echo "  Data files: $WORK_DIR/data/"
    if [ "$WORK_DIR_REAL" != "$INSTALL_DIR_REAL" ]; then
        echo ""
        echo "  Uninstall: sudo $WORK_DIR/uninstall.sh"
        echo "  Reinstall: run install.sh from the original package"
    else
        echo ""
        echo "  Uninstall: sudo ./uninstall.sh"
        echo "  Reinstall: sudo ./install.sh"
    fi
    echo "========================================="
else
    echo "[ERROR] Service failed to start!"
    echo "  Check logs: journalctl -u $SERVICE_NAME -n 50"
    echo "  Check log file: $WORK_DIR/logs/etcdmonitor.log"
    exit 1
fi
