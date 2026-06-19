# 网卡多 IP + OpenWrt 接口全量对齐 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让「内外网设置」的一个接口能配主 IP + 多个附加 IP（OpenWrt 原生 `list ipaddr`），补齐全量接口字段（metric/peerdns/broadcast/force_link/auto + IPv6 单值 PD），修复 clone_mac 不写入的 bug，新建接口自动并入防火墙 zone，并加上防自锁护栏——能在真实 OpenWrt 跑通、用起来简单。

**Architecture:** 沿用 uci 后端「先存内嵌 store 旁车 → 投射 UCI」的既有模式。`NetIface` 仍以 UCI 为地址/拓扑权威（直读直写，反映真实配置），但 `SaveNetIface` 额外把整条 `NetIface` 存进内嵌 store 一份，读时按 id+地址叠加 store 里的 per-IP 备注。地址统一用 `list ipaddr`（CIDR）；clone_mac 写到 `config device` 段；防火墙 zone 并入是 uci 后端内部行为，store 后端 no-op。校验分两层：纯字段校验（`validateNetIface`）+ Service 层关系校验（跨接口冲突/池一致/删最后 LAN）。

**Tech Stack:** Go 1.25（标准库 + `pkg/netutil` 纯函数）、fake-exec 单测、React 19 + TypeScript + Vite + Ant Design 6。

**设计文档：** `docs/superpowers/specs/2026-06-19-iface-multi-ip-and-openwrt-parity-design.md`（v2）

---

## 前置：分支

- [ ] **Step 0：创建特性分支**

```bash
git checkout -b feat/iface-multi-ip
```

预期：切到新分支 `feat/iface-multi-ip`（当前在 `main`，按项目规矩不直接在 main 上做）。

---

## 文件结构（改动地图）

**后端**
- `internal/netcfg/iface_types.go` — 新增 `IfaceAddr` 类型；`NetIface` 增 `ExtraAddrs` + 全量字段（含 json tag）。
- `internal/netcfg/types.go` — `CloneState` 深拷贝 `ExtraAddrs` + `*bool`；新增 `cloneBoolPtr`。
- `internal/netcfg/iface_store.go` — `cloneIface` 深拷贝 `ExtraAddrs` + `*bool`。
- `internal/netcfg/iface.go` — 扩展 `validateNetIface`（字段层）；`Service.SaveNetIface` 加关系校验 `checkIfaceRelations`；`Service.DeleteNetIface` 加 `canDeleteNetIface`（G2）。
- `internal/netcfg/iface_uci.go` — 新增 `writeAddrList`/`writeIfaceExtraOpts`/`setBoolOptOrDel`/`ensureDeviceMAC`/`firewallZoneForRole`/`ensureZoneMembership`/`removeIfaceFromZones`；改 `SaveNetIface`/`NetIfaces`/`DeleteNetIface`/`writeBridge`。
- `internal/api/openapi.yaml` — `NetIface` schema 同步 + `IfaceAddr`。

**前端**
- `web/src/api/netcfg.ts` — `IfaceAddr` 类型 + `NetIface` 扩展字段。
- `web/src/pages/NetOverview.tsx` — 抽屉加附加 IP 列表、高级折叠分组、掩码统一 CIDR、G3 二次确认、一键预填 DHCP。

**测试**
- `internal/netcfg/iface_uci_test.go` — 扩展 fake-exec 用例。
- `internal/netcfg/iface_test.go` — 新增校验单测（若不存在则新建）。

> ⚠️ 全程 snake_case；`decodeJSON` 启用 `DisallowUnknownFields()`，前端**不得**发送 struct 里没有的 key。`netutil` 已有 `IsMAC`/`IsIPv6`/`SameSubnet`/`NetworkBase`/`MaskToPrefix`/`PrefixToMask`/`DHCPStartLimit`，**无需新增**。

---

## Task 1：数据模型 + 深拷贝

**Files:**
- Modify: `internal/netcfg/iface_types.go`（NetIface 结尾，约 :60-65 之间插字段；文件尾加 `IfaceAddr`）
- Modify: `internal/netcfg/types.go:197-200`（CloneState 的 NetIfaces 循环）+ 文件尾加 `cloneBoolPtr`
- Modify: `internal/netcfg/iface_store.go:6-9`（cloneIface）
- Test: `internal/netcfg/iface_test.go`（新建）

- [ ] **Step 1：写失败测试（clone 独立性）**

新建 `internal/netcfg/iface_test.go`：

```go
package netcfg

import "testing"

func TestCloneIfaceDeepCopy(t *testing.T) {
	tr := true
	orig := NetIface{
		ID: "lan", Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0",
		ExtraAddrs: []IfaceAddr{{Address: "10.0.0.1", Prefix: 24, Family: "ipv4", Remark: "nas", Enabled: true}},
		PeerDNS:    &tr,
	}
	c := cloneIface(orig)
	c.ExtraAddrs[0].Remark = "changed"
	*c.PeerDNS = false
	if orig.ExtraAddrs[0].Remark != "nas" {
		t.Errorf("ExtraAddrs not deep-copied: %q", orig.ExtraAddrs[0].Remark)
	}
	if orig.PeerDNS == nil || *orig.PeerDNS != true {
		t.Errorf("PeerDNS pointer aliased")
	}
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run TestCloneIfaceDeepCopy -v`
预期：编译失败（`IfaceAddr` 未定义、`NetIface` 无 `ExtraAddrs`/`PeerDNS`）。

- [ ] **Step 3：加 `IfaceAddr` 类型 + `NetIface` 字段**

在 `iface_types.go` 的 `NetIface` 结构体内，`Remark` 字段之后、`// Runtime` 之前插入：

```go
	// ExtraAddrs 是附加 IP（次地址/管理地址，可同/异子网）。投射为 list ipaddr。
	// per-IP 的 remark 旁车权威（uci 无此字段）。不发 DHCP（见设计 §4.4）。
	ExtraAddrs []IfaceAddr `json:"extra_addrs"`

	// OpenWrt 接口全量对齐（默认折叠在前端「高级」）。
	Metric    int    `json:"metric,omitempty"`     // option metric，多 WAN 优先级（越小越优先）
	PeerDNS   *bool  `json:"peerdns,omitempty"`    // option peerdns（nil=默认）
	Broadcast string `json:"broadcast,omitempty"`  // option broadcast（static 广播地址）
	ForceLink *bool  `json:"force_link,omitempty"` // option force_link（无链路也配地址）
	Auto      *bool  `json:"auto,omitempty"`       // option auto（开机自启，nil/true=默认）
	IP6Assign int    `json:"ip6assign,omitempty"`  // option ip6assign（委派前缀长度，0=不设）
	IP6Hint   string `json:"ip6hint,omitempty"`    // option ip6hint（hex 子前缀 ID）
	IP6Addr   string `json:"ip6addr,omitempty"`    // option ip6addr（单条静态 IPv6/CIDR）
	IP6Gw     string `json:"ip6gw,omitempty"`      // option ip6gw（IPv6 默认网关）
```

在文件尾追加：

```go
// IfaceAddr 是接口上的一个附加 IP。落地 OpenWrt `list ipaddr '<address>/<prefix>'`。
type IfaceAddr struct {
	Address string `json:"address"` // 点分 IPv4，如 10.0.0.1
	Prefix  int    `json:"prefix"`  // CIDR 位数，如 24
	Family  string `json:"family"`  // 本期固定 "ipv4"；预留 "ipv6"
	Remark  string `json:"remark"`  // 备注（仅旁车）
	Enabled bool   `json:"enabled"` // 关闭=不投射（本期 UI 不暴露禁用，默认 true）
}
```

- [ ] **Step 4：`cloneIface` 深拷贝（iface_store.go:6-9）**

把 `cloneIface` 改为：

```go
func cloneIface(x NetIface) NetIface {
	x.Ports = append([]string(nil), x.Ports...)
	x.ExtraAddrs = append([]IfaceAddr(nil), x.ExtraAddrs...)
	x.PeerDNS = cloneBoolPtr(x.PeerDNS)
	x.ForceLink = cloneBoolPtr(x.ForceLink)
	x.Auto = cloneBoolPtr(x.Auto)
	return x
}
```

- [ ] **Step 5：`cloneBoolPtr` + `CloneState`（types.go）**

在 `types.go` 尾部追加：

```go
// cloneBoolPtr 深拷贝一个 *bool（nil 仍 nil），避免快照间共享同一底层 bool。
func cloneBoolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
```

把 `CloneState` 中 NetIfaces 的循环（types.go:197-200）改为：

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
```

- [ ] **Step 6：运行测试通过**

Run: `go test ./internal/netcfg/ -run TestCloneIfaceDeepCopy -v`
预期：PASS。

- [ ] **Step 7：提交**

```bash
git add internal/netcfg/iface_types.go internal/netcfg/types.go internal/netcfg/iface_store.go internal/netcfg/iface_test.go
git commit -m "feat(iface): NetIface 增加附加IP与全量字段数据模型 + 深拷贝"
```

---

## Task 2：字段级校验（validateNetIface）

**Files:**
- Modify: `internal/netcfg/iface.go:183-225`（validateNetIface）
- Test: `internal/netcfg/iface_test.go`

- [ ] **Step 1：写失败测试**

在 `iface_test.go` 追加：

```go
func TestValidateNetIfaceExtraAddrs(t *testing.T) {
	base := func() NetIface {
		return NetIface{Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"}
	}
	// 合法附加 IP
	ok := base()
	ok.ExtraAddrs = []IfaceAddr{{Address: "10.0.0.1", Prefix: 24, Family: "ipv4", Enabled: true}}
	if err := validateNetIface(&ok); err != nil {
		t.Errorf("valid extra addr rejected: %v", err)
	}
	// 非法附加 IP
	bad := base()
	bad.ExtraAddrs = []IfaceAddr{{Address: "999.1.1.1", Prefix: 24, Family: "ipv4", Enabled: true}}
	if err := validateNetIface(&bad); err == nil {
		t.Error("invalid extra IP accepted")
	}
	// prefix 越界
	bp := base()
	bp.ExtraAddrs = []IfaceAddr{{Address: "10.0.0.1", Prefix: 33, Family: "ipv4", Enabled: true}}
	if err := validateNetIface(&bp); err == nil {
		t.Error("prefix 33 accepted")
	}
	// 与主 IP 重复
	dup := base()
	dup.ExtraAddrs = []IfaceAddr{{Address: "192.168.1.1", Prefix: 24, Family: "ipv4", Enabled: true}}
	if err := validateNetIface(&dup); err == nil {
		t.Error("duplicate of primary IP accepted")
	}
	// 非法 clone_mac
	mac := base()
	mac.CloneMAC = "zz:zz"
	if err := validateNetIface(&mac); err == nil {
		t.Error("invalid MAC accepted")
	}
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run TestValidateNetIfaceExtraAddrs -v`
预期：FAIL（当前 validateNetIface 不校验这些）。

- [ ] **Step 3：扩展 validateNetIface**

在 `validateNetIface` 的 `return nil` 之前（iface.go:224 附近）插入：

```go
	// 附加 IP + 去重（与主 IP、彼此）
	seen := map[string]bool{}
	if in.IPAddr != "" {
		seen[in.IPAddr] = true
	}
	for i := range in.ExtraAddrs {
		a := &in.ExtraAddrs[i]
		if a.Family == "" {
			a.Family = FamilyIPv4
		}
		if a.Family != FamilyIPv4 {
			return errors.New("附加 IP 本期仅支持 IPv4")
		}
		if !netutil.IsIPv4(a.Address) {
			return errors.New("附加 IP 地址不合法：" + a.Address)
		}
		if a.Prefix < 1 || a.Prefix > 32 {
			return errors.New("附加 IP 掩码位必须在 1-32：" + a.Address)
		}
		if seen[a.Address] {
			return errors.New("附加 IP 与已有地址重复：" + a.Address)
		}
		seen[a.Address] = true
	}
	// clone_mac 格式
	if strings.TrimSpace(in.CloneMAC) != "" && !netutil.IsMAC(in.CloneMAC) {
		return errors.New("克隆 MAC 格式不合法")
	}
	// 全量字段轻校验
	if in.Metric < 0 {
		return errors.New("线路优先级（metric）不能为负")
	}
	if in.IP6Assign < 0 || in.IP6Assign > 64 {
		return errors.New("IPv6 委派前缀长度（ip6assign）应在 0-64")
	}
	if in.IP6Addr != "" && !netutil.IsIPv6(strings.SplitN(in.IP6Addr, "/", 2)[0]) {
		return errors.New("IPv6 地址（ip6addr）不合法")
	}
	if in.IP6Gw != "" && !netutil.IsIPv6(in.IP6Gw) {
		return errors.New("IPv6 网关（ip6gw）不合法")
	}
	if in.Broadcast != "" && !netutil.IsIPv4(in.Broadcast) {
		return errors.New("广播地址不合法")
	}
```

- [ ] **Step 4：运行测试通过**

Run: `go test ./internal/netcfg/ -run TestValidateNetIfaceExtraAddrs -v`
预期：PASS。

- [ ] **Step 5：提交**

```bash
git add internal/netcfg/iface.go internal/netcfg/iface_test.go
git commit -m "feat(iface): validateNetIface 校验附加IP/MAC/IPv6/全量字段"
```

---

## Task 3：关系校验 + 删最后 LAN 护栏（Service 层 G2/G4/G8）

**Files:**
- Modify: `internal/netcfg/iface.go`（SaveNetIface 56-71、DeleteNetIface 74-82，新增两个纯函数）
- Test: `internal/netcfg/iface_test.go`

- [ ] **Step 1：写失败测试**

在 `iface_test.go` 追加：

```go
func TestCheckIfaceRelations(t *testing.T) {
	existing := []NetIface{
		{ID: "lan", Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"},
	}
	servers := []DHCPServer{
		{ID: "d_lan", Interface: "lan", IPStart: "192.168.1.100", IPEnd: "192.168.1.200"},
	}
	// 跨接口 IP 冲突
	conflict := NetIface{ID: "lan2", Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"}
	if err := checkIfaceRelations(conflict, existing, servers); err == nil {
		t.Error("cross-iface duplicate IP accepted")
	}
	// 改子网导致绑定的 DHCP 池越界（G8）
	moved := NetIface{ID: "lan", Role: RoleLAN, IPAddr: "10.9.9.1", Netmask: "255.255.255.0"}
	if err := checkIfaceRelations(moved, existing, servers); err == nil {
		t.Error("subnet change orphaning DHCP pool accepted")
	}
	// 正常新增
	ok := NetIface{ID: "lan3", Role: RoleLAN, IPAddr: "192.168.5.1", Netmask: "255.255.255.0"}
	if err := checkIfaceRelations(ok, existing, servers); err != nil {
		t.Errorf("valid iface rejected: %v", err)
	}
}

func TestCanDeleteLastLAN(t *testing.T) {
	one := []NetIface{{ID: "lan", Role: RoleLAN}}
	if err := canDeleteNetIface("lan", one); err == nil {
		t.Error("deleting the only LAN was allowed")
	}
	two := []NetIface{{ID: "lan", Role: RoleLAN}, {ID: "lan2", Role: RoleLAN}}
	if err := canDeleteNetIface("lan2", two); err != nil {
		t.Errorf("deleting one of two LANs rejected: %v", err)
	}
	withWan := []NetIface{{ID: "lan", Role: RoleLAN}, {ID: "wan", Role: RoleWAN}}
	if err := canDeleteNetIface("wan", withWan); err != nil {
		t.Errorf("deleting WAN rejected: %v", err)
	}
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run 'TestCheckIfaceRelations|TestCanDeleteLastLAN' -v`
预期：FAIL（函数未定义）。

- [ ] **Step 3：实现两个纯函数（iface.go）**

在 `iface.go` 尾部追加：

```go
// allIfaceIPs 收集一个接口的全部 IPv4（主 + 附加）。
func allIfaceIPs(in NetIface) []string {
	var out []string
	if in.IPAddr != "" {
		out = append(out, in.IPAddr)
	}
	for _, a := range in.ExtraAddrs {
		if a.Address != "" {
			out = append(out, a.Address)
		}
	}
	return out
}

// checkIfaceRelations 做需要全局视图的关系校验：跨接口 IP 冲突（G4）、
// 改子网导致绑定的 DHCP 池越界（G8）。in 为待保存项，existing 为现有接口列表。
func checkIfaceRelations(in NetIface, existing []NetIface, servers []DHCPServer) error {
	mine := map[string]bool{}
	for _, ip := range allIfaceIPs(in) {
		mine[ip] = true
	}
	for _, x := range existing {
		if x.ID == in.ID {
			continue // 同一接口（更新）跳过自身
		}
		for _, ip := range allIfaceIPs(x) {
			if mine[ip] {
				return errors.New("IP 地址 " + ip + " 已被接口 " + x.Name + " 占用")
			}
		}
	}
	// G8：本接口若改了主 IP/子网，检查绑定它的 DHCP 池是否仍在子网内
	if in.IPAddr != "" && in.Netmask != "" {
		for _, s := range servers {
			if s.Interface != in.ID {
				continue
			}
			if s.IPStart != "" && !netutil.SameSubnet(s.IPStart, in.IPAddr, in.Netmask) ||
				s.IPEnd != "" && !netutil.SameSubnet(s.IPEnd, in.IPAddr, in.Netmask) {
				return errors.New("该内网已有 DHCP 地址池（" + s.IPStart + "-" + s.IPEnd + "）不在新子网内，请先到「DHCP 服务端」调整后再改子网")
			}
		}
	}
	return nil
}

// canDeleteNetIface 实现 G2：不允许删除最后一个内网（否则失去管理入口）。
func canDeleteNetIface(id string, existing []NetIface) error {
	lanCount, isLAN := 0, false
	for _, x := range existing {
		if x.Role == RoleLAN {
			lanCount++
			if x.ID == id {
				isLAN = true
			}
		}
	}
	if isLAN && lanCount <= 1 {
		return errors.New("至少保留一个内网（LAN），否则将失去管理入口")
	}
	return nil
}
```

- [ ] **Step 4：接进 Service.SaveNetIface / DeleteNetIface**

把 `Service.SaveNetIface`（iface.go:56-71）改为（在 validate 后、保存前加关系校验）：

```go
func (s *Service) SaveNetIface(in NetIface) (NetIface, error) {
	if err := validateNetIface(&in); err != nil {
		return NetIface{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, _ := s.be.NetIfaces()
	if in.ID == "" {
		in.ID = s.nextIfaceID(in.Role)
		in.Name = in.ID
	}
	servers, _ := s.be.DHCPServers()
	if err := checkIfaceRelations(in, existing, servers); err != nil {
		return NetIface{}, err
	}
	if err := s.be.SaveNetIface(in); err != nil {
		return NetIface{}, err
	}
	s.publish(eventbus.TypeIfaceChanged, "save", 0)
	return in, nil
}
```

把 `Service.DeleteNetIface`（iface.go:74-82）改为：

```go
func (s *Service) DeleteNetIface(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, _ := s.be.NetIfaces()
	if err := canDeleteNetIface(id, existing); err != nil {
		return err
	}
	if err := s.be.DeleteNetIface(id); err != nil {
		return err
	}
	s.publish(eventbus.TypeIfaceChanged, "delete", 0)
	return nil
}
```

- [ ] **Step 5：运行测试通过**

Run: `go test ./internal/netcfg/ -run 'TestCheckIfaceRelations|TestCanDeleteLastLAN' -v`
预期：PASS。

- [ ] **Step 6：提交**

```bash
git add internal/netcfg/iface.go internal/netcfg/iface_test.go
git commit -m "feat(iface): Service 层关系校验（跨接口冲突/池越界 G8）+ 不删最后内网 G2"
```

---

## Task 4：uci 写——地址统一 list ipaddr + 持久化旁车

**Files:**
- Modify: `internal/netcfg/iface_uci.go`（新增 `writeAddrList`；改 `SaveNetIface` 的 LAN 地址段 329-333、WAN-static 地址段 347-348；函数开头加 store 持久化）
- Test: `internal/netcfg/iface_uci_test.go`

- [ ] **Step 1：写失败测试**

在 `iface_uci_test.go` 追加：

```go
func TestSaveNetIfaceMultiIP(t *testing.T) {
	// 旧接口是单 option ipaddr，升级为多 IP，必须先清残留再统一 list。
	show := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='br-lan'\nnetwork.lan.ipaddr='192.168.1.1'\nnetwork.lan.netmask='255.255.255.0'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show, "firewall": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "br-lan", Ports: []string{"eth1"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0",
		ExtraAddrs: []IfaceAddr{
			{Address: "10.0.0.1", Prefix: 24, Family: "ipv4", Enabled: true},
			{Address: "172.16.0.1", Prefix: 16, Family: "ipv4", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"delete network.lan.ipaddr",
		"delete network.lan.netmask",
		"add_list network.lan.ipaddr='192.168.1.1/24'",
		"add_list network.lan.ipaddr='10.0.0.1/24'",
		"add_list network.lan.ipaddr='172.16.0.1/16'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("multi-IP batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
	// 不能再写 option 形式
	if strings.Contains(b, "set network.lan.ipaddr=") || strings.Contains(b, "set network.lan.netmask=") {
		t.Errorf("must not write option ipaddr/netmask alongside list\n%s", b)
	}
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run TestSaveNetIfaceMultiIP -v`
预期：FAIL（仍写 `set ...ipaddr`）。

- [ ] **Step 3：新增 `writeAddrList` 助手（iface_uci.go）**

在 `iface_uci.go` 的小助手区（`setOpt` 附近）追加：

```go
// writeAddrList 把主 IP + 启用的附加 IP 统一投射为 `list ipaddr`（CIDR）。
// 先 delete 清掉任意旧的 option/list 形式（二者不能并存），再逐条 add_list。
// 主 IP 永远是第一条。
func writeAddrList(sb *strings.Builder, id string, in NetIface) {
	fmt.Fprintf(sb, "delete network.%s.ipaddr\n", id)
	fmt.Fprintf(sb, "delete network.%s.netmask\n", id)
	if in.IPAddr != "" {
		p, ok := netutil.MaskToPrefix(in.Netmask)
		if !ok {
			p = 24
		}
		fmt.Fprintf(sb, "add_list network.%s.ipaddr='%s/%d'\n", id, in.IPAddr, p)
	}
	for _, a := range in.ExtraAddrs {
		if !a.Enabled || a.Address == "" {
			continue
		}
		fmt.Fprintf(sb, "add_list network.%s.ipaddr='%s/%d'\n", id, a.Address, a.Prefix)
	}
}
```

- [ ] **Step 4：在 SaveNetIface 用 writeAddrList 替换单 IP 写法 + 开头持久化旁车**

在 `SaveNetIface`（iface_uci.go:296）函数体最开头（`id := uciName(in.ID)` 之后）加：

```go
	// 旁车权威：先把整条 NetIface（含附加 IP 备注）存进内嵌 store，再投射 UCI。
	_ = b.storeBackend.SaveNetIface(in)
```

把 LAN 分支的地址写入（iface_uci.go:329-333 的 `if in.IPAddr...` / `if in.Netmask...` 两块）整体替换为：

```go
		writeAddrList(&sb, id, in)
```

把 WAN-static 分支里的（iface_uci.go:347-348）：

```go
			setOpt(&sb, id, "ipaddr", in.IPAddr)
			setOpt(&sb, id, "netmask", in.Netmask)
```

替换为：

```go
			writeAddrList(&sb, id, in)
```

- [ ] **Step 5：运行测试通过**

Run: `go test ./internal/netcfg/ -run 'TestSaveNetIfaceMultiIP|TestSaveNetIfaceLANBridge|TestSaveNetIfaceLANSingleNIC' -v`
预期：PASS（注意旧用例 `TestSaveNetIfaceLANBridge` 断言 `set network.lan.ipaddr='192.168.9.1'` 会失效——下一步修旧断言）。

- [ ] **Step 6：更新受影响的旧断言**

`TestSaveNetIfaceLANBridge`：把 `"set network.lan.ipaddr='192.168.9.1'"` 改为 `"add_list network.lan.ipaddr='192.168.9.1/24'"`。
`TestSaveNetIfaceLANSingleNIC`：把 `"set network.lan.device='eth0'"` 保留；地址断言（若有 `set network.lan.ipaddr`）改为 `add_list network.lan.ipaddr='192.168.2.11/19'`。

Run: `go test ./internal/netcfg/ -v`
预期：全 PASS。

- [ ] **Step 7：提交**

```bash
git add internal/netcfg/iface_uci.go internal/netcfg/iface_uci_test.go
git commit -m "feat(iface): 主+附加IP统一投射 list ipaddr（CIDR）并持久化旁车"
```

---

## Task 5：uci 写——全量字段投射

**Files:**
- Modify: `internal/netcfg/iface_uci.go`（新增 `writeIfaceExtraOpts`/`setBoolOptOrDel`；在 SaveNetIface 末尾 remark 之后调用）
- Test: `internal/netcfg/iface_uci_test.go`

- [ ] **Step 1：写失败测试**

```go
func TestSaveNetIfaceFullFields(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	peer := false
	auto := true
	err := be.SaveNetIface(NetIface{
		ID: "wan", Role: RoleWAN, Proto: ProtoStatic, Device: "eth0",
		IPAddr: "1.1.1.2", Netmask: "255.255.255.0", Gateway: "1.1.1.1",
		Metric: 10, PeerDNS: &peer, Auto: &auto, Broadcast: "1.1.1.255",
		IP6Assign: 60, IP6Hint: "10", IP6Addr: "2001:db8::1/64", IP6Gw: "2001:db8::1",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"set network.wan.metric='10'",
		"set network.wan.peerdns='0'",
		"set network.wan.auto='1'",
		"set network.wan.broadcast='1.1.1.255'",
		"set network.wan.ip6assign='60'",
		"set network.wan.ip6hint='10'",
		"set network.wan.ip6addr='2001:db8::1/64'",
		"set network.wan.ip6gw='2001:db8::1'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("full-fields batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run TestSaveNetIfaceFullFields -v`
预期：FAIL。

- [ ] **Step 3：实现 helper + 调用**

在 `iface_uci.go` 助手区追加：

```go
// setBoolOptOrDel 写一个布尔型 option（nil→删除；否则 '0'/'1'）。
func setBoolOptOrDel(sb *strings.Builder, id, opt string, v *bool) {
	if v == nil {
		fmt.Fprintf(sb, "delete network.%s.%s\n", id, opt)
		return
	}
	val := "0"
	if *v {
		val = "1"
	}
	fmt.Fprintf(sb, "set network.%s.%s='%s'\n", id, opt, val)
}

// writeIfaceExtraOpts 投射 OpenWrt 接口全量对齐字段（空/0/nil 即删除回归默认）。
func writeIfaceExtraOpts(sb *strings.Builder, id string, in NetIface) {
	if in.Metric > 0 {
		fmt.Fprintf(sb, "set network.%s.metric='%d'\n", id, in.Metric)
	} else {
		fmt.Fprintf(sb, "delete network.%s.metric\n", id)
	}
	setBoolOptOrDel(sb, id, "peerdns", in.PeerDNS)
	setOptOrDel(sb, id, "broadcast", in.Broadcast)
	setBoolOptOrDel(sb, id, "force_link", in.ForceLink)
	setBoolOptOrDel(sb, id, "auto", in.Auto)
	if in.IP6Assign > 0 {
		fmt.Fprintf(sb, "set network.%s.ip6assign='%d'\n", id, in.IP6Assign)
	} else {
		fmt.Fprintf(sb, "delete network.%s.ip6assign\n", id)
	}
	setOptOrDel(sb, id, "ip6hint", in.IP6Hint)
	setOptOrDel(sb, id, "ip6addr", in.IP6Addr)
	setOptOrDel(sb, id, "ip6gw", in.IP6Gw)
}
```

在 `SaveNetIface` 里 `setOptOrDel(&sb, id, "remark", in.Remark)`（iface_uci.go:379）之后加一行：

```go
	writeIfaceExtraOpts(&sb, id, in)
```

- [ ] **Step 4：运行测试通过**

Run: `go test ./internal/netcfg/ -run TestSaveNetIfaceFullFields -v`
预期：PASS。

- [ ] **Step 5：提交**

```bash
git add internal/netcfg/iface_uci.go internal/netcfg/iface_uci_test.go
git commit -m "feat(iface): 投射 metric/peerdns/broadcast/force_link/auto + IPv6 单值字段"
```

---

## Task 6：uci 写——修复 clone_mac（写到 config device 段）

**Files:**
- Modify: `internal/netcfg/iface_uci.go`（新增 `ensureDeviceMAC`；`writeBridge` 加 managed_by；SaveNetIface 末尾调用）
- Test: `internal/netcfg/iface_uci_test.go`

- [ ] **Step 1：写失败测试**

```go
func TestSaveNetIfaceCloneMAC(t *testing.T) {
	// 单网卡直连（无 device 段）：建 dev_lan 承载 macaddr。
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "eth0", Ports: []string{"eth0"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0", CloneMAC: "AA:BB:CC:DD:EE:FF",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"set network.dev_lan=device",
		"set network.dev_lan.name='eth0'",
		"set network.dev_lan.macaddr='AA:BB:CC:DD:EE:FF'",
		"set network.dev_lan.managed_by='kwrt-net-manager'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("clone-mac batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestSaveNetIfaceCloneMACOnBridge(t *testing.T) {
	// 已是网桥（dev_lan 存在）：macaddr 写到现有 device 段，不新建。
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": sampleNetIfaceShow, "firewall": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "br-lan", Ports: []string{"eth1", "eth2"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0", CloneMAC: "AA:BB:CC:DD:EE:01",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	if !strings.Contains(b, "set network.dev_lan.macaddr='AA:BB:CC:DD:EE:01'") {
		t.Errorf("bridge clone-mac should write to dev_lan\n%s", b)
	}
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run 'TestSaveNetIfaceCloneMAC' -v`
预期：FAIL。

- [ ] **Step 3：实现 ensureDeviceMAC + writeBridge 加 managed_by + 调用**

在 `iface_uci.go` 追加：

```go
// ensureDeviceMAC 把克隆 MAC 写到接口对应的 `config device` 段（DSA 正确位置）。
// dev 已有 device 段→直接写；单网卡直连无 device 段→新建 dev_<id>（name=物理口，
// 打 managed_by）承载，interface.device 仍按名字引用该 device。mac 为空则清除。
func (b *uciBackend) ensureDeviceMAC(sb *strings.Builder, id, dev, mac string) {
	if dev == "" {
		return
	}
	devSec := b.deviceSectionByName(dev)
	if strings.TrimSpace(mac) == "" {
		if devSec != "" {
			fmt.Fprintf(sb, "delete network.%s.macaddr\n", devSec)
		}
		return
	}
	if devSec == "" {
		devSec = uciName("dev_" + id)
		fmt.Fprintf(sb, "set network.%s=device\n", devSec)
		fmt.Fprintf(sb, "set network.%s.name='%s'\n", devSec, dev)
		fmt.Fprintf(sb, "set network.%s.%s='%s'\n", devSec, managedOpt, managedMarker)
	}
	fmt.Fprintf(sb, "set network.%s.macaddr='%s'\n", devSec, mac)
}
```

在 `writeBridge`（iface_uci.go:393）创建段后，给桥 device 段也打上 managed_by——在 `fmt.Fprintf(sb, "set network.%s.name='%s'\n", sec, dev)`（:401）之后加：

```go
	fmt.Fprintf(sb, "set network.%s.%s='%s'\n", sec, managedOpt, managedMarker)
```

在 `SaveNetIface` 里，需要拿到最终 `dev`。在 LAN/WAN 分支里把所选 device 记到一个局部变量 `chosenDev`：
- LAN 三个 case 各自 `chosenDev = <dev or ports[0]>`（与写 `set network.<id>.device=` 的值一致）。
- WAN 分支 `chosenDev = dev`。

然后在 `writeIfaceExtraOpts(&sb, id, in)`（Task 5 加的那行）之后加：

```go
	b.ensureDeviceMAC(&sb, id, chosenDev, in.CloneMAC)
```

> 实现提示：最小改动是在每个写 `set network.%s.device='X'` 的地方把 `X` 同时赋给提前声明的 `chosenDev` 变量（LAN switch 上方 `var chosenDev string`）。

- [ ] **Step 4：运行测试通过**

Run: `go test ./internal/netcfg/ -run 'TestSaveNetIfaceCloneMAC|TestSaveNetIfaceLAN' -v`
预期：PASS。

- [ ] **Step 5：提交**

```bash
git add internal/netcfg/iface_uci.go internal/netcfg/iface_uci_test.go
git commit -m "fix(iface): 修复克隆MAC从不写入——投射到 config device 段(DSA)"
```

---

## Task 7：uci 写——新建接口自动并入防火墙 zone（G1）

**Files:**
- Modify: `internal/netcfg/iface_uci.go`（新增 `firewallZoneForRole`/`ensureZoneMembership`/`removeIfaceFromZones`；SaveNetIface 末尾、DeleteNetIface 内调用）
- Test: `internal/netcfg/iface_uci_test.go`

- [ ] **Step 1：写失败测试**

```go
func TestSaveNetIfaceJoinsFirewallZone(t *testing.T) {
	fw := "firewall.lanzone=zone\nfirewall.lanzone.name='lan'\nfirewall.lanzone.network='lan'\n" +
		"firewall.wanzone=zone\nfirewall.wanzone.name='wan'\nfirewall.wanzone.network='wan' 'wan6'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": fw}}
	be := newTestUCI(t, f)
	// 新建 lan2 → 自动进 lan zone
	if err := be.SaveNetIface(NetIface{ID: "lan2", Role: RoleLAN, Device: "eth3", Ports: []string{"eth3"}, IPAddr: "192.168.5.1", Netmask: "255.255.255.0"}); err != nil {
		t.Fatal(err)
	}
	fwb := f.batchContaining("commit firewall")
	if !strings.Contains(fwb, "add_list firewall.lanzone.network='lan2'") {
		t.Errorf("lan2 should join lan zone\n%s", fwb)
	}
}

func TestSaveMainLANSkipsZone(t *testing.T) {
	fw := "firewall.lanzone=zone\nfirewall.lanzone.name='lan'\nfirewall.lanzone.network='lan'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": fw}}
	be := newTestUCI(t, f)
	if err := be.SaveNetIface(NetIface{ID: "lan", Role: RoleLAN, Device: "eth1", Ports: []string{"eth1"}, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.allBatches(), "firewall.lanzone.network='lan'") {
		t.Error("main lan already in zone, must not re-add")
	}
}
```

> 若 `fakeRunner` 没有 `allBatches()`，在测试文件内用 `f.batchContaining("commit firewall")`（不命中返回空串）判断：`if f.batchContaining("commit firewall") != "" { t.Error(...) }`。先看 `iface_uci_test.go` 顶部/同包 `fakeRunner` 定义选择可用方法。

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run 'TestSaveNetIfaceJoinsFirewallZone' -v`
预期：FAIL。

- [ ] **Step 3：实现防火墙 helper + 调用**

在 `iface_uci.go` 追加：

```go
// firewallZoneForRole 按“成员含 lan/wan”定位接口角色的默认防火墙 zone 段名（非按名字）。
func (b *uciBackend) firewallZoneForRole(role string) string {
	canonical := "lan"
	if role == RoleWAN {
		canonical = "wan"
	}
	show, err := b.uciShow("firewall")
	if err != nil {
		return ""
	}
	for _, s := range parseUci(show, "firewall") {
		if s.typ != "zone" {
			continue
		}
		for _, n := range s.opts["network"] {
			if n == canonical {
				return s.name
			}
		}
	}
	return ""
}

// ensureZoneMembership 把新建独立接口 id 并入其角色默认 zone（G1）。主 lan/wan 已在
// 默认 zone，跳过；找不到 zone 则置 pending 提示。best-effort，reload 失败置 pending。
func (b *uciBackend) ensureZoneMembership(id, role string) {
	if id == "lan" || id == "wan" {
		return
	}
	zsec := b.firewallZoneForRole(role)
	if zsec == "" {
		b.pending = true
		b.pendingMsg = "接口 " + id + " 已配置，但未找到匹配的防火墙区域，请手动将其加入防火墙区域后才能转发/上网"
		return
	}
	// 已是成员则跳过
	show, _ := b.uciShow("firewall")
	for _, s := range parseUci(show, "firewall") {
		if s.name == zsec {
			for _, n := range s.opts["network"] {
				if n == id {
					return
				}
			}
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "add_list firewall.%s.network='%s'\n", zsec, id)
	sb.WriteString("commit firewall\n")
	if out, err := b.run.Run(sb.String(), "uci", "batch"); err != nil {
		b.pending = true
		b.pendingMsg = "防火墙区域更新失败：" + strings.TrimSpace(out)
		return
	}
	if initdExists("firewall") {
		if _, err := b.run.Run("", "/etc/init.d/firewall", "reload"); err != nil {
			b.pending = true
			b.pendingMsg = "已保存，但防火墙 reload 失败，请重试"
		}
	}
}

// removeIfaceFromZones 删接口时从所有 zone 的 network 列表移除该接口名（只动自己的 id）。
func (b *uciBackend) removeIfaceFromZones(id string) {
	show, err := b.uciShow("firewall")
	if err != nil {
		return
	}
	var sb strings.Builder
	changed := false
	for _, s := range parseUci(show, "firewall") {
		if s.typ != "zone" {
			continue
		}
		for _, n := range s.opts["network"] {
			if n == id {
				fmt.Fprintf(&sb, "del_list firewall.%s.network='%s'\n", s.name, id)
				changed = true
			}
		}
	}
	if !changed {
		return
	}
	sb.WriteString("commit firewall\n")
	if _, err := b.run.Run(sb.String(), "uci", "batch"); err == nil && initdExists("firewall") {
		_, _ = b.run.Run("", "/etc/init.d/firewall", "reload")
	}
}
```

在 `SaveNetIface` 的 `network reload`（iface_uci.go:385-387）之后加：

```go
	b.ensureZoneMembership(id, in.Role)
```

- [ ] **Step 4：运行测试通过**

Run: `go test ./internal/netcfg/ -run 'TestSaveNetIfaceJoinsFirewallZone|TestSaveMainLANSkipsZone' -v`
预期：PASS。

- [ ] **Step 5：提交**

```bash
git add internal/netcfg/iface_uci.go internal/netcfg/iface_uci_test.go
git commit -m "feat(iface): 新建接口自动并入防火墙 zone（G1，治“配好却不通”）"
```

---

## Task 8：uci 删除——清理 device 段 + 退出防火墙 zone

**Files:**
- Modify: `internal/netcfg/iface_uci.go`（DeleteNetIface 491-503）
- Test: `internal/netcfg/iface_uci_test.go`

- [ ] **Step 1：写失败测试**

```go
func TestDeleteNetIfaceCleansUp(t *testing.T) {
	net := "network.lan2=interface\nnetwork.lan2.proto='static'\nnetwork.lan2.device='br-lan2'\n" +
		"network.dev_lan2=device\nnetwork.dev_lan2.type='bridge'\nnetwork.dev_lan2.name='br-lan2'\nnetwork.dev_lan2.managed_by='kwrt-net-manager'\nnetwork.dev_lan2.ports='eth3'\n"
	fw := "firewall.lanzone=zone\nfirewall.lanzone.name='lan'\nfirewall.lanzone.network='lan' 'lan2'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": net, "firewall": fw}}
	be := newTestUCI(t, f)
	if err := be.DeleteNetIface("lan2"); err != nil {
		t.Fatal(err)
	}
	netb := f.batchContaining("commit network")
	if !strings.Contains(netb, "delete network.lan2") {
		t.Errorf("should delete interface\n%s", netb)
	}
	if !strings.Contains(netb, "delete network.dev_lan2") {
		t.Errorf("should delete managed device section\n%s", netb)
	}
	fwb := f.batchContaining("commit firewall")
	if !strings.Contains(fwb, "del_list firewall.lanzone.network='lan2'") {
		t.Errorf("should leave firewall zone\n%s", fwb)
	}
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run TestDeleteNetIfaceCleansUp -v`
预期：FAIL。

- [ ] **Step 3：改 DeleteNetIface**

把 `DeleteNetIface`（iface_uci.go:491-503）改为：

```go
func (b *uciBackend) DeleteNetIface(id string) error {
	id = uciName(id)
	_ = b.storeBackend.DeleteNetIface(id) // 同步旁车
	var sb strings.Builder
	fmt.Fprintf(&sb, "delete network.%s\n", id)
	// 删除本工具托管的 dev_<id> device 段（桥/克隆MAC 段），不碰 stock/手改段。
	devSec := uciName("dev_" + id)
	if b.isManagedSection("network", devSec) {
		fmt.Fprintf(&sb, "delete network.%s\n", devSec)
	}
	sb.WriteString("commit network\n")
	if out, err := b.run.Run(sb.String(), "uci", "batch"); err != nil {
		return fmt.Errorf("delete interface: %v (%s)", err, strings.TrimSpace(out))
	}
	if initdExists("network") {
		_, _ = b.run.Run("", "/etc/init.d/network", "reload")
	}
	b.removeIfaceFromZones(id)
	return nil
}

// isManagedSection 判断 config.section 是否带 managed_by=kwrt-net-manager 标记。
func (b *uciBackend) isManagedSection(config, section string) bool {
	show, err := b.uciShow(config)
	if err != nil {
		return false
	}
	for _, s := range parseUci(show, config) {
		if s.name == section && first(s.opts[managedOpt]) == managedMarker {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4：运行测试通过**

Run: `go test ./internal/netcfg/ -run TestDeleteNetIfaceCleansUp -v`
预期：PASS。

- [ ] **Step 5：提交**

```bash
git add internal/netcfg/iface_uci.go internal/netcfg/iface_uci_test.go
git commit -m "feat(iface): 删接口时清理托管 device 段并退出防火墙 zone"
```

---

## Task 9：uci 读——解析 list ipaddr + 全量字段 + clone_mac + 旁车备注叠加

**Files:**
- Modify: `internal/netcfg/iface_uci.go`（NetIfaces 190-265）
- Test: `internal/netcfg/iface_uci_test.go`

- [ ] **Step 1：写失败测试**

```go
func TestNetIfacesReadsMultiIP(t *testing.T) {
	show := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='br-lan'\n" +
		"network.lan.ipaddr='192.168.1.1/24' '10.0.0.1/24'\n" +
		"network.lan.metric='5'\nnetwork.lan.peerdns='0'\nnetwork.lan.ip6assign='60'\n" +
		"network.dev_lan=device\nnetwork.dev_lan.type='bridge'\nnetwork.dev_lan.name='br-lan'\nnetwork.dev_lan.macaddr='AA:BB:CC:DD:EE:FF'\nnetwork.dev_lan.ports='eth1'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show}}
	be := newTestUCI(t, f)
	ifaces, err := be.NetIfaces()
	if err != nil {
		t.Fatal(err)
	}
	lan := ifaces[0]
	if lan.IPAddr != "192.168.1.1" || lan.Netmask != "255.255.255.0" {
		t.Errorf("primary = %s/%s", lan.IPAddr, lan.Netmask)
	}
	if len(lan.ExtraAddrs) != 1 || lan.ExtraAddrs[0].Address != "10.0.0.1" || lan.ExtraAddrs[0].Prefix != 24 {
		t.Errorf("extra addrs = %+v", lan.ExtraAddrs)
	}
	if lan.Metric != 5 || lan.PeerDNS == nil || *lan.PeerDNS != false || lan.IP6Assign != 60 {
		t.Errorf("full fields = metric:%d peerdns:%v ip6assign:%d", lan.Metric, lan.PeerDNS, lan.IP6Assign)
	}
	if lan.CloneMAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("clone_mac from device = %q", lan.CloneMAC)
	}
}
```

- [ ] **Step 2：运行确认失败**

Run: `go test ./internal/netcfg/ -run TestNetIfacesReadsMultiIP -v`
预期：FAIL。

- [ ] **Step 3：升级 NetIfaces 读路径**

在 `NetIfaces`（iface_uci.go:215-256）的解析里做四处改动：

(a) 地址解析（替换 230-244 的 `ip := first(...)` 那段）：优先 list、回退 option，第一条为主、其余进 ExtraAddrs：

```go
		// addressing: 优先 list ipaddr（多条 CIDR），回退 option ipaddr+netmask
		addrs := s.opts["ipaddr"] // parseUci 对 option 与 list 同名都收进切片
		if len(addrs) > 0 {
			for idx, raw := range addrs {
				a, mask := raw, ""
				prefix := 0
				if j := strings.IndexByte(raw, '/'); j >= 0 {
					a = raw[:j]
					prefix = atoiSafe(raw[j+1:])
					mask = netutil.PrefixToMask(prefix)
				} else {
					mask = first(s.opts["netmask"])
					if p, ok := netutil.MaskToPrefix(mask); ok {
						prefix = p
					}
				}
				if idx == 0 {
					ni.IPAddr, ni.Netmask = a, mask
				} else {
					ni.ExtraAddrs = append(ni.ExtraAddrs, IfaceAddr{Address: a, Prefix: prefix, Family: FamilyIPv4, Enabled: true})
				}
			}
		}
```

(b) 全量字段回读（在 `ni.DefaultGW = ...`（:228）附近加）：

```go
		ni.Metric = atoiSafe(first(s.opts["metric"]))
		ni.PeerDNS = parseBoolOpt(s.opts["peerdns"])
		ni.Broadcast = first(s.opts["broadcast"])
		ni.ForceLink = parseBoolOpt(s.opts["force_link"])
		ni.Auto = parseBoolOpt(s.opts["auto"])
		ni.IP6Assign = atoiSafe(first(s.opts["ip6assign"]))
		ni.IP6Hint = first(s.opts["ip6hint"])
		ni.IP6Addr = first(s.opts["ip6addr"])
		ni.IP6Gw = first(s.opts["ip6gw"])
```

(c) clone_mac 优先从 device 段读（替换 226 行 `CloneMAC: first(s.opts["macaddr"])`，改为读完 Device 后补）：在 ports 解析（:245-250）之后加：

```go
		// clone_mac: 优先 device 段的 macaddr，回退 interface 段
		if devSec := b.deviceSectionByName(ni.Device); devSec != "" {
			if m := b.deviceMacAddr(devSec); m != "" {
				ni.CloneMAC = m
			}
		}
```

并把结构体字面量里的 `CloneMAC: first(s.opts["macaddr"]),`（:226）保留作为回退默认。

(d) 旁车备注叠加（在 `out = append(out, ni)`（:256）之前加）：

```go
		mergeExtraRemarks(&ni, b.storeBackend)
```

在 `iface_uci.go` 助手区追加：

```go
// parseBoolOpt 把 uci 选项值（"0"/"1"/缺失）转成 *bool（缺失→nil）。
func parseBoolOpt(vals []string) *bool {
	v := first(vals)
	if v == "" {
		return nil
	}
	b := v != "0"
	return &b
}

// deviceMacAddr 读某个 device 段的 macaddr 选项。
func (b *uciBackend) deviceMacAddr(section string) string {
	show, err := b.uciShow("network")
	if err != nil {
		return ""
	}
	for _, s := range parseUci(show, "network") {
		if s.name == section {
			return first(s.opts["macaddr"])
		}
	}
	return ""
}

// mergeExtraRemarks 用旁车 store 里同 id+地址的备注，回填到从 UCI 读出的附加 IP。
func mergeExtraRemarks(ni *NetIface, sb *storeBackend) {
	stored, _ := sb.NetIfaces()
	for _, s := range stored {
		if s.ID != ni.ID {
			continue
		}
		rem := map[string]string{}
		for _, a := range s.ExtraAddrs {
			if a.Remark != "" {
				rem[a.Address] = a.Remark
			}
		}
		for i := range ni.ExtraAddrs {
			if r, ok := rem[ni.ExtraAddrs[i].Address]; ok {
				ni.ExtraAddrs[i].Remark = r
			}
		}
		return
	}
}
```

> 说明：`parseUci` 会把同名 `option` 与 `list` 都聚到 `opts[key]` 切片（现有 `s.opts["dns"]`/`["ports"]` 即多值），故 `s.opts["ipaddr"]` 对单 option 是 1 条、对 list 是多条，统一处理。

- [ ] **Step 4：运行测试通过**

Run: `go test ./internal/netcfg/ -run 'TestNetIfacesReadsMultiIP|TestNetIfacesParsing|TestNetIfacesSkipsIPv6Companion' -v`
预期：全 PASS（旧 `TestNetIfacesParsing`/`SkipsIPv6Companion` 用单 option，应仍通过）。

- [ ] **Step 5：跑全包测试 + vet**

Run: `go test ./... && go vet ./...`
预期：全 PASS、无 vet 报错。

- [ ] **Step 6：提交**

```bash
git add internal/netcfg/iface_uci.go internal/netcfg/iface_uci_test.go
git commit -m "feat(iface): 读路径解析 list ipaddr/全量字段/device MAC + 旁车备注叠加"
```

---

## Task 10：API 文档同步（openapi.yaml）

**Files:**
- Modify: `internal/api/openapi.yaml`（NetIface schema）

- [ ] **Step 1：定位 NetIface schema**

打开 `internal/api/openapi.yaml`，找到 `NetIface` 的 `components.schemas`（搜 `NetIface:`）。

- [ ] **Step 2：补字段 + 新增 IfaceAddr schema**

在 `NetIface` 的 `properties` 下补：

```yaml
        extra_addrs:
          type: array
          items: { $ref: '#/components/schemas/IfaceAddr' }
        metric: { type: integer }
        peerdns: { type: boolean, nullable: true }
        broadcast: { type: string }
        force_link: { type: boolean, nullable: true }
        auto: { type: boolean, nullable: true }
        ip6assign: { type: integer }
        ip6hint: { type: string }
        ip6addr: { type: string }
        ip6gw: { type: string }
```

在 `components.schemas` 下新增：

```yaml
    IfaceAddr:
      type: object
      properties:
        address: { type: string }
        prefix: { type: integer }
        family: { type: string, enum: [ipv4, ipv6] }
        remark: { type: string }
        enabled: { type: boolean }
```

- [ ] **Step 3：校验 yaml 可解析（启动一次或 gen）**

Run: `go build ./... && go test ./internal/api/ -run TestOpenAPI -v`（若有该测试；否则 `make build-host` 启动一次确认 `/api/docs` 不报错）
预期：无解析错误。

- [ ] **Step 4：提交**

```bash
git add internal/api/openapi.yaml
git commit -m "docs(api): openapi 同步 NetIface 附加IP与全量字段"
```

---

## Task 11：前端类型同步（netcfg.ts）

**Files:**
- Modify: `web/src/api/netcfg.ts:290-313`

- [ ] **Step 1：加 IfaceAddr 类型 + 扩展 NetIface**

在 `web/src/api/netcfg.ts` 的 `export interface NetIface {`（:290）之前插：

```ts
export interface IfaceAddr {
  address: string;
  prefix: number;
  family: 'ipv4' | 'ipv6';
  remark: string;
  enabled: boolean;
}
```

在 `NetIface` 接口里（`remark: string;` 之后、`up:` 之前）插：

```ts
  extra_addrs: IfaceAddr[];
  metric?: number;
  peerdns?: boolean | null;
  broadcast?: string;
  force_link?: boolean | null;
  auto?: boolean | null;
  ip6assign?: number;
  ip6hint?: string;
  ip6addr?: string;
  ip6gw?: string;
```

> `NetIfaceInput = Omit<NetIface,'up'|'runtime_ip'>` 自动带上新字段。新字段都是可选（`?`），前端可不发——`DisallowUnknownFields` 只拒绝**未知 key**，缺 key 后端取零值/nil。

- [ ] **Step 2：类型检查**

Run: `cd web && npx tsc -b`
预期：通过（仅类型扩展，无使用点变化）。

- [ ] **Step 3：提交**

```bash
git add web/src/api/netcfg.ts
git commit -m "feat(web): netcfg.ts 同步附加IP与全量接口字段类型"
```

---

## Task 12：前端抽屉——附加 IP 列表 + 高级折叠 + 掩码 CIDR + 二次确认

**Files:**
- Modify: `web/src/pages/NetOverview.tsx`（IfaceDrawer 220-444）

- [ ] **Step 1：掩码改 CIDR 位数 + 表单初值带 extra_addrs**

把顶部 `MASKS`（:16）替换为 CIDR 选项：

```ts
const PREFIXES = [
  { v: 24, t: '/24 (255.255.255.0)' }, { v: 16, t: '/16 (255.255.0.0)' },
  { v: 8, t: '/8 (255.0.0.0)' }, { v: 25, t: '/25' }, { v: 26, t: '/26' },
  { v: 23, t: '/23' }, { v: 22, t: '/22' }, { v: 30, t: '/30' },
];
```

把 `IfaceDrawer` 里 `editing` 分支的 `form.setFieldsValue`（:232-238）改为用 prefix + 带 extra_addrs：

```ts
      form.setFieldsValue({
        id: editing.id, proto: editing.proto, ipaddr: editing.ipaddr,
        prefix: editing.netmask ? maskToPrefix(editing.netmask) : 24,
        gateway: editing.gateway, dns_primary: editing.dns_primary, dns_secondary: editing.dns_secondary,
        username: editing.username, password: editing.password, service: editing.service, ac: editing.ac,
        mtu: editing.mtu || 1500, default_gw: editing.default_gw, remark: editing.remark,
        ports: editing.ports, device: editing.device, clone_mac: editing.clone_mac,
        extra_addrs: editing.extra_addrs || [],
        metric: editing.metric || 0, peerdns: editing.peerdns ?? undefined,
        broadcast: editing.broadcast || '', force_link: editing.force_link ?? undefined,
        auto: editing.auto ?? undefined, ip6assign: editing.ip6assign || 0,
        ip6hint: editing.ip6hint || '', ip6addr: editing.ip6addr || '', ip6gw: editing.ip6gw || '',
      });
```

新增 `else` 分支初值（:240-244）加 `prefix: 24, extra_addrs: []`。

在文件顶部（`protoLabel` 附近）加掩码互转工具：

```ts
function maskToPrefix(mask: string): number {
  const m = mask.split('.').map(Number);
  if (m.length !== 4 || m.some((x) => isNaN(x))) return 24;
  return m.reduce((acc, o) => acc + (o.toString(2).match(/1/g)?.length || 0), 0);
}
function prefixToMask(p: number): string {
  const full = Math.floor(p / 8), rem = p % 8;
  const o = [0, 0, 0, 0].map((_, i) => (i < full ? 255 : i === full ? 256 - 2 ** (8 - rem) : 0));
  return o.join('.');
}
```

- [ ] **Step 2：onSave 组装新字段（含主 IP 掩码由 prefix 转点分）**

把 `onSave` 的 `body`（:257-277）改为：

```ts
    const ports: string[] = v.ports || [];
    const extra: net.IfaceAddr[] = (v.extra_addrs || [])
      .filter((a: any) => a && a.address)
      .map((a: any) => ({ address: a.address, prefix: a.prefix || 24, family: 'ipv4', remark: a.remark || '', enabled: true }));
    const body: net.NetIfaceInput = {
      id: editing?.id || v.id || '',
      name: editing?.name || v.id || '',
      role,
      proto: role === 'lan' ? 'static' : v.proto,
      device: role === 'wan' ? (ports[0] || v.device || '') : (editing?.device || ''),
      ports,
      ipaddr: v.ipaddr || '',
      netmask: v.ipaddr ? prefixToMask(v.prefix || 24) : '',
      gateway: v.gateway || '',
      dns_primary: v.dns_primary || '',
      dns_secondary: v.dns_secondary || '',
      username: v.username || '',
      password: v.password || '',
      service: v.service || '',
      ac: v.ac || '',
      mtu: v.mtu || 0,
      default_gw: !!v.default_gw,
      clone_mac: v.clone_mac || '',
      remark: v.remark || '',
      extra_addrs: extra,
      metric: v.metric || 0,
      peerdns: v.peerdns,
      broadcast: v.broadcast || '',
      force_link: v.force_link,
      auto: v.auto,
      ip6assign: v.ip6assign || 0,
      ip6hint: v.ip6hint || '',
      ip6addr: v.ip6addr || '',
      ip6gw: v.ip6gw || '',
    };
```

- [ ] **Step 3：主 IP 掩码下拉改 prefix + 附加 IP 列表 + 高级折叠**

把「子网掩码」`Form.Item`（:391-395）改为 prefix 下拉：

```tsx
            <Form.Item label="子网掩码" name="prefix">
              <Select showSearch optionFilterProp="label"
                options={PREFIXES.map((p) => ({ value: p.v, label: p.t }))} />
            </Form.Item>
```

在 IP/掩码之后插入「附加 IP」区（用 `Form.List`）：

```tsx
            <Form.Item label="附加 IP" tooltip="同接口的次地址，可同/异子网，仅作管理/路由，不发 DHCP；需发地址请新建内网口">
              <Form.List name="extra_addrs">
                {(fields, { add, remove }) => (
                  <Space direction="vertical" style={{ width: '100%' }} size={6}>
                    {fields.map(({ key, name, ...rest }) => (
                      <Space key={key} align="baseline" wrap>
                        <Form.Item {...rest} name={[name, 'address']} noStyle rules={[{ required: true, message: 'IP' }]}>
                          <Input placeholder="10.0.0.1" style={{ width: 150 }} />
                        </Form.Item>
                        <Form.Item {...rest} name={[name, 'prefix']} noStyle initialValue={24}>
                          <Select style={{ width: 110 }} options={PREFIXES.map((p) => ({ value: p.v, label: '/' + p.v }))} />
                        </Form.Item>
                        <Form.Item {...rest} name={[name, 'remark']} noStyle>
                          <Input placeholder="备注" style={{ width: 120 }} />
                        </Form.Item>
                        <DeleteOutlined onClick={() => remove(name)} />
                      </Space>
                    ))}
                    <Button type="dashed" onClick={() => add({ prefix: 24 })} icon={<PlusOutlined />} block>新增附加 IP</Button>
                  </Space>
                )}
              </Form.List>
            </Form.Item>
```

把现有 MTU / 克隆 MAC / 备注（:427-435）连同新全量字段，挪进「高级设置」折叠（在 `<Form ...>` 内末尾、提示段之前）：

```tsx
        <Collapse ghost items={[{
          key: 'adv', label: '高级设置',
          children: (
            <>
              {role === 'wan' && (
                <Form.Item label="线路优先级 (metric)" name="metric" tooltip="多 WAN 时数值越小越优先">
                  <InputNumber min={0} max={9999} style={{ width: 160 }} addonAfter={
                    <Space size={4}>
                      <a onClick={() => form.setFieldValue('metric', 0)}>主</a>
                      <a onClick={() => form.setFieldValue('metric', 100)}>备</a>
                    </Space>} />
                </Form.Item>
              )}
              <Form.Item label="使用上游下发 DNS (peerdns)" name="peerdns" tooltip="留空=默认；关闭则只用手填 DNS">
                <Select allowClear placeholder="默认" options={[{ value: true, label: '是' }, { value: false, label: '否' }]} style={{ width: 160 }} />
              </Form.Item>
              <Form.Item label="开机自启 (auto)" name="auto">
                <Select allowClear placeholder="默认(是)" options={[{ value: true, label: '是' }, { value: false, label: '否' }]} style={{ width: 160 }} />
              </Form.Item>
              <Form.Item label="无链路也配置 (force_link)" name="force_link">
                <Select allowClear placeholder="默认" options={[{ value: true, label: '是' }, { value: false, label: '否' }]} style={{ width: 160 }} />
              </Form.Item>
              <Form.Item label="广播地址" name="broadcast"><Input placeholder="192.168.1.255" /></Form.Item>
              <Form.Item label="MTU" name="mtu"><InputNumber min={576} max={9200} style={{ width: 160 }} /></Form.Item>
              <Form.Item label="克隆 MAC" name="clone_mac" tooltip="留空使用网卡原 MAC"><Input placeholder="AA:BB:CC:DD:EE:FF" /></Form.Item>
              <Form.Item label="IPv6 委派前缀 (ip6assign)" name="ip6assign" tooltip="LAN 常用 60；0=不设"><InputNumber min={0} max={64} style={{ width: 160 }} /></Form.Item>
              <Form.Item label="IPv6 子前缀提示 (ip6hint)" name="ip6hint"><Input placeholder="hex，如 10" /></Form.Item>
              <Form.Item label="静态 IPv6 (ip6addr)" name="ip6addr"><Input placeholder="2001:db8::1/64" /></Form.Item>
              <Form.Item label="IPv6 网关 (ip6gw)" name="ip6gw"><Input placeholder="2001:db8::1" /></Form.Item>
              <Form.Item label="备注" name="remark"><Input /></Form.Item>
            </>
          ),
        }]} />
```

在顶部 import 加 `Collapse`（antd）。删除原来散落的 MTU/克隆MAC/备注三个 Form.Item（已并入折叠）。

- [ ] **Step 4：G3 危险操作二次确认（改主 IP/子网时）**

把 `onSave` 开头改为：先判断是否在改「当前内网」的主 IP/子网，是则二次确认：

```ts
  const onSave = async () => {
    let v;
    try { v = await form.validateFields(); } catch { return; }
    if (editing && role === 'lan') {
      const newMask = v.ipaddr ? prefixToMask(v.prefix || 24) : '';
      if (v.ipaddr !== editing.ipaddr || newMask !== editing.netmask) {
        const go = await new Promise<boolean>((res) => {
          Modal.confirm({
            title: '更改内网管理地址',
            content: `将把 ${editing.ipaddr} 改为 ${v.ipaddr}/${v.prefix}。保存后本机网络会重载，当前页面可能短暂断连，请用新地址 ${v.ipaddr} 重新访问。确定继续？`,
            okText: '确定更改', cancelText: '取消',
            onOk: () => res(true), onCancel: () => res(false),
          });
        });
        if (!go) return;
      }
    }
    // …（继续原有 body 组装 + 保存）
```

保存成功的 message 改为：若改了主 IP，提示「已保存，若无法访问请用新地址 X 打开」。

- [ ] **Step 5：类型检查 + 构建**

Run: `cd web && npx tsc -b && npm run build`
预期：通过。

- [ ] **Step 6：提交**

```bash
git add web/src/pages/NetOverview.tsx
git commit -m "feat(web): 接口抽屉支持附加IP列表/高级折叠/掩码CIDR/改管理IP二次确认"
```

---

## Task 13：一键预填 DHCP（降低职责分离摩擦，可选增强）

**Files:**
- Modify: `web/src/pages/NetOverview.tsx`（LAN 接口抽屉底部）

- [ ] **Step 1：在 LAN 编辑抽屉加「为该内网启用 DHCP」入口**

当 `editing && role === 'lan'` 且该接口无绑定 DHCP 池时，底部加按钮，点开后跳「DHCP 服务端」页并通过 query/state 预填 `interface`/子网/默认池（`.100~.200`、网关=主 IP）。最小实现：跳转 `/#/dhcp?iface=<id>&ip=<主IP>&mask=<掩码>`，由 DHCP 页读取 query 预填新建抽屉（若改动较大可本任务仅放「去配置 DHCP」跳转按钮，预填逻辑随 DHCP 页迭代）。

```tsx
{editing && role === 'lan' && (
  <Button type="link" icon={<ThunderboltOutlined />}
    onClick={() => { window.location.hash = `#/dhcp?iface=${editing.id}`; }}>
    为该内网启用 DHCP（去 DHCP 服务端配置）
  </Button>
)}
```

- [ ] **Step 2：类型检查 + 构建**

Run: `cd web && npx tsc -b`
预期：通过。

- [ ] **Step 3：提交**

```bash
git add web/src/pages/NetOverview.tsx
git commit -m "feat(web): LAN 抽屉提供一键跳转配置 DHCP 入口"
```

> 注：若 DHCP 页预填读取 query 的改动较大，本任务可仅保留跳转按钮，预填作为后续小迭代。属可选增强，不阻塞核心功能。

---

## Task 14：整体验证（构建 + 真机冒烟）

- [ ] **Step 1：后端全量验证**

Run: `make test && go vet ./...`
预期：全 PASS、无 vet 报错。

- [ ] **Step 2：本机 store 后端端到端**

```bash
KWRTNET_API_TOKEN=dev KWRTNET_DATA_DIR=./tmp/data KWRTNET_NETCFG_BACKEND=store ./bin/kwrtmgrd serve
```
（先 `make build-host`）浏览器开前端 dev（`cd web && npm run dev`，:5173），登录 token=dev：
- 编辑一个内网，加 2 个附加 IP（同/异子网）+ 备注，保存 → 重开抽屉应回显附加 IP 与备注。
- 删除唯一 LAN → 应被拒（G2 提示）。
- 改主 IP → 弹二次确认（G3）。
- 高级里填 metric/clone_mac/ip6assign，保存回显。

- [ ] **Step 3：真机冒烟（optest，ImmortalWrt 192.168.1.12）**

用 `optest` skill 部署：
- `ip addr show br-lan` 应看到主 + 附加 IP 都挂上。
- 新建 lan2（占空闲网卡）→ `uci show firewall` 应含 `network='lan2'`；客户端能上网。
- clone_mac 填一个 → `ip link show` 对应 device MAC 已变。
- 改内网子网且有越界 DHCP 池 → 被 G8 拒绝。

- [ ] **Step 4：合并/收尾**

按 `superpowers:finishing-a-development-branch` 决定合并 PR 或留分支；提交前确认 `make test`/`go vet`/`tsc -b` 全绿（事实为准，§验证）。

---

## 自查（Self-Review）

**Spec 覆盖：** §4 数据模型→Task1；§5.1 list ipaddr→Task4；§5.2 全量字段→Task5；§5.3 clone_mac→Task6；§5.4 防火墙 zone→Task7；§5.5 读/删除→Task8/9；§6 护栏 G1→Task7、G2→Task3、G3→Task12、G4/G8→Task3、G5→Task12（主 IP 固定行）、G7→Task2；§7 校验分层→Task2/3；§8 前端→Task11/12/13；§9 openapi/store→Task10/Task1/4；§10 测试→各 Task + Task14。**全覆盖。**

**类型一致性：** `IfaceAddr`(address/prefix/family/remark/enabled) 在 Go(Task1)、TS(Task11)、openapi(Task10) 三处字段一致；`writeAddrList`/`ensureDeviceMAC`/`firewallZoneForRole`/`ensureZoneMembership`/`removeIfaceFromZones`/`checkIfaceRelations`/`canDeleteNetIface`/`parseBoolOpt`/`mergeExtraRemarks`/`setBoolOptOrDel`/`writeIfaceExtraOpts`/`cloneBoolPtr` 命名前后一致。

**无占位符：** 各步含可运行代码与命令。Task13 标注为可选增强（预填若复杂可降级为跳转），已显式说明，非占位。
