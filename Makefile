SHELL := /bin/sh
VERSION ?= dev
BUILD_DATE := $(shell date -u +%Y-%m-%d)
LDFLAGS := -s -w \
    -X github.com/nue-mic/kwrt-net-manager/pkg/version.Number=$(VERSION) \
    -X github.com/nue-mic/kwrt-net-manager/pkg/version.BuildDate=$(BUILD_DATE)

.PHONY: build build-host web web-install test vet tidy clean docker run ipk dist-local dist-docker

# 前端依赖 — 仅在 node_modules 缺失时跑一次完整 install
web-install:
	test -d web/node_modules || (cd web && npm ci)

# 构建前端 dist —— 嵌入到 Go 二进制需要的产物
# 必须在 build / docker 之前执行；否则 //go:embed dist 会失败或得到空 FS
web: web-install
	cd web && npm run build

# Go 跨平台 (Linux/amd64) 构建 daemon —— 镜像里用这个
# 自动先 build web，确保 dist 是最新的
build: web
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kwrtmgrd ./cmd/kwrtmgrd

# 本机平台构建（Windows/Mac/Linux 通用），用于本地开发跑 daemon
build-host: web
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kwrtmgrd ./cmd/kwrtmgrd

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/ web/dist/

# Docker 镜像构建：Dockerfile 自带 node:20 + golang:1.25 多阶段，
# 内部完成 npm build + go build。任何环境（本地 / CI / 干净 clone）
# 都可直接跑，无前置依赖。
docker:
	docker build -f deploy/Dockerfile -t kwrt-net-manager:$(VERSION) \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg BUILD_DATE=$(BUILD_DATE) \
	  .

run: build-host
	KWRTNET_API_TOKEN=dev KWRTNET_DATA_DIR=./tmp/data ./bin/kwrtmgrd serve

# OpenWrt 单个 all 架构 ipk（壳子包，装时由 kwrtmgrd-fetch 按 CPU 拉二进制）。
# 需 nfpm：go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
# VERSION 决定 fetcher 默认拉取的二进制版本，发布时由 CI 注入真实版本号。
ipk:
	./openwrt/build-ipk.sh --version $(VERSION) --out dist-ipk

# 本机打出与正式发布同名的全套产物：OpenWrt ipk（openwrt-dist/）+ 18 个平台的
# tar.gz/zip + checksums.txt（dist/）。跨平台编译是纯 Go + CGO_ENABLED=0，不需要
# 任何 C 工具链，也不需要 Docker。
#   make dist-local VERSION=0.0.55
# VERSION 必须是 GitHub Release 上真实存在的版本：ipk 是壳子包，装机时由
# kwrtmgrd-fetch 按这个版本号去拉对应 CPU 的二进制。
# 需要 goreleaser 与 nfpm 在 PATH（go install 装的在 `go env GOPATH`/bin）。
dist-local:
	./scripts/dist-local.sh --version $(VERSION)

# 同上，但全部在容器里完成（node:20 + golang:1.25，与 CI 同口径），本机除 Docker
# 外什么都不用装：
#   make dist-docker VERSION=0.0.55
# 首次运行要在容器内编译 goreleaser/nfpm，耗时几分钟；产物同样落 dist/ 与 openwrt-dist/。
dist-docker:
	./scripts/dist-local.sh --version $(VERSION) --docker
