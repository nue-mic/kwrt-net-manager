package netcfg

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// Managed-section marker. Only UCI sections carrying option managed_by =
// managedMarker are ever deleted by this backend, so it never removes stock or
// LuCI/operator-authored sections it has not adopted.
const (
	managedMarker = "kwrt-net-manager"
	managedOpt    = "managed_by"
)

// uciBackend is the OpenWrt backend. Design for multi-version compatibility &
// safety on a live router:
//
//   - The sidecar JSON (embedded *storeBackend) is the working state. On first
//     run it is IMPORTED from the machine's existing /etc/config/{dhcp,network}
//     so the UI shows the real configuration — it never fabricates demo data.
//   - apply() projects state into UCI by MODIFYING sections in place (adopting
//     stock sections, preserving options it does not manage) using primitives
//     present since ≤19.07; it deletes only sections it has marked managed.
//   - DHCP service control is resilient: it detects dnsmasq vs odhcpd vs none
//     and never exec()s a missing init script.
//   - commit and reload are distinct phases; a failed/absent reload sets Pending.
type uciBackend struct {
	*storeBackend // sidecar working state
	run           runner
	log           *slog.Logger
	applyMu       sync.Mutex
	pending       bool
	pendingMsg    string
}

func newUCIBackend(run runner, sidecar string, log *slog.Logger) (*uciBackend, error) {
	// No demo seeding on a real OpenWrt box — import the existing config instead.
	sb, err := newStoreBackend(sidecar, log, false)
	if err != nil {
		return nil, err
	}
	b := &uciBackend{storeBackend: sb, run: run, log: log}
	if sb.fresh {
		if err := b.importExisting(); err != nil && log != nil {
			log.Warn("netcfg(uci): import existing config failed", slog.Any("err", err))
		} else if log != nil {
			log.Info("netcfg(uci): imported existing /etc/config dhcp+network into UI")
		}
	}
	// Deliberately NO apply() on boot: a fresh import must not write back to UCI
	// until the operator actually changes something; an existing sidecar was
	// already applied on its last save.
	return b, nil
}

func (b *uciBackend) Kind() string { return KindUCI }

// ---- write methods: persist sidecar, then project to UCI ----

func (b *uciBackend) SaveDHCPServers(list []DHCPServer) error {
	if err := b.storeBackend.SaveDHCPServers(list); err != nil {
		return err
	}
	return b.apply()
}

func (b *uciBackend) SaveStatics(list []StaticLease, arpBind bool) error {
	if err := b.storeBackend.SaveStatics(list, arpBind); err != nil {
		return err
	}
	return b.apply()
}

func (b *uciBackend) SaveACL(acl ACL) error {
	if err := b.storeBackend.SaveACL(acl); err != nil {
		return err
	}
	return b.apply()
}

func (b *uciBackend) SaveRoutes(list []Route) error {
	if err := b.storeBackend.SaveRoutes(list); err != nil {
		return err
	}
	return b.apply()
}

// ---- DHCP service detection (dnsmasq | odhcpd | none) ----

func initdExists(name string) bool {
	_, err := os.Stat("/etc/init.d/" + name)
	return err == nil
}

// dhcpService returns the init.d name of the DHCP server in use, or "" if none
// is installed (e.g. a minimal box without dnsmasq). It is a var so tests can
// stub the host's installed daemon.
var dhcpService = func() string {
	if initdExists("dnsmasq") {
		return "dnsmasq"
	}
	if initdExists("odhcpd") {
		return "odhcpd"
	}
	return ""
}

func (b *uciBackend) RestartDHCP() error {
	svc := dhcpService()
	if svc == "" {
		return errors.New("未检测到 DHCP 服务（dnsmasq / odhcpd 均未安装），无法重启 DHCP")
	}
	if out, err := b.run.Run("", "/etc/init.d/"+svc, "restart"); err != nil {
		return fmt.Errorf("%s restart 失败：%v（%s）", svc, err, strings.TrimSpace(out))
	}
	return nil
}

func (b *uciBackend) Status() (Status, error) {
	ok, msg := !b.pending, b.pendingMsg
	if dhcpService() == "" {
		ok = false
		if msg == "" {
			msg = "未检测到 DHCP 服务（dnsmasq / odhcpd 未安装），DHCP 配置可保存但无法在本机下发"
		}
	}
	return Status{Backend: KindUCI, DHCPOK: ok, Pending: b.pending, Message: msg}, nil
}

// ---- apply: project the sidecar state into UCI (modify-in-place + adopt) ----

func (b *uciBackend) apply() error {
	b.applyMu.Lock()
	defer b.applyMu.Unlock()

	// Project the sidecar (store) working state into UCI. These read the
	// embedded storeBackend's data verbatim — the uciBackend deliberately does
	// not override them.
	servers, _ := b.DHCPServers()
	statics, _ := b.Statics()
	acl, _ := b.ACL()
	routes, _ := b.Routes()
	arpBind, _ := b.ARPBind()
	whitelist := acl.Mode == ACLWhitelist

	existDhcp := b.managedNames("dhcp")
	existNet := b.managedNames("network")

	keepDhcp := map[string]bool{}
	anyDHCPEnabled := false
	var d strings.Builder
	for _, s := range servers {
		if !s.Managed {
			continue // imported stock section, display-only until the user edits it
		}
		id := uciName(s.ID)
		keepDhcp[id] = true
		start, limit := b.startLimit(s)
		fmt.Fprintf(&d, "set dhcp.%s=dhcp\n", id)
		fmt.Fprintf(&d, "set dhcp.%s.%s='%s'\n", id, managedOpt, managedMarker)
		fmt.Fprintf(&d, "set dhcp.%s.interface='%s'\n", id, s.Interface)
		fmt.Fprintf(&d, "set dhcp.%s.start='%d'\n", id, start)
		fmt.Fprintf(&d, "set dhcp.%s.limit='%d'\n", id, limit)
		fmt.Fprintf(&d, "set dhcp.%s.leasetime='%dm'\n", id, s.LeaseMinutes)
		// 强制下发：force='1' 跳过 dnsmasq init 的"本网段已有 DHCP 服务器则礼让退出"探测
		// （dhcp_check）。旁路由/同网段多 DHCP 场景必备，否则探测命中后整段不发地址。
		if s.Force {
			fmt.Fprintf(&d, "set dhcp.%s.force='1'\n", id)
		} else {
			fmt.Fprintf(&d, "delete dhcp.%s.force\n", id)
		}
		if s.Enabled {
			fmt.Fprintf(&d, "delete dhcp.%s.ignore\n", id)
			// Make the pool actually serve DHCPv4: dhcpv4='server' is required by
			// odhcpd and harmless/standard for dnsmasq. This is what turns a
			// web "启用" into a real DHCP server across both backends.
			fmt.Fprintf(&d, "set dhcp.%s.dhcpv4='server'\n", id)
			anyDHCPEnabled = true
		} else {
			fmt.Fprintf(&d, "set dhcp.%s.ignore='1'\n", id)
		}
		// Replace only OUR dhcp_option list (gateway/dns/custom); other options
		// on an adopted stock section (dhcpv4/ra/...) are left untouched.
		fmt.Fprintf(&d, "delete dhcp.%s.dhcp_option\n", id)
		if s.Gateway != "" {
			fmt.Fprintf(&d, "add_list dhcp.%s.dhcp_option='3,%s'\n", id, s.Gateway)
		}
		if dns := joinDNS(s.DNSPrimary, s.DNSSecondary); dns != "" {
			fmt.Fprintf(&d, "add_list dhcp.%s.dhcp_option='6,%s'\n", id, dns)
		}
		for _, o := range s.CustomOptions {
			fmt.Fprintf(&d, "add_list dhcp.%s.dhcp_option='%d,%s'\n", id, o.Code, o.Value)
		}
		// 白名单模式 = 仅服务已知/静态主机：dnsmasq `dhcp-range=...,static`（option dynamicdhcp '0'）。
		if whitelist {
			fmt.Fprintf(&d, "set dhcp.%s.dynamicdhcp='0'\n", id)
		} else {
			fmt.Fprintf(&d, "delete dhcp.%s.dynamicdhcp\n", id)
		}
		// 排除地址：dnsmasq 无「池内挖洞」原语，按 OpenWrt 习惯用占位 host 保留每个被排除的
		// IP（保留地址不再被动态分配）。仅在池启用时投射；停用/删除后由 GC 清除。
		if s.Enabled {
			for _, ip := range expandExclude(s.Exclude) {
				xid := uciName(id + "_x_" + ip)
				keepDhcp[xid] = true
				fmt.Fprintf(&d, "set dhcp.%s=host\n", xid)
				fmt.Fprintf(&d, "set dhcp.%s.%s='%s'\n", xid, managedOpt, managedMarker)
				fmt.Fprintf(&d, "set dhcp.%s.mac='%s'\n", xid, placeholderMAC(ip))
				fmt.Fprintf(&d, "set dhcp.%s.ip='%s'\n", xid, ip)
			}
		}
	}
	for _, s := range statics {
		if !s.Enabled || !s.Managed {
			continue
		}
		id := uciName(s.ID)
		keepDhcp[id] = true
		fmt.Fprintf(&d, "set dhcp.%s=host\n", id)
		fmt.Fprintf(&d, "set dhcp.%s.%s='%s'\n", id, managedOpt, managedMarker)
		fmt.Fprintf(&d, "set dhcp.%s.mac='%s'\n", id, s.MAC)
		fmt.Fprintf(&d, "set dhcp.%s.ip='%s'\n", id, s.IP)
		// 仅把 DNS 合法的主机名下发给 dnsmasq；含中文/空格等非法字符的名字会让 dnsmasq
		// 报 "bad DHCP host name" 整体崩溃。原始名仍保留在旁车里供前端展示，绑定(mac+ip)不受影响。
		if h := dnsSafeHostname(s.Hostname); h != "" {
			fmt.Fprintf(&d, "set dhcp.%s.name='%s'\n", id, h)
		} else {
			fmt.Fprintf(&d, "delete dhcp.%s.name\n", id)
		}
		// 每条静态分配的网关/DNS：OpenWrt 的 config host 没有 option 3/6，原生经 dnsmasq tag
		// 下发——建一个具名 `config tag` 承载 option 3/6，再把该 host 打上同名 tag。
		if opts := hostTagOptions(s); len(opts) > 0 {
			tagid := id + "_t"
			keepDhcp[tagid] = true
			fmt.Fprintf(&d, "set dhcp.%s=tag\n", tagid)
			fmt.Fprintf(&d, "set dhcp.%s.%s='%s'\n", tagid, managedOpt, managedMarker)
			fmt.Fprintf(&d, "delete dhcp.%s.dhcp_option\n", tagid)
			for _, o := range opts {
				fmt.Fprintf(&d, "add_list dhcp.%s.dhcp_option='%s'\n", tagid, o)
			}
			fmt.Fprintf(&d, "set dhcp.%s.tag='%s'\n", id, tagid)
		} else {
			fmt.Fprintf(&d, "delete dhcp.%s.tag\n", id)
		}
	}
	// MAC 黑/白名单 —— 必须用 OpenWrt dnsmasq 真正认得的原语（实测得知：config host 的
	// `option ignore` 根本不被 init 识别，且无 ip/name/hostid 的 host 会被直接丢弃）：
	//   黑名单：每个 MAC -> `option ip 'ignore'`，生成 dhcp-host=MAC,ignore 拒发；
	//   白名单：各池已置 dynamicdhcp='0'（仅服务有保留 IP 的已知主机），故给每个放行 MAC
	//          在「首个启用池」内分配一个空闲保留 IP；名单外设备拿不到地址。
	if whitelist {
		nextIP := whitelistAllocator(servers, usedPoolIPs(servers, statics))
		for _, e := range acl.Entries {
			if !e.Enabled || !e.Managed {
				continue
			}
			ip := nextIP()
			if ip == "" { // 无启用池或池内地址耗尽
				if b.log != nil {
					b.log.Warn("netcfg(uci): whitelist 地址池已满，部分放行 MAC 未分配", slog.String("mac", e.MAC))
				}
				break
			}
			id := uciName(e.ID)
			keepDhcp[id] = true
			fmt.Fprintf(&d, "set dhcp.%s=host\n", id)
			fmt.Fprintf(&d, "set dhcp.%s.%s='%s'\n", id, managedOpt, managedMarker)
			fmt.Fprintf(&d, "set dhcp.%s.mac='%s'\n", id, e.MAC)
			fmt.Fprintf(&d, "set dhcp.%s.ip='%s'\n", id, ip)
		}
	} else {
		for _, e := range acl.Entries {
			if !e.Enabled || !e.Managed {
				continue
			}
			id := uciName(e.ID)
			keepDhcp[id] = true
			fmt.Fprintf(&d, "set dhcp.%s=host\n", id)
			fmt.Fprintf(&d, "set dhcp.%s.%s='%s'\n", id, managedOpt, managedMarker)
			fmt.Fprintf(&d, "set dhcp.%s.mac='%s'\n", id, e.MAC)
			fmt.Fprintf(&d, "set dhcp.%s.ip='ignore'\n", id) // 原生黑名单：dhcp-host=MAC,ignore
		}
	}
	for n := range existDhcp {
		if !keepDhcp[n] {
			fmt.Fprintf(&d, "delete dhcp.%s\n", n)
		}
	}
	// Multi-backend DHCPv4 server selection — only ever touch an existing odhcpd section:
	//   - odhcpd-only box (no dnsmasq): a pool only serves once odhcpd is the main
	//     DHCPv4 server, so flip maindhcp on when a pool is enabled.
	//   - dnsmasq present: dnsmasq must stay the DHCPv4 server. A stale maindhcp=1
	//     makes dnsmasq's init skip dhcp-range entirely and nothing serves DHCPv4
	//     (devices get no lease), so force it back to 0. This keeps the toggle
	//     symmetric — without it a box that was ever odhcpd-only stays broken after
	//     dnsmasq is installed.
	if _, err := b.uciGet("dhcp.odhcpd"); err == nil {
		switch dhcpService() {
		case "odhcpd":
			if anyDHCPEnabled {
				d.WriteString("set dhcp.odhcpd.maindhcp='1'\n")
			}
		case "dnsmasq":
			d.WriteString("set dhcp.odhcpd.maindhcp='0'\n")
		}
	}
	d.WriteString("commit dhcp\n")

	keepNet := map[string]bool{}
	var nb strings.Builder
	for _, r := range routes {
		if !r.Enabled || !r.Managed {
			continue // disabled or imported-unedited → not projected to the kernel
		}
		id := uciName(r.ID)
		keepNet[id] = true
		typ := "route"
		if r.Family == FamilyIPv6 {
			typ = "route6"
		}
		fmt.Fprintf(&nb, "set network.%s=%s\n", id, typ)
		fmt.Fprintf(&nb, "set network.%s.%s='%s'\n", id, managedOpt, managedMarker)
		if r.Interface != "" && r.Interface != "auto" {
			fmt.Fprintf(&nb, "set network.%s.interface='%s'\n", id, r.Interface)
		} else {
			fmt.Fprintf(&nb, "delete network.%s.interface\n", id)
		}
		if r.Family == FamilyIPv6 {
			fmt.Fprintf(&nb, "set network.%s.target='%s/%d'\n", id, r.Target, r.Prefix)
			fmt.Fprintf(&nb, "delete network.%s.netmask\n", id)
		} else {
			fmt.Fprintf(&nb, "set network.%s.target='%s'\n", id, r.Target)
			fmt.Fprintf(&nb, "set network.%s.netmask='%s'\n", id, r.Netmask)
		}
		if r.Gateway != "" {
			fmt.Fprintf(&nb, "set network.%s.gateway='%s'\n", id, r.Gateway)
		} else {
			fmt.Fprintf(&nb, "delete network.%s.gateway\n", id)
		}
		fmt.Fprintf(&nb, "set network.%s.metric='%d'\n", id, r.Metric)
	}
	for n := range existNet {
		if !keepNet[n] {
			fmt.Fprintf(&nb, "delete network.%s\n", n)
		}
	}
	nb.WriteString("commit network\n")

	var firstErr error
	if out, err := b.run.Run(d.String(), "uci", "batch"); err != nil {
		firstErr = fmt.Errorf("uci batch dhcp: %v (%s)", err, strings.TrimSpace(out))
	}
	if out, err := b.run.Run(nb.String(), "uci", "batch"); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("uci batch network: %v (%s)", err, strings.TrimSpace(out))
	}

	// Reload phase — resilient to a missing DHCP server.
	b.pending, b.pendingMsg = false, ""
	if svc := dhcpService(); svc == "" {
		b.pending, b.pendingMsg = true, "未检测到 DHCP 服务（dnsmasq / odhcpd 均未安装），DHCP 配置已写入 UCI 但无法在本机下发生效"
	} else if _, err := b.run.Run("", "/etc/init.d/"+svc, "reload"); err != nil {
		b.pending, b.pendingMsg = true, svc+" reload 失败，配置已保存但未生效"
	}
	if initdExists("network") {
		_, _ = b.run.Run("", "/etc/init.d/network", "reload")
	}
	// 兼容 ARP 绑定：把启用的托管静态分配下成内核静态邻居表项（防 IP↔MAC 漂移）。
	// best-effort，失败不影响主流程；不跨重启持久（下次保存会重新下发）。
	b.applyARP(statics, arpBind)
	return firstErr
}

// managedNames returns the names of UCI sections in <config> carrying our marker.
func (b *uciBackend) managedNames(config string) map[string]bool {
	out := map[string]bool{}
	if show, err := b.uciShow(config); err == nil {
		for _, n := range managedSections(show, config) {
			out[n] = true
		}
	}
	return out
}

// ---- import existing config (first run reflection) ----

func (b *uciBackend) importExisting() error {
	st := State{ACL: ACL{Mode: ACLBlacklist, Entries: []ACLEntry{}}}

	ipmask := map[string][2]string{} // iface -> {ipv4, netmask}
	if ifaces, err := b.Interfaces(); err == nil {
		for _, i := range ifaces {
			ipmask[i.Name] = [2]string{i.IPv4, i.Netmask}
		}
	}

	if show, err := b.uciShow("dhcp"); err == nil {
		for _, s := range parseUci(show, "dhcp") {
			switch s.typ {
			case "dhcp":
				iface := first(s.opts["interface"])
				if iface == "" {
					continue
				}
				srv := DHCPServer{
					ID: s.name, Interface: iface,
					Enabled:      first(s.opts["ignore"]) != "1",
					Force:        first(s.opts["force"]) == "1",
					LeaseMinutes: leasetimeToMin(first(s.opts["leasetime"])),
					Exclude:      []string{}, CustomOptions: []CustomOption{},
				}
				im := ipmask[iface]
				srv.Netmask = im[1]
				srv.IPStart, srv.IPEnd = startLimitToRange(im[0], im[1],
					atoiSafe(first(s.opts["start"])), atoiSafe(first(s.opts["limit"])))
				for _, o := range s.opts["dhcp_option"] {
					switch {
					case strings.HasPrefix(o, "3,"):
						srv.Gateway = o[2:]
					case strings.HasPrefix(o, "6,"):
						parts := strings.Split(o[2:], ",")
						if len(parts) > 0 {
							srv.DNSPrimary = parts[0]
						}
						if len(parts) > 1 {
							srv.DNSSecondary = parts[1]
						}
					default:
						if code, val, ok := splitOpt(o); ok {
							srv.CustomOptions = append(srv.CustomOptions, CustomOption{Code: code, Value: val})
						}
					}
				}
				if srv.LeaseMinutes <= 0 {
					srv.LeaseMinutes = 120
				}
				st.DHCPServers = append(st.DHCPServers, srv)
			case "host":
				mac := netutil.NormalizeMAC(first(s.opts["mac"]))
				if mac == "" {
					continue
				}
				if first(s.opts["ignore"]) == "1" {
					st.ACL.Entries = append(st.ACL.Entries, ACLEntry{ID: s.name, MAC: mac, Remark: first(s.opts["name"]), Enabled: true})
				} else {
					st.Statics = append(st.Statics, StaticLease{ID: s.name, Hostname: first(s.opts["name"]), IP: first(s.opts["ip"]), MAC: mac, Enabled: true})
				}
			}
		}
	}

	if show, err := b.uciShow("network"); err == nil {
		for _, s := range parseUci(show, "network") {
			if s.typ != "route" && s.typ != "route6" {
				continue
			}
			r := Route{
				ID: s.name, Interface: orAuto(first(s.opts["interface"])),
				Gateway: first(s.opts["gateway"]), Metric: atoiSafe(first(s.opts["metric"])),
				Enabled: first(s.opts["disabled"]) != "1",
			}
			if s.typ == "route6" {
				r.Family = FamilyIPv6
				tgt := first(s.opts["target"])
				if i := strings.IndexByte(tgt, '/'); i >= 0 {
					r.Target, r.Prefix = tgt[:i], atoiSafe(tgt[i+1:])
				} else {
					r.Target = tgt
				}
			} else {
				r.Family = FamilyIPv4
				r.Target = first(s.opts["target"])
				r.Netmask = first(s.opts["netmask"])
				if p, ok := netutil.MaskToPrefix(r.Netmask); ok {
					r.Prefix = p
				}
			}
			st.Routes = append(st.Routes, r)
		}
	}
	b.importIPv6Into(&st)
	return b.replaceState(st)
}

// startLimit computes dnsmasq start (host offset) + limit (count) from the pool
// start/end and the interface IP/mask. Falls back to the /24 host octet.
func (b *uciBackend) startLimit(srv DHCPServer) (int, int) {
	// A dnsmasq pool has no independent netmask: start/limit are offsets into the
	// BOUND INTERFACE's network. Always compute against the interface IP/mask, not
	// the form netmask (which is only a display/advertised value). The Service
	// layer already rejects an out-of-subnet range, so DHCPStartLimit should
	// succeed; the last-octet fallback is purely defensive for a missing interface.
	ip := b.ifaceIP(srv.Interface)
	mask, _ := b.uciGet("network." + srv.Interface + ".netmask")
	if ip != "" && mask != "" {
		if s, l, ok := netutil.DHCPStartLimit(ip, mask, srv.IPStart, srv.IPEnd); ok {
			return s, l
		}
	}
	su, _ := netutil.IPv4ToUint32(srv.IPStart)
	cnt, _ := netutil.RangeCount(srv.IPStart, srv.IPEnd)
	return int(su & 0xFF), cnt
}

// ifaceIP returns the interface's IPv4 (strips any /prefix from uci ipaddr).
func (b *uciBackend) ifaceIP(iface string) string {
	ip, err := b.uciGet("network." + iface + ".ipaddr")
	if err != nil || ip == "" {
		return ""
	}
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		ip = ip[:i]
	}
	return ip
}

// ifaceDevice resolves the L2 device of a logical interface (for `ip neigh`).
// On a bridged LAN this is br-<name>; on this project's test box, plain eth0.
func (b *uciBackend) ifaceDevice(iface string) string {
	if iface == "" {
		iface = "lan"
	}
	if dev, err := b.uciGet("network." + iface + ".device"); err == nil && dev != "" {
		return dev
	}
	return ""
}

// applyARP installs/removes static neighbour entries for managed reservations, so
// the configured IP↔MAC bindings are also enforced at the ARP layer when the
// global ARP-bind toggle is on. best-effort; errors are intentionally ignored.
func (b *uciBackend) applyARP(statics []StaticLease, on bool) {
	for _, s := range statics {
		if !s.Managed {
			continue
		}
		dev := b.ifaceDevice(s.Interface)
		if dev == "" {
			continue
		}
		if on && s.Enabled {
			_, _ = b.run.Run("", "ip", "neigh", "replace", s.IP, "lladdr", s.MAC, "dev", dev, "nud", "permanent")
		} else {
			_, _ = b.run.Run("", "ip", "neigh", "del", s.IP, "dev", dev)
		}
	}
}

// dnsSafeHostname returns name if it is a valid DNS host label (so dnsmasq won't
// reject it with "bad DHCP host name" and crash-loop), else "". dnsmasq allows
// alphanumerics plus '-'/'_' (not at either end); anything else — Chinese chars,
// spaces, dots — is rejected. The original name still lives in the sidecar for UI.
func dnsSafeHostname(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 63 {
		return ""
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '-' || c == '_') && i != 0 && i != len(name)-1:
		default:
			return ""
		}
	}
	return name
}

// hostTagOptions returns the per-host dhcp_option values (option 3 gateway,
// option 6 DNS) a static reservation needs delivered via a dnsmasq tag.
func hostTagOptions(s StaticLease) []string {
	var out []string
	if strings.TrimSpace(s.Gateway) != "" {
		out = append(out, "3,"+s.Gateway)
	}
	if dns := joinDNS(s.DNSPrimary, s.DNSSecondary); dns != "" {
		out = append(out, "6,"+dns)
	}
	return out
}

// expandExclude flattens iKuai-style exclude lines ("ip" or "ip-ip") into the
// individual IPv4 addresses to reserve. Capped to avoid a pathological range
// blowing up the config; overflow is dropped (excludes are sidecar-authoritative
// and such ranges are not a real use case).
func expandExclude(lines []string) []string {
	const limit = 1024
	var out []string
	for _, line := range lines {
		a, c, ok := netutil.ParseExcludeLine(line)
		if !ok {
			continue
		}
		au, _ := netutil.IPv4ToUint32(a)
		cu, _ := netutil.IPv4ToUint32(c)
		for u := au; u <= cu && len(out) < limit; u++ {
			out = append(out, netutil.Uint32ToIPv4(u))
		}
	}
	return out
}

// placeholderMAC derives a stable, locally-administered MAC (02:00:o1:o2:o3:o4)
// from an IPv4 — used to reserve an excluded address so dnsmasq keeps it out of
// the dynamic pool, without colliding with any real device.
func placeholderMAC(ip string) string {
	u, ok := netutil.IPv4ToUint32(ip)
	if !ok {
		return "02:00:00:00:00:00"
	}
	return fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
}

// usedPoolIPs collects addresses already spoken for (enabled managed static
// reservations + excluded placeholders) so the whitelist allocator skips them.
func usedPoolIPs(servers []DHCPServer, statics []StaticLease) map[string]bool {
	used := map[string]bool{}
	for _, s := range statics {
		if s.Enabled && s.Managed && s.IP != "" {
			used[s.IP] = true
		}
	}
	for _, srv := range servers {
		for _, ip := range expandExclude(srv.Exclude) {
			used[ip] = true
		}
	}
	return used
}

// whitelistAllocator yields successive free IPs from the FIRST enabled managed
// pool's range, skipping already-used addresses. Returns "" when no pool exists
// or the range is exhausted. (Whitelist is global; multi-pool boxes only get
// allocations from the first pool — the common single-LAN case is exact.)
func whitelistAllocator(servers []DHCPServer, used map[string]bool) func() string {
	var cur, end uint32
	have := false
	for _, srv := range servers {
		if !srv.Managed || !srv.Enabled {
			continue
		}
		su, ok1 := netutil.IPv4ToUint32(srv.IPStart)
		eu, ok2 := netutil.IPv4ToUint32(srv.IPEnd)
		if ok1 && ok2 && eu >= su {
			cur, end, have = su, eu, true
			break
		}
	}
	return func() string {
		if !have {
			return ""
		}
		for cur <= end {
			ip := netutil.Uint32ToIPv4(cur)
			cur++
			if !used[ip] {
				used[ip] = true
				return ip
			}
		}
		return ""
	}
}

func (b *uciBackend) uciGet(key string) (string, error) {
	out, err := b.run.Run("", "uci", "-q", "get", key)
	return strings.TrimSpace(out), err
}

func (b *uciBackend) uciShow(config string) (string, error) {
	return b.run.Run("", "uci", "-q", "show", config)
}

// ---- live runtime reads (leases, interfaces, route table) ----

// Leases reads the dnsmasq lease file (if present) and annotates each lease.
// On odhcpd-only boxes the file may be absent → empty list (not an error).
func (b *uciBackend) Leases() ([]Lease, error) {
	path := "/tmp/dhcp.leases"
	if p, err := b.uciGet("dhcp.@dnsmasq[0].leasefile"); err == nil && p != "" {
		path = p
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return []Lease{}, nil
	}
	statics, _ := b.Statics()
	servers, _ := b.DHCPServers()
	staticByMAC := make(map[string]StaticLease, len(statics))
	for _, s := range statics {
		staticByMAC[s.MAC] = s
	}
	now := time.Now().Unix()
	var out []Lease
	for _, line := range strings.Split(string(raw), "\n") {
		pl, ok := netutil.ParseLeaseLine(line)
		if !ok {
			continue
		}
		l := Lease{Hostname: pl.Hostname, IP: pl.IP, MAC: pl.MAC, Expiry: pl.Expiry, Interface: ifaceForIP(pl.IP, servers)}
		if pl.Expiry > 0 && pl.Expiry > now {
			l.RemainingSeconds = pl.Expiry - now
		}
		if s, ok := staticByMAC[pl.MAC]; ok {
			l.Static = true
			l.Remark = s.Remark
			if l.Hostname == "" {
				l.Hostname = s.Hostname
			}
			if l.Interface == "" {
				l.Interface = s.Interface
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// Interfaces lists logical interfaces (config interface sections) with an IPv4.
func (b *uciBackend) Interfaces() ([]Interface, error) {
	show, err := b.uciShow("network")
	if err != nil {
		return []Interface{{Name: "lan", IPv4: "192.168.1.1", Netmask: "255.255.255.0", Prefix: 24, Up: true}}, nil
	}
	out := []Interface{}
	for _, s := range parseUci(show, "network") {
		if s.typ != "interface" {
			continue
		}
		ip := first(s.opts["ipaddr"])
		if ip == "" {
			continue
		}
		mask := first(s.opts["netmask"])
		prefix := 0
		if i := strings.IndexByte(ip, '/'); i >= 0 { // ipaddr may be CIDR
			prefix = atoiSafe(ip[i+1:])
			ip = ip[:i]
			if mask == "" {
				mask = netutil.PrefixToMask(prefix)
			}
		} else if mask != "" {
			prefix, _ = netutil.MaskToPrefix(mask)
		}
		if s.name == "loopback" {
			continue
		}
		out = append(out, Interface{Name: s.name, IPv4: ip, Netmask: mask, Prefix: prefix, Up: true})
	}
	if len(out) == 0 {
		out = append(out, Interface{Name: "lan", IPv4: "192.168.1.1", Netmask: "255.255.255.0", Prefix: 24, Up: true})
	}
	return out, nil
}

// RouteTable parses `ip [-6] route show` into rows.
func (b *uciBackend) RouteTable(family string) ([]RouteEntry, error) {
	args := []string{"route", "show"}
	if family == FamilyIPv6 {
		args = []string{"-6", "route", "show"}
	}
	out, err := b.run.Run("", "ip", args...)
	if err != nil {
		return []RouteEntry{}, nil
	}
	return parseIPRoute(out, family), nil
}

// parseIPRoute decodes `ip route show` text output into RouteEntry rows.
func parseIPRoute(out, family string) []RouteEntry {
	var rows []RouteEntry
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		e := RouteEntry{}
		dest := fields[0]
		if dest == "default" {
			if family == FamilyIPv6 {
				e.Target, e.Netmask = "::", "/0"
			} else {
				e.Target, e.Netmask = "0.0.0.0", "0.0.0.0"
			}
		} else {
			target, mask := splitCIDR(dest, family)
			if target == "" {
				continue
			}
			e.Target, e.Netmask = target, mask
		}
		for i := 1; i < len(fields)-1; i++ {
			switch fields[i] {
			case "via":
				e.Gateway = fields[i+1]
			case "dev":
				e.Interface = fields[i+1]
			case "metric":
				e.Metric = atoiSafe(fields[i+1])
			}
		}
		rows = append(rows, e)
	}
	return rows
}

// splitCIDR turns "10.0.0.0/24" into ("10.0.0.0","255.255.255.0") for ipv4, or
// ("2001:db8::","/48") for ipv6.
func splitCIDR(s, family string) (string, string) {
	target, prefixStr, hasSlash := strings.Cut(s, "/")
	if family == FamilyIPv6 {
		if !netutil.IsIPv6(target) {
			return "", ""
		}
		if !hasSlash {
			prefixStr = "128"
		}
		return target, "/" + prefixStr
	}
	if !netutil.IsIPv4(target) {
		return "", ""
	}
	if !hasSlash {
		return target, "255.255.255.255"
	}
	return target, netutil.PrefixToMask(atoiSafe(prefixStr))
}

// ---- uci show parsing ----

type uciSec struct {
	name string
	typ  string
	opts map[string][]string
}

// parseUci parses `uci show <config>` output into ordered sections. List options
// (shown as space-joined single-quoted values) become multi-element slices.
func parseUci(show, config string) []uciSec {
	prefix := config + "."
	var secs []uciSec
	idx := map[string]int{}
	ensure := func(name string) *uciSec {
		if i, ok := idx[name]; ok {
			return &secs[i]
		}
		idx[name] = len(secs)
		secs = append(secs, uciSec{name: name, opts: map[string][]string{}})
		return &secs[len(secs)-1]
	}
	for _, line := range strings.Split(show, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		body := strings.TrimPrefix(line, prefix)
		eq := strings.IndexByte(body, '=')
		if eq < 0 {
			continue
		}
		left, right := body[:eq], body[eq+1:]
		if !strings.Contains(left, ".") { // section decl: name=type
			ensure(left).typ = strings.Trim(right, "'")
			continue
		}
		dot := strings.IndexByte(left, '.')
		name, opt := left[:dot], left[dot+1:]
		ensure(name).opts[opt] = parseUciValues(right)
	}
	return secs
}

// parseUciValues splits a uci show RHS into values, respecting single quotes.
func parseUciValues(s string) []string {
	var out []string
	var cur strings.Builder
	inq := false
	for _, r := range s {
		switch {
		case r == '\'':
			inq = !inq
		case r == ' ' && !inq:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ---- small helpers ----

func first(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}

func orAuto(s string) string {
	if strings.TrimSpace(s) == "" {
		return "auto"
	}
	return s
}

// uciName makes an id safe as a UCI section name (alnum + _), preserving stock
// names like "lan" so apply() modifies them in place rather than duplicating.
func uciName(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "nm_" + s // uci section names cannot start with a digit
	}
	return s
}

// leasetimeToMin converts a uci leasetime ("12h","120m","1d","infinite") to minutes.
func leasetimeToMin(s string) int {
	s = strings.TrimSpace(s)
	if s == "" || s == "infinite" {
		return 0
	}
	unit := s[len(s)-1]
	num := atoiSafe(s[:len(s)-1])
	switch unit {
	case 'h':
		return num * 60
	case 'd':
		return num * 1440
	case 'm':
		return num
	case 's':
		if num < 60 {
			return 1
		}
		return num / 60
	default:
		return atoiSafe(s) / 60 // bare seconds
	}
}

// startLimitToRange computes the absolute pool start/end from a uci start offset
// + limit count, relative to the interface network base.
func startLimitToRange(ip, mask string, start, limit int) (string, string) {
	base, ok := netutil.NetworkBase(ip, mask)
	if !ok || limit <= 0 {
		return "", ""
	}
	bu, _ := netutil.IPv4ToUint32(base)
	su := bu + uint32(start)
	eu := su + uint32(limit) - 1
	return netutil.Uint32ToIPv4(su), netutil.Uint32ToIPv4(eu)
}

// splitOpt parses a "code,value" dhcp_option into (code,value).
func splitOpt(o string) (int, string, bool) {
	i := strings.IndexByte(o, ',')
	if i <= 0 {
		return 0, "", false
	}
	code := atoiSafe(o[:i])
	if code <= 0 {
		return 0, "", false
	}
	return code, o[i+1:], true
}

func joinDNS(a, b string) string {
	var parts []string
	if strings.TrimSpace(a) != "" {
		parts = append(parts, a)
	}
	if strings.TrimSpace(b) != "" {
		parts = append(parts, b)
	}
	return strings.Join(parts, ",")
}

func ifaceForIP(ip string, servers []DHCPServer) string {
	for _, s := range servers {
		if netutil.IPInRange(ip, s.IPStart, s.IPEnd) {
			return s.Interface
		}
	}
	return ""
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
