package netcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// ---- NIC inventory via /sys/class/net (no iproute2 dependency) ----

func (b *uciBackend) NICs() ([]NIC, error) {
	const dir = "/sys/class/net"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []NIC{}, nil
	}
	bound := b.deviceToIface()
	addrs := b.nicAddrs()
	var out []NIC
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		base := dir + "/" + name
		n := NIC{
			Name:    name,
			MAC:     netutil.NormalizeMAC(readTrim(base + "/address")),
			MTU:     atoiSafe(readTrim(base + "/mtu")),
			Running: readTrim(base+"/operstate") == "up",
			Up:      readTrim(base+"/carrier") == "1",
			Duplex:  readTrim(base + "/duplex"),
			RxBytes: atou64(readTrim(base + "/statistics/rx_bytes")),
			TxBytes: atou64(readTrim(base + "/statistics/tx_bytes")),
			Kind:    nicKind(base, name),
		}
		if sp := atoiSafe(readTrim(base + "/speed")); sp > 0 {
			n.SpeedMb = sp
		}
		if r, ok := bound[name]; ok {
			n.Bound, n.Role = r.iface, r.role
		}
		n.IPAddrs = addrs[name]
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// nicAddrs 跑 `ip -o addr show`，返回 设备名 → [全部地址(CIDR)]（IPv4+IPv6，跳过本地回环）。
// 行形如：`2: eth0    inet 192.168.1.12/24 brd ... scope global eth0\  ...`
//
//	`3: br-lan  inet6 fe80::.../64 scope link \  ...`
func (b *uciBackend) nicAddrs() map[string][]string {
	out := map[string][]string{}
	res, err := b.run.Run("", "ip", "-o", "addr", "show")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(res, "\n") {
		f := strings.Fields(line)
		// f[0]="2:" f[1]=dev f[2]="inet"|"inet6" f[3]=addr/plen
		if len(f) < 4 || (f[2] != "inet" && f[2] != "inet6") {
			continue
		}
		dev, cidr := f[1], f[3]
		if dev == "lo" || strings.HasPrefix(cidr, "127.") || cidr == "::1/128" {
			continue
		}
		out[dev] = append(out[dev], cidr)
	}
	return out
}

func nicKind(base, name string) string {
	if _, err := os.Stat(base + "/bridge"); err == nil {
		return NICBridge
	}
	if _, err := os.Stat(base + "/wireless"); err == nil {
		return NICWifi
	}
	if _, err := os.Stat(base + "/phy80211"); err == nil {
		return NICWifi
	}
	if strings.Contains(name, ".") {
		return NICVLAN
	}
	if _, err := os.Stat(base + "/device"); err == nil {
		return NICPhysical
	}
	return NICVirtual
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func atou64(s string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return n
}

type ifaceRef struct{ iface, role string }

// deviceToIface maps a device/port name → the interface (and role) that uses it,
// so the NIC list can show what each card is bound to.
func (b *uciBackend) deviceToIface() map[string]ifaceRef {
	out := map[string]ifaceRef{}
	show, err := b.uciShow("network")
	if err != nil {
		return out
	}
	secs := parseUci(show, "network")
	// bridge device name → its member ports
	bridgePorts := map[string][]string{}
	for _, s := range secs {
		if s.typ == "device" && first(s.opts["type"]) == "bridge" {
			if nm := first(s.opts["name"]); nm != "" {
				bridgePorts[nm] = append([]string(nil), s.opts["ports"]...)
			}
		}
	}
	for _, s := range secs {
		if s.typ != "interface" || s.name == "loopback" {
			continue
		}
		// Skip IPv6-companion / unconfigured interfaces so the binding label
		// reflects the real IPv4 LAN/WAN, not its dhcpv6 sibling on the same NIC.
		if skipIfaceProto(first(s.opts["proto"])) {
			continue
		}
		role := ifaceRole(s)
		dev := first(s.opts["device"])
		if dev == "" {
			dev = first(s.opts["ifname"]) // legacy
		}
		if dev == "" {
			continue
		}
		out[dev] = ifaceRef{s.name, role}
		for _, p := range bridgePorts[dev] {
			out[p] = ifaceRef{s.name, role}
		}
	}
	return out
}

// skipIfaceProto reports whether an interface proto should be hidden from
// 内外网设置 (the IPv4 LAN/WAN view). IPv6-companion transports and raw/empty
// interfaces are kept out — iKuai surfaces IPv6 in a separate menu, and a
// dhcpv6 sibling must not show up as its own network port.
func skipIfaceProto(p string) bool {
	switch p {
	case "none", "dhcpv6", "6in4", "6to4", "6rd", "slaac", "grev6", "grev6tap":
		return true
	}
	return false
}

// ifaceRole classifies an interface as LAN or WAN. Name is the most reliable
// signal (lan*/wan* is the OpenWrt convention); proto dhcp/pppoe implies WAN.
// We deliberately DON'T treat "has a gateway" as WAN — a secondary/downstream
// device's LAN legitimately has an upstream gateway and must still read as LAN.
func ifaceRole(s uciSec) string {
	switch {
	case strings.HasPrefix(s.name, "wan"):
		return RoleWAN
	case strings.HasPrefix(s.name, "lan"):
		return RoleLAN
	}
	switch first(s.opts["proto"]) {
	case ProtoDHCP, ProtoPPPoE:
		return RoleWAN
	}
	return RoleLAN
}

// ---- configured LAN/WAN interfaces ----

func (b *uciBackend) NetIfaces() ([]NetIface, error) {
	show, err := b.uciShow("network")
	if err != nil {
		return []NetIface{}, nil
	}
	secs := parseUci(show, "network")
	bridgePorts := map[string][]string{}
	for _, s := range secs {
		if s.typ == "device" && first(s.opts["type"]) == "bridge" {
			if nm := first(s.opts["name"]); nm != "" {
				bridgePorts[nm] = append([]string(nil), s.opts["ports"]...)
			}
		}
	}
	var out []NetIface
	for _, s := range secs {
		if s.typ != "interface" || s.name == "loopback" {
			continue
		}
		// Skip unconfigured / IPv6-companion interfaces (proto 'none' for docker
		// veth/raw bridges, 'dhcpv6'/'slaac'/… for the IPv6 sibling of a LAN).
		// iKuai's 内外网设置 only lists real IPv4 LAN/WAN networks.
		if skipIfaceProto(first(s.opts["proto"])) {
			continue
		}
		ni := NetIface{
			ID: s.name, Name: s.name, Role: ifaceRole(s),
			Proto:    orDefault(first(s.opts["proto"]), ProtoStatic),
			Device:   firstOf(s.opts["device"], s.opts["ifname"]),
			Gateway:  first(s.opts["gateway"]),
			Username: first(s.opts["username"]),
			Password: first(s.opts["password"]),
			Service:  first(s.opts["service"]),
			AC:       first(s.opts["ac"]),
			MTU:      atoiSafe(first(s.opts["mtu"])),
			Remark:   first(s.opts["remark"]),
			CloneMAC: first(s.opts["macaddr"]),
		}
		ni.DefaultGW = first(s.opts["defaultroute"]) != "0"
		// addressing
		ip := first(s.opts["ipaddr"])
		if i := strings.IndexByte(ip, '/'); i >= 0 {
			ni.IPAddr = ip[:i]
			ni.Netmask = netutil.PrefixToMask(atoiSafe(ip[i+1:]))
		} else {
			ni.IPAddr = ip
			ni.Netmask = first(s.opts["netmask"])
		}
		dns := s.opts["dns"]
		if len(dns) > 0 {
			ni.DNSPrimary = dns[0]
		}
		if len(dns) > 1 {
			ni.DNSSecondary = dns[1]
		}
		// ports: bridge members, or the single device
		if p, ok := bridgePorts[ni.Device]; ok {
			ni.Ports = p
		} else if ni.Device != "" {
			ni.Ports = []string{ni.Device}
		}
		// runtime
		if st, ok := b.ifaceStatus(s.name); ok {
			ni.Up = st.up
			ni.RuntimeIP = st.ip
		}
		out = append(out, ni)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role == RoleWAN // WANs first
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

type ifStatus struct {
	up bool
	ip string
}

// ifaceStatus reads runtime state via ubus (best effort).
func (b *uciBackend) ifaceStatus(id string) (ifStatus, bool) {
	out, err := b.run.Run("", "ubus", "call", "network.interface."+id, "status")
	if err != nil || strings.TrimSpace(out) == "" {
		return ifStatus{}, false
	}
	var raw struct {
		Up          bool `json:"up"`
		IPv4Address []struct {
			Address string `json:"address"`
		} `json:"ipv4-address"`
	}
	if json.Unmarshal([]byte(out), &raw) != nil {
		return ifStatus{}, false
	}
	st := ifStatus{up: raw.Up}
	if len(raw.IPv4Address) > 0 {
		st.ip = raw.IPv4Address[0].Address
	}
	return st, true
}

// ---- write ----

func (b *uciBackend) SaveNetIface(in NetIface) error {
	id := uciName(in.ID)
	var sb strings.Builder
	fmt.Fprintf(&sb, "set network.%s=interface\n", id)

	if in.Role == RoleLAN {
		in.Proto = ProtoStatic
		fmt.Fprintf(&sb, "set network.%s.proto='static'\n", id)
		// Device/bridge selection by port count:
		//   ≥2 ports → a real bridge (DSA `config device type bridge`, 21.02+);
		//   1 port  → bind that NIC directly (no bridge — works on every version
		//             and avoids creating a bridge named after the NIC itself,
		//             which would collide with the physical device);
		//   0 ports → keep the existing device, leave topology untouched.
		ports := dedupePorts(in.Ports)
		switch {
		case len(ports) >= 2:
			dev := in.Device
			if dev == "" || !b.isBridgeDevice(dev) {
				dev = "br-" + id
			}
			fmt.Fprintf(&sb, "set network.%s.device='%s'\n", id, dev)
			b.writeBridge(&sb, dev, ports)
		case len(ports) == 1:
			fmt.Fprintf(&sb, "set network.%s.device='%s'\n", id, ports[0])
			b.detachPorts(&sb, "", ports) // 独占该物理口：从其它网桥摘除
		default:
			dev := in.Device
			if dev == "" {
				dev = "br-" + id
			}
			fmt.Fprintf(&sb, "set network.%s.device='%s'\n", id, dev)
		}
		if in.IPAddr != "" {
			fmt.Fprintf(&sb, "set network.%s.ipaddr='%s'\n", id, in.IPAddr)
		}
		if in.Netmask != "" {
			fmt.Fprintf(&sb, "set network.%s.netmask='%s'\n", id, in.Netmask)
		}
	} else {
		// WAN
		switch in.Proto {
		case ProtoPPPoE:
			fmt.Fprintf(&sb, "set network.%s.proto='pppoe'\n", id)
			setOpt(&sb, id, "username", in.Username)
			setOpt(&sb, id, "password", in.Password)
			setOptOrDel(&sb, id, "service", in.Service)
			setOptOrDel(&sb, id, "ac", in.AC)
			delOpt(&sb, id, "ipaddr", "netmask", "gateway")
		case ProtoStatic:
			fmt.Fprintf(&sb, "set network.%s.proto='static'\n", id)
			setOpt(&sb, id, "ipaddr", in.IPAddr)
			setOpt(&sb, id, "netmask", in.Netmask)
			setOptOrDel(&sb, id, "gateway", in.Gateway)
			delOpt(&sb, id, "username", "password")
			fmt.Fprintf(&sb, "delete network.%s.dns\n", id)
			if dns := joinDNS(in.DNSPrimary, in.DNSSecondary); dns != "" {
				for _, d := range strings.Split(dns, ",") {
					fmt.Fprintf(&sb, "add_list network.%s.dns='%s'\n", id, d)
				}
			}
		default: // dhcp
			fmt.Fprintf(&sb, "set network.%s.proto='dhcp'\n", id)
			delOpt(&sb, id, "ipaddr", "netmask", "gateway", "username", "password")
			fmt.Fprintf(&sb, "delete network.%s.dns\n", id)
		}
		dev := in.Device
		if dev == "" && len(in.Ports) > 0 {
			dev = in.Ports[0]
		}
		if dev != "" {
			fmt.Fprintf(&sb, "set network.%s.device='%s'\n", id, dev)
			b.detachPorts(&sb, "", []string{dev}) // WAN takes the NIC exclusively
		}
		if in.DefaultGW {
			fmt.Fprintf(&sb, "delete network.%s.defaultroute\n", id) // default is on
		} else {
			fmt.Fprintf(&sb, "set network.%s.defaultroute='0'\n", id)
		}
	}
	if in.MTU > 0 {
		fmt.Fprintf(&sb, "set network.%s.mtu='%d'\n", id, in.MTU)
	}
	setOptOrDel(&sb, id, "remark", in.Remark)
	sb.WriteString("commit network\n")

	if out, err := b.run.Run(sb.String(), "uci", "batch"); err != nil {
		return fmt.Errorf("uci batch network: %v (%s)", err, strings.TrimSpace(out))
	}
	if initdExists("network") {
		_, _ = b.run.Run("", "/etc/init.d/network", "reload")
	}
	return nil
}

// writeBridge ensures a `config device` bridge named dev exists with the given
// member ports, reusing the existing section (found by its name option).
func (b *uciBackend) writeBridge(sb *strings.Builder, dev string, ports []string) {
	sec := b.deviceSectionByName(dev)
	if sec == "" {
		// create a named bridge section keyed off the device name
		sec = uciName("dev_" + dev)
		fmt.Fprintf(sb, "set network.%s=device\n", sec)
	}
	fmt.Fprintf(sb, "set network.%s.type='bridge'\n", sec)
	fmt.Fprintf(sb, "set network.%s.name='%s'\n", sec, dev)
	fmt.Fprintf(sb, "delete network.%s.ports\n", sec)
	for _, p := range ports {
		if p != "" && p != dev {
			fmt.Fprintf(sb, "add_list network.%s.ports='%s'\n", sec, p)
		}
	}
	// Exclusivity: a NIC can only belong to one bridge — detach these ports from
	// every other bridge so re-binding a card actually moves it.
	b.detachPorts(sb, sec, ports)
}

// detachPorts removes the given ports from every bridge `config device` section
// except keepSection, so binding a NIC to one LAN/WAN unbinds it elsewhere.
func (b *uciBackend) detachPorts(sb *strings.Builder, keepSection string, ports []string) {
	if len(ports) == 0 {
		return
	}
	want := map[string]bool{}
	for _, p := range ports {
		want[p] = true
	}
	show, err := b.uciShow("network")
	if err != nil {
		return
	}
	for _, s := range parseUci(show, "network") {
		if s.typ != "device" || s.name == keepSection || first(s.opts["type"]) != "bridge" {
			continue
		}
		for _, e := range s.opts["ports"] {
			if want[e] {
				fmt.Fprintf(sb, "del_list network.%s.ports='%s'\n", s.name, e)
			}
		}
	}
}

// dedupePorts trims, drops empties, and removes duplicates while preserving
// order — so a port list from the UI maps cleanly to bridge members.
func dedupePorts(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// isBridgeDevice reports whether dev names a bridge: either the conventional
// br-* device, or an existing `config device type 'bridge'` with that name.
func (b *uciBackend) isBridgeDevice(dev string) bool {
	if dev == "" {
		return false
	}
	if strings.HasPrefix(dev, "br-") {
		return true
	}
	show, err := b.uciShow("network")
	if err != nil {
		return false
	}
	for _, s := range parseUci(show, "network") {
		if s.typ == "device" && first(s.opts["name"]) == dev && first(s.opts["type"]) == "bridge" {
			return true
		}
	}
	return false
}

// deviceSectionByName returns the section name of the `config device` whose
// name option equals dev, or "".
func (b *uciBackend) deviceSectionByName(dev string) string {
	show, err := b.uciShow("network")
	if err != nil {
		return ""
	}
	for _, s := range parseUci(show, "network") {
		if s.typ == "device" && first(s.opts["name"]) == dev {
			return s.name
		}
	}
	return ""
}

func (b *uciBackend) DeleteNetIface(id string) error {
	id = uciName(id)
	var sb strings.Builder
	fmt.Fprintf(&sb, "delete network.%s\n", id)
	sb.WriteString("commit network\n")
	if out, err := b.run.Run(sb.String(), "uci", "batch"); err != nil {
		return fmt.Errorf("delete interface: %v (%s)", err, strings.TrimSpace(out))
	}
	if initdExists("network") {
		_, _ = b.run.Run("", "/etc/init.d/network", "reload")
	}
	return nil
}

func (b *uciBackend) WANAction(id, action string) error {
	cmd := ""
	switch action {
	case "connect", "up":
		cmd = "ifup"
	case "disconnect", "down":
		cmd = "ifdown"
	case "restart", "redial":
		if out, err := b.run.Run("", "ifdown", id); err != nil {
			return fmt.Errorf("ifdown %s: %v (%s)", id, err, strings.TrimSpace(out))
		}
		cmd = "ifup"
	default:
		return fmt.Errorf("不支持的操作：%s", action)
	}
	if out, err := b.run.Run("", cmd, id); err != nil {
		return fmt.Errorf("%s %s: %v (%s)", cmd, id, err, strings.TrimSpace(out))
	}
	return nil
}

// ---- small helpers ----

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func firstOf(a, b []string) string {
	if v := first(a); v != "" {
		return v
	}
	return first(b)
}

func setOpt(sb *strings.Builder, id, opt, val string) {
	fmt.Fprintf(sb, "set network.%s.%s='%s'\n", id, opt, val)
}

func setOptOrDel(sb *strings.Builder, id, opt, val string) {
	if strings.TrimSpace(val) == "" {
		fmt.Fprintf(sb, "delete network.%s.%s\n", id, opt)
	} else {
		fmt.Fprintf(sb, "set network.%s.%s='%s'\n", id, opt, val)
	}
}

func delOpt(sb *strings.Builder, id string, opts ...string) {
	for _, o := range opts {
		fmt.Fprintf(sb, "delete network.%s.%s\n", id, o)
	}
}

// ---- DHCP service info + 一键安装 dnsmasq ----

func (b *uciBackend) pkgManager() string {
	if out, _ := b.run.Run("", "sh", "-c", "command -v opkg"); strings.TrimSpace(out) != "" {
		return "opkg"
	}
	if out, _ := b.run.Run("", "sh", "-c", "command -v apk"); strings.TrimSpace(out) != "" {
		return "apk"
	}
	return ""
}

func (b *uciBackend) DHCPServiceInfo() (DHCPSvcInfo, error) {
	info := DHCPSvcInfo{
		DnsmasqInstalled: initdExists("dnsmasq"),
		OdhcpdInstalled:  initdExists("odhcpd"),
		Daemon:           dhcpService(),
		PkgManager:       b.pkgManager(),
	}
	info.CanInstall = info.PkgManager != "" && !info.DnsmasqInstalled
	return info, nil
}

func (b *uciBackend) InstallDHCP() (string, error) {
	pm := b.pkgManager()
	if pm == "" {
		return "", errors.New("未找到包管理器（opkg / apk），无法自动安装 dnsmasq")
	}
	var log strings.Builder
	switch pm {
	case "apk":
		o1, _ := b.run.Run("", "apk", "update")
		log.WriteString(tail(o1, 600) + "\n")
		o2, err := b.run.Run("", "apk", "add", "dnsmasq")
		log.WriteString(tail(o2, 1600))
		if err != nil {
			return log.String(), fmt.Errorf("apk add dnsmasq 失败：%v", err)
		}
	default: // opkg
		o1, _ := b.run.Run("", "opkg", "update")
		log.WriteString(tail(o1, 600) + "\n")
		o2, err := b.run.Run("", "opkg", "install", "dnsmasq")
		log.WriteString(tail(o2, 1600))
		if err != nil {
			return log.String(), fmt.Errorf("opkg install dnsmasq 失败：%v", err)
		}
	}
	// Enable + start so it serves immediately.
	_, _ = b.run.Run("", "/etc/init.d/dnsmasq", "enable")
	_, _ = b.run.Run("", "/etc/init.d/dnsmasq", "start")
	return log.String(), nil
}

// tail returns the last n bytes of s (so opkg's verbose output stays bounded).
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
