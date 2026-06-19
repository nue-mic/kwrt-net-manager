# 接口 OpenWrt 全量对齐（续）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development。步骤用 `- [ ]`。
> 续接已合并的「网卡多 IP」特性（`2026-06-19-iface-multi-ip.md`）。本轮只做 **OpenWrt 真实支持**的剩余项；不支持的不做。

**Goal:** 补齐 OpenWrt 支持的剩余接口能力——多条 IPv6 地址（`list ip6addr`，真机已验证 ImmortalWrt 24.10 支持）、PPPoE keepalive/IPv6、ip6prefix/ip6ifaceid，以及一键 DHCP 预填池、openapi 端点文档。能在真机跑通、用起来简单。

**不做**：alias 同物理口第二子网发 DHCP（OpenWrt 已知不可靠 #15159）、apply-rollback（非 OpenWrt 原生）、ip6class（过冷门）、完整 VLAN/防火墙管理页（独立大子系统）。

**Architecture:** 与已合并特性对称。`ExtraAddrs` 既有 `family` 字段——`family=ipv6` 的项投射为 `list ip6addr`（与 `list ipaddr` 平行）。主 IPv6 仍是 `IP6Addr`（作 list ip6addr 第一条）。新单值字段走 `setOptOrDel`。DHCP 预填走前端跳转带 query + DhcpServers 页读 query 开预填抽屉（复用现有 createDHCPServer，无新后端）。

**Tech Stack:** Go（标准库 + netutil）、fake-exec 单测、React 19 + TS + Vite + AntD 6。真机 optest @192.168.1.12。

---

## 文件结构
- `internal/netcfg/iface_types.go` — NetIface 加 `IP6Prefix/IP6IfaceID/Keepalive string` + `PPPoEv6 *bool`；`IfaceAddr.Family` 注释更新（支持 ipv6）。
- `internal/netcfg/iface.go` — `validateNetIface` 支持 ipv6 附加地址 + 新字段校验。
- `internal/netcfg/types.go` / `iface_store.go` — `cloneBoolPtr`/`cloneIface` 增 `PPPoEv6` 深拷贝。
- `internal/netcfg/iface_uci.go` — 新增 `writeAddr6List`；`writeIfaceExtraOpts` 移除 ip6addr（改由 writeAddr6List）、加 ip6prefix/ip6ifaceid；PPPoE 分支加 keepalive/ipv6；读路径解析 list ip6addr + 新字段。
- `internal/api/openapi.yaml` — NetIface 加新字段 + 补 `/api/v1/ifaces` 系列 paths。
- `web/src/api/netcfg.ts` — NetIface 加新字段（可选）。
- `web/src/pages/NetOverview.tsx` — 附加 IP 行加 IPv4/IPv6 family 选择；高级加 ip6prefix/ip6ifaceid/keepalive/pppoe-ipv6；一键 DHCP 跳转带 query。
- `web/src/pages/DhcpServers.tsx` — 读 query（iface/ip/mask）开预填新建抽屉。

> 约束沿用：snake_case；`DisallowUnknownFields`（新字段前端可选、不发未知 key）；netutil 已有 `IsIPv6/IsIPv4/IsMAC/MaskToPrefix/PrefixToMask`；`reload` 不 `restart`；Bash 工具是 Git Bash。**真机已确认 `list ip6addr` 多地址生效。**

---

## Task 1：数据模型 + 校验（IPv6 附加地址 + 新字段）

**Files:** `iface_types.go`、`iface.go`、`types.go`、`iface_store.go`、`iface_test.go`

- [ ] **Step 1：失败测试**（`iface_test.go` 追加）

```go
func TestValidateNetIfaceIPv6Extra(t *testing.T) {
	base := NetIface{Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"}
	ok := base
	ok.ExtraAddrs = []IfaceAddr{{Address: "2001:db8::1", Prefix: 64, Family: "ipv6", Enabled: true}}
	if err := validateNetIface(&ok); err != nil {
		t.Errorf("valid ipv6 extra rejected: %v", err)
	}
	bad := base
	bad.ExtraAddrs = []IfaceAddr{{Address: "2001:db8::1", Prefix: 129, Family: "ipv6", Enabled: true}}
	if err := validateNetIface(&bad); err == nil {
		t.Error("ipv6 prefix 129 accepted")
	}
	badip := base
	badip.ExtraAddrs = []IfaceAddr{{Address: "192.168.1.5", Prefix: 64, Family: "ipv6", Enabled: true}}
	if err := validateNetIface(&badip); err == nil {
		t.Error("ipv4 address with family ipv6 accepted")
	}
}
```

- [ ] **Step 2：跑确认失败** `go test ./internal/netcfg/ -run TestValidateNetIfaceIPv6Extra -v`

- [ ] **Step 3：实现**
  - `iface_types.go` NetIface 在 IPv6 单值字段后加：
    ```go
    IP6Prefix  string `json:"ip6prefix,omitempty"`  // option ip6prefix（向下游分发的前缀 CIDR）
    IP6IfaceID string `json:"ip6ifaceid,omitempty"` // option ip6ifaceid（接口 ID 后缀）
    Keepalive  string `json:"keepalive,omitempty"`  // PPPoE option keepalive（如 "5 25"）
    PPPoEv6    *bool  `json:"pppoe_ipv6,omitempty"` // PPPoE 上启用 IPv6（option ipv6 '1'）
    ```
    `IfaceAddr.Family` 注释改为「"ipv4" | "ipv6"」。
  - `iface.go` `validateNetIface` 的 ExtraAddrs 循环改为按 family 分发校验：
    ```go
    switch a.Family {
    case "", FamilyIPv4:
        a.Family = FamilyIPv4
        if !netutil.IsIPv4(a.Address) { return errors.New("附加 IPv4 地址不合法：" + a.Address) }
        if a.Prefix < 1 || a.Prefix > 32 { return errors.New("附加 IPv4 掩码位需在 1-32：" + a.Address) }
    case FamilyIPv6:
        if !netutil.IsIPv6(a.Address) { return errors.New("附加 IPv6 地址不合法：" + a.Address) }
        if a.Prefix < 1 || a.Prefix > 128 { return errors.New("附加 IPv6 前缀需在 1-128：" + a.Address) }
    default:
        return errors.New("附加 IP family 必须是 ipv4 或 ipv6")
    }
    ```
    去重仍按 Address 字符串（v4/v6 不会撞）。新字段：`IP6Prefix` 非空校验含 `/` 的 IPv6 CIDR（`netutil.IsIPv6(split[0])`）；其余 string 不强校验，`PPPoEv6 *bool` 无需校验。
  - `types.go` CloneState 循环 + `iface_store.go` cloneIface 增 `PPPoEv6 = cloneBoolPtr(src.PPPoEv6)`。

- [ ] **Step 4：跑通** `go test ./internal/netcfg/ -run 'TestValidateNetIface' -v` 全绿
- [ ] **Step 5：提交** `git commit -m "feat(iface): 支持 IPv6 附加地址校验 + ip6prefix/ip6ifaceid/keepalive/pppoe-ipv6 字段"`

---

## Task 2：uci 写——list ip6addr + 新字段投射

**Files:** `iface_uci.go`、`iface_uci_test.go`

- [ ] **Step 1：失败测试**

```go
func TestSaveNetIfaceMultiIPv6(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "eth0", Ports: []string{"eth0"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0",
		IP6Addr: "2001:db8::1/64",
		ExtraAddrs: []IfaceAddr{
			{Address: "10.0.0.1", Prefix: 24, Family: "ipv4", Enabled: true},
			{Address: "fd00::1", Prefix: 64, Family: "ipv6", Enabled: true},
			{Address: "fd11::1", Prefix: 64, Family: "ipv6", Enabled: true},
		},
	})
	if err != nil { t.Fatal(err) }
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"delete network.lan.ip6addr",
		"add_list network.lan.ip6addr='2001:db8::1/64'",
		"add_list network.lan.ip6addr='fd00::1/64'",
		"add_list network.lan.ip6addr='fd11::1/64'",
		"add_list network.lan.ipaddr='192.168.1.1/24'",
		"add_list network.lan.ipaddr='10.0.0.1/24'",
	} {
		if !strings.Contains(b, w) { t.Errorf("ipv6 batch missing %q\n%s", w, b) }
	}
	// 不得再写 option ip6addr
	if strings.Contains(b, "set network.lan.ip6addr=") { t.Errorf("must not write option ip6addr with list\n%s", b) }
}

func TestSaveNetIfacePPPoEv6Keepalive(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	v6 := true
	if err := be.SaveNetIface(NetIface{ID: "wan", Role: RoleWAN, Proto: ProtoPPPoE, Device: "eth1",
		Username: "u", Password: "p", Keepalive: "5 25", PPPoEv6: &v6}); err != nil { t.Fatal(err) }
	b := f.batchContaining("commit network")
	for _, w := range []string{"set network.wan.keepalive='5 25'", "set network.wan.ipv6='1'"} {
		if !strings.Contains(b, w) { t.Errorf("pppoe batch missing %q\n%s", w, b) }
	}
}
```

- [ ] **Step 2：跑确认失败**

- [ ] **Step 3：实现**
  - 新增 `writeAddr6List`（与 `writeAddrList` 平行）：
    ```go
    // writeAddr6List 把主 IPv6(IP6Addr) + 启用的 ipv6 附加地址统一投射为 list ip6addr。
    // 有任何 ipv6 地址时先 delete 清掉 option/list 旧形式，再逐条 add_list（主 IPv6 第一条）。
    func writeAddr6List(sb *strings.Builder, id string, in NetIface) {
        var v6 []string
        if strings.TrimSpace(in.IP6Addr) != "" { v6 = append(v6, in.IP6Addr) } // 已是 CIDR
        for _, a := range in.ExtraAddrs {
            if a.Family == FamilyIPv6 && a.Enabled && a.Address != "" {
                v6 = append(v6, fmt.Sprintf("%s/%d", a.Address, a.Prefix))
            }
        }
        fmt.Fprintf(sb, "delete network.%s.ip6addr\n", id)
        for _, a := range v6 {
            fmt.Fprintf(sb, "add_list network.%s.ip6addr='%s'\n", id, a)
        }
    }
    ```
  - `writeIfaceExtraOpts`：**删除**其中的 `setOptOrDel(sb,id,"ip6addr",in.IP6Addr)` 一行（改由 writeAddr6List 处理）；**新增** `setOptOrDel(sb,id,"ip6prefix",in.IP6Prefix)` 与 `setOptOrDel(sb,id,"ip6ifaceid",in.IP6IfaceID)`。
  - `SaveNetIface`：在 LAN 分支 `writeAddrList(&sb,id,in)` 后、WAN-static 分支 `writeAddrList` 后，各加 `writeAddr6List(&sb,id,in)`。（LAN 与 WAN-static 都支持静态 IPv6。）
  - PPPoE 分支（`case ProtoPPPoE`）末尾加：`setOptOrDel(&sb,id,"keepalive",in.Keepalive)` 和 `setBoolOptOrDel(&sb,id,"ipv6",in.PPPoEv6)`。
  - 注意：`writeIfaceExtraOpts` 末尾仍会处理 ip6assign/ip6hint/ip6gw（保留）。确保 ip6addr 不再被它写。

- [ ] **Step 4：跑通**（含旧测试不回归）`go test ./internal/netcfg/ -v`
- [ ] **Step 5：提交** `git commit -m "feat(iface): 投射 list ip6addr 多IPv6 + ip6prefix/ip6ifaceid + PPPoE keepalive/ipv6"`

---

## Task 3：uci 读——解析 list ip6addr + 新字段

**Files:** `iface_uci.go`、`iface_uci_test.go`

- [ ] **Step 1：失败测试**

```go
func TestNetIfacesReadsMultiIPv6(t *testing.T) {
	show := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='eth0'\n" +
		"network.lan.ipaddr='192.168.1.1/24'\n" +
		"network.lan.ip6addr='2001:db8::1/64' 'fd00::1/64'\n" +
		"network.lan.ip6prefix='2001:db80:1::/48'\nnetwork.lan.ip6ifaceid='::1'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show}}
	be := newTestUCI(t, f)
	ifaces, _ := be.NetIfaces()
	lan := ifaces[0]
	if lan.IP6Addr != "2001:db8::1/64" { t.Errorf("primary v6 = %q", lan.IP6Addr) }
	v6 := 0
	for _, a := range lan.ExtraAddrs { if a.Family == FamilyIPv6 { v6++; if a.Address != "fd00::1" || a.Prefix != 64 { t.Errorf("v6 extra=%+v", a) } } }
	if v6 != 1 { t.Errorf("want 1 ipv6 extra, got %d (%+v)", v6, lan.ExtraAddrs) }
	if lan.IP6Prefix != "2001:db80:1::/48" || lan.IP6IfaceID != "::1" { t.Errorf("ip6prefix=%q ip6ifaceid=%q", lan.IP6Prefix, lan.IP6IfaceID) }
}
```

- [ ] **Step 2：跑确认失败**

- [ ] **Step 3：实现**（`NetIfaces()` 读路径）
  - IPv6 地址解析：读 `s.opts["ip6addr"]`（option+list 统一切片），第一条 → `ni.IP6Addr`（保留 CIDR 原样），其余 → `ni.ExtraAddrs` 追加 `{Address, Prefix, Family: FamilyIPv6, Enabled: true}`（按 `/` 拆 Address 与 Prefix；无 `/` 时 Prefix=128）。放在现有 ipv4 ipaddr 解析之后。
  - 新字段回读：`ni.IP6Prefix = first(s.opts["ip6prefix"])`、`ni.IP6IfaceID = first(s.opts["ip6ifaceid"])`、`ni.Keepalive = first(s.opts["keepalive"])`、`ni.PPPoEv6 = parseBoolOpt(s.opts["ipv6"])`。
  - 注意现有 `writeIfaceExtraOpts` 读对应物在 NetIfaces 里是逐字段 `first(...)`；ip6addr 改为上面的多值解析（不要再 `first(s.opts["ip6addr"])` 单值覆盖）。

- [ ] **Step 4：跑通** `go test ./internal/netcfg/ -v` 全绿；`go test ./... && go vet ./...`
- [ ] **Step 5：提交** `git commit -m "feat(iface): 读路径解析 list ip6addr 多地址 + ip6prefix/ip6ifaceid/keepalive/ipv6"`

---

## Task 4：openapi 同步（新字段 + 补 ifaces 端点文档）

**Files:** `internal/api/openapi.yaml`

- [ ] **Step 1：NetIface schema 加新字段**：`ip6prefix`/`ip6ifaceid`/`keepalive`(string)、`pppoe_ipv6`(boolean,nullable)；`IfaceAddr.family` enum 已含 ipv6。
- [ ] **Step 2：补 paths**（照本文件现有 path 写法/缩进）：`GET/POST /api/v1/ifaces`、`GET/PUT/DELETE /api/v1/ifaces/{id}`、`POST /api/v1/ifaces/{id}/action`、`GET /api/v1/nics`、`GET /api/v1/netcfg/overview`，请求/响应引用 `NetIface`/`IfaceAddr`/`NIC`/`NetOverview`（NIC/NetOverview 若无 schema 一并补最小定义）。先读 `internal/api/netcfg_iface.go` 确认方法/路径/请求体形状再写。
- [ ] **Step 3：校验可解析** `go build ./...`；`python -c "import yaml;yaml.safe_load(open('internal/api/openapi.yaml',encoding='utf-8'))" && echo OK`
- [ ] **Step 4：提交** `git commit -m "docs(api): openapi 补 ifaces/nics 端点与 IPv6/PPPoE 新字段"`

---

## Task 5：前端类型（netcfg.ts）

**Files:** `web/src/api/netcfg.ts`

- [ ] **Step 1：NetIface 加** `ip6prefix?: string; ip6ifaceid?: string; keepalive?: string; pppoe_ipv6?: boolean | null;`（`IfaceAddr.family` 已是 `'ipv4'|'ipv6'`）。
- [ ] **Step 2：** `cd web && npx tsc -b`（exit 0）
- [ ] **Step 3：提交** `git commit -m "feat(web): netcfg.ts 同步 IPv6/PPPoE 新字段"`

---

## Task 6：前端抽屉（附加 IP 支持 IPv6 + 新高级字段）

**Files:** `web/src/pages/NetOverview.tsx`

- [ ] **Step 1：附加 IP 行加 family 选择**。每行前加 IPv4/IPv6 下拉（默认 ipv4）；prefix 下拉项随 family 变（ipv4: PREFIXES；ipv6: 常用 /64 /48 /56 /128 等）。onSave 组装 extra_addrs 时 `family` 取该行选择（不再固定 'ipv4'），`enabled:true`。区顶提示同时支持 IPv4/IPv6 附加地址、均不发 DHCP。
- [ ] **Step 2：高级面板加字段**：静态 IPv6（`ip6addr`，主 IPv6/CIDR，单条）、`ip6prefix`、`ip6ifaceid`；PPPoE 分支（proto=pppoe）下加 `keepalive`（placeholder "5 25"）与「PPPoE 启用 IPv6」(`pppoe_ipv6`，可清空 Select：是/否/默认)。回填 editing 对应值。
- [ ] **Step 3：onSave body** 带上 `ip6prefix/ip6ifaceid/keepalive/pppoe_ipv6`；`ip6addr` 已有则保留。确认 body 不含后端没有的 key。
- [ ] **Step 4：** `cd web && npx tsc -b && npm run build`（exit 0）
- [ ] **Step 5：提交** `git commit -m "feat(web): 附加IP支持IPv6 + 高级补 ip6prefix/ip6ifaceid/PPPoE keepalive/ipv6"`

---

## Task 7：一键 DHCP 预填池

**Files:** `web/src/pages/NetOverview.tsx`、`web/src/pages/DhcpServers.tsx`

- [ ] **Step 1：NetOverview 跳转带 query**。把「为该内网启用 DHCP」改为 `navigate('/dhcp/servers?iface=' + editing.id + '&ip=' + editing.ipaddr + '&mask=' + editing.netmask)`。
- [ ] **Step 2：DhcpServers 读 query 开预填抽屉**。先读 `DhcpServers.tsx` 现有新建抽屉/表单逻辑与 `createDHCPServer` 字段。用 `useSearchParams()` 读 iface/ip/mask；若存在则打开新建抽屉并预填：`interface=iface`、由 ip/mask 推默认池（如网段 `.100`~`.200`、网关=ip、netmask=mask）。读完用 `setSearchParams({})` 清掉参数避免重复触发。务必对齐后端 `DHCPServer` 字段（snake_case，先读 `internal/netcfg/types.go` DHCPServer 与 `web/src/api/netcfg.ts` 的 DHCP 类型）。
- [ ] **Step 3：** `cd web && npx tsc -b && npm run build`（exit 0）
- [ ] **Step 4：提交** `git commit -m "feat(web): 一键DHCP预填（NetOverview跳转带网段, DhcpServers读query开预填抽屉)"`

---

## Task 8：整体验证
- [ ] `go test ./... && go vet ./...` 全绿
- [ ] `cd web && npx tsc -b && npm run build` 全绿
- [ ] 真机 optest（独立 :18080 实例，不动生产 :8443）：① 给 lan 加 IPv4+IPv6 附加地址 → `ip addr`/`ip -6 addr` 双族生效 → 还原；② GET ifaces 读回含 ipv6 extras/新字段；③ 一键 DHCP 预填路径 UI 自查。
- [ ] 收尾 `superpowers:finishing-a-development-branch`。

---

## 自查
- Spec：多 IPv6(list ip6addr)→T2/T3；ip6prefix/ip6ifaceid/keepalive/pppoe_ipv6→T1/T2/T3/T6；openapi→T4；前端→T5/T6；DHCP 预填→T7。
- 类型一致：`writeAddr6List`、`PPPoEv6`(json `pppoe_ipv6`)、`IP6Prefix`(ip6prefix)、`IP6IfaceID`(ip6ifaceid)、`Keepalive`(keepalive) 前后端/openapi 三处一致。
- 不支持项明确不做（alias-DHCP / apply-rollback / ip6class / VLAN / 防火墙页）。
