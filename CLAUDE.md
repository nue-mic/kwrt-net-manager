# CLAUDE.md — kwrt-net-manager 项目指南

> 本文件为本仓库的项目级指令，供 Claude Code 在本项目中工作时遵循。
> 全局通用规范（语言、Windows Shell、Git、各专家 Skill）见用户级 `~/.claude/CLAUDE.md`，此处**不重复**，只记录本项目特有、且最容易踩坑的信息。

---

## 1. 这是什么

一个面向 **OpenWrt** 的 **网络管理面板**（仿爱快 iKuai）：把 DHCP（dnsmasq）与静态路由的繁琐配置，变成「装上守护进程 → 打开网页 → 点鼠标」。本仓库由 frpc-manager 改造而来——**frpc 核心已整体移除，壳子（打包/自升级/备份/鉴权/品牌/监控）保留**。

- 后端：Go 守护进程 **`kwrtmgrd`**，对外暴露 HTTP API + WebSocket。网络配置通过**可插拔后端**落地：
  - **uci 后端**（OpenWrt）：读写 `/etc/config/{dhcp,network}`（经 `uci`）、读 `/tmp/dhcp.leases`、`ip route` 读路由表、`/etc/init.d/{dnsmasq,network} reload` 生效。
  - **store 后端**（开发/CI/非 OpenWrt）：状态持久到 `DATA_DIR/netcfg.json` + 模拟租约，全部页面可在 Windows 端到端跑通。
  - 由 `KWRTNET_NETCFG_BACKEND=uci|store|auto`（默认 auto，探测到 uci 即用）选择。
- 前端：React + TypeScript + Vite + Ant Design 单页，构建产物 `web/dist` 通过 `//go:embed` 嵌进二进制，生产同域。
- 单二进制交付，install.sh 自带 systemd / **procd(OpenWrt)** / OpenRC / launchd 服务注册（busybox 无 `install` applet 已回退 `cp`+`chmod`），另有 OpenWrt 单 all 架构 ipk。

## 2. 命名约定（改造后，务必遵守）

| 项 | 值 |
|---|---|
| Go 模块路径 | `github.com/nue-mic/kwrt-net-manager` |
| 守护进程 / 服务 / 入口 | `kwrtmgrd`（`cmd/kwrtmgrd`，产物 `bin/kwrtmgrd`） |
| 环境变量前缀 | `KWRTNET_`（如 `KWRTNET_API_TOKEN`） |
| OpenWrt UCI 配置 | `/etc/config/kwrtmgrd` |
| luci 包 / 管理命令 | `luci-app-kwrtmgrd` / `kmc` |
| 前端品牌注入变量 | `window.__KWRTNET_BRANDING__` |
| 默认品牌 | 「KWRT 网络管理」/「DHCP · 静态路由」 |

## 3. 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.25、标准库 `net/http`、`log/slog`、`coder/websocket`、`go-chi`、`gopsutil`（监控）、`minio-go`/`gowebdav`（备份）|
| 前端 | React 19 + TypeScript + Vite + Ant Design 6 + axios |
| 交付 | 单二进制（embed dist）、Docker 多阶段、`scripts/install.{sh,ps1}`、统一管理命令 `kmc`、OpenWrt 单 all 架构 ipk |

## 4. 架构与目录

```
cmd/kwrtmgrd/main.go      # 入口：serve / health / version
internal/
  api/                   # HTTP 层：server.go 路由 + netcfg_*.go / 壳子 handler + openapi.yaml
    netcfg_routes.go     #   DHCP/路由 路由注册 + 公共(接口/状态/路由表)
    netcfg_dhcp.go       #   DHCP 服务端/静态分配/终端列表/黑白名单 handler
    netcfg_route.go      #   静态路由 handler
    exportsource.go      #   /export/all、/import/zip（meta + netcfg.json 打包/还原）
    ui.go syscfg.go update.go system.go events.go backup.go docs.go  # 壳子
  netcfg/                # 领域核心
    types.go service.go validate.go backend.go    # 类型/Service(校验+事件)/后端接口
    backend_store.go     #   store 后端（JSON + 模拟租约）
    backend_uci.go uciexec.go   # uci 后端（旁车权威 + 版本无关投射）
  store/                 # meta.json 存储（品牌/系统配置/备份）
  appcfg/ eventbus/ sysinfo/ conntrack/ logtail/ selfupdate/ backup/   # 壳子
pkg/
  netutil/               # IP/掩码/CIDR/MAC/range/lease 纯函数（含单测）
  version/ sec/ util/
web/src/
  api/netcfg.ts          # 网络配置 axios 客户端 + 手写 TS 类型（snake_case 对齐后端）
  components/{MainLayout,PageCard}.tsx   # 爱快风格布局 + 页面外壳
  hooks/useNetData.ts    # 列表加载 + WS 事件自动重载
  pages/{Dhcp*,Routes,RouteTable,Dashboard,...}.tsx
scripts/                 # install.sh / install.ps1（含生成的 kmc 管理命令）
openwrt/                 # ipk 打包：init.d/UCI/kwrtmgrd-fetch + luci-app-kwrtmgrd
```

请求链路：`api/server.go` 路由 → `api/netcfg_*.go` handler → `netcfg.Service`（校验/事件）→ `Backend`（store/uci）→ 变更经 `eventbus` 推送到前端 WS → 前端 `useNetData` 自动刷新。

## 5. 常用命令（根目录 Makefile）

```bash
make build-host   # 本机平台构建（先构建前端 dist 再 go build）→ bin/kwrtmgrd
make build        # Linux/amd64 构建（发布/镜像用）
make web          # 仅构建前端 dist
make test         # go test ./...
make vet          # go vet ./...
make run          # 本机构建并以 dev token 启动（store 后端）
make ipk          # OpenWrt 单 all 架构 ipk
```

前端在 `web/` 下：`npm run dev`（vite，:5173，已代理 /api 与 WS 到 :18080）、`npm run build`（`tsc -b && vite build`）、`npm run gen:api`（由 openapi.yaml 生成 schema）。

## 6. 本地开发流程

1. 起后端：`KWRTNET_API_TOKEN=dev KWRTNET_DATA_DIR=./tmp/data KWRTNET_NETCFG_BACKEND=store ./bin/kwrtmgrd serve`（:18080，store 后端，含演示 seed 数据）。
2. 起前端：`cd web && npm run dev`（:5173，走代理）。
3. 浏览器开 `http://localhost:5173`，登录页填 token（dev）。token 存 localStorage（`kwrtnet_api_token`），axios 拦截器自动加 `Authorization: Bearer`，401 跳登录。

## 7. ⚠️ 前后端 API 字段绑定（本项目第一大坑）

**改任何 `web/src/**` 里调用 `/api/v1/...` 的代码前，先读 Go 源（`internal/netcfg/types.go` + `internal/api/netcfg_*.go`）确认字段名。**

- 新领域 JSON **一律 snake_case**（与壳子 Snapshot/system 一致），已无 frp 时代的不规则 camelCase。
- `decodeJSON`（[internal/api/helpers.go](internal/api/helpers.go)）启用 `DisallowUnknownFields()`：请求体**多发一个 key 会直接 400**。前端创建/编辑用 `Omit<T,'id'|'remaining'>` 输入类型，字段必须与后端 struct 完全一致。
- 列表返回多为 `{ "items": [...] }`（静态分配另带 `arp_bind`，路由表带 `family`）。
- 权威字段表见 [internal/api/openapi.yaml](internal/api/openapi.yaml)（也是 `/api/docs` 来源）。前端类型集中在 [web/src/api/netcfg.ts](web/src/api/netcfg.ts)。

## 8. ⚠️ uci 后端：多 OpenWrt 版本兼容（务必维持）

uci 后端**不以 UCI 为权威源**，而是：

- **旁车 `DATA_DIR/netcfg.json` 为权威**（读不解析 UCI，升级/换固件不丢字段、能存 UCI 无法表达的 per-host 网关/DNS、备注、禁用项）。
- **写只用 ≤19.07 起就有的通用原语**，**刻意不用 `option disabled`**（21.02 才有）——禁用项不投射、仅留旁车。
- **`option managed_by 'kwrt-net-manager'` 托管标记 + 具名节**：只增删自己的节，**绝不碰 stock/LuCI/运维手改配置**（升级不冲突）。
- **commit 与 reload 分阶段**（reload 不 restart），reload 失败置 pending 上报。
- 全部语义校验在 Go 层、commit 之前（uci 不校验，重复 IP/空 hostname 会让 dnsmasq 崩）。
- 命令生成由 `backend_uci_test.go` 的 fake-exec 锁定，无需真机。

新增/修改 uci 投射逻辑时，遵守以上原则；详见设计文档 `docs/superpowers/specs/2026-06-15-kwrt-net-manager-dhcp-static-route-design.md` 决策 2.1。

## 9. 配置（全部经环境变量，前缀 `KWRTNET_`）

由 [internal/appcfg](internal/appcfg) 读取：

| 变量 | 默认 | 说明 |
|---|---|---|
| `KWRTNET_API_TOKEN` | （必填） | API 鉴权令牌 |
| `KWRTNET_HTTP_ADDR` | `:18080` | 监听地址 |
| `KWRTNET_DATA_DIR` | `/data` | 数据根目录（meta.json / netcfg.json / logs） |
| `KWRTNET_NETCFG_BACKEND` | `auto` | `uci`/`store`/`auto` 网络后端 |
| `KWRTNET_CORS_ORIGINS` | `*` | CORS 白名单 |
| `KWRTNET_LOG_LEVEL` | `info` | trace/debug/info/warn/error |
| `KWRTNET_DOCS_ENABLED` | `true` | 是否开放 `/api/docs` |

OpenWrt 上由 init.d 从 UCI `/etc/config/kwrtmgrd` 转成 `KWRTNET_*` 注入；数据目录默认 `/usr/lib/kwrtmgrd`。

## 10. 提交规范

Conventional Commits + **中文描述**：`feat(dhcp): …`、`fix(route): …`、`chore(deps): …`。

## 11. 其它约束

- **Windows 开发环境**：遵循全局 `windows-shell`。含中文的 `.ps1` 必须带 UTF-8 BOM；`.cmd`/JSON/Go/TS 等不要 BOM。
- 改 `internal/api` 的请求/响应结构后，同步 `openapi.yaml`，必要时 `npm run gen:api`。
- 验证以事实为准：声称「修好了」前，后端跑 `make test`/`go vet`，前端跑 `tsc -b`，对接的再看一次真实 Network 请求。
- **真机测试**：要在真实 OpenWrt 机器上验证部署/发布，用项目内 `optest` skill（`.claude/skills/optest/`，封装对测试机的 SSH 接入与 `kwrtmgrd` 部署/health 验证）。
- 历史文档（`CHANGELOG.md`、`docs/superpowers/specs|plans/` 中 2026-06-15 之前的设计稿）记录的是旧 frpc 项目，**不要据其行事**，以本文件与新设计文档为准。
