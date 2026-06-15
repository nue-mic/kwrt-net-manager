package netcfg

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// Managed-section marker. Only UCI sections carrying option managed_by =
// managedMarker are ever read/modified/deleted by this backend, so it never
// clobbers stock or LuCI/operator-authored config — the key to staying
// upgrade-safe across OpenWrt versions.
const (
	managedMarker = "kwrt-net-manager"
	managedOpt    = "managed_by"
)

// uciBackend is the OpenWrt backend. Design for multi-version compatibility:
//
//   - Source of truth is the sidecar JSON (embedded *storeBackend), so reads
//     never depend on version-specific UCI parsing and never lose fields UCI
//     can't represent (per-host gateway/DNS, remark, disabled items).
//   - Writes project ONLY the active config into UCI using primitives present
//     since ≤19.07 (config dhcp/host/route/route6 + interface/start/limit/
//     leasetime/dhcp_option/mac/ip/name/ignore/target/netmask/gateway/metric).
//     Disabled items are simply not rendered (kept in the sidecar), avoiding the
//     21.02 `option disabled` vs older `disabled_route` split entirely.
//   - Every managed section carries option managed_by; apply deletes only marked
//     sections, never touching stock/LuCI config.
//   - commit and reload are distinct phases; a failed reload sets Pending.
type uciBackend struct {
	*storeBackend // sidecar persistence + authoritative reads
	run        runner
	log        *slog.Logger
	applyMu    sync.Mutex
	pending    bool
	pendingMsg string
}

func newUCIBackend(run runner, sidecar string, log *slog.Logger) (*uciBackend, error) {
	sb, err := newStoreBackend(sidecar, log)
	if err != nil {
		return nil, err
	}
	b := &uciBackend{storeBackend: sb, run: run, log: log}
	// Best-effort: project current state into UCI on boot so /etc/config matches
	// what we persisted last run.
	if err := b.apply(); err != nil && log != nil {
		log.Warn("netcfg(uci): initial apply failed", slog.Any("err", err))
	}
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

func (b *uciBackend) RestartDHCP() error {
	_, err := b.run.Run("", "/etc/init.d/dnsmasq", "restart")
	return err
}

func (b *uciBackend) Status() (Status, error) {
	return Status{Backend: KindUCI, DHCPOK: !b.pending, Pending: b.pending, Message: b.pendingMsg}, nil
}

// ---- apply: regenerate managed UCI sections from the sidecar state ----

func (b *uciBackend) apply() error {
	b.applyMu.Lock()
	defer b.applyMu.Unlock()

	servers, _ := b.storeBackend.DHCPServers()
	statics, _ := b.storeBackend.Statics()
	acl, _ := b.storeBackend.ACL()
	routes, _ := b.storeBackend.Routes()

	dhcpBatch := b.buildDHCPBatch(servers, statics, acl)
	netBatch := b.buildNetworkBatch(routes)

	var firstErr error
	if out, err := b.run.Run(dhcpBatch, "uci", "batch"); err != nil {
		firstErr = fmt.Errorf("uci batch dhcp: %v (%s)", err, strings.TrimSpace(out))
	}
	if out, err := b.run.Run(netBatch, "uci", "batch"); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("uci batch network: %v (%s)", err, strings.TrimSpace(out))
	}

	// Reload is a distinct phase; failure means committed-but-not-live.
	b.pending, b.pendingMsg = false, ""
	if _, err := b.run.Run("", "/etc/init.d/dnsmasq", "reload"); err != nil {
		b.pending, b.pendingMsg = true, "dnsmasq reload 失败，配置已保存但未生效"
	}
	if _, err := b.run.Run("", "/etc/init.d/network", "reload"); err != nil {
		b.pending, b.pendingMsg = true, "network reload 失败，配置已保存但未生效"
	}
	return firstErr
}

func (b *uciBackend) buildDHCPBatch(servers []DHCPServer, statics []StaticLease, acl ACL) string {
	var sb strings.Builder
	if show, err := b.uciShow("dhcp"); err == nil {
		for _, name := range managedSections(show, "dhcp") {
			fmt.Fprintf(&sb, "delete dhcp.%s\n", name)
		}
	}
	for _, srv := range servers {
		if !srv.Enabled {
			continue
		}
		n := sanitizeSectionName(srv.ID)
		start, limit := b.startLimit(srv)
		fmt.Fprintf(&sb, "set dhcp.%s=dhcp\n", n)
		fmt.Fprintf(&sb, "set dhcp.%s.%s='%s'\n", n, managedOpt, managedMarker)
		fmt.Fprintf(&sb, "set dhcp.%s.interface='%s'\n", n, srv.Interface)
		fmt.Fprintf(&sb, "set dhcp.%s.start='%d'\n", n, start)
		fmt.Fprintf(&sb, "set dhcp.%s.limit='%d'\n", n, limit)
		fmt.Fprintf(&sb, "set dhcp.%s.leasetime='%dm'\n", n, srv.LeaseMinutes)
		if srv.RelayOnly {
			fmt.Fprintf(&sb, "set dhcp.%s.ignore='1'\n", n)
		}
		if srv.Gateway != "" {
			fmt.Fprintf(&sb, "add_list dhcp.%s.dhcp_option='3,%s'\n", n, srv.Gateway)
		}
		if dns := joinDNS(srv.DNSPrimary, srv.DNSSecondary); dns != "" {
			fmt.Fprintf(&sb, "add_list dhcp.%s.dhcp_option='6,%s'\n", n, dns)
		}
		for _, o := range srv.CustomOptions {
			fmt.Fprintf(&sb, "add_list dhcp.%s.dhcp_option='%d,%s'\n", n, o.Code, o.Value)
		}
	}
	for _, s := range statics {
		if !s.Enabled {
			continue
		}
		n := sanitizeSectionName(s.ID)
		fmt.Fprintf(&sb, "set dhcp.%s=host\n", n)
		fmt.Fprintf(&sb, "set dhcp.%s.%s='%s'\n", n, managedOpt, managedMarker)
		fmt.Fprintf(&sb, "set dhcp.%s.mac='%s'\n", n, s.MAC)
		fmt.Fprintf(&sb, "set dhcp.%s.ip='%s'\n", n, s.IP)
		// name MUST be omitted when empty — an empty name crashes dnsmasq.
		if s.Hostname != "" {
			fmt.Fprintf(&sb, "set dhcp.%s.name='%s'\n", n, s.Hostname)
		}
	}
	// Blacklist entries → host sections with ignore (broadly supported).
	if acl.Mode == ACLBlacklist {
		for _, e := range acl.Entries {
			if !e.Enabled {
				continue
			}
			n := sanitizeSectionName(e.ID)
			fmt.Fprintf(&sb, "set dhcp.%s=host\n", n)
			fmt.Fprintf(&sb, "set dhcp.%s.%s='%s'\n", n, managedOpt, managedMarker)
			fmt.Fprintf(&sb, "set dhcp.%s.mac='%s'\n", n, e.MAC)
			fmt.Fprintf(&sb, "set dhcp.%s.ignore='1'\n", n)
		}
	}
	sb.WriteString("commit dhcp\n")
	return sb.String()
}

func (b *uciBackend) buildNetworkBatch(routes []Route) string {
	var sb strings.Builder
	if show, err := b.uciShow("network"); err == nil {
		for _, name := range managedSections(show, "network") {
			fmt.Fprintf(&sb, "delete network.%s\n", name)
		}
	}
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		n := sanitizeSectionName(r.ID)
		secType := "route"
		if r.Family == FamilyIPv6 {
			secType = "route6"
		}
		fmt.Fprintf(&sb, "set network.%s=%s\n", n, secType)
		fmt.Fprintf(&sb, "set network.%s.%s='%s'\n", n, managedOpt, managedMarker)
		if r.Interface != "" && r.Interface != "auto" {
			fmt.Fprintf(&sb, "set network.%s.interface='%s'\n", n, r.Interface)
		}
		if r.Family == FamilyIPv6 {
			fmt.Fprintf(&sb, "set network.%s.target='%s/%d'\n", n, r.Target, r.Prefix)
		} else {
			fmt.Fprintf(&sb, "set network.%s.target='%s'\n", n, r.Target)
			fmt.Fprintf(&sb, "set network.%s.netmask='%s'\n", n, r.Netmask)
		}
		if r.Gateway != "" {
			fmt.Fprintf(&sb, "set network.%s.gateway='%s'\n", n, r.Gateway)
		}
		fmt.Fprintf(&sb, "set network.%s.metric='%d'\n", n, r.Metric)
	}
	sb.WriteString("commit network\n")
	return sb.String()
}

// startLimit computes dnsmasq start (host offset) + limit (count) from the
// pool's absolute start/end, using the interface's IP/mask. Falls back to the
// /24 host octet when the interface address can't be read.
func (b *uciBackend) startLimit(srv DHCPServer) (int, int) {
	mask := srv.Netmask
	if mask == "" {
		mask, _ = b.uciGet("network." + srv.Interface + ".netmask")
	}
	if ip, err := b.uciGet("network." + srv.Interface + ".ipaddr"); err == nil && ip != "" && mask != "" {
		if s, l, ok := netutil.DHCPStartLimit(ip, mask, srv.IPStart, srv.IPEnd); ok {
			return s, l
		}
	}
	su, _ := netutil.IPv4ToUint32(srv.IPStart)
	cnt, _ := netutil.RangeCount(srv.IPStart, srv.IPEnd)
	return int(su & 0xFF), cnt
}

func (b *uciBackend) uciGet(key string) (string, error) {
	out, err := b.run.Run("", "uci", "-q", "get", key)
	return strings.TrimSpace(out), err
}

func (b *uciBackend) uciShow(config string) (string, error) {
	return b.run.Run("", "uci", "-q", "show", config)
}

// ---- live runtime reads (leases, interfaces, route table) ----

// Leases reads the dnsmasq lease file and annotates each lease with whether it
// matches one of our reservations and the interface it belongs to.
func (b *uciBackend) Leases() ([]Lease, error) {
	path := "/tmp/dhcp.leases"
	if p, err := b.uciGet("dhcp.@dnsmasq[0].leasefile"); err == nil && p != "" {
		path = p
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return []Lease{}, nil // no leases yet is not an error
	}
	statics, _ := b.storeBackend.Statics()
	servers, _ := b.storeBackend.DHCPServers()
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
	type acc struct{ ipaddr, netmask string }
	ifaces := map[string]*acc{}
	order := []string{}
	for _, line := range strings.Split(show, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "network.") {
			continue
		}
		body := strings.TrimPrefix(line, "network.")
		if eq := strings.IndexByte(body, '='); eq >= 0 && !strings.Contains(body[:eq], ".") {
			// section decl: network.<name>=interface
			if strings.Trim(body[eq+1:], "'") == "interface" {
				name := body[:eq]
				if ifaces[name] == nil {
					ifaces[name] = &acc{}
					order = append(order, name)
				}
			}
			continue
		}
		// option line: network.<name>.<opt>='val'
		dot := strings.IndexByte(body, '.')
		eq := strings.IndexByte(body, '=')
		if dot < 0 || eq < 0 || eq < dot {
			continue
		}
		name, opt := body[:dot], body[dot+1:eq]
		val := strings.Trim(body[eq+1:], "'")
		if ifaces[name] == nil {
			continue
		}
		switch opt {
		case "ipaddr":
			ifaces[name].ipaddr = val
		case "netmask":
			ifaces[name].netmask = val
		}
	}
	out := []Interface{}
	for _, name := range order {
		a := ifaces[name]
		if a.ipaddr == "" {
			continue
		}
		prefix, _ := netutil.MaskToPrefix(a.netmask)
		out = append(out, Interface{Name: name, IPv4: a.ipaddr, Netmask: a.netmask, Prefix: prefix, Up: true})
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
// ("2001:db8::","/48") for ipv6. A bare address gets a host mask.
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

// ---- helpers ----

func joinDNS(a, b string) string {
	parts := []string{}
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
