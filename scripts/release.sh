#!/bin/bash
# ==============================================================================
# Go Gin 应用程序编译与打包发布包脚本 (release.sh)
# 用法: scripts/release.sh [staging|production]
# ==============================================================================
set -euo pipefail

export GOFLAGS="${GOFLAGS:-} -mod=mod"

# 构建与打包路径：均可通过环境变量覆盖，默认值即仓库约定
APP_MAIN=${APP_MAIN:-./cmd/api}
BUILD_DIR=${BUILD_DIR:-build}
SERVER_BINARY_NAME=${SERVER_BINARY_NAME:-$(go list -m 2>/dev/null | awk -F/ '{print $NF}' || echo "bgin")}
DEPLOY_DIR=${DEPLOY_DIR:-deploy}

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1" >&2; }

show_help() {
    echo "Deployment packager for $SERVER_BINARY_NAME"
    echo ""
    echo "Usage: $0 [environment]"
    echo ""
    echo "Environments:"
    echo "  staging       Build and package for staging"
    echo "  production    Build and package for production"
    echo ""
    echo "Output:"
    echo "  ${DEPLOY_DIR}/release-<environment>.tar.gz"
    echo ""
    echo "Examples:"
    echo "  $0 staging"
    echo "  $0 production"
}

# ── 验证环境 ──────────────────────────────────────────────────────────────────

ENVIRONMENT="${1:-}"
case "$ENVIRONMENT" in
    staging|production) ;;
    help|--help|-h) show_help; exit 0 ;;
    "")
        log_error "Environment required."
        show_help
        exit 1
        ;;
    *)
        log_error "Unknown environment: $ENVIRONMENT"
        show_help
        exit 1
        ;;
esac

log_info "Target environment: $ENVIRONMENT"

# ── 代码质量与测试检查 ─────────────────────────────────────────────────────────

log_info "Running code quality checks and tests..."
just check

# ── 编译 ───────────────────────────────────────────────────────────────────────

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
LDFLAGS="-w -s -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME -X main.GitCommit=$GIT_COMMIT"

BINARY="$BUILD_DIR/${SERVER_BINARY_NAME}-linux-amd64"
mkdir -p "$BUILD_DIR"

log_info "Building $ENVIRONMENT binary (linux/amd64) — version: $VERSION..."
GOOS=linux GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "$BINARY" "$APP_MAIN"
log_success "Build complete: $BINARY"

# ── 打包 ───────────────────────────────────────────────────────────────────────

RELEASE_DIR="$(mktemp -d)"
trap 'rm -rf "$RELEASE_DIR"' EXIT

cp "$BINARY" "$RELEASE_DIR/$SERVER_BINARY_NAME"
[ -d configs ] && cp -r configs "$RELEASE_DIR/"
[ -d database ] && cp -r database "$RELEASE_DIR/"
[ -f .env.example ] && cp .env.example "$RELEASE_DIR/.env.example"

mkdir -p "$DEPLOY_DIR"
ARCHIVE="$DEPLOY_DIR/release-${ENVIRONMENT}.tar.gz"
tar -czf "$ARCHIVE" -C "$RELEASE_DIR" .

log_success "Release archive: $ARCHIVE"
log_info "Contents:"
tar -tf "$ARCHIVE" | sed 's/^/  /'

