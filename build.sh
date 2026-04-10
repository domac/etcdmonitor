#!/bin/bash
set -e

echo "========================================="
echo "  etcdmonitor - Build Script"
echo "========================================="

cd "$(dirname "$0")"
PROJECT_DIR="$(pwd)"

# 检测 Go 环境
if ! command -v go &> /dev/null; then
    echo "[ERROR] Go is not installed. Please install Go 1.21+"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
echo "[INFO] Go version: $GO_VERSION"

# 读取版本号
VERSION_FILE="${PROJECT_DIR}/version"
if [ -f "$VERSION_FILE" ]; then
    VERSION=$(head -1 "$VERSION_FILE" | tr -d '[:space:]')
else
    VERSION="dev"
fi
echo "[INFO] Version: $VERSION"

# 下载依赖
echo "[INFO] Downloading dependencies..."
go mod tidy

# 编译 Linux amd64 版本（用于 CentOS 部署）
echo "[INFO] Building for Linux amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o etcdmonitor-linux-amd64 ./cmd/etcdmonitor

echo "========================================="
echo "[OK] Build successful!"
echo "  Version: $VERSION"
echo "  Binary:  ./etcdmonitor-linux-amd64"
echo "========================================="
