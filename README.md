# KWRT 网络管理（kwrtmgrd）

> 面向 **OpenWrt** 的网络管理面板，仿爱快 iKuai 交互：用浏览器点鼠标就能管好 **DHCP** 与**静态路由**，告别手写 `/etc/config` + `uci` + `ip route`。

单 Go 二进制（无 cgo），前端 React/Ant Design 内嵌，自带 systemd / OpenRC / Windows 服务与 OpenWrt ipk 安装脚本。本项目由 frpc-manager 的「壳子」改造而来——保留了打包、自升级、定时备份、鉴权、品牌定制、系统监控，换上了全新的网络管理内核。

---

## ✨ 功能

**DHCP 设置**
- **DHCP 服务端**：多服务端增删改 / 启停 / 批量 / 重启；地址池起止、排除地址、子网掩码、网关、主备 DNS、租期、过期保留、检查 IP、DHCP 中继、关联接口、自定义 DHCP 选项、剩余地址。
- **DHCP 静态分配**：IP-MAC 绑定（主机名 / 网关 / 接口 / DNS / 备注），兼容 ARP 绑定开关。
- **DHCP 终端列表**：实时租约，区分静态/动态；一键「加入静态分配 / 加入 MAC 黑名单 / 固定同网段」；按接口/状态/关键字过滤。
- **DHCP 黑白名单**：黑/白名单模式切换 + MAC 条目管理。

**静态路由**
- **静态路由**：IPv4 / IPv6 路由增删改、复制、启停、批量；线路（自动/接口）、目的地址、子网掩码（点分 + CIDR）、网关、优先级、备注。
- **当前路由表**：实时内核路由表，IPv4 / IPv6 Tab，只读。

## 🧱 架构

- **可插拔后端**：
  - `uci` 后端（OpenWrt）—— 经 `uci` 读写 `/etc/config/{dhcp,network}`、读 `/tmp/dhcp.leases`、`ip route` 读路由表、`/etc/init.d/{dnsmasq,network} reload` 生效。
  - `store` 后端（开发/CI/非 OpenWrt）—— 状态持久到 `netcfg.json` + 模拟租约，全部页面在 Windows 端到端可跑。
  - 由 `KWRTNET_NETCFG_BACKEND=uci|store|auto`（默认 auto，探测到 OpenWrt 即用 uci）选择。
- **多 OpenWrt 版本兼容**：uci 后端以旁车 JSON 为权威、只用老版本就有的 UCI 原语、带 `managed_by` 托管标记只动自己的节、reload 不 restart —— 升级/换固件不破坏、不与 LuCI 冲突。
- **单二进制**：前端 `web/dist` 经 `//go:embed` 嵌入，生产同域。

## 🚀 快速开始

### Docker
```bash
docker run -d --name kwrtmgrd --network host \
  -e KWRTNET_API_TOKEN=$(openssl rand -hex 32) \
  -e KWRTNET_NETCFG_BACKEND=auto \
  -v /var/lib/kwrtmgrd:/data \
  ghcr.io/nue-mic/kwrt-net-manager:latest
```

### 二进制 / 一键脚本（Linux）
```bash
curl -fsSL https://raw.githubusercontent.com/nue-mic/kwrt-net-manager/main/scripts/install.sh | sh
# 安装后用统一管理命令：kmc start|stop|restart|status|logs -f|url|update|uninstall
```

### OpenWrt（单 all 架构 ipk）
```bash
make ipk            # 生成 luci-app-kwrtmgrd_*.ipk
opkg install luci-app-kwrtmgrd_*.ipk
# 装时由 kwrtmgrd-fetch 按 CPU 联网拉取对应二进制到 /usr/bin/kwrtmgrd
# 配置在 UCI /etc/config/kwrtmgrd，改完：uci commit kwrtmgrd; /etc/init.d/kwrtmgrd restart
```

### 本地开发（前后端分离）
```bash
make run            # 后端 :18080，store 后端，dev token，数据写 ./tmp/data
cd web && npm run dev   # 前端 :5173，已代理 /api 与 WS 到 :18080
# 浏览器开 http://localhost:5173，登录 token 填 dev
```

## ⚙️ 配置（环境变量，前缀 `KWRTNET_`）

| 变量 | 默认 | 说明 |
|---|---|---|
| `KWRTNET_API_TOKEN` | （必填） | Bearer 鉴权令牌 |
| `KWRTNET_HTTP_ADDR` | `:18080` | 监听地址 |
| `KWRTNET_DATA_DIR` | `/data` | 数据根目录（meta.json / netcfg.json / logs） |
| `KWRTNET_NETCFG_BACKEND` | `auto` | `uci` / `store` / `auto` |
| `KWRTNET_CORS_ORIGINS` | `*` | CORS 白名单 |
| `KWRTNET_LOG_LEVEL` | `info` | trace/debug/info/warn/error |
| `KWRTNET_DOCS_ENABLED` | `true` | 是否开放 `/api/docs` |

OpenWrt 上经 init.d 从 UCI `/etc/config/kwrtmgrd` 转成 `KWRTNET_*` 注入。

## 📖 API

- 鉴权：`Authorization: Bearer <token>`。
- 交互式文档：`/api/docs`（Scalar UI），契约见 [internal/api/openapi.yaml](internal/api/openapi.yaml)。
- 实时事件：WebSocket `/api/v1/events`（dhcp/static/lease/acl/route 变更推送）。
- 主要分组：`/api/v1/dhcp/{servers,statics,leases,acl}`、`/api/v1/routes`、`/api/v1/route-table`、`/api/v1/interfaces`、`/api/v1/netcfg/status`。

## 🛠 构建

```bash
make build-host   # 本机平台 → bin/kwrtmgrd（先构建前端 dist）
make build        # Linux/amd64
make test         # go test ./...
make vet          # go vet ./...
```

## 📄 许可证

GPL-3.0，见 [LICENSE](LICENSE)。
