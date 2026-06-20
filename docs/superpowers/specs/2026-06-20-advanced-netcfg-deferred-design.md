# 进阶网络功能设计（顺延实现，待评审）

> 2026-06-20。本轮自主已完成并推送：DNS 防重绑定白名单、域名分流多上游、策略路由(config rule)+路由表。
> 本文给出两个**顺延项**的 OpenWrt 原生设计，因风险/冲突不在无人值守期间实现，留待评审后落地。

---

## A3. DHCPv6 / RA 控制融入 DHCP 服务端

**OpenWrt 机制**：`config dhcp` 段除 IPv4 选项外，还支持 `dhcpv6`(disabled/server/relay)、`ra`(disabled/server/relay)、`ra_management`(0/1/2)、`ra_slaac`、`ra_default` 等（真机 `dhcp.lan` 已见这些 option）。

**为何顺延（冲突）**：本项目现有独立 IPv6 模块（LANv6/WANv6/前缀静态/ACLv6）**已经在管理同一个 `config dhcp` 段的 dhcpv6/ra 选项**。若再在「DHCP 服务端」表单写这些 option，会与 IPv6 模块**双写冲突**（两边各自 apply 互相覆盖）。

**正确做法（需重构，非简单加字段）**：二选一——
1. **单一权威**：把 RA/DHCPv6 的投射收敛到一个模块（建议保留在 IPv6 模块），DHCP 服务端页只读展示该接口的 IPv6 状态 + 一个「去 IPv6 设置」跳转链接（零冲突、最小改动）。
2. **合并页**：把 IPv6 内网(LANv6) 的 DHCPv6/RA 控制以「IPv6」分页形式嵌进 DHCP 服务端编辑抽屉，底层仍走 IPv6 模块的 SaveLANv6，避免双写。

**推荐**：方案 1（只读展示 + 跳转），改动小、零冲突、符合「简单易懂」。

---

## B5. 域名 → 指定线路分流（dnsmasq nftset + fwmark + ip rule）

即爱快「应用分流 / 域名分流」：让访问某些域名的流量走指定 WAN/线路。**OpenWrt 原生可行**（真机已确认：dnsmasq 编译含 `nftset`、`nft`/`fw4` 在位、`table inet dnsmasq` 存在、`ip rule` 可用）。

### 机制（4 系统联动）
1. **dnsmasq 填集合**：`nftset=/域名/4#inet#fw4#<set>`（IPv4）/ `6#inet#fw4#<set6>`（IPv6）——dnsmasq 解析该域时把结果 IP 写进 nft 集合 `<set>`。
2. **防火墙打标**：在 `inet fw4` 的 mangle/prerouting 链加 `ip daddr @<set> meta mark set 0x<m>`（fw4 用 `config rule`/`option set_mark` 或自管 nft 片段）。
3. **策略路由**：`ip rule add fwmark 0x<m> lookup <table>`（复用本轮已实现的 PolicyRule，加 `mark` 字段）。
4. **线路表**：`<table>` 内放默认路由 `default via <线路网关>`（复用本轮已实现的「带 table 的静态路由」）。

### 数据模型（建议）
```
type DomainSplit struct {
  ID, Family    string
  Domains       []string   // 走该线路的域名（含子域）
  OutInterface  string     // 目标线路（WAN 接口）
  Enabled       bool
  Remark        string
  // 内部自动分配：nft set 名、fwmark、table 号
}
```
后端为每条 DomainSplit 自动协调：nft 集合 + dnsmasq nftset 列表 + fw4 打标规则 + PolicyRule(fwmark→table) + 该 table 的默认路由（via 选定线路网关）。

### 为何顺延（风险）
- 要写 **fw4/nftables 规则**：投射有误可能**断掉连通性甚至 SSH**，必须有人盯着真机测、能即时回滚——不适合无人值守自主跑。
- 涉及 nft 集合生命周期、fw4 重载时机、fwmark 不撞 mwan3/其它标记、dnsmasq restart 顺序等真机细节，需逐一在真机验证。

### 落地建议（分阶段、低风险优先）
1. **阶段 1**：只做 dnsmasq `nftset=/域名/...` + 自管一个**独立 nft 表**（不动 fw4 现有规则），把域名 IP 收集进集合——零连通性风险，可先验证 dnsmasq 侧。
2. **阶段 2**：加 fwmark 打标 + PolicyRule(mark) + 线路表，**在测试机有人监督下**逐步打通；用独立 chain/table 限制爆炸半径，base 连通性（main 表 + SSH 放行）始终不动。
3. UI：一个「域名分流」页——填域名列表 + 选目标线路即可，底层全自动；保留手动（高级可改 fwmark/table）。

---

## 结论
- A3：建议方案 1（只读+跳转），下次小改即可。
- B5：原生可行、价值高，但需**有人监督的真机分阶段实施**，故本轮只设计不实现。
