package netcfg

import (
	"fmt"
	"strings"
)

// uci 后端的 IPv6 写投射。两种托管策略，刻意与 IPv4 apply() 隔离：
//
//   - WANv6 外网接口 / 前缀静态分配 host / ACLv6(DUID拒发) host：本工具新建的
//     独立具名节，用 option managed_by='kwrt-net-manager-v6'（独立于 IPv4 的
//     managed_by 值），keep 集合整节增删 —— IPv4 apply 不会误删，反之亦然。
//   - LANv6：借用 stock 的 lan 接口（ip6assign/ip6class/ip6hint）与 dhcp.<lan>
//     节（dhcpv6/ra/ra_management/…），只做选项级 set/disable，用一个轻量标记
//     option managed_v6='1' 追踪我们碰过的 dhcp 节，删除时把 v6 服务设为
//     disabled 而不删节，绝不动 stock 的 ip6assign 默认。
//
// commit 与 reload 分阶段：reload network + reload odhcpd（不 restart）。
const (
	managedMarkerV6 = "kwrt-net-manager-v6" // WANv6/前缀/ACL host 的整节标记
	managedOptV6    = "managed_v6"          // LANv6 借用 dhcp 节的轻量标记
)

// ---- write overrides：存旁车 → 投射到 UCI ----

func (b *uciBackend) SaveWANv6s(list []WANv6) error {
	if err := b.storeBackend.SaveWANv6s(list); err != nil {
		return err
	}
	return b.applyIPv6()
}

func (b *uciBackend) SaveLANv6s(list []LANv6) error {
	if err := b.storeBackend.SaveLANv6s(list); err != nil {
		return err
	}
	return b.applyIPv6()
}

func (b *uciBackend) SavePrefixStaticsV6(list []PrefixStaticV6) error {
	if err := b.storeBackend.SavePrefixStaticsV6(list); err != nil {
		return err
	}
	return b.applyIPv6()
}

func (b *uciBackend) SaveACLv6(acl ACLv6) error {
	if err := b.storeBackend.SaveACLv6(acl); err != nil {
		return err
	}
	return b.applyIPv6()
}

// ---- applyIPv6：把 IPv6 旁车状态投射进 UCI ----

func (b *uciBackend) applyIPv6() error {
	b.applyMu.Lock()
	defer b.applyMu.Unlock()

	wans, _ := b.storeBackend.WANv6s() // 显式走 store：uci override 了 WANv6s（会富化/递归）
	lans, _ := b.storeBackend.LANv6s()
	prefixes, _ := b.PrefixStaticsV6()
	acl, _ := b.ACLv6()

	var nb, db strings.Builder // network / dhcp batch
	keepNet := map[string]bool{}
	keepDhcp := map[string]bool{}
	keepLan := map[string]bool{}

	for _, w := range wans {
		if !w.Managed || !w.Enabled {
			continue // 停用/导入未编辑 → 不投射，仅留旁车
		}
		id := uciName(w.ID)
		keepNet[id] = true
		projectWANv6(&nb, id, w)
	}
	for _, l := range lans {
		if !l.Managed {
			continue
		}
		id := uciName(l.Interface)
		keepLan[id] = true
		projectLANv6(&nb, &db, id, l)
	}
	for _, p := range prefixes {
		if !p.Managed || !p.Enabled {
			continue
		}
		id := uciName(p.ID)
		keepDhcp[id] = true
		projectPrefixV6(&db, id, p)
	}
	if acl.Mode == ACLBlacklist {
		for _, e := range acl.Entries {
			if !e.Enabled || !e.Managed || e.Method != ACLv6MethodDUID {
				continue // L2/whitelist 不投射（OpenWrt 原生不支持，详见设计文档）
			}
			id := uciName(e.ID)
			keepDhcp[id] = true
			projectACLv6DUID(&db, id, e)
		}
	}

	// 孤儿清理：删除本工具曾建、现已不在 keep 的 v6 具名节；停用不再管理的 LANv6。
	for n := range b.managedNamesV6("network") {
		if !keepNet[n] {
			fmt.Fprintf(&nb, "delete network.%s\n", n)
		}
	}
	for n := range b.managedNamesV6("dhcp") {
		if !keepDhcp[n] {
			fmt.Fprintf(&db, "delete dhcp.%s\n", n)
		}
	}
	for n := range b.borrowedV6Names("dhcp") {
		if !keepLan[n] {
			disableLANv6(&nb, &db, n)
		}
	}

	nb.WriteString("commit network\n")
	db.WriteString("commit dhcp\n")

	var firstErr error
	if out, err := b.run.Run(nb.String(), "uci", "batch"); err != nil {
		firstErr = fmt.Errorf("uci batch network(v6): %v (%s)", err, strings.TrimSpace(out))
	}
	if out, err := b.run.Run(db.String(), "uci", "batch"); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("uci batch dhcp(v6): %v (%s)", err, strings.TrimSpace(out))
	}

	// reload 分阶段（不 restart）。失败置 pending 上报。
	if initdExists("network") {
		_, _ = b.run.Run("", "/etc/init.d/network", "reload")
	}
	if initdExists("odhcpd") {
		if _, err := b.run.Run("", "/etc/init.d/odhcpd", "reload"); err != nil {
			b.pending, b.pendingMsg = true, "odhcpd reload 失败，IPv6 配置已保存但未生效"
		}
	}
	return firstErr
}

// projectWANv6 投射一条 IPv6 外网接口（独立具名节）。
func projectWANv6(nb *strings.Builder, id string, w WANv6) {
	key := "network." + id
	setType(nb, key, "interface")
	setKV(nb, key+"."+managedOpt, managedMarkerV6)
	dev := w.Device
	if dev == "" && w.WANIface != "" {
		dev = "@" + w.WANIface
	}
	setKVOrDel(nb, key+".device", dev)
	// 切换接入方式前，先清空所有 proto 专属选项，避免旧配置残留（uci set 不会
	// 删未提及的选项）；随后各 case 只设本 proto 需要的。
	delK(nb, key+".ip6addr", key+".ip6gw", key+".reqprefix", key+".reqaddress",
		key+".clientid", key+".norelease", key+".peerdns", key+".dns",
		key+".peeraddr", key+".ip6prefix", key+".tunlink")
	switch w.Proto {
	case ProtoStatic6:
		setKV(nb, key+".proto", "static")
		setKVOrDel(nb, key+".ip6addr", w.StaticIP6)
		setKVOrDel(nb, key+".ip6gw", w.StaticGW)
	case Proto6in4, Proto6to4, Proto6rd:
		setKV(nb, key+".proto", w.Proto)
		setKVOrDel(nb, key+".peeraddr", w.PeerAddr)
		setKVOrDel(nb, key+".ip6prefix", w.TunPrefix)
		setKVOrDel(nb, key+".tunlink", w.WANIface)
	default: // dhcpv6
		setKV(nb, key+".proto", "dhcpv6")
		reqprefix := w.FixedPrefix
		if reqprefix == "" {
			reqprefix = w.ReqPrefix
		}
		setKVOrDel(nb, key+".reqprefix", reqprefix)
		if w.ForcePrefix {
			setKV(nb, key+".reqaddress", "force")
		} else {
			setKV(nb, key+".reqaddress", "try")
		}
		setKVOrDel(nb, key+".clientid", w.ClientID)
		if w.NoRelease {
			setKV(nb, key+".norelease", "1")
		} else {
			delK(nb, key+".norelease")
		}
		delK(nb, key+".dns")
		if w.PeerDNS {
			delK(nb, key+".peerdns")
		} else {
			setKV(nb, key+".peerdns", "0")
			for _, d := range []string{w.DNSPrimary, w.DNSSecondary} {
				if strings.TrimSpace(d) != "" {
					fmt.Fprintf(nb, "add_list %s.dns='%s'\n", key, d)
				}
			}
		}
	}
	if w.MTU > 0 {
		setKV(nb, key+".mtu", itoa(w.MTU))
	} else {
		delK(nb, key+".mtu")
	}
}

// projectLANv6 投射一条 IPv6 内网（借用 stock lan 接口 + dhcp 节，选项级）。
func projectLANv6(nb, db *strings.Builder, id string, l LANv6) {
	nkey := "network." + id
	if l.PrefixAssignLen > 0 {
		setKV(nb, nkey+".ip6assign", itoa(l.PrefixAssignLen))
	}
	delK(nb, nkey+".ip6class")
	if l.BindWAN != "" {
		fmt.Fprintf(nb, "add_list %s.ip6class='%s'\n", nkey, l.BindWAN)
	}
	setKVOrDel(nb, nkey+".ip6hint", l.PrefixHint)

	dkey := "dhcp." + id
	setType(db, dkey, "dhcp")
	setKV(db, dkey+".interface", l.Interface)
	setKV(db, dkey+"."+managedOptV6, "1")
	if l.Enabled && l.DHCPv6Enabled {
		setKV(db, dkey+".dhcpv6", "server")
		setKV(db, dkey+".ra", "server")
		setKV(db, dkey+".ra_management", raManagement(l.DHCPv6Mode))
		if l.LeaseMinutes > 0 {
			setKV(db, dkey+".leasetime", itoa(l.LeaseMinutes)+"m")
		}
		if l.RAMTUEnabled && l.RAMTU > 0 {
			setKV(db, dkey+".ra_mtu", itoa(l.RAMTU))
		} else {
			delK(db, dkey+".ra_mtu")
		}
		delK(db, dkey+".dns")
		if l.IPv6DNSEnabled {
			for _, d := range l.DNSServers {
				if strings.TrimSpace(d) != "" {
					fmt.Fprintf(db, "add_list %s.dns='%s'\n", dkey, d)
				}
			}
		}
	} else {
		setKV(db, dkey+".dhcpv6", "disabled")
		setKV(db, dkey+".ra", "disabled")
	}
}

// projectPrefixV6 投射一条前缀静态分配（odhcpd host，固定 IID）。
func projectPrefixV6(db *strings.Builder, id string, p PrefixStaticV6) {
	key := "dhcp." + id
	setType(db, key, "host")
	setKV(db, key+"."+managedOpt, managedMarkerV6)
	setKVOrDel(db, key+".duid", p.DUID)
	setKVOrDel(db, key+".mac", p.MAC)
	setKVOrDel(db, key+".hostid", strings.TrimPrefix(p.HostID, "::"))
	setKVOrDel(db, key+".name", p.Remark)
}

// projectACLv6DUID 投射一条「按 DUID 拒发」（odhcpd host hostid='ignore'）。
func projectACLv6DUID(db *strings.Builder, id string, e ACLv6Entry) {
	key := "dhcp." + id
	setType(db, key, "host")
	setKV(db, key+"."+managedOpt, managedMarkerV6)
	setKV(db, key+".duid", e.DUID)
	setKV(db, key+".hostid", "ignore")
}

// disableLANv6 把一个不再管理的借用 LAN 节的 v6 服务停用（保留 stock ip6assign）。
func disableLANv6(nb, db *strings.Builder, n string) {
	setKV(db, "dhcp."+n+".dhcpv6", "disabled")
	setKV(db, "dhcp."+n+".ra", "disabled")
	delK(db, "dhcp."+n+"."+managedOptV6)
	delK(nb, "network."+n+".ip6class", "network."+n+".ip6hint")
}

// raManagement 把 DHCPv6 模式映射为 odhcpd 的 ra_management 整数（≤19.07 通用）。
func raManagement(mode string) string {
	switch mode {
	case DHCPv6Stateless:
		return "0"
	case DHCPv6StatefulOnly:
		return "2"
	default: // stateful
		return "1"
	}
}

// ---- 托管节扫描 ----

func (b *uciBackend) managedNamesV6(config string) map[string]bool {
	out := map[string]bool{}
	if show, err := b.uciShow(config); err == nil {
		for _, n := range managedSectionsMarker(show, config, managedMarkerV6) {
			out[n] = true
		}
	}
	return out
}

func (b *uciBackend) borrowedV6Names(config string) map[string]bool {
	out := map[string]bool{}
	if show, err := b.uciShow(config); err == nil {
		for _, n := range namesByOption(show, config, managedOptV6, "1") {
			out[n] = true
		}
	}
	return out
}

// namesByOption returns section names in `uci show <config>` where option opt
// equals val (used to find LANv6 borrowed nodes by their managed_v6 marker).
func namesByOption(show, config, opt, val string) []string {
	suffix := "." + opt + "='" + val + "'"
	prefix := config + "."
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(show, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
			continue
		}
		rest := strings.TrimPrefix(line, prefix)
		dot := strings.IndexByte(rest, '.')
		if dot <= 0 {
			continue
		}
		name := rest[:dot]
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// ---- import existing v6 config (first-run reflection) ----

// importIPv6Into reflects the machine's existing IPv6 config into the sidecar
// (managed=false → display-only until edited), called from importExisting.
func (b *uciBackend) importIPv6Into(st *State) {
	st.ACLv6 = ACLv6{Mode: ACLBlacklist, Entries: []ACLv6Entry{}}

	ip6assign := map[string]int{}
	if show, err := b.uciShow("network"); err == nil {
		for _, s := range parseUci(show, "network") {
			if s.typ != "interface" || s.name == "loopback" {
				continue
			}
			if v := first(s.opts["ip6assign"]); v != "" {
				ip6assign[s.name] = atoiSafe(v)
			}
			proto := first(s.opts["proto"])
			isV6 := proto == ProtoDHCPv6 || proto == Proto6in4 || proto == Proto6to4 || proto == Proto6rd ||
				(proto == "static" && first(s.opts["ip6addr"]) != "")
			if !isV6 {
				continue
			}
			w := WANv6{
				ID: s.name, Name: s.name, Proto: importProtoV6(proto, s),
				Device: first(s.opts["device"]), ReqPrefix: first(s.opts["reqprefix"]),
				ForcePrefix: first(s.opts["reqaddress"]) == "force",
				ClientID:    first(s.opts["clientid"]), NoRelease: first(s.opts["norelease"]) == "1",
				StaticIP6: first(s.opts["ip6addr"]), StaticGW: first(s.opts["ip6gw"]),
				PeerAddr: first(s.opts["peeraddr"]), TunPrefix: first(s.opts["ip6prefix"]),
				MTU: atoiSafe(first(s.opts["mtu"])), PeerDNS: true, Enabled: true, Managed: false,
			}
			if strings.HasPrefix(w.Device, "@") {
				w.WANIface = strings.TrimPrefix(w.Device, "@")
			}
			st.WANv6s = append(st.WANv6s, w)
		}
	}

	if show, err := b.uciShow("dhcp"); err == nil {
		for _, s := range parseUci(show, "dhcp") {
			if s.typ != "dhcp" {
				continue
			}
			d6, ra := first(s.opts["dhcpv6"]), first(s.opts["ra"])
			if d6 == "" && ra == "" {
				continue // 无 v6 服务配置 → 不导入（本机 dhcp.lan 即此，符合"未开 v6 server"）
			}
			iface := first(s.opts["interface"])
			if iface == "" {
				iface = s.name
			}
			st.LANv6s = append(st.LANv6s, LANv6{
				ID: s.name, Interface: iface, ConfigType: ConfigTypeAuto,
				PrefixAssignLen: ip6assign[iface], DHCPv6Enabled: d6 == "server",
				DHCPv6Mode:   importRaMgmt(first(s.opts["ra_management"])),
				LeaseMinutes: leasetimeToMin(first(s.opts["leasetime"])),
				RAMTUEnabled: first(s.opts["ra_mtu"]) != "", RAMTU: atoiSafe(first(s.opts["ra_mtu"])),
				DNSServers: s.opts["dns"], Enabled: true, Managed: false,
			})
		}
	}
}

func importProtoV6(proto string, s uciSec) string {
	switch proto {
	case ProtoDHCPv6, Proto6in4, Proto6to4, Proto6rd:
		return proto
	default:
		return ProtoStatic6
	}
}

func importRaMgmt(v string) string {
	switch v {
	case "0":
		return DHCPv6Stateless
	case "2":
		return DHCPv6StatefulOnly
	default:
		return DHCPv6Stateful
	}
}

// ---- batch key helpers ----

// setType declares a section type (no quotes), matching the IPv4 apply() style:
//
//	set dhcp.foo=host
func setType(sb *strings.Builder, key, typ string) {
	fmt.Fprintf(sb, "set %s=%s\n", key, typ)
}

func setKV(sb *strings.Builder, key, val string) {
	fmt.Fprintf(sb, "set %s='%s'\n", key, val)
}

func setKVOrDel(sb *strings.Builder, key, val string) {
	if strings.TrimSpace(val) == "" {
		fmt.Fprintf(sb, "delete %s\n", key)
	} else {
		fmt.Fprintf(sb, "set %s='%s'\n", key, val)
	}
}

func delK(sb *strings.Builder, keys ...string) {
	for _, k := range keys {
		fmt.Fprintf(sb, "delete %s\n", k)
	}
}
