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

# 平台列表：支持通过 PLATFORMS 环境变量覆盖
DEFAULT_PLATFORMS=("linux-amd64" "linux-arm64")
if [ -n "$PLATFORMS" ]; then
    IFS=' ' read -r -a BUILD_PLATFORMS <<< "$PLATFORMS"
else
    BUILD_PLATFORMS=("${DEFAULT_PLATFORMS[@]}")
fi

DIST_DIR="${PROJECT_DIR}/dist"

echo "[INFO] Version:    ${VERSION} (from version file)"
echo "[INFO] Platforms:  ${BUILD_PLATFORMS[*]}"
echo "[INFO] Output dir: dist/"

# Step 1: 检查 Go 环境
if ! command -v go &> /dev/null; then
    echo "[ERROR] Go is not installed. Please install Go 1.21+ first."
    exit 1
fi
GO_VERSION=$(go version | awk '{print $3}')
echo "[INFO] Go:         $GO_VERSION"

# Step 2: 下载依赖
echo ""
echo "[INFO] Downloading dependencies..."
go mod tidy

# Step 3: 清理旧的 dist 产物
mkdir -p "$DIST_DIR"

# 跟踪所有生成的临时二进制和 zip 文件
TEMP_BINARIES=()
ZIP_FILES=()

# Step 4: 循环编译和打包每个平台
for PLATFORM in "${BUILD_PLATFORMS[@]}"; do
    # 解析 os-arch 格式
    GOOS_VAL="${PLATFORM%-*}"
    GOARCH_VAL="${PLATFORM#*-}"

    # 校验平台格式
    if [ -z "$GOOS_VAL" ] || [ -z "$GOARCH_VAL" ] || [ "$GOOS_VAL" = "$GOARCH_VAL" ]; then
        echo "[ERROR] Invalid platform format: '$PLATFORM' (expected: <os>-<arch>, e.g. linux-amd64)"
        exit 1
    fi

    PACKAGE_NAME="etcdmonitor-v${VERSION}-${PLATFORM}"
    ZIPFILE="${DIST_DIR}/${PACKAGE_NAME}.zip"
    BINARY="${PROJECT_DIR}/etcdmonitor-${PLATFORM}"
    STAGING_DIR="${DIST_DIR}/${PACKAGE_NAME}"

    echo ""
    echo "-----------------------------------------"
    echo "[INFO] Building ${PLATFORM}..."
    echo "-----------------------------------------"

    # 编译
    CGO_ENABLED=0 GOOS="$GOOS_VAL" GOARCH="$GOARCH_VAL" go build \
        -ldflags="-s -w -X main.Version=${VERSION}" \
        -o "$BINARY" ./cmd/etcdmonitor

    if [ ! -f "$BINARY" ]; then
        echo "[ERROR] Build failed for ${PLATFORM}: binary not found"
        exit 1
    fi
    echo "[OK] Build successful: ${PLATFORM}"

    TEMP_BINARIES+=("$BINARY")

    # 清理旧的 staging 和 zip
    rm -rf "$STAGING_DIR"
    rm -f "$ZIPFILE"

    # 创建打包目录，组装文件（二进制统一命名为 etcdmonitor）
    echo "[INFO] Assembling package for ${PLATFORM}..."
    mkdir -p "$STAGING_DIR"

    cp "$BINARY"                        "$STAGING_DIR/etcdmonitor"
    cp "${PROJECT_DIR}/config.yaml"     "$STAGING_DIR/config.yaml"
    cp "${PROJECT_DIR}/install.sh"      "$STAGING_DIR/install.sh"
    cp "${PROJECT_DIR}/uninstall.sh"    "$STAGING_DIR/uninstall.sh"

    # 打入证书生成脚本
    mkdir -p "$STAGING_DIR/tools"
    cp "${PROJECT_DIR}/tools/gen-certs.sh" "$STAGING_DIR/tools/gen-certs.sh"
    chmod +x "$STAGING_DIR/tools/gen-certs.sh"

    # 打入 certs/README.md 指引；不内置任何 .key / .crt
    mkdir -p "$STAGING_DIR/certs"
    cp "${PROJECT_DIR}/certs/README.md"  "$STAGING_DIR/certs/README.md"

    chmod +x "$STAGING_DIR/etcdmonitor"
    chmod +x "$STAGING_DIR/install.sh"
    chmod +x "$STAGING_DIR/uninstall.sh"

    # 打包为 zip
    echo "[INFO] Creating zip: ${PACKAGE_NAME}.zip"
    cd "$DIST_DIR"
    zip -rq "$ZIPFILE" "$PACKAGE_NAME"/
    cd "$PROJECT_DIR"

    # 防御性校验 —— zip 内禁止出现任何私钥 / 证书文件
    if unzip -l "$ZIPFILE" | grep -Eq '\.(key|crt|pem|p12|pfx)$'; then
        echo "[ERROR] Package ${PLATFORM} contains certificate / private-key files. Aborting."
        unzip -l "$ZIPFILE" | grep -E '\.(key|crt|pem|p12|pfx)$'
        exit 1
    fi

    # 清理临时组装目录
    rm -rf "$STAGING_DIR"

    ZIP_FILES+=("$ZIPFILE")
    echo "[OK] Package created: dist/${PACKAGE_NAME}.zip"
done

# Step 5: 清理项目根目录下的所有临时二进制文件
for BIN in "${TEMP_BINARIES[@]}"; do
    rm -f "$BIN"
done

# Step 6: 输出构建摘要
echo ""
echo "========================================="
echo "[OK] All packages created successfully!"
echo ""
echo "  Version:   ${VERSION}"
echo "  Platforms:  ${BUILD_PLATFORMS[*]}"
echo ""
for ZF in "${ZIP_FILES[@]}"; do
    ZF_NAME=$(basename "$ZF")
    ZF_SIZE=$(ls -lh "$ZF" | awk '{print $5}')
    echo "  ${ZF_NAME}  (${ZF_SIZE})"
done
echo ""
echo "  Deploy to server:"
echo "    scp dist/etcdmonitor-v${VERSION}-linux-<arch>.zip root@<server>:/data/services/"
echo "    ssh root@<server>"
echo "    mkdir -p /data/services && cd /data/services"
echo "    unzip etcdmonitor-v${VERSION}-linux-<arch>.zip"
echo "    cd etcdmonitor-v${VERSION}-linux-<arch>"
echo "    vim config.yaml      # edit config"
echo "    sudo ./install.sh    # install & start"
echo "========================================="
