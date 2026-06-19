package netcfg

import (
	"os"
	"strconv"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// NICDetail builds the 综合详情 for a single NIC, reading everything OpenWrt is
// guaranteed to expose: /sys/class/net/<name> (link/stats/master/bridge) and
// iproute2 `ip` (addresses + vlan). ethtool is best-effort — when it's absent
// (busybox/musl boxes often ship without it) the driver/link-capability fields
// stay empty and no error is raised.
func (b *uciBackend) NICDetail(name string) (NICDetail, error) {
	const root = "/sys/class/net"
	base := root + "/" + name
	if _, err := os.Stat(base); err != nil {
		return NICDetail{}, ErrNotFound
	}

	d := NICDetail{
		BridgePorts: []string{},
		Addrs:       []NICAddr{},
	}
	// Base NIC fields (mirror NICs()): reuse the same /sys read style + bound map.
	d.NIC = NIC{
		Name:    name,
		MAC:     netutil.NormalizeMAC(readTrim(base + "/address")),
		MTU:     atoiSafe(readTrim(base + "/mtu")),
		Running: readTrim(base+"/operstate") == "up",
		Up:      readTrim(base+"/carrier") == "1",
		Duplex:  readTrim(base + "/duplex"),
		RxBytes: atou64(readTrim(base + "/statistics/rx_bytes")),
		TxBytes: atou64(readTrim(base + "/statistics/tx_bytes")),
		Kind:    nicKind(base, name),
		IPAddrs: []string{},
	}
	if sp := atoiSafe(readTrim(base + "/speed")); sp > 0 {
		d.SpeedMb = sp
	}
	if r, ok := b.deviceToIface()[name]; ok {
		d.Bound, d.Role = r.iface, r.role
	}

	// Link-layer detail from sysfs.
	d.IfIndex = atoiSafe(readTrim(base + "/ifindex"))
	d.Operstate = readTrim(base + "/operstate")
	d.Carrier = readTrim(base+"/carrier") == "1"
	d.CarrierChanges = atoiSafe(readTrim(base + "/carrier_changes"))
	d.TxQueueLen = atoiSafe(readTrim(base + "/tx_queue_len"))
	d.IfAlias = readTrim(base + "/ifalias")

	// master: symlink → the bridge this NIC is enslaved to (basename only).
	if target, err := os.Readlink(base + "/master"); err == nil {
		d.Master = lastPathElem(target)
	}
	// bridge_ports: if this device IS a bridge, list its member ports.
	if entries, err := os.ReadDir(base + "/brif"); err == nil {
		for _, e := range entries {
			d.BridgePorts = append(d.BridgePorts, e.Name())
		}
	}

	// Statistics block.
	d.Stats = NICStats{
		RxBytes:    atou64(readTrim(base + "/statistics/rx_bytes")),
		TxBytes:    atou64(readTrim(base + "/statistics/tx_bytes")),
		RxPackets:  atou64(readTrim(base + "/statistics/rx_packets")),
		TxPackets:  atou64(readTrim(base + "/statistics/tx_packets")),
		RxErrors:   atou64(readTrim(base + "/statistics/rx_errors")),
		TxErrors:   atou64(readTrim(base + "/statistics/tx_errors")),
		RxDropped:  atou64(readTrim(base + "/statistics/rx_dropped")),
		TxDropped:  atou64(readTrim(base + "/statistics/tx_dropped")),
		Multicast:  atou64(readTrim(base + "/statistics/multicast")),
		Collisions: atou64(readTrim(base + "/statistics/collisions")),
	}

	// Addresses (structured) via `ip -o addr show dev <name>`.
	if out, err := b.run.Run("", "ip", "-o", "addr", "show", "dev", name); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if a, ok := parseAddrLine(line); ok {
				d.Addrs = append(d.Addrs, a)
				d.IPAddrs = append(d.IPAddrs, fmtCIDR(a))
			}
		}
	}

	// VLAN tag from `ip -d link show <name>` (best effort).
	if out, err := b.run.Run("", "ip", "-d", "link", "show", name); err == nil {
		if id, proto, ok := parseVlanLine(out); ok {
			d.VlanID, d.VlanProto = id, proto
		}
	}

	// ethtool — strictly best-effort. Missing binary or parse failure → leave blank.
	if out, err := b.run.Run("", "ethtool", name); err == nil {
		e := parseEthtool(out)
		d.SupportedModes = e.supported
		d.AdvertisedModes = e.advertised
		d.Autoneg = e.autoneg
		d.Port = e.port
		if e.speedMb > 0 && d.SpeedMb == 0 {
			d.SpeedMb = e.speedMb
		}
		if e.duplex != "" && d.Duplex == "" {
			d.Duplex = e.duplex
		}
		if e.linkDetected != nil {
			d.Up = *e.linkDetected
		}
	}
	if out, err := b.run.Run("", "ethtool", "-i", name); err == nil {
		i := parseEthtoolDriver(out)
		d.Driver, d.DriverVersion, d.Firmware, d.BusInfo = i.driver, i.version, i.firmware, i.busInfo
	}
	if out, err := b.run.Run("", "ethtool", "-P", name); err == nil {
		if mac := parseEthtoolPerm(out); mac != "" {
			d.PermMAC = mac
		}
	}

	if d.SupportedModes == nil {
		d.SupportedModes = []string{}
	}
	if d.AdvertisedModes == nil {
		d.AdvertisedModes = []string{}
	}
	return d, nil
}

// lastPathElem returns the final path segment of a (possibly symlink) target,
// e.g. "../../br-lan" → "br-lan".
func lastPathElem(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// fmtCIDR renders a NICAddr back to its "address/prefix" form.
func fmtCIDR(a NICAddr) string {
	return a.Address + "/" + strconv.Itoa(a.Prefix)
}

// ---- pure parsers (unit-tested in iface_uci_detail_test.go) ----

// parseAddrLine parses one `ip -o addr show` line into a NICAddr.
//
//	2: eth0    inet 192.168.1.12/24 brd 192.168.1.255 scope global eth0\  ...
//	3: br-lan  inet6 fe80::.../64 scope link \  ...
//
// Loopback addresses are skipped (ok=false). family→ipv4/ipv6, scope from the
// "scope <x>" token (defaults to "global" if absent).
func parseAddrLine(line string) (NICAddr, bool) {
	f := strings.Fields(line)
	// f[0]="2:" f[1]=dev f[2]="inet"|"inet6" f[3]=addr/plen
	if len(f) < 4 || (f[2] != "inet" && f[2] != "inet6") {
		return NICAddr{}, false
	}
	cidr := f[3]
	if strings.HasPrefix(cidr, "127.") || cidr == "::1/128" {
		return NICAddr{}, false
	}
	a := NICAddr{Scope: "global"}
	if f[2] == "inet6" {
		a.Family = FamilyIPv6
	} else {
		a.Family = FamilyIPv4
	}
	a.Address = cidr
	if j := strings.IndexByte(cidr, '/'); j >= 0 {
		a.Address = cidr[:j]
		a.Prefix = atoiSafe(cidr[j+1:])
	}
	for i := 4; i+1 < len(f); i++ {
		if f[i] == "scope" {
			a.Scope = f[i+1]
			break
		}
	}
	return a, true
}

// parseVlanLine extracts the VLAN id/proto from `ip -d link show <name>` output.
// The detail line looks like: `vlan protocol 802.1Q id 10 <...>`. Returns ok=false
// when no vlan info is present.
func parseVlanLine(out string) (id int, proto string, ok bool) {
	f := strings.Fields(out)
	for i := 0; i < len(f); i++ {
		if f[i] == "vlan" {
			for j := i + 1; j+1 < len(f); j++ {
				switch f[j] {
				case "protocol":
					proto = f[j+1]
				case "id":
					id = atoiSafe(f[j+1])
					ok = true
				}
			}
			if ok {
				return id, proto, true
			}
		}
	}
	return 0, "", false
}

type ethtoolInfo struct {
	supported, advertised []string
	autoneg, port, duplex string
	speedMb               int
	linkDetected          *bool
}

// parseEthtool parses `ethtool <name>` output (Speed/Duplex/Auto-negotiation/
// Port/Supported|Advertised link modes/Link detected). Tolerant of missing
// fields and the multi-line "link modes" continuation blocks.
func parseEthtool(out string) ethtoolInfo {
	var e ethtoolInfo
	lines := strings.Split(out, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		key, val := splitColon(line)
		switch key {
		case "Speed":
			// e.g. "1000Mb/s"
			v := strings.TrimSpace(val)
			v = strings.TrimSuffix(v, "Mb/s")
			e.speedMb = atoiSafe(strings.TrimSpace(v))
		case "Duplex":
			e.duplex = strings.ToLower(strings.TrimSpace(val))
		case "Auto-negotiation":
			e.autoneg = strings.ToLower(strings.TrimSpace(val))
		case "Port":
			e.port = strings.TrimSpace(val)
		case "Link detected":
			b := strings.EqualFold(strings.TrimSpace(val), "yes")
			e.linkDetected = &b
		case "Supported link modes":
			e.supported = collectModes(val, lines, &i)
		case "Advertised link modes":
			e.advertised = collectModes(val, lines, &i)
		}
	}
	return e
}

// collectModes gathers space-separated link-mode tokens from the first value
// line plus any indented continuation lines (which carry no colon). It advances
// *i past the continuation lines it consumes.
func collectModes(firstVal string, lines []string, i *int) []string {
	var modes []string
	modes = append(modes, strings.Fields(firstVal)...)
	for j := *i + 1; j < len(lines); j++ {
		next := lines[j]
		if strings.Contains(next, ":") || strings.TrimSpace(next) == "" {
			break
		}
		modes = append(modes, strings.Fields(next)...)
		*i = j
	}
	if len(modes) == 0 {
		return nil
	}
	return modes
}

type ethtoolDriverInfo struct {
	driver, version, firmware, busInfo string
}

// parseEthtoolDriver parses `ethtool -i <name>` (driver/version/firmware-version/
// bus-info).
func parseEthtoolDriver(out string) ethtoolDriverInfo {
	var d ethtoolDriverInfo
	for _, line := range strings.Split(out, "\n") {
		key, val := splitColon(strings.TrimSpace(line))
		switch key {
		case "driver":
			d.driver = strings.TrimSpace(val)
		case "version":
			d.version = strings.TrimSpace(val)
		case "firmware-version":
			d.firmware = strings.TrimSpace(val)
		case "bus-info":
			d.busInfo = strings.TrimSpace(val)
		}
	}
	return d
}

// parseEthtoolPerm parses `ethtool -P <name>` → "Permanent address: aa:bb:..".
func parseEthtoolPerm(out string) string {
	_, val := splitColon(strings.TrimSpace(out))
	return netutil.NormalizeMAC(strings.TrimSpace(val))
}

// splitColon splits "Key: value" into trimmed key + raw remainder (everything
// after the first colon). Returns ("", line) when there's no colon.
func splitColon(line string) (string, string) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", line
	}
	return strings.TrimSpace(line[:i]), line[i+1:]
}
