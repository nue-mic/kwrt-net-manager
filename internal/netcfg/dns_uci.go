package netcfg

import (
	"fmt"
	"strings"
)

// uci 后端的 DNS 写投射。安全红线（用户离线、生产旁路由）：
//   - 改 stock @dnsmasq[0] 标量前先快照旧值入旁车 SavedStock，DNS 关闭时回写旧值而非删 key；
//   - @dnsmasq[0] 的 server/address 列表：只 del_list 本工具上次写过的精确值再 add_list，
//     绝不整列表 delete，保留 stock/LuCI/https-dns-proxy 的条目；
//   - 自定义解析精确域用独立 config hostrecord 具名节（marker 隔离，整节 GC）；
//   - 强制客户端代理用固定具名 firewall redirect 节（ipv4/ipv6 各一条，劫持到本机省略 dest_ip）；
//   - DNS.Enabled=false 且无记录/路由/DoH 时，applyDNS 把 @dnsmasq[0] 还原到接管前状态。
const (
	dnsmasqSec     = "dhcp.@dnsmasq[0]"
	dnsForceV4     = "kwrtdns_force_v4"
	dnsForceV6     = "kwrtdns_force_v6"
	dnsDoHInstance = "kwrtdns_doh" // 本工具托管的 https-dns-proxy 实例名
	dnsResolvAuto  = "/tmp/resolv.conf.auto"
	dnsLANZone     = "lan"
)

// 本工具管理的 @dnsmasq[0] 标量（快照/回滚集合）。
var dnsManagedScalars = []string{"filter_aaaa", "cachesize", "local_ttl", "min_cache_ttl", "max_cache_ttl", "noresolv", "resolvfile"}

// ---- write overrides：存旁车 → applyDNS ----

func (b *uciBackend) SaveDNSSettings(s DNSSettings) error {
	if err := b.storeBackend.SaveDNSSettings(s); err != nil {
		return err
	}
	return b.applyDNS()
}

func (b *uciBackend) SaveDNSDoH(d DNSDoH) error {
	if err := b.storeBackend.SaveDNSDoH(d); err != nil {
		return err
	}
	return b.applyDNS()
}

func (b *uciBackend) SaveDNSRecords(list []DNSRecord) error {
	if err := b.storeBackend.SaveDNSRecords(list); err != nil {
		return err
	}
	return b.applyDNS()
}

func (b *uciBackend) SaveDNSDomainRoutes(list []DNSDomainRoute) error {
	if err := b.storeBackend.SaveDNSDomainRoutes(list); err != nil {
		return err
	}
	return b.applyDNS()
}

// ---- applyDNS：把 DNS 旁车状态安全投射进 UCI ----

func (b *uciBackend) applyDNS() error {
	b.applyMu.Lock()
	defer b.applyMu.Unlock()

	st, _ := b.storeBackend.DNSSettings() // 显式取旁车原值：DNSSettings 在 uci 后端被重写带 uci 反射，须绕过
	doh, _ := b.DNSDoH()                  // 下三个 getter uci 后端未重写，直接走内嵌 store（避免 staticcheck QF1008）
	records, _ := b.DNSRecords()
	routes, _ := b.DNSDomainRoutes()

	var db strings.Builder // dhcp(@dnsmasq[0] + hostrecord) batch

	// managed = 是否在接管 @dnsmasq[0]（DNS 设置开 或 DoH 开，二者任一都会改上游/noresolv）。
	managed := st.Enabled || doh.Enabled

	// 1. 首次接管时快照 stock 标量旧值（空串=原本无该 option）。
	if managed && st.SavedStock == nil {
		st.SavedStock = map[string]string{}
		for _, k := range dnsManagedScalars {
			v, _ := b.uciGet(dnsmasqSec + "." + k)
			st.SavedStock[k] = v
		}
	}

	// 2. stock 标量投射 / 回滚。
	if managed {
		// 用户面板标量仅在「托管 DNS 设置」开启时投射；DoH-only 不动这些（保持 stock）。
		if st.Enabled {
			if st.FilterAAAA && b.filterAAAASupported() {
				setKV(&db, dnsmasqSec+".filter_aaaa", "1")
			} else {
				setKV(&db, dnsmasqSec+".filter_aaaa", "0")
			}
			setIntOrDel(&db, dnsmasqSec+".cachesize", st.CacheSize)
			setIntOrDel(&db, dnsmasqSec+".local_ttl", st.LocalTTL)
			setIntOrDel(&db, dnsmasqSec+".min_cache_ttl", st.MinCacheTTL)
			setIntOrDel(&db, dnsmasqSec+".max_cache_ttl", st.MaxCacheTTL)
		}
		// noresolv：开 DoH（必须只走本地代理）或「仅用指定上游」时置 1；同时显式钉住 resolvfile
		// 防止路由器自身 resolv 失管（#6597/#5838）。
		if doh.Enabled || (st.Enabled && st.NoResolv && (st.DNSPrimary != "" || st.DNSSecondary != "")) {
			setKV(&db, dnsmasqSec+".noresolv", "1")
			setKV(&db, dnsmasqSec+".resolvfile", dnsResolvAuto)
		} else {
			delK(&db, dnsmasqSec+".noresolv")
		}
	} else if st.SavedStock != nil {
		// 完全关闭（DNS 与 DoH 都关）：逐项回写接管前的旧值（空串=删除该 option）。
		for _, k := range dnsManagedScalars {
			restoreStock(&db, dnsmasqSec+"."+k, st.SavedStock[k])
		}
		st.SavedStock = nil
	}

	// 3. @dnsmasq[0] 的 server / address 列表：只删自己上次写过的精确值再加当前值。
	newServers := desiredDNSServers(st, doh, routes)
	newAddrs := desiredDNSAddrs(records)
	for _, v := range st.PrevServers {
		fmt.Fprintf(&db, "del_list %s.server='%s'\n", dnsmasqSec, v)
	}
	for _, v := range newServers {
		fmt.Fprintf(&db, "add_list %s.server='%s'\n", dnsmasqSec, v)
	}
	for _, v := range st.PrevAddrs {
		fmt.Fprintf(&db, "del_list %s.address='%s'\n", dnsmasqSec, v)
	}
	for _, v := range newAddrs {
		fmt.Fprintf(&db, "add_list %s.address='%s'\n", dnsmasqSec, v)
	}
	st.PrevServers, st.PrevAddrs = newServers, newAddrs

	// 4. 自定义解析精确域 → 独立 config hostrecord 具名节（marker 隔离 + GC）。
	keep := map[string]bool{}
	for _, r := range records {
		if !r.Enabled || !r.Managed || r.Wildcard {
			continue // 通配域走上面的 address 列表
		}
		id := uciName(r.ID)
		keep[id] = true
		key := "dhcp." + id
		setType(&db, key, "hostrecord")
		setKV(&db, key+"."+managedOpt, managedMarkerDNS)
		setKV(&db, key+".name", r.Domain)
		setKV(&db, key+".ip", r.Address)
	}
	for n := range b.managedNamesDNS("dhcp") {
		if !keep[n] {
			fmt.Fprintf(&db, "delete dhcp.%s\n", n)
		}
	}
	db.WriteString("commit dhcp\n")

	// 持久化簿记（SavedStock / Prev*）——每次都写，保证 Prev* 恒等于我们刚写入 UCI 的值，
	// 杜绝下次 apply 因簿记漂移而漏删旧 server/address（曾出现的残留孤儿）。
	_ = b.saveDNSBookkeeping(st.SavedStock, st.PrevServers, st.PrevAddrs)

	var firstErr error
	if out, err := b.run.Run(db.String(), "uci", "batch"); err != nil {
		firstErr = fmt.Errorf("uci batch dns: %v (%s)", err, strings.TrimSpace(out))
	}

	// 5. DoH：托管 https-dns-proxy 实例（仅在已安装时）。
	b.applyDoH(doh)

	// 6. 强制客户端 DNS 代理：firewall redirect（固定具名节）。
	b.applyDNSHijack(st.Enabled && st.ForceProxy)

	// 必须 restart（不能 reload）：dnsmasq 的 SIGHUP/reload 不会重读 conf 里的 address= / server=
	// 等指令，只有 restart 才会重新加载——否则自定义解析/上游写进了 conf 却不生效。
	if svc := dhcpService(); svc != "" {
		if _, err := b.run.Run("", "/etc/init.d/"+svc, "restart"); err != nil {
			b.pending, b.pendingMsg = true, svc+" restart 失败，DNS 配置已保存但未生效"
		}
	}
	return firstErr
}

// applyDoH 托管 https-dns-proxy 实例（关其自动改写 dnsmasq，由我们自己往 server 写 127.0.0.1#port）。
func (b *uciBackend) applyDoH(doh DNSDoH) {
	if !initdExists("https-dns-proxy") {
		return // 未安装：DoH 的 server 条目也不会被加（desiredDNSServers 仍会加，但无监听——故仅在已装时才让 dnsmasq 指过去）
	}
	var fb strings.Builder
	// 关闭其自动改写 dnsmasq（我们自管）。
	setType(&fb, "https-dns-proxy.config", "main")
	setKV(&fb, "https-dns-proxy.config.dnsmasq_config_update", "-")
	setKV(&fb, "https-dns-proxy.config.update_dnsmasq_config", "-")
	if doh.Enabled {
		key := "https-dns-proxy." + dnsDoHInstance
		setType(&fb, key, "https-dns-proxy")
		setKV(&fb, key+".listen_addr", "127.0.0.1")
		setKV(&fb, key+".listen_port", itoa(doh.ListenPort))
		setKVOrDel(&fb, key+".resolver_url", doh.ResolverURL)
		setKVOrDel(&fb, key+".bootstrap_dns", doh.BootstrapDNS)
	} else {
		delK(&fb, "https-dns-proxy."+dnsDoHInstance)
	}
	fb.WriteString("commit https-dns-proxy\n")
	_, _ = b.run.Run(fb.String(), "uci", "batch")
	if doh.Enabled {
		_, _ = b.run.Run("", "/etc/init.d/https-dns-proxy", "restart")
	} else {
		_, _ = b.run.Run("", "/etc/init.d/https-dns-proxy", "reload")
	}
}

// applyDNSHijack 用固定具名 firewall redirect 节强制客户端 53 流量到本机（ipv4/ipv6 各一条，
// 劫持到本机省略 dest_ip）。off 时删除这两个节。best-effort。
func (b *uciBackend) applyDNSHijack(on bool) {
	if !initdExists("firewall") {
		return
	}
	var fb strings.Builder
	delK(&fb, "firewall."+dnsForceV4, "firewall."+dnsForceV6)
	if on {
		for _, c := range []struct{ name, fam string }{{dnsForceV4, "ipv4"}, {dnsForceV6, "ipv6"}} {
			key := "firewall." + c.name
			setType(&fb, key, "redirect")
			setKV(&fb, key+".name", "kwrt-dns-force-"+c.fam)
			setKV(&fb, key+".src", dnsLANZone)
			setKV(&fb, key+".src_dport", "53")
			setKV(&fb, key+".dest", dnsLANZone)
			setKV(&fb, key+".dest_port", "53")
			setKV(&fb, key+".target", "DNAT")
			setKV(&fb, key+".family", c.fam)
			fmt.Fprintf(&fb, "add_list firewall.%s.proto='tcp'\n", c.name)
			fmt.Fprintf(&fb, "add_list firewall.%s.proto='udp'\n", c.name)
		}
	}
	fb.WriteString("commit firewall\n")
	if _, err := b.run.Run(fb.String(), "uci", "batch"); err == nil {
		_, _ = b.run.Run("", "/etc/init.d/firewall", "reload")
	}
}

// managedNamesDNS 返回 @dnsmasq 同 config 下带 DNS marker 的具名节（与 v4/v6 隔离）。
func (b *uciBackend) managedNamesDNS(config string) map[string]bool {
	out := map[string]bool{}
	if show, err := b.uciShow(config); err == nil {
		for _, n := range managedSectionsMarker(show, config, managedMarkerDNS) {
			out[n] = true
		}
	}
	return out
}

// ---- 纯函数辅助 ----

// desiredDNSServers 汇总要写入 @dnsmasq[0].server 的全部精确值：上游(首选/备选) + DoH 本地代理
// + 域名分流（/域名/上游[@iface]）。
func desiredDNSServers(st DNSSettings, doh DNSDoH, routes []DNSDomainRoute) []string {
	var out []string
	if st.Enabled {
		if st.DNSPrimary != "" {
			out = append(out, st.DNSPrimary)
		}
		if st.DNSSecondary != "" {
			out = append(out, st.DNSSecondary)
		}
	}
	if doh.Enabled {
		out = append(out, fmt.Sprintf("127.0.0.1#%d", doh.ListenPort))
	}
	for _, r := range routes {
		if !r.Enabled || !r.Managed {
			continue
		}
		v := "/" + strings.TrimPrefix(r.Domain, "*.") + "/" + r.Server
		if r.OutIface != "" {
			v += "@" + r.OutIface
		}
		out = append(out, v)
	}
	return out
}

// desiredDNSAddrs 汇总通配自定义解析要写入 @dnsmasq[0].address 的精确值（/域名/IP）。
func desiredDNSAddrs(records []DNSRecord) []string {
	var out []string
	for _, r := range records {
		if !r.Enabled || !r.Managed || !r.Wildcard {
			continue
		}
		out = append(out, "/"+strings.TrimPrefix(r.Domain, "*.")+"/"+r.Address)
	}
	return out
}

// setIntOrDel：>0 则 set，否则 delete（用于 cachesize/ttl 这类 0=不设）。
func setIntOrDel(sb *strings.Builder, key string, v int) {
	if v > 0 {
		setKV(sb, key, itoa(v))
	} else {
		delK(sb, key)
	}
}

// restoreStock：回写接管前的旧值；空串表示原本无该 option → 删除。
func restoreStock(sb *strings.Builder, key, old string) {
	if old == "" {
		delK(sb, key)
	} else {
		setKV(sb, key, old)
	}
}
