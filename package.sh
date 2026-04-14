#!/bin/bash
set -e

echo "========================================="
echo "  etcdmonitor - Package Script"
echo "========================================="

cd "$(dirname "$0")"
PROJECT_DIR="$(pwd)"

# 读取版本号
VERSION_FILE="${PROJECT_DIR}/version"
if [ ! -f "$VERSION_FILE" ]; then
    echo "[ERROR] version file not found: $VERSION_FILE"
    exit 1
fi
VERSION=$(head -1 "$VERSION_FILE" | tr -d '[:space:]')
if [ -z "$VERSION" ]; then
    echo "[ERROR] version file is empty"
    exit 1
fi

PACKAGE_NAME="etcdmonitor-v${VERSION}-linux-amd64"
DIST_DIR="${PROJECT_DIR}/dist"
ZIPFILE="${DIST_DIR}/${PACKAGE_NAME}.zip"
BINARY="${PROJECT_DIR}/etcdmonitor-linux-amd64"
STAGING_DIR="${DIST_DIR}/${PACKAGE_NAME}"

echo "[INFO] Version:  ${VERSION} (from version file)"
echo "[INFO] Output:   dist/${PACKAGE_NAME}.zip"

# Step 1: 检查 Go 环境
if ! command -v go &> /dev/null; then
    echo "[ERROR] Go is not installed. Please install Go 1.21+ first."
    exit 1
fi
GO_VERSION=$(go version | awk '{print $3}')
echo "[INFO] Go:       $GO_VERSION"

# Step 2: 强制重新编译
echo ""
echo "[INFO] Downloading dependencies..."
go mod tidy

echo "[INFO] Compiling for Linux amd64 (version: ${VERSION})..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o "$BINARY" ./cmd/etcdmonitor
echo "[OK] Build successful."

# Step 3: 清理旧的 dist 产物
rm -rf "$STAGING_DIR"
rm -f "$ZIPFILE"
mkdir -p "$DIST_DIR"

# Step 4: 创建打包目录，组装文件
echo "[INFO] Assembling package..."
mkdir -p "$STAGING_DIR"

cp "$BINARY"                        "$STAGING_DIR/etcdmonitor"
cp "${PROJECT_DIR}/config.yaml"     "$STAGING_DIR/config.yaml"
cp "${PROJECT_DIR}/install.sh"      "$STAGING_DIR/install.sh"
cp "${PROJECT_DIR}/uninstall.sh"    "$STAGING_DIR/uninstall.sh"

# 生成 TLS 证书打入安装包
echo "[INFO] Generating TLS certificates..."
mkdir -p "$STAGING_DIR/certs"
openssl req -x509 -newkey rsa:2048 \
    -keyout "$STAGING_DIR/certs/server.key" \
    -out "$STAGING_DIR/certs/server.crt" \
    -days 365 -nodes \
    -subj "/CN=etcdmonitor" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:0.0.0.0" 2>/dev/null
chmod 600 "$STAGING_DIR/certs/server.key"

chmod +x "$STAGING_DIR/etcdmonitor"
chmod +x "$STAGING_DIR/install.sh"
chmod +x "$STAGING_DIR/uninstall.sh"
chmod +x "$STAGING_DIR/uninstall.sh"

# Step 5: 打包为 zip
echo "[INFO] Creating zip package..."
cd "$DIST_DIR"
zip -rq "$ZIPFILE" "$PACKAGE_NAME"/

# Step 6: 清理
rm -rf "$STAGING_DIR"   # 清理临时组装目录
rm -f "$BINARY"         # 删除项目根目录下的二进制文件

# Step 7: 输出结果
ZIPFILE_SIZE=$(ls -lh "$ZIPFILE" | awk '{print $5}')

echo ""
echo "========================================="
echo "[OK] Package created successfully!"
echo ""
echo "  Version: ${VERSION}"
echo "  File:    dist/${PACKAGE_NAME}.zip"
echo "  Size:    ${ZIPFILE_SIZE}"
echo ""
echo "  Deploy to server:"
echo "    scp dist/${PACKAGE_NAME}.zip root@<server>:/data/services/"
echo "    ssh root@<server>"
echo "    mkdir -p /data/services && cd /data/services"
echo "    unzip ${PACKAGE_NAME}.zip"
echo "    cd ${PACKAGE_NAME}"
echo "    vim config.yaml      # edit config"
echo "    sudo ./install.sh    # install & start"
echo "========================================="
