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
		// 全量字段回读
		ni.Metric = atoiSafe(first(s.opts["metric"]))
		ni.PeerDNS = parseBoolOpt(s.opts["peerdns"])
		ni.Broadcast = first(s.opts["broadcast"])
		ni.ForceLink = parseBoolOpt(s.opts["force_link"])
		ni.Auto = parseBoolOpt(s.opts["auto"])
		ni.IP6Assign = atoiSafe(first(s.opts["ip6assign"]))
		ni.IP6Hint = first(s.opts["ip6hint"])
		ni.IP6Addr = first(s.opts["ip6addr"])
		ni.IP6Gw = first(s.opts["ip6gw"])
		// addressing: 优先 list ipaddr（多条 CIDR），回退 option ipaddr+netmask。
		// parseUci 对 option 与 list 同名都收进切片，第一条为主、其余进 ExtraAddrs。
		addrs := s.opts["ipaddr"]
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
		// clone_mac: 优先 device 段的 macaddr，回退 interface 段（上面的字面量已置默认）
		if devSec := b.deviceSectionByName(ni.Device); devSec != "" {
			if m := b.deviceMacAddr(devSec); m != "" {
				ni.CloneMAC = m
			}
		}
		// runtime
		if st, ok := b.ifaceStatus(s.name); ok {
			ni.Up = st.up
			ni.RuntimeIP = st.ip
		}
		mergeExtraRemarks(&ni, b.storeBackend)
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
	// 构造 batch 前（基于当前 UCI）记录接口原先承载的托管 device 段：拓扑/设备切换后
	// 若它不再被新的 chosenDev 使用，需回收（否则旧桥/旧 clone_mac 段残留 → MAC 仍生效）。
	oldDevSec := b.managedDeviceSectionOf(id)
	// 旁车权威：先把整条 NetIface（含附加 IP 备注）存进内嵌 store，再投射 UCI。
	_ = b.storeBackend.SaveNetIface(in)
	var sb strings.Builder
	fmt.Fprintf(&sb, "set network.%s=interface\n", id)

	var chosenDev string // 最终落到 interface.device 的设备名（供 clone_mac 定位）

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
			chosenDev = dev
		case len(ports) == 1:
			fmt.Fprintf(&sb, "set network.%s.device='%s'\n", id, ports[0])
			b.detachPorts(&sb, "", ports) // 独占该物理口：从其它网桥摘除
			chosenDev = ports[0]
		default:
			dev := in.Device
			if dev == "" {
				dev = "br-" + id
			}
			fmt.Fprintf(&sb, "set network.%s.device='%s'\n", id, dev)
			chosenDev = dev
		}
		writeAddrList(&sb, id, in)
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
			writeAddrList(&sb, id, in)
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
			chosenDev = dev
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
	writeIfaceExtraOpts(&sb, id, in)
	b.ensureDeviceMAC(&sb, id, chosenDev, in.CloneMAC)
	// 拓扑/设备切换回收：旧托管 device 段若不再对应新选中的 chosenDev，则删除，
	// 避免孤儿桥段或旧 clone_mac 段残留（newSec 基于旧 UCI；新建的桥段尚不在其中）。
	if oldDevSec != "" {
		newSec := b.deviceSectionByName(chosenDev)
		if oldDevSec != newSec {
			fmt.Fprintf(&sb, "delete network.%s\n", oldDevSec)
		}
	}
	sb.WriteString("commit network\n")

	if out, err := b.run.Run(sb.String(), "uci", "batch"); err != nil {
		return fmt.Errorf("uci batch network: %v (%s)", err, strings.TrimSpace(out))
	}
	if initdExists("network") {
		_, _ = b.run.Run("", "/etc/init.d/network", "reload")
	}
	b.ensureZoneMembership(id, in.Role)
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
	fmt.Fprintf(sb, "set network.%s.%s='%s'\n", sec, managedOpt, managedMarker)
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

// managedDeviceSectionOf 返回接口 id 的 device 选项当前指向的、且由本工具托管(managed_by)
// 的 `config device` 段名；找不到返回 ""。用于删除/拓扑切换时精确回收，不靠 dev_<id> 猜名。
func (b *uciBackend) managedDeviceSectionOf(id string) string {
	show, err := b.uciShow("network")
	if err != nil {
		return ""
	}
	secs := parseUci(show, "network")
	var dev string
	for _, s := range secs {
		if s.typ == "interface" && s.name == id {
			dev = firstOf(s.opts["device"], s.opts["ifname"])
		}
	}
	if dev == "" {
		return ""
	}
	for _, s := range secs {
		if s.typ == "device" && first(s.opts["name"]) == dev && first(s.opts[managedOpt]) == managedMarker {
			return s.name
		}
	}
	return ""
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

func (b *uciBackend) DeleteNetIface(id string) error {
	id = uciName(id)
	// 删段前（基于当前 UCI）按接口 device 选项指向的托管段精确定位，不靠 dev_<id> 猜名。
	devSec := b.managedDeviceSectionOf(id)
	_ = b.storeBackend.DeleteNetIface(id) // 同步旁车
	var sb strings.Builder
	fmt.Fprintf(&sb, "delete network.%s\n", id)
	// 删除本工具托管的承载 device 段（桥/克隆MAC 段），不碰 stock/手改段。
	if devSec != "" {
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
