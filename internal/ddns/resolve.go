package ddns

// device 解析：把目标 LAN 终端的 MAC 反查成它当前「稳定的全球 IPv6（GUA）」。
//
// 这是仿爱快「按终端 MAC 解析某 LAN 设备 IP」的核心——OpenWrt 原生 ddns-scripts 做不到
// （它的 ip_source 只能产出路由器自身/某接口地址）。我们在 Go 侧解析，把结果写进缓存文件，
// 再让 ddns-scripts 用 ip_source='script' 读该文件推送（见 apply.go）。
//
// 仅 IPv6：IPv4 LAN 私网地址推公网 DNS 无意义，故 device 源强制 AAAA（见 validate）。
//
// 数据源（与 netcfg 一致，但本包自闭环、无 netcfg 依赖，便于用 fakeRunner 单测）：
//   - `ubus call dhcp ipv6leases`：DHCPv6 租约（按 DUID 反推 MAC），地址恒为稳定分配，最优先；
//   - `ip -6 neighbor show`：邻居表，含 SLAAC/隐私/链路本地全部地址。
//
// 排序（pickStableGUA）：DHCPv6 租约 > EUI-64 稳定 SLAAC（IID 可由 MAC 推出、可校验归属）
//   > 其它全球地址（可能是 RFC8981 临时地址，best-effort）。隐私扩展设备只能 best-effort 锁定，
//   局限见前端文案与设计文档。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// Device 是一台候选 LAN 终端（供前端选择 + 展示当前解析到的 GUA）。
type Device struct {
	MAC      string `json:"mac"`
	Hostname string `json:"hostname,omitempty"`
	IPv6     string `json:"ipv6,omitempty"`   // 选中的稳定 GUA（空=当前无可用全球地址）
	Source   string `json:"source,omitempty"` // dhcpv6 | slaac | neighbor
	Vendor   string `json:"vendor,omitempty"` // OUI 厂商识别
}

// v6cand 是一条 MAC→IPv6 候选（来自租约或邻居表）。
type v6cand struct {
	mac      string // 归一化 MAC
	ip       net.IP // 16 字节
	fromDHCP bool   // 来自 DHCPv6 租约（恒稳定）
}

// resolveDeviceGUA 反查 mac 当前最稳定的全球 IPv6。返回 (ip, source, err)。
func resolveDeviceGUA(run Runner, mac string) (string, string, error) {
	target := netutil.NormalizeMAC(mac)
	if target == "" {
		return "", "", errors.New("无效的 MAC 地址")
	}
	var cands []v6cand
	if out, err := run.Run("", "ubus", "call", "dhcp", "ipv6leases"); err == nil {
		cands = append(cands, parseLeaseCandsV6(out)...)
	}
	if out, err := run.Run("", "ip", "-6", "neighbor", "show"); err == nil {
		cands = append(cands, parseNeighborCands(out)...)
	}
	ip, src := pickStableGUA(target, cands)
	if ip == "" {
		return "", "", fmt.Errorf("未发现终端 %s 的可用全球 IPv6（GUA）地址", target)
	}
	return ip, src, nil
}

// pickStableGUA 在候选里挑出 mac 最稳定的全球地址。无则返回 ("","")。
func pickStableGUA(mac string, cands []v6cand) (string, string) {
	target := netutil.NormalizeMAC(mac)
	if target == "" {
		return "", ""
	}
	iid := eui64IID(target) // 8 字节，或 nil

	bestRank := 99
	bestIP := ""
	bestSrc := ""
	for _, c := range cands {
		if c.mac != target || !isGlobalGUA(c.ip) {
			continue
		}
		rank, src := 2, "neighbor"
		switch {
		case c.fromDHCP:
			rank, src = 0, "dhcpv6"
		case iid != nil && bytes.Equal(c.ip[len(c.ip)-8:], iid):
			rank, src = 1, "slaac"
		}
		ipStr := c.ip.String()
		// 同 rank 取字典序最小，保证确定性。
		if rank < bestRank || (rank == bestRank && (bestIP == "" || ipStr < bestIP)) {
			bestRank, bestIP, bestSrc = rank, ipStr, src
		}
	}
	return bestIP, bestSrc
}

// isGlobalGUA 判断是否为可路由的全球单播 IPv6（排除 IPv4 / 链路本地 fe80 / ULA fc00::/7 / 回环/组播）。
func isGlobalGUA(ip net.IP) bool {
	return ip != nil && ip.To4() == nil && ip.IsGlobalUnicast() && !ip.IsPrivate()
}

// eui64IID 由 MAC 推出 SLAAC EUI-64 的 64 位接口标识（8 字节）。非法 MAC 返回 nil。
func eui64IID(mac string) []byte {
	hw, err := net.ParseMAC(netutil.NormalizeMAC(mac))
	if err != nil || len(hw) != 6 {
		return nil
	}
	return []byte{hw[0] ^ 0x02, hw[1], hw[2], 0xff, 0xfe, hw[3], hw[4], hw[5]}
}

// parseNeighborCands 解析 `ip -6 neighbor show` 输出为候选。
func parseNeighborCands(out string) []v6cand {
	var cs []v6cand
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		ip := net.ParseIP(f[0])
		if ip == nil {
			continue
		}
		mac := ""
		for i := 1; i < len(f); i++ {
			if f[i] == "lladdr" && i+1 < len(f) {
				mac = netutil.NormalizeMAC(f[i+1])
				i++
			}
		}
		if mac == "" {
			continue // 无 lladdr（FAILED/INCOMPLETE）
		}
		cs = append(cs, v6cand{mac: mac, ip: ip.To16(), fromDHCP: false})
	}
	return cs
}

// parseLeaseCandsV6 解析 `ubus call dhcp ipv6leases` JSON 为候选。
func parseLeaseCandsV6(out string) []v6cand {
	var raw struct {
		Device map[string]struct {
			Leases []struct {
				DUID     string `json:"duid"`
				IPv6Addr []struct {
					Address string `json:"address"`
				} `json:"ipv6-addr"`
			} `json:"leases"`
		} `json:"device"`
	}
	if json.Unmarshal([]byte(out), &raw) != nil {
		return nil
	}
	var cs []v6cand
	for _, d := range raw.Device {
		for _, l := range d.Leases {
			mac := netutil.NormalizeMAC(duidToMAC(l.DUID))
			if mac == "" {
				continue
			}
			for _, a := range l.IPv6Addr {
				if ip := net.ParseIP(a.Address); ip != nil {
					cs = append(cs, v6cand{mac: mac, ip: ip.To16(), fromDHCP: true})
				}
			}
		}
	}
	return cs
}

// duidToMAC 从 DUID-LL(0003 0001 …) 或 DUID-LLT(0001 0001 <time> …) 抽出以太网 MAC；否则 ""。
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

// listDevices 汇总当前可见的 LAN 终端（邻居表 + DHCPv6 租约 + dnsmasq 租约取主机名），
// 每台给出最稳定的 GUA，供前端「选择目标设备」下拉。
func listDevices(run Runner, leaseFile string) []Device {
	neigh, _ := run.Run("", "ip", "-6", "neighbor", "show")
	lease6, _ := run.Run("", "ubus", "call", "dhcp", "ipv6leases")
	cands := append(parseLeaseCandsV6(lease6), parseNeighborCands(neigh)...)

	hosts := leaseHostnames(leaseFile)

	macs := map[string]bool{}
	for _, c := range cands {
		macs[c.mac] = true
	}
	for m := range hosts {
		macs[m] = true
	}

	out := make([]Device, 0, len(macs))
	for m := range macs {
		ip, src := pickStableGUA(m, cands)
		out = append(out, Device{MAC: m, Hostname: hosts[m], IPv6: ip, Source: src, Vendor: netutil.Vendor(m)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hostname != out[j].Hostname {
			return out[i].Hostname < out[j].Hostname
		}
		return out[i].MAC < out[j].MAC
	})
	return out
}

// leaseHostnames 从 dnsmasq 租约文件读 MAC→主机名（best-effort，文件不存在返回空）。
func leaseHostnames(leaseFile string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(leaseFile)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(b), "\n") {
		// dnsmasq: <expiry> <mac> <ip> <hostname> <clientid>
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		mac := netutil.NormalizeMAC(f[1])
		if mac == "" {
			continue
		}
		if name := f[3]; name != "" && name != "*" {
			m[mac] = name
		}
	}
	return m
}
