# kwrt-net-manager · IPv6 全套功能设计（2026-06-16）

> 基于真机探测（ImmortalWrt 24.10.6 / odhcpd+dnsmasq）+ 4 路并行研究综合而成。
> 目标：把爱快 iKuai 的 IPv6 菜单全套功能，以 Web 形式落到 OpenWrt（21.02–24.10，DSA/netifd + odhcpd）。
> 第一准则：**只做真实可用的；OpenWrt 做不到的诚实降级并明示，绝不用假控件冒充。**

## 1. 范围与可行性（爱快 IPv6 菜单 8 项）

| 功能 | 页面 | 可行性 | OpenWrt 实现 / 数据源 |
|---|---|---|---|
| IPv6 设置（外网+内网双表） | `/ipv6/settings` | full | 读 `uci show network`(WANv6) + `uci show dhcp`(LANv6) + `ubus … status` |
| IPv6 外网编辑（odhcp6c 客户端） | 设置页 Drawer | full | `config interface` proto=dhcpv6/static6/6in4/6to4/6rd（reqprefix/reqaddress/clientid/peerdns/dns） |
| IPv6 内网编辑（odhcpd RA+DHCPv6） | 设置页 Drawer | partial | `config dhcp`(dhcpv6/ra/ra_management/ra_mtu/leasetime/dns) + `config interface`(ip6assign/ip6class) |
| IPv6 线路详情 | `/ipv6/line-detail` | partial | 接口 v6 字节累计 + 前端差分速率 + conntrack v6 连接数 |
| DHCPv6 终端 | `/ipv6/leases` | partial | `ubus call dhcp ipv6leases` + `ip -6 neigh` 补 MAC（本机未开 server 时友好空态） |
| 前缀静态分配 | `/ipv6/prefix-static` | partial | `config host`(duid+hostid 固定 IID，**非整段 PD**) |
| DHCPv6 黑白名单 | `/ipv6/acl` | **infeasible(原生)** | 降级：①按 DUID 拒发(hostid=ignore，可靠) ②按 MAC L2 拦截(nftables，实验/延后) |
| 邻居列表（NDP） | `/ipv6/neighbors` | full | `ip -6 neighbor show` / `ip -6 neigh del` / `flush`（本机唯一有实时数据页） |

## 2. 架构决策

- **IPv6 自成领域**：`internal/netcfg/ipv6_*.go`，与 IPv4 的 `NetIface` 写路径解耦（IPv4 写路径不适配 reqprefix/ip6assign；`skipIfaceProto` 已故意排除 dhcpv6/6in4 等）。
- **沿用既有四大约束**：snake_case JSON + `DisallowUnknownFields`；`managed_by='kwrt-net-manager'` 具名节只动自己；旁车 `netcfg.json` 为权威；commit≠reload（reload 不 restart，失败置 pending）。
- **目标平台 21.02+（DSA 网桥）**，但 UCI 投射仍坚持 ≤19.07 通用原语 + 删除/旁车表达禁用，不用 21.02 才有的 `option disabled`。
- **DHCPv6 模式映射**：stateless→`ra_management='0'`，stateful→`'1'`（混合，兼容不支持 DHCPv6 的 Android），stateful_only→`'2'`。坚持整数 `ra_management`，不用 `ra_flags`。
- **租期换算**：前端分钟 `n` ↔ 后端 `leasetime='<n>m'`（复用 `leasetimeToMin`）。
- **DUID 随机生成**：后端 `crypto/rand` 生成 type-4 DUID-UUID（`0004`+16B），供「重新生成」动作；留空=用 OpenWrt 默认（基于 MAC 的 DUID-LL）。
- **reload 分阶段**：改 network 段→`/etc/init.d/network reload`；改 dhcp 段→`/etc/init.d/odhcpd reload`；失败置 `pending`。

## 3. 诚实降级（必须在 UI/代码体现）

1. **DHCPv6 MAC 黑白名单 — 最大降级点**：odhcpd 无按 MAC 拒发开关（DHCPv6 以 DUID 标识，RFC8415 禁拆 MAC）。一期实现「按 DUID 拒发」(hostid=ignore，可靠原生) + store 模拟 + UI；「按 MAC L2 拦截」标实验/延后，且必同时拦 RA 否则 SLAAC 绕过。页面诚实命名「DHCPv6 接入控制（实验）」，绝不假称"已按 MAC 拦截"。
2. **前缀静态分配**：odhcpd `config host` 只能固定 IID（hostid），不能为某终端保留独立 PD 前缀；UI 标「固定接口 ID，非整段前缀」；未开 LAN dhcpv6=server 时置灰提示。
3. **IPv6 网关只读**：ubus status 无 gateway 键，从 `route[]`(target=::/mask=0) 的 nexthop 推导，dhcpv6 常为 fe80:: link-local。
4. **DHCPv6 终端本机恒空**：本机为单网卡下游设备（lan6 是 dhcpv6 客户端，dhcp.lan 仅 dhcpv4=server），`ubus dhcp ipv6leases` 恒 `{device:{}}`。UI 显示「LAN 未开启 DHCPv6 服务端」而非加载失败。MAC 优先邻居表 lladdr；EUI-64 反推仅 IID 含 ff:fe 时有效。
5. **绑定外网线路（多WAN PD 选源）**：单上游全自动；真多 WAN 依赖上游独立委派，MVP 默认「自动」，多WAN 标高级可选。
6. **6in4/6to4/6rd**：依赖同名 opkg/apk 包，未装则该 proto 置灰并提示一键安装；dhcpv6/static6 总可用。
7. **BusyBox ip 无 neigh 子命令**需 ip-full（ImmortalWrt 24.10 默认带，仍探测降级）。

## 4. 后端落点

- `ipv6_types.go`：WANv6/LANv6/LeaseV6/PrefixStaticV6/ACLv6(+Entry)/NeighborV6/LineV6 + 常量；State 增 4 个旁车字段 + CloneState 深拷贝。
- `ipv6_validate.go`：各 struct 校验（复用 netutil.IsIPv6/IsIP，新增 CIDR/前缀 0-128 边界）。
- `ipv6_service.go`：CRUD + 校验 + 事件 + DUID 生成（照搬 service.go 的 mutex+idFn+publish）。
- `ipv6_uci.go`：uci 投射（network+dhcp 段）+ 运行态读（ubus/ip）+ 包探测；`ipv6_store.go`：store 模拟（可信 2408::/2001:db8:: 数据）。
- `backend.go`：Backend 接口加 IPv6 方法（不动现有签名）。
- `internal/api/netcfg_ipv6.go` + `netcfg_routes.go` 挂 `/api/v1/ipv6/*`；`eventbus` 增 `TypeIPv6Changed`。
- `ipv6_uci_test.go`：fake-exec 锁定生成的 uci 命令序列。

## 5. 前端落点

- `web/src/api/ipv6.ts`：TS 类型逐字段镜像 Go struct（snake_case）+ Input(Omit id/只读) + 函数。
- `web/src/pages/ipv6/`：Ipv6Settings（双表+双 Drawer）/ Ipv6LineDetail / Ipv6Leases / Ipv6PrefixStatic / Ipv6Acl / Ipv6Neighbors。
- `MainLayout.tsx` 导航加「IPv6」一级菜单（6 子项，对齐爱快）；`App.tsx` 加 `/ipv6/*` 路由。

## 6. 真机验证策略

本机为下游单网卡设备：以 **store 后端端到端 + uci fake-exec 单测** 为主要验证；真机重点验 **邻居列表（有实时数据）、外网 v6 只读、odhcpd/ip-full 探测、安全写**。多 NIC/多 WAN/LAN v6 server 造数据风险高（可能与上游冲突或断网），不在单网卡机器上做破坏性真机写测。
