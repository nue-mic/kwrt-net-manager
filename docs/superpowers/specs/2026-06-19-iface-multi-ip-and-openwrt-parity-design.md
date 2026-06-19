# 内外网设置：网卡多 IP + OpenWrt 接口全量对齐 设计文档（v2）

> 日期：2026-06-19　状态：待评审　作者：Claude（与 @rthink 头脑风暴 + 对抗式复核产出）
> v2 变更：依据对抗式复核，**砍掉 alias(`@父接口`) 方案**（OpenWrt 已知限制无法可靠发 DHCP，且与"附加 IP"职责重叠）、修正防火墙 zone 定位/生效、明确 clone_mac 写法、校验分层、`*bool` 深拷贝与 TS↔Go 契约、掩码统一 CIDR。
> 关联现状：`internal/netcfg/{iface_types.go,iface_uci.go,iface.go,iface_store.go,types.go,backend.go,backend_uci.go,dns_uci.go}`、`internal/api/{netcfg_iface.go,helpers.go,openapi.yaml}`、`pkg/netutil/netutil.go`、`web/src/{api/netcfg.ts,pages/NetOverview.tsx}`
> 前置设计：`2026-06-15-...-design.md`（uci 兼容原则）、`2026-06-16-dns-full-design.md`（firewall redirect 既有用法）

---

## 1. 背景与目标

「内外网设置」(`NetIface`) 已能配 LAN/WAN 的角色/协议/绑卡组桥/网关/DNS/MTU/PPPoE/备注，但**一个接口只能配一个 IP**（`IPAddr/Netmask` 标量）。同时网卡列表 (`NIC.IPAddrs[]`) 已能**展示**多地址，形成「看得到、改不了」割裂。

目标（按用户确认 + 复核收敛）：

1. **网卡多 IP**：一个接口 = 主 IP + 多个附加 IP，落地 OpenWrt 原生 `list ipaddr`（CIDR）。
2. **接口字段全量对齐 OpenWrt**：补 `metric/peerdns/broadcast/force_link/auto` 及 IPv6 **单值** PD 字段（`ip6assign/ip6hint/ip6addr/ip6gw`）。
3. **额外子网可参与 DHCP（本期）**：经「新建独立内网 + DHCP 页（一键预填）」实现，可靠且主流（§4.4）。
4. **修复既有 bug**：`clone_mac` 当前**读取但从不写入**（`iface_uci.go:226` 读 `macaddr`，`SaveNetIface` 无写回），UI 填了不生效——本期修复。

### 1.1 三条硬约束（用户明确，贯穿全文）

- **市面最流行形态**：主 IP + 附加 IP 列表（iKuai/LuCI/pfSense 共识）；一个内网口 = 一个 DHCP 作用域。
- **防呆防自锁**：网络面板最大风险是把自己关门外（改正在连的管理 IP、删唯一 LAN、拆承载管理连接的网桥、新建口忘进防火墙区导致"配好却不通"）。安全护栏列为一等公民（§6）。
- **简单易懂、不复杂**：合理默认 + 高级折叠，普通用户**只填主 IP 即可用**；复杂能力默认隐藏、显式开启；宁可砍功能也不留"魔法式联动"。

---

## 2. 范围

### 2.1 本期做

- `NetIface` 增加附加 IP 列表（IPv4 CIDR）+ 全量接口字段（含 IPv6 单值 PD 字段）。
- uci 后端：主+附加 IP 统一 `list ipaddr` 投射；全量字段投射；**修复 clone_mac 写入**；**新建独立接口自动并入对应防火墙 zone**（核心防呆 G1）。
- store 后端：附加 IP + 全量字段持久化（Windows 端到端可跑）。
- 校验分层：纯字段校验（`validateNetIface`）+ 关系校验（`Service` 层：跨接口冲突、子网/池一致、删最后 LAN）。
- 前端：抽屉重构（主 IP + 附加 IP 列表 + 高级折叠分组 + 危险操作二次确认 + 一键预填 DHCP）。
- API/openapi 同步；fake-exec 单测锁定新命令。

### 2.2 本期不做（明确留待后续，§13）

- **alias 同物理口第二子网发 DHCP**：OpenWrt netifd/dnsmasq 已知不为同一 L2 上的第二逻辑接口可靠生成 dhcp-range（GitHub #15159）。故**额外可发 DHCP 子网必须是独立 netdev（独立物理口 / VLAN 子接口）**。本期不引入 `@父接口` alias。
- **一个接口挂多条 IPv6（`list ip6addr`）**：netifd 跨版本对多值支持不一 + PD 交互复杂。IPv6 本期仅单值字段；`Family` 字段预留。
- **完整防火墙管理子系统**（zone/转发/端口转发 UI）：本期只做"把本工具新建接口并入其角色默认 zone"这一最小可逆动作。
- **apply-rollback 计时回滚**（LuCI 式）：以 G3 二次确认 + 新地址引导替代（更简单）。

---

## 3. 设计原则（沿用既有纪律 + 本次新增）

沿用 `2026-06-15` 决策：

- **旁车 `DATA_DIR/netcfg.json` 为权威**：UCI 无法表达的 per-IP 备注/启用、禁用项只存旁车，不依赖 UCI 回读。
- **只用通用原语**：多地址 `list ipaddr`+CIDR（≤19.07 起即支持，21.02+ 普遍、25.12 默认）；**刻意不用 21.02 才有的 `option disabled`**（禁用项不投射、仅旁车）。
- **托管标记 + 具名节**：本工具新建的 section 打 `option managed_by 'kwrt-net-manager'`，只增删自己的节，**绝不碰 stock/LuCI/手改节**。
- **commit 与 reload 分阶段**：`commit` 后 `reload`（不 `restart`），reload 失败置 `pending` 上报。
- **语义校验全在 Go 层、commit 之前**。

本次新增：

- **职责分离优先于一站式魔法**：地址（接口页）与 DHCP 池（DHCP 页）各归其位，不在接口页隐式增删 DHCP/防火墙对象。
- **新建即可用（含防火墙）**：本工具新建的独立接口自动并入其角色默认 zone，消灭"配好却不通"。
- **危险操作显式确认**：可能中断管理的操作（改主 IP/子网、解绑承载管理的网卡、删接口）在前端二次确认并提示后果与新访问地址。

---

## 4. 数据模型

### 4.1 新增 `IfaceAddr`（附加 IP 项，`internal/netcfg/iface_types.go`）

```go
// IfaceAddr 是接口上的一个附加 IP（次地址/管理地址，可与主 IP 同或异子网）。
// 落地 OpenWrt `list ipaddr '<address>/<prefix>'`。永不发 DHCP（要发 DHCP 的网段
// 用独立接口，见 §4.4）。per-IP 的 remark/enabled 旁车权威。
type IfaceAddr struct {
    Address string `json:"address"` // 点分 IPv4，如 10.0.0.1
    Prefix  int    `json:"prefix"`  // CIDR 位数，如 24
    Family  string `json:"family"`  // 本期固定 "ipv4"；预留 "ipv6"
    Remark  string `json:"remark"`  // 备注（仅旁车，UCI 无此字段）
    Enabled bool   `json:"enabled"` // 关闭=不投射（不用 option disabled，仅旁车）
}
```

### 4.2 `NetIface` 扩展（保留现有 `IPAddr/Netmask` 作主 IP）

```go
// —— 附加地址 ——
ExtraAddrs []IfaceAddr `json:"extra_addrs"`

// —— OpenWrt 接口全量对齐（默认折叠在「高级」，全部 snake_case）——
Metric    int    `json:"metric,omitempty"`     // option metric，多 WAN/路由优先级（越小越优先）
PeerDNS   *bool  `json:"peerdns,omitempty"`    // option peerdns，是否用上游下发 DNS（nil=默认）
Broadcast string `json:"broadcast,omitempty"`  // option broadcast（static 广播地址）
ForceLink *bool  `json:"force_link,omitempty"` // option force_link，无链路也配地址/路由
Auto      *bool  `json:"auto,omitempty"`       // option auto，开机自启（nil/true=默认拉起）

// —— IPv6 单值（本期不做 list ip6addr 多地址）——
IP6Assign int    `json:"ip6assign,omitempty"`  // option ip6assign，委派前缀长度（如 60；0=不设）
IP6Hint   string `json:"ip6hint,omitempty"`    // option ip6hint，子前缀 ID（hex）
IP6Addr   string `json:"ip6addr,omitempty"`    // option ip6addr，单条静态 IPv6（CIDR）
IP6Gw     string `json:"ip6gw,omitempty"`      // option ip6gw，IPv6 默认网关
```

> `*bool` 区分「未设置（用 OpenWrt 默认）」与「显式 false」，避免无脑覆盖 stock 默认。`omitempty` 让 nil/零值不出现在 JSON 中（编码侧），解码侧缺键即零值/nil（见 §8.1 契约表）。
> **v2 已移除 `Parent` 字段**（alias 砍掉）。

### 4.3 `State` / `CloneState`（`internal/netcfg/types.go`）

`State.NetIfaces []NetIface` 已存在。`CloneState`（types.go:197-200）需深拷贝 `ExtraAddrs` **和 `*bool` 指针**（避免多快照共享同一 bool 的隐性 bug）：

```go
out.NetIfaces = append([]NetIface(nil), s.NetIfaces...)
for i := range out.NetIfaces {
    src := s.NetIfaces[i]
    out.NetIfaces[i].Ports = append([]string(nil), src.Ports...)
    out.NetIfaces[i].ExtraAddrs = append([]IfaceAddr(nil), src.ExtraAddrs...)
    out.NetIfaces[i].PeerDNS = cloneBoolPtr(src.PeerDNS)
    out.NetIfaces[i].ForceLink = cloneBoolPtr(src.ForceLink)
    out.NetIfaces[i].Auto = cloneBoolPtr(src.Auto)
}
// func cloneBoolPtr(p *bool) *bool { if p == nil { return nil }; v := *p; return &v }
```

### 4.4 「额外子网参与 DHCP」的职责分离（回应用户 Q3「本期参与 DHCP」）

**不在接口页隐式生成 DHCP/alias**。透明、可靠、主流的两步：

1. **新建独立内网**（接口页已支持，`nextIfaceID`→lan2/lan3…）：绑定一个**空闲物理网卡**（或手填 VLAN 子接口设备名如 `eth0.20`）。它是**独立 netdev**，dnsmasq 能可靠为其发 dhcp-range。保存时由 G1 **自动并入 lan 防火墙 zone**，客户端即可上网/达路由。
2. **为它配 DHCP**：DHCP 服务端页已支持 `DHCPServer.Interface` 绑定任意接口。为降低摩擦：LAN 接口若**尚无**绑定它的 DHCP 池，卡片/抽屉显示**「一键启用 DHCP」**——打开 DHCP 抽屉并**预填**接口名/子网/默认池（如 `.100~.200`、网关=主 IP），**用户点保存才落地**（复用现有 `createDHCPServer`，无新后端方法、无孤儿风险）。

> **同一物理口要多个子网**：附加 IP（`list ipaddr` 异子网）可做**路由/管理**子网；若该子网还要**发 DHCP**，本期请改用独立物理口/VLAN（见 §2.2 的 OpenWrt 限制）。UI 在「附加 IP」区注明「附加 IP 不发 DHCP；需发地址请新建内网口」。

---

## 5. UCI 后端投射（`internal/netcfg/iface_uci.go`）

### 5.1 地址写入：主 + 附加统一 `list ipaddr`（核心）

`SaveNetIface` 的 LAN 分支与 WAN-static 分支，地址写入改为：

```
# 1) 清残留：uci delete 会移除该 key 的任意形式（option 或 list），一并清 netmask
delete network.<id>.ipaddr
delete network.<id>.netmask
# 2) 主 IP 永远第一条（CIDR）
add_list network.<id>.ipaddr='<主IP>/<主prefix>'
# 3) 附加 IP（仅 Enabled 的）逐条
add_list network.<id>.ipaddr='<附加IP>/<prefix>'
...
```

- 主 prefix 由 `netutil.MaskToPrefix(in.Netmask)`（netutil.go:80）求得；附加 IP 自带 prefix。
- **`delete network.<id>.ipaddr` 同时清掉旧的 `option ipaddr` 与既存 `list ipaddr`**（uci 语义：按 key 删除，不分 option/list）——从旧式单 IP 升级无残留。
- `list ipaddr` 必须带前缀、不能与 `option netmask` 并存——步骤 1 的 `delete netmask` 已保证。
- **D1 决策**：即便只有主 IP 也统一写 `list ipaddr`（读路径已兼容两形式，统一更一致）。

### 5.2 全量字段投射

`SaveNetIface` 末尾（`remark` 附近）追加，均 `setOptOrDel`（空值即删，回归默认）：

| 字段 | UCI | 备注 |
|---|---|---|
| `Metric` | `option metric`（>0 才写） | 多 WAN 优先级 |
| `PeerDNS` | `option peerdns '0'/'1'`（nil 不写） | static/dhcp/pppoe 通用 |
| `Broadcast` | `option broadcast` | static |
| `ForceLink` | `option force_link '0'/'1'`（nil 不写） | |
| `Auto` | `option auto '0'/'1'`（nil 不写=默认拉起） | |
| `IP6Assign` | `option ip6assign`（>0 才写，单值） | LAN 常用 60 |
| `IP6Hint` | `option ip6hint`（单值） | hex 子前缀 ID |
| `IP6Addr` | `option ip6addr`（单值，**非 list**） | 单条静态 IPv6/CIDR |
| `IP6Gw` | `option ip6gw`（单值） | |

> 本期所有 IPv6 字段均为单值 `option`，**多地址/多 PD 留后续**（§2.2）。

### 5.3 修复 clone_mac 写入（DSA 正确位置，新增 `ensureDeviceMAC`）

DSA 下 `macaddr` 必须写在 `config device` 段（非 `config interface`）。新增 helper：

```
# ensureDeviceMAC(sb, id, dev, mac)
若 mac == "":
    若存在本工具托管的 device 段 dev_<id> → delete network.dev_<id>.macaddr（保留桥结构，仅删 MAC）
    返回
# 桥接口（已由 writeBridge 建 dev_<id>，name=br-<id>）：直接写到该段
set network.dev_<id>.macaddr='<mac>'
# 单网卡直连（无 device 段）：新建 dev_<id> 承载 MAC，并让 interface 指向它
set network.dev_<id>=device
set network.dev_<id>.name='<物理口>'        # 必须，netifd 据此匹配
set network.dev_<id>.macaddr='<mac>'
set network.dev_<id>.managed_by='kwrt-net-manager'
set network.<id>.device='dev_<id>'           # interface.device 指向该 device 段名
```

- device 段统一命名 `dev_<id>`（与现有 `writeBridge` 一致，iface_uci.go:397）。
- 新建的 device 段打 `managed_by`；`DeleteNetIface` 时一并删（§5.5）。
- **版本兼容**：探测不支持 `config device`（极老 swconfig）时回退 `set network.<id>.macaddr`（interface 段）。
- 写前校验 MAC（§7）。

### 5.4 自动并入防火墙 zone（核心防呆 G1，新增 `findZoneForRole`）

`SaveNetIface` 成功 `commit network` 后，对**新建的独立接口**（非主 lan/wan）确保其在角色默认 zone 的 `list network`：

```
# 按“成员”而非“名字”定位 zone：解析 uci show firewall，找 type=zone 且其
# list network 含 'lan'(LAN 角色) 或 'wan'(WAN 角色) 的第一个 zone 段
zsec = findZoneForRole(role)            # 返回段名，或 ""（未找到）
若 zsec == "" → 置 pending + 文案「请手动将接口 <id> 加入防火墙区域」（不报错中断）
否则若该 zone 的 network 列表不含 <id>：
    add_list firewall.<zsec>.network='<id>'
    commit firewall
    /etc/init.d/firewall reload           # 必需！否则 zone↔network 映射不在 nft/iptables 生效（fw4 与 fw3 通用）
```

- **只增删自己**：只 `add_list` 本接口 `<id>`；`DeleteNetIface` 用同一 `findZoneForRole` `del_list <id>` 并清理 §5.3 的 `dev_<id>` device 段。
- 主 `lan`/`wan` 默认已在对应 zone → 跳过。
- 与 `dns_uci.go` 的 `dnsLANZone="lan"`（DNS 劫持 redirect 用）**互不干扰**：那是 redirect 规则的源 zone 名，本逻辑动的是 zone 的 `list network` 成员；二者职责不同，本期不合并（§11-D3）。
- best-effort：firewall reload 失败 → `pending`（network 已生效），前端展示「已保存，防火墙未生效，请重试」。

### 5.5 读 / 导入（`NetIfaces()`，iface_uci.go:190-265）+ 删除

- **地址解析升级**：优先读 `list ipaddr`（多条，CIDR 拆 prefix）；**若 list 为空则回退 `option ipaddr`+`option netmask`**（兼容老数据/stock）。**第一条作主 IP**（`IPAddr/Netmask`），其余进 `ExtraAddrs`（`Family=ipv4`、`Enabled=true`，remark 从旁车补）。
- **全量字段回读**：`metric/peerdns/broadcast/force_link/auto/ip6assign/ip6hint/ip6addr/ip6gw`（`*bool` 字段：缺键=nil，'0'→false，'1'/其它→true）。
- **clone_mac 回读**：优先读 `dev_<id>` device 段的 `macaddr`，回退 interface 段（兼容现状 iface_uci.go:226）。
- **旁车合并**：per-IP `remark/enabled`、禁用的附加 IP（未投射 UCI）从旁车补——读返回 = UCI 投影 ∪ 旁车权威。
- **删除**：`DeleteNetIface` = delete `network.<id>` + 清 `dev_<id>`（若托管）+ `del_list` 防火墙 zone 成员 + commit network/firewall + reload。

### 5.6 生效

`commit network`（+ `commit firewall` 若有改动）→ `/etc/init.d/network reload` →（若 zone 改动）`/etc/init.d/firewall reload`。任一 reload 失败置 `pending` + 文案，前端「已保存未生效」。沿用现有 `Status.Pending` 机制（前端 `useNetData` 已轮询 + WS 事件驱动重载，pending 在下次成功保存/reload 后清除）。

---

## 6. 防呆与防自锁护栏（一等公民）

| 编号 | 护栏 | 实现层 | 说明 |
|---|---|---|---|
| **G1** | **新建独立接口自动并入防火墙 zone** | uci `SaveNetIface`/`DeleteNetIface`（§5.4） | 消灭"新建 lan2/wan2 看着配好却不通"（多 WAN NAT、多内网转发都靠它）。仅对**新建独立接口**；主 lan/wan 跳过。 |
| **G2** | **不可删除最后一个 LAN** | `Service.DeleteNetIface` | 删 role=lan 前统计；若为最后一个 LAN → 拒绝并报「至少保留一个内网，否则将失去管理入口」。 |
| **G3** | **改主管理 IP/子网、解绑管理网卡 → 二次确认 + 新址引导** | 前端 Modal | 弹确认：「将更改管理地址/拓扑，保存后可能短暂断网，需用新地址重新访问」；保存后前端显示「如无法访问，请用新地址 X.X.X.X 重新打开」。后端照常 reload（不做 apply-rollback，§13）。 |
| **G4** | **地址唯一/不重叠校验** | `validateNetIface`(字段) + `Service`(关系) | 主 IP 与各附加 IP 互不重复；与**其它接口**的 IP 不冲突；附加 IP 必须合法 CIDR。 |
| **G5** | **主 IP 必填、不可删除** | 前端 + 校验 | UI 主 IP 固定行（只改不删）；附加 IP 可自由增删。保证接口恒有有效基址。 |
| **G6** | **端口独占冲突可见** | 前端 | 勾选被占用网卡高亮「占用:xxx」（现状已有），保存即从原桥摘除（`detachPorts` 已实现）；移除承载管理连接的网卡走 G3。 |
| **G7** | **MAC 格式校验** | `validateNetIface`+`netutil.IsValidMAC` | clone_mac 非空须合法 MAC（修复后才真正下发，更需校验）。 |
| **G8** | **改子网导致绑定 DHCP 池越界 → 拒绝** | `Service.SaveNetIface` | 若改主 IP/子网，且存在绑定本接口的 DHCP 池其 start/limit 将越界 → 拒绝并提示「请先调整该内网的 DHCP 池」。 |
| **G9** | **保存失败/未生效显式上报** | 现有 `pending` 机制 | reload/firewall 失败 → `Status.Pending=true`+文案，前端「已保存未生效」并可重试。 |
| **G10** | **删接口二次确认 + 关联提示** | 前端 Popconfirm（现状已有） | 保留，并对「该接口有绑定的 DHCP 池/静态分配」追加提示。 |

> **防自锁核心**：G1（新建即通）+ G2（不丢最后内网）+ G3（改管理地址先确认+新址引导）。

---

## 7. 校验（`internal/netcfg/iface.go`）

**分两层**（呼应现有 `iface.go:57` 调 `validateNetIface(&in)`）：

- **`validateNetIface(*NetIface)`（纯函数，保留+扩展，可单测）**——本地字段校验：
  - 现有：角色/协议/主 IP/MTU/DNS（iface.go:183-225）。
  - 新增：遍历 `ExtraAddrs`（`Address` 合法 IPv4、`Prefix`∈[1,32]、`Family`=ipv4）；主 IP + 附加 IP 列表内 IP 去重；`CloneMAC` 非空→`netutil.IsValidMAC`；`Metric>=0`；`IP6Assign`∈[0,64]；`IP6Addr`/`IP6Gw` 非空→`netutil.IsIPv6`；`Broadcast` 非空→`netutil.IsIPv4`。
- **`Service.SaveNetIface` 内的关系校验（新增 `checkIfaceRelations`，纯函数+可单测）**——需全局视图（读 `be.NetIfaces()`/`be.DHCPServers()`）：
  - 跨接口 IP 重复（本接口所有 IP vs 其它接口所有 IP）。
  - G8：主 IP/子网变更后，绑定本接口的 DHCP 池 start/limit 是否越界。
  - 调用顺序：`validateNetIface(&in)` → 读现有列表 → `checkIfaceRelations(in, ifaces, servers)` → `be.SaveNetIface`。
- **`Service.DeleteNetIface`**：G2 删最后一个 LAN 检查（新增 `canDeleteNetIface`）。

新增 `pkg/netutil` 纯函数 + 表驱动单测：
- `IsValidMAC(s)`：接受 `AA:BB:CC:DD:EE:FF` 与 `aa-bb-cc-dd-ee-ff`，大小写不敏感（参考 LuCI network.js）。
- `IsIPv6(s)`：支持压缩 `::`、可选 `/prefix`（CIDR）；示例 `2001:db8::/32`、`fd00::1/64`。

---

## 8. 前端（`web/src/`）

### 8.1 类型同步 + TS↔Go JSON 契约表（`web/src/api/netcfg.ts`）

⚠️ `decodeJSON` 启用 `DisallowUnknownFields()`（helpers.go）：**多发一个未知 key 即 400；缺键则 Go 取零值/nil（不报错）**。故新字段在 TS 设为**可选**，前端可不发；只要不发 struct 里没有的 key 即安全。

| Go 字段 / json tag | Go 类型 | 编解码语义 | TS 类型 |
|---|---|---|---|
| `extra_addrs` | `[]IfaceAddr` | 始终数组 | `extra_addrs: IfaceAddr[]` |
| `metric,omitempty` | `int` | 0 不发 / 缺=0 | `metric?: number` |
| `peerdns,omitempty` | `*bool` | nil 不发 / 缺=nil；`true/false/null` | `peerdns?: boolean \| null` |
| `force_link,omitempty` | `*bool` | 同上 | `force_link?: boolean \| null` |
| `auto,omitempty` | `*bool` | 同上 | `auto?: boolean \| null` |
| `broadcast/ip6hint/ip6addr/ip6gw,omitempty` | `string` | 空不发 | `broadcast?: string` … |
| `ip6assign,omitempty` | `int` | 0 不发 | `ip6assign?: number` |

```ts
export interface IfaceAddr {
  address: string; prefix: number; family: 'ipv4' | 'ipv6';
  remark: string; enabled: boolean;
}
export interface NetIface { /* …现有… */ extra_addrs: IfaceAddr[];
  metric?: number; peerdns?: boolean | null; broadcast?: string;
  force_link?: boolean | null; auto?: boolean | null;
  ip6assign?: number; ip6hint?: string; ip6addr?: string; ip6gw?: string;
}
export type NetIfaceInput = Omit<NetIface, 'up' | 'runtime_ip'>;
```

> 改前必读 Go 源（web-api-binding 技能）。无新端点（复用 PUT/POST/DELETE `/api/v1/ifaces`）。

### 8.2 抽屉重构（`NetOverview.tsx` `IfaceDrawer`）——简单优先

自上而下，**默认只露常用项**：

1. 接口名称（新增时）/ 绑定网卡（LAN 多选组桥 / WAN 单选）——现状保留。
2. 接入方式（WAN）/ **主 IP + 掩码** / 网关 / DNS。
3. **附加 IP** 区（默认折叠/空）：`+ 新增` 行 = IP + 掩码位(/24) + 备注 + 启用 + 删除。主 IP 不入列（G5）。区顶注明「附加 IP 仅作管理/路由，不发 DHCP；需发地址请新建内网口」。
4. **「高级设置」折叠面板**（`Collapse` 默认收起），**分组 + 每组一行说明 + 字段 tooltip/placeholder**：
   - 多 WAN 优先级：`metric`（WAN 提供「主线路 metric=0 / 备用线路 metric=100」快速预设按钮）。
   - DNS 选项：`peerdns`。
   - 链路控制：`force_link`、`auto`。
   - IPv6：`ip6assign`、`ip6hint`、`ip6addr`、`ip6gw`。
   - 其它：`broadcast`（placeholder `192.168.1.255`）、`MTU`、`克隆 MAC`、`备注`。
5. 底部提示 + 危险操作二次确认（G3/G10）。

**掩码统一 CIDR 位数**（v2 决策，主+附加一致，与 `list ipaddr`/iKuai/LuCI 一致）：主 IP 掩码下拉由点分改为位数（/24…）；读老数据时 `option netmask` 经 `MaskToPrefix` 显示为位数。复用 `pkg/netutil` 互转。

### 8.3 一键启用 DHCP（§4.4 步骤 2）

LAN 接口卡片/抽屉：若无绑定该接口的 DHCP 池 → 「为该内网启用 DHCP」按钮 → 打开 DHCP 抽屉并预填 `interface`/子网/默认池（用户确认保存，复用 `createDHCPServer`）。

### 8.4 网卡列表 / Dashboard

`NICs.tsx`/`Dashboard.tsx` 已展示 `ip_addrs`，读写一致后无需改（割裂消除）。

---

## 9. API / openapi / store 后端 / Backend 接口

- **复用现有端点**：`GET/POST/PUT/DELETE /api/v1/ifaces`、`POST /api/v1/ifaces/{id}/action`（结构体扩展自然带上新字段，无新端点）。
- **Backend 接口不变**：防火墙 zone 并入是 **uci 后端 `SaveNetIface`/`DeleteNetIface` 内部行为**，不暴露 API、不进 `NetIface` 字段、不加 Backend 方法。**store 后端 `SaveNetIface` 仅持久化 JSON、无防火墙动作**（dev 无防火墙概念）。
- `internal/api/openapi.yaml`：同步 `NetIface` 增 `extra_addrs`（`IfaceAddr` 定义）+ 全量字段。必要时 `npm run gen:api`。
- store 后端（`iface_store.go`）：`cloneIface` 增 `ExtraAddrs` + `*bool` 深拷贝；`State.NetIfaces` 已基于 JSON，扩展字段自动持久化。seed 可加一条带附加 IP 的 LAN 演示。
- `NetOverview` 卡片副标题可显示「主 IP（+N）」表示有附加 IP。

---

## 10. 测试策略

- **`pkg/netutil`**：`IsValidMAC`/`IsIPv6` 表驱动单测（含两种 MAC 格式、压缩 IPv6/CIDR）。
- **`iface_uci_test.go`（fake-exec）**新增/扩展锁定生成命令：
  - 多 IP → `delete ipaddr/netmask` + 多条 `add_list ipaddr='x/24'`（主 IP 第一条）。
  - **从旧式 `option ipaddr` 升级** → 验证清理无残留、转为 list。
  - clone_mac → 写 `dev_<id>.macaddr`（桥/单卡两分支）+ 空值删 macaddr。
  - 全量字段 → 对应 `option ...`（`*bool` 的 '0'/'1'/不写）。
  - 自动并入 zone → `findZoneForRole` 命中 → `add_list firewall.<zone>.network='lan2'`+`commit firewall`+`firewall reload`；删除 → `del_list`+清 `dev_`。
  - 读路径：含多条 `list ipaddr` 的 UCI 样本 → 正确还原主/附加 IP；仅 `option ipaddr` 样本 → 回退解析。
- **校验单测**：重复 IP、跨接口冲突、非法 MAC/IPv6、删最后一个 LAN（G2）被拒、改子网致 DHCP 池越界（G8）被拒。
- **store 往返**：保存含附加 IP + `*bool` 的接口 → netcfg.json → 读回一致（含 `*bool` 深拷贝不串扰）。
- 全量：`make test` + `go vet` + 前端 `tsc -b`。
- **真机（`optest`，ImmortalWrt 192.168.1.12）**：多 IP `ip addr` 真实生效；新建独立内网 + DHCP 发地址通；wan2 NAT 通；**重点验证防自锁 G1/G2/G3**。

---

## 11. 迁移与兼容性决策记录

- **D1 统一 `list ipaddr`**：即便单主 IP 也写 list。读路径已兼容两形式；统一更一致、避免 option↔list 切换分支。（家用路由器接口数 <20，体积/性能影响可忽略，不做量化。）
- **D2 托管标记**：本工具**新建的** interface/device 段打 `managed_by`；导入的 stock 主 lan/wan **不打**（不动其语义）。**删除不以 marker 为门槛**（用户在面板里看到的接口本就是自己的网络），唯一硬门槛是 G2（不删最后 LAN）；marker 仅用于识别本工具建的 `dev_<id>` 等附属段以便随接口清理。
- **D3 防火墙**：只做「并入角色默认 zone」最小可逆动作（按 `list network` 成员定位 zone）；不做 zone 管理 UI；找不到 zone 降级 pending 提示而非报错。与 DNS 劫持的 `dnsLANZone` 各自独立。
- **D4 IPv6**：本期单值字段；多地址（`list ip6addr`）留后续，`Family` 已预留。
- **D5 砍掉 alias(`@父接口`)**：OpenWrt 无法可靠为同 L2 第二逻辑接口发 DHCP；且"附加 IP 异子网"已覆盖同口路由子网。额外可发 DHCP 子网=独立 netdev。
- **向后兼容**：旧 netcfg.json 无新字段 → 反序列化零值/nil/空切片，行为等同现状，无需数据迁移。

---

## 12. 涉及文件清单

**后端**
- `internal/netcfg/iface_types.go` — `IfaceAddr` 新类型 + `NetIface` 扩展字段（含 json tag）。
- `internal/netcfg/iface_uci.go` — 地址 list 化、全量字段、`ensureDeviceMAC`（clone_mac 修复）、`findZoneForRole`（zone 并入/移除）、读/导入升级、Delete 清理。
- `internal/netcfg/iface.go` — `validateNetIface` 字段校验扩展 + `checkIfaceRelations`（关系校验）+ `canDeleteNetIface`（G2）。
- `internal/netcfg/iface_store.go` — `cloneIface` 深拷贝 `ExtraAddrs`+`*bool`；firewall no-op；seed。
- `internal/netcfg/types.go` — `CloneState` 深拷贝 `ExtraAddrs`+`*bool`（`cloneBoolPtr`）。
- `pkg/netutil/netutil.go` — `IsValidMAC`/`IsIPv6`（+单测）。
- `internal/api/openapi.yaml` — schema 同步。

**前端**
- `web/src/api/netcfg.ts` — 类型 + 契约表对齐（无新端点）。
- `web/src/pages/NetOverview.tsx` — 抽屉重构（附加 IP 列表 / 高级分组折叠 / 二次确认 / 一键预填 DHCP / 掩码统一 CIDR）。

**测试**
- `internal/netcfg/iface_uci_test.go`、`pkg/netutil/*_test.go`、校验/store 往返测试（按现状归属）。

---

## 13. 后续 / 可选增强（明确不在本期）

- alias / 同物理口多 DHCP 子网（待 OpenWrt 改善或改用 VLAN 向导）。
- apply-rollback 计时确认（防自锁金标准，较重）。
- 一个接口多条 IPv6（`list ip6addr`）。
- VLAN 子接口可视化向导（本期仅支持手填 `eth0.X` 设备名）。
- 完整防火墙 zone/转发/端口转发管理页。

---

## 14. 开放问题（请评审确认）

1. **D5 砍掉 alias**：额外可发 DHCP 子网改走「新建独立内网（占物理口/VLAN）」是否认可？（这是相对最初方案的最大简化，也规避了 OpenWrt 已知限制。）
2. **D1 统一 list ipaddr** 是否认可？
3. **§8.2 掩码统一改 CIDR 位数**（含主 IP）是否认可？（与附加 IP 一致、更简洁；老数据自动转换显示。）
4. **G3 用二次确认 + 新址引导而非 apply-rollback** 是否接受？
