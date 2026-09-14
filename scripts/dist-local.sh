#!/usr/bin/env bash
# =============================================================================
# dist-local.sh — 在本地打出与正式发布同名的全套产物
#
#   openwrt-dist/luci-app-kwrtmgrd_<ver>-1_all.ipk    OpenWrt 单 all 架构 ipk
#   dist/kwrt-net-manager_<ver>_<os>_<arch>.tar.gz    18 个平台的归档（windows 为 zip）
#   dist/checksums.txt                                sha256 校验和
#
#   跨平台编译是纯 Go + CGO_ENABLED=0，不需要 C 工具链，本机模式下也不需要 Docker。
#
# 参数：
#   --version <x.y.z>  版本号（也可用环境变量 VERSION）。必填，且必须是 GitHub
#                      Release 上真实存在的版本 —— ipk 是壳子包，装机时由
#                      kwrtmgrd-fetch 按这个版本号去拉对应 CPU 的二进制。
#   --docker           改在容器里构建（node:20 + golang:1.25，与 CI 同口径），
#                      本机除 Docker 外什么都不用装。首次运行要在容器内编译
#                      goreleaser/nfpm，需要几分钟。
#   --skip-web         跳过前端构建（web/dist 已是最新时用，省一次 npm）。
#   --help
#
# 示例：
#   bash scripts/dist-local.sh --version 0.0.55
#   bash scripts/dist-local.sh --version 0.0.55 --docker
#
# 本机模式依赖： node / go / goreleaser / nfpm
#   go install github.com/goreleaser/goreleaser/v2@latest
#   go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION="${VERSION:-}"
USE_DOCKER=0
SKIP_WEB=0

C_GRN=''; C_RED=''; C_RST=''
if [ -t 1 ]; then C_GRN='\033[0;32m'; C_RED='\033[0;31m'; C_RST='\033[0m'; fi
info() { printf "%b\n" "[*] $*"; }
ok()   { printf "%b\n" "${C_GRN}[+]${C_RST} $*"; }
die()  { printf "%b\n" "${C_RED}[x]${C_RST} $*" >&2; exit 1; }

usage() { sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
	case "$1" in
		--version)   VERSION="${2:-}"; shift 2 ;;
		--docker)    USE_DOCKER=1; shift ;;
		--skip-web)  SKIP_WEB=1; shift ;;
		-h|--help)   usage; exit 0 ;;
		*)           die "未知参数: $1 (--help 查看用法)" ;;
	esac
done

[ -n "$VERSION" ] || die "缺少版本号：用 --version 或环境变量 VERSION（例：--version 0.0.55）"
VERSION="${VERSION#v}"

cd "$ROOT_DIR"

if [ "$USE_DOCKER" = 1 ]; then
	command -v docker >/dev/null 2>&1 || die "未找到 docker"
	docker info >/dev/null 2>&1 || die "Docker 引擎没在运行，请先启动 Docker Desktop"
	# git-bash 下 -v 要 F:/... 形态，且要挡掉 MSYS 对 /src 的路径转换
	HOSTPWD="$(pwd -W 2>/dev/null || pwd)"
	export MSYS_NO_PATHCONV=1
	if [ "$SKIP_WEB" = 0 ]; then
		info "容器内构建前端 dist（node:20）"
		docker run --rm -v "$HOSTPWD:/src" -w /src/web node:20 \
			sh -c "npm ci --no-audit --no-fund --legacy-peer-deps && npm run build"
	fi
	info "容器内打包 ipk + 全平台归档（golang:1.25）"
	docker run --rm -v "$HOSTPWD:/src" -w /src -e VERSION="$VERSION" golang:1.25 bash -c '
		set -euo pipefail
		go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
		go install github.com/goreleaser/goreleaser/v2@latest
		export PATH="/go/bin:$PATH"
		./openwrt/build-ipk.sh --version "$VERSION" --out openwrt-dist
		GORELEASER_CURRENT_TAG="v$VERSION" goreleaser release --clean --skip=publish,validate'
else
	command -v go >/dev/null 2>&1 || die "未找到 go"
	command -v nfpm >/dev/null 2>&1 || die "未找到 nfpm: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest（装完确认 $(go env GOPATH)/bin 在 PATH 里）"
	command -v goreleaser >/dev/null 2>&1 || die "未找到 goreleaser: go install github.com/goreleaser/goreleaser/v2@latest"
	if [ "$SKIP_WEB" = 0 ]; then
		command -v npm >/dev/null 2>&1 || die "未找到 npm（前端 dist 要嵌进二进制；已是最新可加 --skip-web）"
		info "构建前端 dist"
		(cd web && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build)
	fi
	[ -d web/dist ] || die "缺少 web/dist —— //go:embed dist 需要它，请去掉 --skip-web"
	info "打包 OpenWrt ipk"
	./openwrt/build-ipk.sh --version "$VERSION" --out openwrt-dist
	info "交叉编译全平台归档（goreleaser）"
	GORELEASER_CURRENT_TAG="v$VERSION" goreleaser release --clean --skip=publish,validate
fi

ok "产物已就绪："
ls -1 openwrt-dist/*.ipk 2>/dev/null || true
printf '    dist/  共 %s 个归档 + checksums.txt\n' "$(ls dist/ 2>/dev/null | grep -cE '\.tar\.gz$|\.zip$' || echo 0)"
