package netcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// uci 后端的 IPv6 运行态读：ubus / ip 真实数据（override store 的模拟）。

// ---- WANv6 / LANv6 列表（旁车 + ubus 运行态富化） ----

func (b *uciBackend) WANv6s() ([]WANv6, error) {
	list, err := b.storeBackend.WANv6s()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if st, ok := b.ifaceStatusV6(list[i].ID); ok {
			list[i].IP6Address = st.addr
			list[i].IP6Prefix = st.prefix
			list[i].IP6Gateway = st.gw
			list[i].Up = st.up
			if st.local != "" {
				list[i].LocalLink = st.local
			}
		}
	}
	return list, nil
}

func (b *uciBackend) LANv6s() ([]LANv6, error) {
	list, err := b.storeBackend.LANv6s()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if st, ok := b.ifaceStatusV6(list[i].Interface); ok {
			list[i].IP6Address = st.addr
			if st.local != "" {
				list[i].LocalLink = st.local
			}
		}
	}
	return list, nil
}

type ifStatusV6 struct {
	addr, prefix, gw, local, l3dev string
	up                             bool
}

// ifaceStatusV6 reads an interface's IPv6 runtime state via ubus.
func (b *uciBackend) ifaceStatusV6(id string) (ifStatusV6, bool) {
	out, err := b.run.Run("", "ubus", "call", "network.interface."+id, "status")
	if err != nil || strings.TrimSpace(out) == "" {
		return ifStatusV6{}, false
	}
	var raw struct {
		Up          bool   `json:"up"`
		L3Device    string `json:"l3_device"`
		IPv6Address []struct {
			Address string `json:"address"`
			Mask    int    `json:"mask"`
		} `json:"ipv6-address"`
		IPv6Prefix []struct {
			Address string `json:"address"`
			Mask    int    `json:"mask"`
		} `json:"ipv6-prefix"`
		Route []struct {
			Target  string `json:"target"`
			Mask    int    `json:"mask"`
			Nexthop string `json:"nexthop"`
		} `json:"route"`
	}
	if json.Unmarshal([]byte(out), &raw) != nil {
		return ifStatusV6{}, false
	}
	st := ifStatusV6{up: raw.Up, l3dev: raw.L3Device}
	if len(raw.IPv6Address) > 0 {
		st.addr = fmt.Sprintf("%s/%d", raw.IPv6Address[0].Address, raw.IPv6Address[0].Mask)
	}
	if len(raw.IPv6Prefix) > 0 {
		st.prefix = fmt.Sprintf("%s/%d", raw.IPv6Prefix[0].Address, raw.IPv6Prefix[0].Mask)
	}
	for _, r := range raw.Route {
		if r.Mask == 0 && (r.Target == "::" || r.Target == "") && r.Nexthop != "" {
			st.gw = r.Nexthop
			break
		}
	}
	if st.l3dev != "" {
		st.local = b.linkLocalOf(st.l3dev)
	}
	return st, true
}

// linkLocalOf returns the fe80:: link-local address on a device (best effort).
func (b *uciBackend) linkLocalOf(dev string) string {
	out, err := b.run.Run("", "ip", "-6", "addr", "show", "dev", dev, "scope", "link")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		for i := 0; i < len(f)-1; i++ {
			if f[i] == "inet6" && strings.HasPrefix(f[i+1], "fe80") {
				return strings.SplitN(f[i+1], "/", 2)[0]
			}
		}
	}
	return ""
}

// ---- DHCPv6 leases (ubus dhcp ipv6leases) ----

func (b *uciBackend) LeasesV6() ([]LeaseV6, error) {
	out, err := b.run.Run("", "ubus", "call", "dhcp", "ipv6leases")
	if err != nil || strings.TrimSpace(out) == "" {
		return []LeaseV6{}, nil
	}
	var raw struct {
		Device map[string]struct {
			Leases []struct {
				DUID     string `json:"duid"`
				IAID     int64  `json:"iaid"`
				Hostname string `json:"hostname"`
				Valid    int64  `json:"valid"`
				IPv6Addr []struct {
					Address string `json:"address"`
				} `json:"ipv6-addr"`
				IPv6Prefix []struct {
					Address string `json:"address"`
					Mask    int    `json:"mask"`
				} `json:"ipv6-prefix"`
			} `json:"leases"`
		} `json:"device"`
	}
	if json.Unmarshal([]byte(out), &raw) != nil {
		return []LeaseV6{}, nil
	}
	// 前缀静态分配（按 DUID）用于标 static。
	statByDUID := map[string]bool{}
	if ps, _ := b.PrefixStaticsV6(); ps != nil {
		for _, p := range ps {
			if p.DUID != "" {
				statByDUID[strings.ToLower(p.DUID)] = true
			}
		}
	}
	out6 := []LeaseV6{}
	for dev, d := range raw.Device {
		for _, l := range d.Leases {
			le := LeaseV6{
				Hostname: l.Hostname, DUID: l.DUID, IAID: fmt.Sprintf("%x", l.IAID),
				Interface: dev, ValidSeconds: l.Valid, MAC: netutil.NormalizeMAC(duidToMAC(l.DUID)),
				Static: statByDUID[strings.ToLower(l.DUID)],
			}
			if len(l.IPv6Addr) > 0 {
				le.IPv6Addr = l.IPv6Addr[0].Address
			} else if len(l.IPv6Prefix) > 0 {
				le.IPv6Addr = fmt.Sprintf("%s/%d", l.IPv6Prefix[0].Address, l.IPv6Prefix[0].Mask)
			}
			out6 = append(out6, le)
		}
	}
	return out6, nil
}

// duidToMAC extracts the MAC from a DUID-LL (0003 0001 …) or DUID-LLT
// (0001 0001 <time> …) when the hardware type is Ethernet; "" otherwise.
func duidToMAC(duid string) string {
	d := strings.ToLower(strings.ReplaceAll(duid, ":", ""))
	var macHex string
	switch {
	case strings.HasPrefix(d, "00030001") && len(d) >= 8+12:
		macHex = d[8 : 8+12]
	case strings.HasPrefix(d, "00010001") && len(d) >= 16+12:
		macHex = d[16 : 16+12]
	default:
		return ""
	}
	var parts []string
	for i := 0; i+2 <= len(macHex); i += 2 {
		parts = append(parts, macHex[i:i+2])
	}
	return strings.Join(parts, ":")
}

// ---- NDP neighbors (ip -6 neighbor) ----

func (b *uciBackend) NeighborsV6() ([]NeighborV6, error) {
	out, err := b.run.Run("", "ip", "-6", "neighbor", "show")
	if err != nil {
		return []NeighborV6{}, nil
	}
	// 备注富化：按 MAC join 前缀静态分配 / 邻居无内置备注。
	out6 := []NeighborV6{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		n := NeighborV6{IPv6: f[0]}
		for i := 1; i < len(f); i++ {
			switch f[i] {
			case "dev":
				if i+1 < len(f) {
					n.Interface = f[i+1]
					i++
				}
			case "lladdr":
				if i+1 < len(f) {
					n.MAC = netutil.NormalizeMAC(f[i+1])
					i++
				}
			case "router":
				n.Router = true
			case "REACHABLE", "STALE", "DELAY", "PROBE", "FAILED", "PERMANENT", "NOARP", "INCOMPLETE":
				n.State = f[i]
			}
		}
		if n.MAC == "" {
			continue // 无 lladdr（FAILED/INCOMPLETE）→ 不是有效终端
		}
		out6 = append(out6, n)
	}
	return out6, nil
}

func (b *uciBackend) DeleteNeighborV6(addr, dev string) error {
	if dev == "" {
		return fmt.Errorf("缺少接口名")
	}
	if out, err := b.run.Run("", "ip", "-6", "neigh", "del", addr, "dev", dev); err != nil {
		return fmt.Errorf("删除邻居失败：%v（%s）", err, strings.TrimSpace(out))
	}
	return nil
}

func (b *uciBackend) FlushNeighborsV6(dev string) error {
	args := []string{"-6", "neigh", "flush", "all"}
	if dev != "" {
		args = []string{"-6", "neigh", "flush", "dev", dev}
	}
	if out, err := b.run.Run("", "ip", args...); err != nil {
		return fmt.Errorf("清空邻居失败：%v（%s）", err, strings.TrimSpace(out))
	}
	return nil
}

// ---- 线路详情 / 服务信息 / 包探测 ----

func (b *uciBackend) LinesV6() ([]LineV6, error) {
	wans, _ := b.storeBackend.WANv6s()
	conns := b.countConntrackV6()
	out := []LineV6{}
	for _, w := range wans {
		if !w.Enabled {
			continue
		}
		dev := w.Device
		if st, ok := b.ifaceStatusV6(w.ID); ok && st.l3dev != "" {
			dev = st.l3dev
		}
		dev = strings.TrimPrefix(dev, "@")
		line := LineV6{Line: w.Name, Connections: conns}
		if dev != "" {
			base := "/sys/class/net/" + dev + "/statistics/"
			line.TotalUp = int64(atou64(readTrim(base + "tx_bytes")))
			line.TotalDown = int64(atou64(readTrim(base + "rx_bytes")))
		}
		out = append(out, line)
	}
	return out, nil
}

// countConntrackV6 counts IPv6 conntrack entries (best effort).
func (b *uciBackend) countConntrackV6() int {
	if out, _ := b.run.Run("", "sh", "-c", "grep -c ipv6 /proc/net/nf_conntrack 2>/dev/null"); strings.TrimSpace(out) != "" {
		if n := atoiSafe(strings.TrimSpace(out)); n > 0 {
			return n
		}
	}
	out, _ := b.run.Run("", "sh", "-c", "conntrack -L -f ipv6 2>/dev/null | wc -l")
	return atoiSafe(strings.TrimSpace(out))
}

func (b *uciBackend) DHCPv6ServiceInfo() (DHCPv6SvcInfo, error) {
	info := DHCPv6SvcInfo{
		OdhcpdInstalled: initdExists("odhcpd"),
		PkgManager:      b.pkgManager(),
	}
	if o, _ := b.run.Run("", "sh", "-c", "pgrep -x odhcpd >/dev/null && echo y"); strings.TrimSpace(o) == "y" {
		info.OdhcpdRunning = true
	}
	if _, err := b.run.Run("", "ip", "-6", "neigh", "show"); err == nil {
		info.IPFull = true
	}
	// LAN 是否已开 DHCPv6 服务端。
	if show, err := b.uciShow("dhcp"); err == nil {
		for _, s := range parseUci(show, "dhcp") {
			if s.typ == "dhcp" && first(s.opts["dhcpv6"]) == "server" {
				info.LanServerOn = true
				break
			}
		}
	}
	return info, nil
}

func (b *uciBackend) TransitionPkg(proto string) (bool, string, error) {
	pkg := transitionPkgName(proto)
	if pkg == "" {
		return true, "", nil // dhcpv6/static6 无需额外包
	}
	installed := initdProtoExists(pkg)
	return installed, pkg, nil
}

// initdProtoExists reports whether a netifd proto handler (/lib/netifd/proto/
// <name>.sh) is present — the reliable "is the 6in4/6to4/6rd package installed?"
// check, without shelling out to opkg.
func initdProtoExists(proto string) bool {
	return fileExists("/lib/netifd/proto/" + proto + ".sh")
}

// fileExists reports whether path exists (file or dir).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
