# bgin 常用工程任务 (基于 justfile)

set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load := true

# 列出全部可用任务
default:
    @just --list

# 开发模式：使用 concurrently 同时启动 API 服务与 Worker (均支持 Air 热重载)
dev: free-port
    concurrently -n "api,worker" -c "bgCyan.black.bold,bgYellow.black.bold" --kill-others \
        "just dev-api" \
        "just dev-worker"

# 仅热重载启动 API 服务 (Air)
dev-api: free-port
    air

# 仅热重载启动 Worker 服务 (Air)
dev-worker:
    air -c .air.worker.toml

# 直接运行 API 服务 (如 just run --port 3400)
run *ARGS:
    @just free-port "$(printf '%s' '{{ARGS}}' | sed -nE 's/.*(-p|--port)[= ]+([0-9]+).*/\2/p' | tail -1)"
    go run ./cmd/api serve {{ARGS}}

# 直接运行 Worker 服务 (如 just work -q high:3,default:1)
work *ARGS:
    go run ./cmd/api queue:work {{ARGS}}

# 运行 CLI 控制台命令 (如 just cli route:list, just cli make:crud blog)
cli *ARGS:
    go run ./cmd/api {{ARGS}}

alias artisan := cli

# 释放指定或默认 APP_PORT 端口
free-port port="":
    @port="{{port}}"; port="${port:-${APP_PORT:-3300}}"; pids=$(lsof -ti tcp:$port -sTCP:LISTEN || true); if [ -z "$pids" ]; then echo "端口 $port 空闲"; else echo "端口 $port 被占用（PID $(echo $pids)），结束它"; kill $pids 2>/dev/null || true; sleep 1; kill -9 $(lsof -ti tcp:$port -sTCP:LISTEN 2>/dev/null || true) 2>/dev/null || true; echo "端口 $port 已释放"; fi

# 编译可执行文件到 bin/bgin
build:
    go build -trimpath \
        -ldflags "-X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev) \
                  -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo none) \
                  -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        -o bin/bgin ./cmd/api

# 整理 go.mod 依赖
tidy:
    go mod tidy

# 清理构建产物与临时文件
clean:
    rm -rf bin tmp coverage.out coverage.html

# 运行单元测试
test:
    go test ./...

# 运行集成测试 (依赖外部服务，标签 integration)
test-int:
    go test -tags=integration -count=1 ./tests/integration/...

# 运行全量测试 (单元测试 + 集成测试)
test-all: test test-int

# 生成全量覆盖率报告
coverage:
    go test -tags=integration -count=1 -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | tail -1
    @echo "明细：go tool cover -html=coverage.out"

# 静态代码检查 (golangci-lint)
lint:
    golangci-lint run

# 代码格式化并就地改写
fmt:
    golangci-lint fmt

# 仅检查代码格式
fmt-check:
    golangci-lint fmt --diff

# 提交前门禁 (fmt-check + lint + test)
check: fmt-check lint test

# 完整 CI 门禁 (fmt-check + lint + test-all)
ci: fmt-check lint test-all

# HTTP 接口基准压测 (wrk)
bench path="/livez" *args="":
    wrk -t4 -c200 -d30s --latency {{args}} "http://localhost:${APP_PORT:-3300}{{path}}"

alias wrk := bench

# 应用未执行的 Atlas 数据库迁移
migrate:
    atlas migrate apply --env app

# 基于 schemas/ 目录的声明式 DDL 差异生成新的版本迁移
migrate-diff name:
    atlas migrate diff {{name}} --env local

# 重算 migrations/ 目录迁移哈希校验链
migrate-hash:
    atlas migrate hash --dir file://migrations

# 幂等注入首个管理员数据
seed:
    go run ./cmd/api db:seed

# 破坏性重建数据库并重新播种 (需确认)
fresh:
    go run ./cmd/api db:fresh

# 破坏性重建数据库并重新播种 (跳过确认)
fresh-force:
    go run ./cmd/api db:fresh --force

# 生成或更新 JWT 签名密钥 (仿照 artisan jwt:secret)
jwt-secret *ARGS:
    go run ./cmd/api jwt:secret {{ARGS}}

# 打开 PostgreSQL 交互终端
db:
    psql "$DB_URL"

alias db-shell := db

# 打开 Redis/Valkey 交互终端
redis:
    docker exec -it -e REDISCLI_AUTH="$(echo "$REDIS_URL" | sed -E 's#^redis://[^@]*:([^@]*)@.*#\1#')" valkey valkey-cli

alias cache-shell := redis

# 生成前台与管理端两套 OpenAPI 接口文档到 internal/shared/apidocs/
docs:
    swag init -d "$(find cmd internal -type f -name '*.go' ! -path '*/admin/*' ! -path '*/admin' -exec dirname {} + | sort -u | paste -sd, -)" -g main.go -o internal/shared/apidocs --parseInternal --outputTypes json
    swag init -d "$(find cmd internal -type f -name '*.go' ! -path '*/features/client*' ! -path '*/features/realtime*' -exec dirname {} + | sort -u | paste -sd, -)" -g admin_doc.go -o internal/shared/apidocs/admin_tmp --parseInternal --outputTypes json
    mv internal/shared/apidocs/admin_tmp/swagger.json internal/shared/apidocs/admin_swagger.json
    rm -rf internal/shared/apidocs/admin_tmp

