# kwrt-net-manager — 爱快式 DNS 功能设计（OpenWrt 落地）

> 2026-06-16。目标：把爱快「网络设置 > DNS 设置 / 多线路DNS」的 web 功能，在 OpenWrt(dnsmasq)
> 能力范围内完整搬过来，主路由/旁路由均可用。一切以 OpenWrt 原生能实现为准；不能原生实现的
> 诚实降级并在 UI 说明，绝不用假控件。前提：用户离线、真机是生产旁路由 —— **最高优先级是绝不
> 搞坏现有 DHCP/DNS**，故所有功能**默认关闭/未托管**，部署后对现状零行为改动。

## 一、功能映射（爱快 → OpenWrt，已对抗式核验 + 真机验证）

| 爱快功能 | OpenWrt 机制 | 判定 | 备注 |
|---|---|---|---|
| 首选/备选 DNS（上游） | `@dnsmasq[0]` `list server` + `option noresolv '1'` + 显式 `resolvfile '/tmp/resolv.conf.auto'` | build | 非严格主备（最快优先）。开 noresolv 前必须有可达上游，否则路由器自身也断 DNS。 |
| 禁止 AAAA(IPv6) 解析 | `@dnsmasq[0]` `option filter_aaaa '1'` → `--filter-AAAA` | build | 真机 dnsmasq 2.90 full 已验证生效；精简构建不支持，写前探测，不支持则置灰。 |
| 老化时间 / 缓存大小 | `option local_ttl` / `min_cache_ttl`(≤3600) / `max_cache_ttl`(≤3600) / `cachesize`(0=关) | build | min/max 超 3600 dnsmasq 静默钳到 3600，Go 层 clamp+提示。 |
| DNS 反向代理 / 自定义解析 | 精确域 → `config hostrecord`（具名节, marker, 自动 PTR, 真机已验证）；通配域 `*.x` → `@dnsmasq[0]` `list address '/x/ip'` | build | 「作用IP段」单 dnsmasq 无法按源 IP 分应答 → 一期全局生效, UI 标注降级。 |
| DNS 加速(DoH) + 模式 + 地址 | `https-dns-proxy` 包(opkg/apk)：实例节 resolver_url/listen 127.0.0.1#5053；我方关其自动改写(`dnsmasq_config_update '-'`)，自己往 `@dnsmasq[0]` 写 `server '127.0.0.1#5053'`+noresolv（可回滚） | build(needs-package) | 默认关；未装包置灰+一键安装；端口 5053≠53。 |
| 强制客户端 DNS 代理 | firewall `config redirect`(DNAT)：**ipv4/ipv6 各一条**, src lan, src_dport 53, proto tcp+udp, **劫持到本机省略 dest_ip**；可选 853 REJECT | build | 默认关；仅拦 53，DoH(443) 拦不住, UI 诚实标注；24.10 IPv6 AAAA DNAT 有泄漏 bug, 注明。 |
| DNS 缓存状态 | 读：Go 本机发 CHAOS TXT 查询 `hits.bind/misses.bind/cachesize.bind/insertions.bind/evictions.bind`；清：`killall -HUP dnsmasq` | adapt | 真机无 dig、busybox nslookup 不支持 chaos → Go 原生 UDP 查询。仅「累计」(自 dnsmasq 启动)，昨日/今日二期(需定时快照)。 |
| 多线路 DNS（按 WAN 线路） | 无原生（单 dnsmasq 不能按出口分流；locally-generated 不受 mwan3 约束） | **降级** | 降为「域名分流DNS」：`@dnsmasq[0]` `list server '/域名/上游[@iface]'`。真·多线路列 phase2(依赖 mwan3/pbr)。 |

## 二、架构（同构 ipv6_*，独立 marker `kwrt-net-manager-dns`）

- `internal/netcfg/dns_types.go`：`DNSSettings`(单例)、`DNSDoH`(单例)、`DNSRecord`(列表, 自定义解析)、
  `DNSDomainRoute`(列表, 域名分流)、只读 `DNSCacheStats`、`DNSSvcInfo`。常量 `managedMarkerDNS`。
  - `DNSSettings` 内含旁车簿记：`SavedStock map[string]string`(改 stock 标量前的旧值)、
    `PrevServers []string`/`PrevAddresses []string`(上次写入 @dnsmasq[0] 的精确值，用于安全 delete_list)。
- `internal/netcfg/types.go` State 增 `DNS DNSSettings` / `DNSDoH` / `DNSRecords` / `DNSDomainRoutes`；CloneState 深拷贝。
- `internal/netcfg/backend.go` 接口增：DNSSettings/SaveDNSSettings、DNSDoH/SaveDNSDoH、DNSRecords/SaveDNSRecords、
  DNSDomainRoutes/SaveDNSDomainRoutes、DNSCacheStats、FlushDNSCache、DNSServiceInfo、InstallDoH。
- `dns_store.go`(旁车 CRUD + 模拟 stats/svcinfo + seed)、`dns_validate.go`、`dns_service.go`(校验/事件/导入导出)。
- `dns_uci.go`(applyDNS：@dnsmasq[0] 标量 setKVOrDel + SavedStock 回滚；server/address 列表「只删自己写过的精确值再 add」；
  hostrecord 具名节 + marker + GC；firewall redirect 具名节；DoH 经 https-dns-proxy) + `dns_uci_read.go`(CHAOS 统计/flush/探测/导入反射)。
- `internal/api/netcfg_dns.go` + 路由注册 + openapi。
- 前端 `web/src/api/dns.ts` + `web/src/pages/dns/{DnsSettings,DnsCacheStatus,DnsRecords,DnsDomainRoutes}.tsx` + App.tsx 路由 + MainLayout 导航。

## 三、安全红线（用户离线 + 生产旁路由）

1. **绝不整列表 delete @dnsmasq[0].server/address**；只 `uci -q delete_list ...='<本工具上次写过的精确值>'` 再 add_list，保留 stock/LuCI/https-dns-proxy 的条目。
2. **改 stock 标量前快照旧值入 SavedStock**；DNS 关闭/删除时回写旧值而非删 key。
3. **noresolv 前置校验**：首选 DNS 非空才允许开；同时写 `resolvfile '/tmp/resolv.conf.auto'`，避免路由器自身断 DNS。
4. **filter_aaaa 写前探测**支持；不支持置灰。
5. **DoH/劫持默认关**；DoH 端口 5053≠53、先确认代理 listen 再让 dnsmasq 指过去；劫持 ipv4/ipv6 分条、劫持到本机省略 dest_ip、默认关。
6. **隔离**：marker `kwrt-net-manager-dns` 与 v4(`kwrt-net-manager`)/v6(`kwrt-net-manager-v6`) 互不删除，`dns_uci_test.go` 含 TestDNSIsolation。
7. **默认全关**：部署后若用户未配置/未开启，applyDNS 不改任何 stock，行为与改动前一致。
