# KWRT 网络管理器 — DHCP 与静态路由（仿爱快）设计文档

> 状态：待用户回归审阅（用户已授权「按推荐方案全权开发并测试」，故在本文写定后直接进入实现；用户回来后据此审阅）。
> 日期：2026-06-15
> 范围：在保留 frpc-manager 全部「壳子」（OpenWrt 打包 / ipk / 自升级 / 备份 / 鉴权 / 品牌 / 系统监控）的前提下，移除 frpc 核心，新增**仿爱快**的 DHCP 管理与静态路由管理整套 Web 功能。

---

## 1. 背景与目标

本仓库由 `frpc-manager` 整仓拷贝而来。原产品是「无头 FRP 客户端管理器」：Go 守护进程 `frpcmgrd` 内嵌 `fatedier/frp`，对外暴露 HTTP API + WebSocket，前端 React/AntD 单页，单二进制交付，自带 OpenWrt/systemd 安装与自升级。

目标：
1. **保留壳子**：OpenWrt ipk 打包（all 架构 + `frpcmgrd-fetch` 按 CPU 拉二进制）、procd 服务脚本、UCI→env 注入、自升级、定时备份、Bearer 鉴权、品牌定制、系统监控、主题、事件流。
2. **移除 frpc 核心**：内嵌 frp、实例生命周期、TOML 配置模型、代理/访问者、NAT 探测、TOML 参考等。
3. **新增仿爱快功能**（本期范围）：
   - **DHCP 设置**：DHCP 服务端（增删改启停 + 多服务端）、DHCP 静态分配、DHCP 终端列表、DHCP 黑白名单。
   - **静态路由**：静态路由（增删改/复制/启停）、当前路由表（IPv4/IPv6）。
4. 这些功能在 OpenWrt 上**真实落地**（dnsmasq + UCI + `ip`），同时在非 OpenWrt（Windows/Mac 开发机、CI）上以**模拟后端**完整可跑、可测、可演示。

爱快 UI 截图见 `docs/爱快系统截图/`，是布局与字段的权威来源（已逐张核对）。

---

## 2. 关键设计决策（含推荐理由）

### 决策 1：保留内部名 `frpcmgrd` / `FRPCMGR_*` / 模块路径，仅在 UI 层改名（强烈推荐）
OpenWrt 壳子深度绑定了 `frpcmgrd`（二进制名、init.d、procd）、`FRPCMGR_*`（UCI→env）、`luci-app-frpcmgrd`（包名）、模块路径 `github.com/mia-clark/frpc-manager`、以及 `frpcmgrd-fetch` 指向特定 GitHub Release 的下载 URL。**全量改名牵动 ~50 文件 + 发布管线 + 下载地址，风险极高，且不是交付本期功能所必需。**
- 做法：内部名一律不动；通过**已有的 branding 系统**（运营可改的 app_name / app_subtitle / html_title）把默认品牌改为「KWRT 网络管理」/「DHCP · 静态路由」。这是零风险改名。
- 影响：UI 顶栏/标题显示新名；二进制、包、env、URL 保持兼容。用户若需彻底改名，列为后续独立任务。

### 决策 2：Provider 抽象 + 双后端（uci / store），自动探测（推荐）
后端不直接耦合 OpenWrt，而是定义 `netcfg.Backend` 接口，两套实现：
- **uci 后端**：在 OpenWrt 上读写 `/etc/config/dhcp`、`/etc/config/network`（经 `uci` 命令），读 `/tmp/dhcp.leases`，调 `ip route` 读实时路由表，`/etc/init.d/{dnsmasq,network} reload` 生效。
- **store 后端**：在开发机/CI/非 OpenWrt 上把状态持久化到 `DATA_DIR/netcfg.json`，并**模拟**租约/路由表，使全部页面与操作在 Windows 上即可端到端跑通与测试。
- **探测**：存在 `uci` 可执行 + `/etc/config/dhcp` + `/etc/config/network` ⇒ uci 后端；否则 store。可用 `FRPCMGR_NETCFG_BACKEND=uci|store|auto` 强制。
理由：开发/测试与生产解耦；用户「离开期间全套开发并测试」必须能在 Windows 上验证；OpenWrt 真机后续可直接切 uci 后端。

### 决策 2.1：uci 后端「旁车权威 + 版本无关投射」（应用户要求：多 OpenWrt 版本兼容、避免升级出问题）
用户明确要求：操作 OpenWrt 时要兼容多版本、避免后续升级出问题。为此 uci 后端不采用「直接读写 UCI 当唯一状态」的常规做法，而是：
1. **旁车 JSON 为权威源**：托管状态以 `DATA_DIR/netcfg.json` 持久（与 store 后端同一份持久层，uci 后端通过组合 `*storeBackend` 复用）。**读路径完全不解析 UCI** —— 因此不依赖任何特定版本的 UCI 字段语义，升级/换固件不丢字段，也能保存 UCI 无法表达的字段（每条保留的网关/DNS、备注、禁用项）。
2. **写路径只用「自古就有」的通用原语**（≤19.07 即存在）：`config dhcp/host/route/route6` + `interface/start/limit/leasetime/dhcp_option/mac/ip/name/ignore/target/netmask/gateway/metric`。**刻意不用** `option disabled`（21.02 才有，老版本要把节类型改成 `disabled_route`）—— 禁用项直接「不投射」（仍存于旁车），从根上规避版本分叉。
3. **托管标记 + 具名节**：每个我们生成的节带 `option managed_by 'kwrt-net-manager'`；apply 时先 `uci show` 找出带标记的节删除、再重建，**只增删自己标记的节，绝不触碰 stock / LuCI / 运维手改的配置** —— 这是「升级不冲突」的关键护栏。
4. **commit 与 reload 分阶段**：`uci batch`(含 `commit dhcp/network`) 落盘后，再 `/etc/init.d/{dnsmasq,network} reload`（用 reload 不用 restart，避免断网/断 SSH）；reload 失败置 `pending`（已保存未生效）并在状态里如实上报。
5. **优雅降级**：dnsmasq/版本无法安全表达的高级特性（同接口多池、whitelist、per-host 网关/DNS via tag、ARP 静态绑定）在 uci 后端**不强行投射**（保留在旁车、记日志），store 后端则完整支持，UI 标注差异。宁可少做、不可做坏。
6. **单测以 fake-exec 验证生成的命令**：`backend_uci_test.go` 断言生成的 `uci batch` 文本（含标记、跳过禁用项、只删标记节、reload 失败→pending、lease/route 解析），无需真机即可保证「生成的命令正确且版本安全」。

> 实测覆盖：托管节标记删除 / 禁用项不渲染 / start-limit 换算 / dhcp_option 3,6 / 路由 route(6) / lease 文件解析与静态标注 / `ip route` 解析 / reload 失败置 pending。真机联调留作后续，但生成逻辑已被单测锁定。

### 决策 3：新领域 JSON 一律 snake_case（推荐）
原项目踩坑点是 frp 遗留的不规则 camelCase。新领域无历史包袱，与保留下来的壳子（Snapshot/system/branding/事件 均 snake_case）保持一致，**全部用 snake_case**。`decodeJSON` 仍启用 `DisallowUnknownFields()`，前端字段必须精确匹配 —— 字段表在本文第 5/6 节给全，前端开发遵循 `web-api-binding` 技能逐字段对核。

### 决策 4：抽出 `internal/store`，删除 `internal/manager`
`manager.Manager` 把「frpc 实例生命周期」与「meta.json 存储（品牌/系统配置/备份/排序）」揉在一起。后者是壳子、被 backup/ui/syscfg 复用。方案：把 meta 存储抽成独立的 `internal/store` 包（实现 `backup.Store`/`Recorder`、品牌、系统配置、ImportMeta、BuildBackupZip），整体删除 `internal/manager` 与 `services/`。

### 决策 5：范围聚焦 DHCP + 静态路由，其余爱快菜单项置灰/不做（推荐 YAGNI）
爱快左栏还有 DNS 设置、终端分组、VLAN、VPN、UPnP、NAT、端口映射、IPv6、IGMP、IPTV 等。本期只做用户明确要求的 **DHCP（4 子页）+ 静态路由（2 子页）**。导航可保留分组骨架但只点亮这两组，避免范围蔓延。

---

## 3. 范围

### 3.1 本期交付页面（仿爱快，逐张截图核对）
| 菜单 | 页面 | 类型 |
|---|---|---|
| DHCP 设置 | DHCP 服务端（列表 + 新增/编辑弹层 + 重启/启停/删除/导入导出） | CRUD |
| DHCP 设置 | DHCP 静态分配（列表 + 新增/编辑 + 启停/删除/导入导出 + 兼容ARP开关） | CRUD |
| DHCP 设置 | DHCP 终端列表（只读租约 + 加入静态分配/加入黑名单/一键固定同网段 + 过滤） | 只读 + 动作 |
| DHCP 设置 | DHCP 黑白名单（黑/白名单模式 + MAC 条目 CRUD） | CRUD |
| 静态路由 | 静态路由（列表 + 新增/编辑/复制 + 启停/删除/导入导出） | CRUD |
| 静态路由 | 当前路由表（只读，IPv4/IPv6 Tab） | 只读 |

### 3.2 不在本期范围
DNS 设置、终端分组、VLAN、VPN 客户端、UPnP、NAT 规则、端口映射、IPv6、IGMP 代理、IPTV、内外网设置。导航保留壳子（仪表盘、系统监控、定时备份、设置、关于、登录）。

---

## 4. 架构总览

```
浏览器 (React/AntD, 爱快风格布局)
   │  axios (Bearer)  +  WS /api/v1/events
   ▼
chi Router (internal/api/server.go)         ← 壳子，重接线
   ├─ 中间件: Recover / AccessLog / CORS / Bearer   ← 壳子保留
   ├─ 壳子路由: health, version, ui/branding, system/*, system/config, system/update, backup/*, events, export/all, import/zip
   └─ 新领域路由: dhcp/*, routes/*, route-table, interfaces, netcfg/status
   ▼
internal/netcfg.Service  (校验 / id 生成 / 事件发布 / 业务逻辑)
   ├─ Backend = ucibackend   → uci / ip / /tmp/dhcp.leases / init.d   (OpenWrt)
   └─ Backend = storebackend → DATA_DIR/netcfg.json + 租约模拟        (开发/CI/非 OpenWrt)
   │
   └─ 变更 → eventbus → WS 推送 → 前端按需刷新
internal/store  (meta.json: 品牌/系统配置/备份/BuildBackupZip)   ← 由 manager 抽出
```

请求链路：前端 → `api/<name>.go` handler → `netcfg.Service` → `Backend` 读写 → 经 `eventbus` 推送变更 → 前端刷新。

---

## 5. 后端设计

### 5.1 包结构（最终态）
```
cmd/frpcmgrd/main.go        # 重接线：去掉 manager，构建 store + netcfg
internal/
  api/                      # 重接线：删 frpc handler，加 netcfg handler
    server.go               # 路由表更新
    dhcp.go                 # DHCP 服务端
    dhcpstatic.go           # 静态分配
    dhcpleases.go           # 终端列表 + 动作
    dhcpacl.go              # 黑白名单
    routes.go               # 静态路由
    routetable.go           # 当前路由表
    netcfg_common.go        # 接口列表 / 状态 / 共享 DTO
    ui.go syscfg.go update.go system.go events.go docs.go backup.go  # 壳子，改依赖 store
    helpers.go errors.go apiresp/ middleware/                         # 壳子保留
  store/                    # 新：meta.json 存储（品牌/系统配置/备份/排序/ImportMeta/BuildBackupZip）
  netcfg/                   # 新：领域核心
    types.go                # DHCPServer/StaticLease/Lease/ACL/Route/RouteEntry/Interface/Status
    service.go              # Service（校验+事件+业务）
    backend.go              # Backend 接口 + AutoDetect
    uci/                    # ucibackend：uci/ip/lease 文件
    store/                  # storebackend：netcfg.json + 租约模拟
    validate.go             # IP/MAC/CIDR/range 校验（可复用 pkg/util）
  appcfg/                   # +NetcfgBackend 字段（env FRPCMGR_NETCFG_BACKEND）
  backup/ eventbus/ sysinfo/ conntrack/ logtail/ selfupdate/         # 壳子保留
pkg/
  version/                  # 去掉 frp 版本依赖
  netutil/                  # 新：IP/掩码/CIDR 互转、range↔count、lease 解析（含单测）
  sec/ util/                # 壳子保留
  consts/                   # 精简（去 frpc state）
```

### 5.2 `netcfg.Backend` 接口（核心）
```go
type Backend interface {
    Kind() string // "uci" | "store"

    // 接口/线路下拉
    ListInterfaces() ([]Interface, error)

    // DHCP 服务端
    ListDHCPServers() ([]DHCPServer, error)
    PutDHCPServers([]DHCPServer) error   // 全量落盘（Service 负责增改后整体写）
    RestartDHCP() error

    // 静态分配
    ListStatics() ([]StaticLease, error)
    PutStatics([]StaticLease, arpBind bool) error
    ARPBind() bool

    // 终端列表（只读，uci 读 lease 文件 / store 模拟）
    ListLeases() ([]Lease, error)

    // 黑白名单
    GetACL() (ACL, error)
    PutACL(ACL) error

    // 静态路由
    ListRoutes() ([]Route, error)
    PutRoutes([]Route) error

    // 当前路由表（只读）
    RouteTable(family string) ([]RouteEntry, error)
}
```
`Service` 持有 `Backend` + `Bus` + `Logger`，对外提供带校验/id 生成/事件发布的 CRUD；写操作走「读全量 → 改 → `Put*` 全量落盘」以简化与 UCI 命名节的对齐。并发用 `sync.Mutex` 串行化写。

### 5.3 数据模型（snake_case JSON；← 截图字段对应）

**DHCPServer**（DHCP 服务端 / 一个 dnsmasq 池）
```jsonc
{
  "id": "lan1",                 // 节名（= interface）
  "interface": "lan1",          // 服务接口
  "enabled": true,              // 状态（启用/停用）
  "ip_start": "192.168.1.31",   // 客户端地址-起
  "ip_end": "192.168.31.254",   // 客户端地址-止
  "netmask": "255.255.224.0",   // 子网掩码
  "gateway": "192.168.1.1",     // 网关
  "dns_primary": "223.5.5.5",   // 首选 DNS
  "dns_secondary": "114.114.114.114", // 备选 DNS
  "lease_minutes": 120,         // 租期（分钟）
  "exclude": ["192.168.1.1","192.168.1.10-192.168.1.20"], // 排除地址（每行一条）
  "expired_keep_hours": 0,      // 过期地址保留时间（小时）
  "check_ip": true,             // 检查接口 IP 有效性
  "relay_only": false,          // 只应用于 DHCP 中继
  "assoc_interface": "all",     // 关联接口（默认全部线路）
  "custom_options": [ {"code": 42, "value": "192.168.1.1"} ], // 自定义 DHCP 选项
  "remaining": 7853             // 剩余地址（只读，计算）
}
```
UCI 映射：`config dhcp '<id>'` → `interface/start(由 ip_start 算偏移)/limit(由 ip_end-ip_start)/leasetime(<min>m)/ignore(!enabled)`；`list dhcp_option '3,<gateway>'`、`list dhcp_option '6,<dns1>,<dns2>'`；`expired_keep_hours/check_ip/relay_only/custom_options` 通过 `dhcp_option`/附加项或本地元数据落地（见第 7 节差异）。

**StaticLease**（DHCP 静态分配 / 保留）
```jsonc
{
  "id": "h_xxx", "hostname": "MacMini",
  "ip": "192.168.1.220", "mac": "14:9B:77:68:C5:1C",
  "gateway": "192.168.1.5", "interface": "lan1",
  "dns_primary": "192.168.1.5", "dns_secondary": "192.168.1.5",
  "remark": "苹果台式机", "enabled": true
}
```
UCI：`config host` → `name/mac/ip`（直接）；`gateway/dns/interface` 经 dnsmasq tag（`set:` + `dhcp_option`）尽力落地，MVP 至少保证 name/mac/ip 生效。全局 `arp_bind`（兼容 ARP 绑定）单独存。

**Lease**（DHCP 终端列表，只读）
```jsonc
{ "hostname":"iPhone","ip":"192.168.1.80","mac":"BE:9C:5A:C2:07:86",
  "expiry":1718000000,"remaining_seconds":6473,"interface":"lan1",
  "static":true,"remark":"IPhone12-蓝容-5G" }
```
来源：uci 读 `/tmp/dhcp.leases`（`<expiry> <mac> <ip> <hostname|*> <clientid|*>`），`static` 由是否命中保留判定，`interface` 由网段匹配 DHCP 服务端推断；store 后端按已配置 server/保留模拟生成。

**ACL**（DHCP 黑白名单）
```jsonc
{ "mode":"blacklist", "entries":[ {"id":"a_x","mac":"AA:BB:..","remark":"","enabled":true} ] }
```
UCI：blacklist → 每 MAC `config host` + `option ignore '1'`；whitelist → tag 方案（仅白名单可获 DHCP，记差异）。

**Route**（静态路由）
```jsonc
{ "id":"r_x","family":"ipv4","interface":"auto","target":"192.168.148.0",
  "netmask":"255.255.255.0","prefix":24,"gateway":"192.168.1.222",
  "metric":1,"remark":"【汉土】公网IP盒子-路由","enabled":true }
```
UCI：`config route`/`config route6` → `interface(auto→留空或主 lan)/target/netmask/gateway/metric/disabled(!enabled)`。`metric` = linux 路由 metric（越小优先级越高，与爱快「优先级」一致）。

**RouteEntry**（当前路由表，只读）
```jsonc
{ "interface":"lan1","target":"10.10.10.0","netmask":"255.255.255.0","gateway":"192.168.1.2","metric":1 }
```
来源：`ip route show` / `ip -6 route show` 解析；store 后端由静态路由 + 合成默认路由模拟。

**Interface / Status**
```jsonc
{ "name":"lan1","ipv4":"192.168.1.1","netmask":"255.255.255.0","up":true }
{ "backend":"store","dhcp_ok":true,"pending":false }
```

### 5.4 API 路由表（authenticated 子树）
```
# DHCP 服务端
GET    /api/v1/dhcp/servers
POST   /api/v1/dhcp/servers
GET    /api/v1/dhcp/servers/{id}
PUT    /api/v1/dhcp/servers/{id}
DELETE /api/v1/dhcp/servers/{id}
POST   /api/v1/dhcp/servers/{id}/toggle        {enabled:bool}
POST   /api/v1/dhcp/servers/batch              {action:enable|disable|delete, ids:[]}
POST   /api/v1/dhcp/restart
GET    /api/v1/dhcp/servers/export             → JSON 下载
POST   /api/v1/dhcp/servers/import             ← JSON 上传（合并/覆盖）
# 静态分配
GET    /api/v1/dhcp/statics
POST   /api/v1/dhcp/statics
PUT    /api/v1/dhcp/statics/{id}
DELETE /api/v1/dhcp/statics/{id}
POST   /api/v1/dhcp/statics/{id}/toggle
POST   /api/v1/dhcp/statics/batch
PUT    /api/v1/dhcp/statics/arp-bind           {enabled:bool}
GET    /api/v1/dhcp/statics/export  /  POST .../import
# 终端列表
GET    /api/v1/dhcp/leases?interface=&status=&q=
POST   /api/v1/dhcp/leases/reserve             {ip,mac,hostname,interface}  加入静态分配
POST   /api/v1/dhcp/leases/blacklist           {mac,remark}                 加入黑名单
POST   /api/v1/dhcp/leases/fix-subnet          {interface}                  一键固定同网段
# 黑白名单
GET    /api/v1/dhcp/acl
PUT    /api/v1/dhcp/acl/mode                    {mode:blacklist|whitelist}
POST   /api/v1/dhcp/acl/entries
PUT    /api/v1/dhcp/acl/entries/{id}
DELETE /api/v1/dhcp/acl/entries/{id}
POST   /api/v1/dhcp/acl/entries/{id}/toggle
# 静态路由
GET    /api/v1/routes
POST   /api/v1/routes
GET    /api/v1/routes/{id}
PUT    /api/v1/routes/{id}
DELETE /api/v1/routes/{id}
POST   /api/v1/routes/{id}/toggle
POST   /api/v1/routes/{id}/duplicate
POST   /api/v1/routes/batch
GET    /api/v1/routes/export  /  POST /api/v1/routes/import
# 当前路由表 / 下拉 / 状态
GET    /api/v1/route-table?family=ipv4|ipv6
GET    /api/v1/interfaces
GET    /api/v1/netcfg/status
```
错误约定沿用壳子 `apiresp` / `WriteError(code,msg)`；404/400/409 语义与现状一致。

### 5.5 事件
`eventbus` 新增类型：`dhcp_changed`、`route_changed`、`lease_changed`（节流）。前端事件流命中后刷新对应列表。复用现有 WS `/api/v1/events`。

---

## 6. 前端设计（仿爱快）

### 6.1 布局骨架（对照截图）
- **顶栏（深蓝）**：左侧产品名/构建号；右侧 CPU/内存/上行/下行 速率（复用 `system/*` 接口轮询）+ 主题切换 + 登出。爱快是横向深蓝顶条，本项目现有顶栏是浅色——改造 `MainLayout` 顶栏为爱快深蓝风。
- **左栏（深色，分组可折叠）**：`网络设置` 大标题下：`DHCP 设置`（DHCP 服务端 / DHCP 静态分配 / DHCP 终端列表 / DHCP 黑白名单）、`静态路由`（静态路由 / 当前路由表）。其余爱快项不放（YAGNI）。底部放壳子项：系统监控 / 定时备份 / 设置 / 关于。
- **内容区**：白色卡片，顶部面包屑（`网络设置 > DHCP设置 > DHCP服务端`）+ 标题；工具条（绿色`添加`、`导入/导出`、`启用/停用/删除`、必要时`重启DHCP服务`）；AntD `Table`（爱快灰底表头、行内`操作`链接：编辑/复制/停用/删除）；底部分页（共 N 条 / 每页 20 / 页码 / 跳转）。
- **编辑表单**：抽屉或弹层，label 左、输入右、必填 `*`、底部`保存`/`取消`，与截图一致。

### 6.2 路由与页面
```
/dhcp/servers   DhcpServers.tsx     列表 + Drawer 表单
/dhcp/statics   DhcpStatics.tsx     列表 + Drawer 表单 + ARP 开关
/dhcp/leases    DhcpLeases.tsx      只读表 + 行动作 + 过滤
/dhcp/acl       DhcpAcl.tsx         模式切换 + 条目 CRUD
/routes         Routes.tsx          列表 + Drawer 表单（含复制）
/route-table    RouteTable.tsx      IPv4/IPv6 Tab 只读表
```
公共组件：`PageCard`（面包屑+标题+工具条卡片）、`DataTable`（爱快风格表格 + 分页 + 行选）、`netcfg api` 客户端（`web/src/api/netcfg.ts`，手写类型与后端 snake_case 逐字段对齐）。

### 6.3 删除/保留的前端
- 删：`Configs.tsx`、`Logs.tsx`、`ImportExport.tsx`(frpc)、`Dashboard.tsx`(frpc)、`ToolsNat.tsx`、`ToolsValidate.tsx`、`TomlReference.tsx`、`tomlSnippets.ts`、相关 `api/types.ts` 中 frpc 类型。
- 留：`Login`、`System`、`Backup`、`Settings`、`About`、`branding/`、`theme/`、`events/`、`api/client.ts`、`UpdateCard`。`MainLayout` 改造为爱快风格。
- 新仪表盘：简版「网络概览」（DHCP 服务端数/活动租约数/路由条数 + 系统指标），替换 frpc 仪表盘。

### 6.4 字段绑定纪律
所有调用 `/api/v1/...` 的前端代码遵循 `web-api-binding` 技能：先读 Go 源确认 snake_case 字段，再写绑定；`decodeJSON` 的 `DisallowUnknownFields()` 会让多发 key 直接 400，故 PUT/POST body 必须与后端 struct 完全一致。

---

## 7. OpenWrt 映射与差异（实现须知；调研工作流补强）

| 爱快能力 | OpenWrt 机制 | 可行性 | 备注 |
|---|---|---|---|
| 多个 DHCP 服务端/接口 | dnsmasq 每接口原生**仅一个**主池 | partial | 单池直接；多池需别名接口或 tagged dhcp-range。本期：每接口一服务端为主，多服务端在 store 后端完整、uci 后端以「附加 dhcp-range + tag」尽力实现并记录限制。 |
| 客户端地址 起-止 | `config dhcp` 的 `start`+`limit`（偏移+数量） | direct | end−start+1=limit；需接口子网换算。 |
| 排除地址 | dnsmasq 无「池内排除」原语 | workaround | 拆分 range / 或对排除 IP 建 ignore host；store 后端精确模拟。 |
| 网关/DNS 下发 | `list dhcp_option '3,<gw>'` / `'6,<dns..>'` | direct | |
| 租期（分钟） | `option leasetime '<n>m'` | direct | |
| 过期地址保留时间 | dnsmasq 无直接项 | partial | 作为元数据保存；行为差异记录。 |
| 检查接口IP有效性 / 只应用于中继 | `option ignore` / relay 配置 | partial | relay 走 `config relay`/dnsmasq relay；本期表单可填，uci 尽力。 |
| 自定义 DHCP 选项 | `list dhcp_option '<code>,<val>'` | direct | |
| 静态分配 name/mac/ip | `config host` | direct | |
| 静态分配 每条 网关/DNS | dnsmasq 需 tag + dhcp_option | partial | tag 方案尽力；MVP 保证 name/mac/ip。 |
| 兼容 ARP 绑定 | `ip neigh`/`arp -s` 静态 ARP | workaround | 全局开关；uci 后端写静态邻居，store 仅存。 |
| 终端列表 | `/tmp/dhcp.leases`（`expiry mac ip host clientid`） | direct | expiry=0 视为无限/静态。 |
| 加入黑名单 | per-mac `config host` + `option ignore '1'` | direct | |
| 黑/白名单模式 | blacklist 直接；whitelist 需 tag/`dhcp-ignore` | partial | whitelist 记差异。 |
| 一键固定同网段 | 批量建 `config host`（含动态租约 IP↔MAC） | direct | |
| 静态路由 | `config route`/`config route6`（interface/target/netmask/gateway/metric/disabled） | direct | netifd 应用，ifup 重放。 |
| 线路=自动 | route 不绑具体 dev（留空/主 lan） | direct | |
| 优先级 | linux route metric（越小越优先） | direct | 与爱快一致。 |
| 当前路由表 | `ip route show` / `ip -6 route show` | direct | 解析 dev/dst/mask/gw/metric。 |

> 注：上表的高风险条目（多池、per-host 选项、黑白名单、metric 语义、lease 文件格式）由后台调研工作流 `wel3wnh3a` 对抗校验。结论见 7.1。

### 7.1 调研结论（已核对，2026-06-15）
后台调研工作流（12 agent / 含对抗校验）确认并校正了以下实现要点，已据此定稿 uci 后端策略：

1. **「多池」是 UCI 限制而非 dnsmasq 限制**：dnsmasq 本身允许同接口多 `dhcp-range`（man：「always allowed to have more than one --dhcp-range in a single subnet」，按子网/ tag 选池）。但 OpenWrt `config dhcp` 一段只表达一个接口一个 start/limit 池。爱快「LAN 主 IP 池 + 扩展 IP 池」⇒ 本期：**store 后端完整支持多服务端；uci 后端每接口主池用 `config dhcp`，附加池以 tagged dhcp-range（写 dnsmasq.conf 片段）尽力实现并 UI 标注**。
2. **leasetime 是带单位字符串**（`120m` / `2h` / `infinite`），非纯分钟数；爱快默认 120 分钟。需换算 `lease_minutes → "<n>m"`。
3. **start/limit 是「末段偏移 + 数量」**，非绝对起止 IP。需用接口基址换算 `ip_start/ip_end ↔ start/limit`（`pkg/netutil` 提供）。
4. **每条保留的 网关/DNS 必须走 tag 间接**（确认）：`config host` 无 gateway/per-host dns-server 字段；要下发须 `option tag '<t>'` + `config tag` 里 `list dhcp_option '3,<gw>'`/`'6,<dns>'`。MVP 保证 name/mac/ip 直写，gw/dns 以 tag 尽力。
5. **托管标记（关键安全护栏）**：uci 后端只操作**具名节 + `option managed_by 'kwrt-net-manager'`** 的段；枚举/改/删只碰带标记者，绝不 `uci delete @host[i]` 盲删，避免覆盖 LuCI/运维手改配置。
6. **commit ≠ reload（两阶段）**：`uci commit dhcp|network` 只落盘，必须再 `/etc/init.d/{dnsmasq,network} reload`（用 reload 不用 restart，避免断网/断 SSH）。`Backend.Apply()` 区分 committed / reloaded，commit 成功但 reload 失败=「已保存未生效（pending）」，须如实上报。
7. **校验必须在 Go 层、commit 之前**：uci 不做语义校验——重复 IP、空 hostname 会让 dnsmasq reload 时崩。`netcfg.Service` 负责 MAC 格式 / IP 唯一 / 空 hostname 省略 name / CIDR 掩码合法 等全部校验；失败 `uci revert`。
8. **`config host` 的 `name` 在 hostname 为空时必须整项省略**（设 `name=''` 或 `'-'` 会让 dnsmasq 解析失败）。
9. **租约读取**：优先 `ubus call dhcp ipv4leases`（结构化），回退读 `uci get dhcp.@dnsmasq[0].leasefile` 指向的 lease 文件（`<expiry> <mac> <ip> <host|*> <clientid|*>`，`*`=无主机名）。
10. **路由禁用**：21.02+ 用 `option disabled '1'`（首选）；极老版本改节类型为 `disabled_route`。读取实时表优先 `ip -j route show` / `ip -6 -j route show`（JSON），回退解析 `/proc/net/route`（hex 小端、逐字节倒序；IP 小端但端口大端，勿统一倒序）。`config route` 的 `interface` 是逻辑接口名（lan/wan），`ip route` 的 `dev` 是内核网卡名（br-lan），展示需映射。
11. **并发**：Go `sync.Mutex` 串行化本进程 uci 批处理；生产可叠加 `flock /var/lock/...` 与 LuCI/CLI 安全竞争。每次取锁后重读再改。
12. **「线路=自动」**：route 不下发 `interface` 字段、仅给 `gateway`，由内核按网关选出口（netifd 接受空 interface 作全局路由）。
13. 选型确认：**优先 exec `uci batch` + init.d reload**（与 OpenWrt 自身工具/锁一致），`digineo/go-uci` 只能读写文件不能 reload，仅作参考不作默认。

---

## 8. frpc 核心移除清单

**删除目录/文件**：`services/`、`internal/manager/`、`pkg/config/`、`internal/api/{configs,proxies,lifecycle,status,nathole,validate,configdto,importexport(frpc 部分),logs,logs_test,docs(保留),errors(保留)}` 中 frpc 专属者、前端 frpc 页面（见 6.3）。
**go.mod**：移除 `github.com/fatedier/frp`、`golib`、以及随之不再被引用的传递依赖（`go mod tidy` 收敛 quic-go/wireguard/kcp/pion/gmsm 等）。
**version.go**：去掉 `fatedier/frp/.../version` 导入与 `FRPVersion`（或置静态「core」串）。
**保留并改依赖**：`backup`、`ui`、`syscfg`、`update`、`system`、`events`、`docs` 改为依赖 `internal/store`；`export/all`+`import/zip` 改为打包 netcfg 状态 + meta.json。

---

## 9. 测试策略

- **pkg/netutil**：IP↔uint32、掩码↔CIDR、range↔count、排除区间拆分、lease 行解析 —— 表驱动单测（纯函数，先 TDD）。
- **internal/netcfg/store**：CRUD + 校验 + 持久化 round-trip + 租约模拟确定性（注入种子）——单测。
- **internal/netcfg/uci**：以「命令执行器」接口注入 fake（记录 uci/ip 调用、回放假输出），断言生成的 uci 批处理与解析逻辑——单测（不需真 OpenWrt）。
- **internal/api**：用 store 后端起 `httptest`，跑每条路由的正常/错误/`DisallowUnknownFields` 用例——单测。
- **前端**：`tsc -b` 通过；关键页用真实后端（store）跑一遍 Network 核对字段。
- **端到端**：`make run`（store 后端）起 daemon，脚本化 curl 全部接口 + 浏览器手测六页；`go test ./...`、`go vet ./...`、`web` 下 `npm run build`/`tsc -b` 全绿方可声称完成（遵循壳子 CLAUDE.md 验证纪律）。

## 10. 风险与回退
- **uci 后端真机未验证**：无 OpenWrt 真机时，uci 后端靠 fake-exec 单测保证「生成的命令正确」，真机联调留作后续；store 后端确保产品在任何环境可用。降级清晰（探测不到 uci 自动 store）。
- **多池/whitelist/per-host 选项的 dnsmasq 限制**：以「store 后端完整 + uci 后端尽力 + UI 标注」三段式交付，差异在 UI/文档显式说明，不静默。
- **改名诱惑**：坚持决策 1，避免壳子断裂。
- **范围蔓延**：坚持决策 5，仅 DHCP+静态路由。

---

## 附：执行阶段（对应 Todo）
1. Phase1 移除 frpc 核心 + 抽 `internal/store` + 重接线至 `go build`/`go vet` 全绿（骨架可跑）。
2. Phase2 `pkg/netutil` + `internal/netcfg`（types/service/backend + store + uci）+ 单测。
3. Phase3 API handler + `openapi.yaml` + `docs/API` 更新。
4. Phase4 前端爱快布局 + 六页 + netcfg api 客户端（web-api-binding 对核）。
5. Phase5 构建嵌入、起 daemon 端到端验证、全量 test/vet/tsc 绿。
