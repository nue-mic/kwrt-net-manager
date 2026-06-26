package netcfg

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/internal/eventbus"
	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// ================= NIC list (网卡列表) =================

// ListNICs returns the physical-NIC inventory.
func (s *Service) ListNICs() ([]NIC, error) {
	nics, err := s.be.NICs()
	if err != nil {
		return nil, err
	}
	if nics == nil {
		nics = []NIC{}
	}
	return nics, nil
}

// NICDetail returns the 综合详情 for one NIC by name (404 via ErrNotFound).
func (s *Service) NICDetail(name string) (NICDetail, error) {
	if !validNICName(name) {
		return NICDetail{}, ErrNotFound
	}
	return s.be.NICDetail(name)
}

// validNICName 限制网卡名为合法 Linux 接口名：非空、≤15(IFNAMSIZ)、仅 [A-Za-z0-9._-]，
// 且不含 ".."（防 /sys 路径穿越）与前导 '-'（防当成 ip/ethtool 参数）。
func validNICName(s string) bool {
	if s == "" || len(s) > 15 || s[0] == '-' || strings.Contains(s, "..") {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// ================= LAN/WAN interfaces (内外网设置) =================

// ListNetIfaces returns the configured LAN/WAN interfaces.
func (s *Service) ListNetIfaces() ([]NetIface, error) {
	out, err := s.be.NetIfaces()
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []NetIface{}
	}
	return out, nil
}

// GetNetIface returns one interface by id.
func (s *Service) GetNetIface(id string) (NetIface, error) {
	list, err := s.be.NetIfaces()
	if err != nil {
		return NetIface{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return NetIface{}, ErrNotFound
}

// SaveNetIface validates + persists a LAN/WAN interface (create or update).
func (s *Service) SaveNetIface(in NetIface) (NetIface, error) {
	if err := validateNetIface(&in); err != nil {
		return NetIface{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, _ := s.be.NetIfaces()
	if in.ID == "" {
		in.ID = s.nextIfaceID(in.Role)
		in.Name = in.ID
	}
	servers, _ := s.be.DHCPServers()
	if err := checkIfaceRelations(in, existing, servers); err != nil {
		return NetIface{}, err
	}
	if err := s.be.SaveNetIface(in); err != nil {
		return NetIface{}, err
	}
	s.publish(eventbus.TypeIfaceChanged, "save", 0)
	return in, nil
}

// DeleteNetIface removes an interface by id.
func (s *Service) DeleteNetIface(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.be.NetIfaces()
	if err != nil {
		return err
	}
	if err := canDeleteNetIface(id, existing); err != nil {
		return err
	}
	if err := s.be.DeleteNetIface(id); err != nil {
		return err
	}
	s.publish(eventbus.TypeIfaceChanged, "delete", 0)
	return nil
}

// WANAction runs connect/disconnect/restart on an interface (ifup/ifdown).
func (s *Service) WANAction(id, action string) error {
	if err := s.be.WANAction(id, action); err != nil {
		return err
	}
	s.publish(eventbus.TypeIfaceChanged, action, 0)
	return nil
}

// nextIfaceID returns the next free section name for a role (lan2, wan2, …).
func (s *Service) nextIfaceID(role string) string {
	list, _ := s.be.NetIfaces()
	used := map[string]bool{}
	for _, x := range list {
		used[x.ID] = true
	}
	if !used[role] {
		return role
	}
	for n := 2; n < 64; n++ {
		cand := role + strconv.Itoa(n)
		if !used[cand] {
			return cand
		}
	}
	return role + "_new"
}

// ================= DHCP service (一键安装 dnsmasq) =================

// DHCPServiceInfo reports the installed/running DHCP daemon.
func (s *Service) DHCPServiceInfo() (DHCPSvcInfo, error) { return s.be.DHCPServiceInfo() }

// InstallDHCP installs dnsmasq via the system package manager.
func (s *Service) InstallDHCP() (string, error) {
	out, err := s.be.InstallDHCP()
	if err == nil {
		s.publish(eventbus.TypeDHCPChanged, "install", 0)
	}
	return out, err
}

// ================= overview (内外网设置总览) =================

// NetOverview builds the LAN/WAN dashboard summary.
func (s *Service) NetOverview() (NetOverview, error) {
	ifaces, err := s.be.NetIfaces()
	if err != nil {
		return NetOverview{}, err
	}
	nics, _ := s.be.NICs()
	servers, _ := s.ListDHCPServers()
	leases, _ := s.ListLeases(LeaseFilter{})

	ov := NetOverview{WANs: []NetIface{}, LANs: []NetIface{}}
	for _, ni := range ifaces {
		if ni.Role == RoleWAN {
			ov.WANCount++
			if ni.Up {
				ov.WANUp++
			}
			ov.WANs = append(ov.WANs, ni)
		} else {
			ov.LANCount++
			if ni.Up {
				ov.LANUp++
			}
			ov.LANs = append(ov.LANs, ni)
		}
	}
	for _, srv := range servers {
		if srv.Enabled {
			ov.DHCPOn++
		}
	}
	ov.Terminals = len(leases)
	for _, n := range nics {
		if n.Kind == NICPhysical && n.Bound == "" {
			ov.FreePorts++
		}
	}
	ov.Connections = conntrackCount()
	return ov, nil
}

// conntrackCount reads the active conntrack entry count (0 if unavailable).
func conntrackCount() int {
	for _, p := range []string{
		"/proc/sys/net/netfilter/nf_conntrack_count",
		"/proc/sys/net/ipv4/netfilter/ip_conntrack_count",
	} {
		if b, err := os.ReadFile(p); err == nil {
			return atoiSafe(strings.TrimSpace(string(b)))
		}
	}
	return 0
}

// validateNetIface checks + normalizes a LAN/WAN interface config.
func validateNetIface(in *NetIface) error {
	if in.Role != RoleLAN && in.Role != RoleWAN {
		return errors.New("接口类型必须是 lan(内网) 或 wan(外网)")
	}
	in.ID = strings.TrimSpace(in.ID)
	if in.Role == RoleLAN {
		in.Proto = ProtoStatic
		if !netutil.IsIPv4(in.IPAddr) {
			return errors.New("内网 IP 地址不合法")
		}
		if in.Netmask != "" && !netutil.IsValidNetmask(in.Netmask) {
			return errors.New("子网掩码不合法")
		}
	} else {
		switch in.Proto {
		case ProtoPPPoE:
			if strings.TrimSpace(in.Username) == "" {
				return errors.New("PPPoE 账号不能为空")
			}
		case ProtoStatic:
			if !netutil.IsIPv4(in.IPAddr) {
				return errors.New("静态 IP 地址不合法")
			}
			if in.Gateway != "" && !netutil.IsIPv4(in.Gateway) {
				return errors.New("网关不是合法的 IPv4 地址")
			}
		case ProtoDHCP, "":
			in.Proto = ProtoDHCP
		default:
			return errors.New("接入方式必须是 dhcp / pppoe / static")
		}
	}
	if in.MTU < 0 || in.MTU > 9200 {
		return errors.New("MTU 越界（0-9200）")
	}
	// 接口自定义 DNS：多条，IPv4/IPv6 通吃；去重并过滤空行（前端动态列表常留空行）。
	// 原地过滤后写回 in.DNS，保证落地的就是归一化后的列表。
	{
		cleaned := in.DNS[:0]
		seen := map[string]bool{}
		for _, d := range in.DNS {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			if !netutil.IsIPv4(d) && !netutil.IsIPv6(d) {
				return errors.New("DNS 服务器不是合法的 IPv4/IPv6 地址：" + d)
			}
			if seen[d] {
				return errors.New("DNS 服务器重复：" + d)
			}
			seen[d] = true
			cleaned = append(cleaned, d)
		}
		in.DNS = cleaned
	}
	if in.DNSMetric < 0 {
		return errors.New("DNS 权重（dns_metric）不能为负")
	}
	// 附加 IP + 去重（与主 IP、彼此）
	seen := map[string]bool{}
	if in.IPAddr != "" {
		seen[in.IPAddr] = true
	}
	for i := range in.ExtraAddrs {
		a := &in.ExtraAddrs[i]
		switch a.Family {
		case "", FamilyIPv4:
			a.Family = FamilyIPv4
			if !netutil.IsIPv4(a.Address) {
				return errors.New("附加 IPv4 地址不合法：" + a.Address)
			}
			if a.Prefix < 1 || a.Prefix > 32 {
				return errors.New("附加 IPv4 掩码位需在 1-32：" + a.Address)
			}
		case FamilyIPv6:
			if !netutil.IsIPv6(a.Address) {
				return errors.New("附加 IPv6 地址不合法：" + a.Address)
			}
			if a.Prefix < 1 || a.Prefix > 128 {
				return errors.New("附加 IPv6 前缀需在 1-128：" + a.Address)
			}
		default:
			return errors.New("附加 IP family 必须是 ipv4 或 ipv6")
		}
		if seen[a.Address] {
			return errors.New("附加 IP 与已有地址重复：" + a.Address)
		}
		seen[a.Address] = true
	}
	// clone_mac 格式
	if strings.TrimSpace(in.CloneMAC) != "" && !netutil.IsMAC(in.CloneMAC) {
		return errors.New("克隆 MAC 格式不合法")
	}
	// 全量字段轻校验
	if in.Metric < 0 {
		return errors.New("线路优先级（metric）不能为负")
	}
	if in.IP6Assign < 0 || in.IP6Assign > 64 {
		return errors.New("IPv6 委派前缀长度（ip6assign）应在 0-64")
	}
	// 静态 IPv6 走 ExtraAddrs(family=ipv6)，其校验在上方附加 IP 循环统一处理。
	if in.IP6Gw != "" && !netutil.IsIPv6(in.IP6Gw) {
		return errors.New("IPv6 网关（ip6gw）不合法")
	}
	if in.IP6Prefix != "" && !netutil.IsIPv6(strings.SplitN(in.IP6Prefix, "/", 2)[0]) {
		return errors.New("IPv6 分发前缀（ip6prefix）不合法")
	}
	if in.Broadcast != "" && !netutil.IsIPv4(in.Broadcast) {
		return errors.New("广播地址不合法")
	}
	return nil
}

// allIfaceIPs 收集一个接口的全部 IPv4（主 + 附加）。
func allIfaceIPs(in NetIface) []string {
	var out []string
	if in.IPAddr != "" {
		out = append(out, in.IPAddr)
	}
	for _, a := range in.ExtraAddrs {
		if a.Address != "" {
			out = append(out, a.Address)
		}
	}
	return out
}

// checkIfaceRelations 做需要全局视图的关系校验：跨接口 IP 冲突（G4）、
// 改子网导致绑定的 DHCP 池越界（G8）。in 为待保存项，existing 为现有接口列表。
func checkIfaceRelations(in NetIface, existing []NetIface, servers []DHCPServer) error {
	mine := map[string]bool{}
	for _, ip := range allIfaceIPs(in) {
		mine[ip] = true
	}
	for _, x := range existing {
		if x.ID == in.ID {
			continue // 同一接口（更新）跳过自身
		}
		for _, ip := range allIfaceIPs(x) {
			if mine[ip] {
				name := x.Name
				if name == "" {
					name = x.ID
				}
				return errors.New("IP 地址 " + ip + " 已被接口 " + name + " 占用")
			}
		}
	}
	// G8：本接口若改了主 IP/子网，检查绑定它的 DHCP 池是否仍在子网内
	if in.IPAddr != "" && in.Netmask != "" {
		for _, s := range servers {
			if s.Interface != in.ID {
				continue
			}
			if s.IPStart != "" && !netutil.SameSubnet(s.IPStart, in.IPAddr, in.Netmask) ||
				s.IPEnd != "" && !netutil.SameSubnet(s.IPEnd, in.IPAddr, in.Netmask) {
				return errors.New("该内网已有 DHCP 地址池（" + s.IPStart + "-" + s.IPEnd + "）不在新子网内，请先到「DHCP 服务端」调整后再改子网")
			}
		}
	}
	return nil
}

// canDeleteNetIface 实现 G2：不允许删除最后一个内网（否则失去管理入口）。
func canDeleteNetIface(id string, existing []NetIface) error {
	lanCount, isLAN := 0, false
	for _, x := range existing {
		if x.Role == RoleLAN {
			lanCount++
			if x.ID == id {
				isLAN = true
			}
		}
	}
	if isLAN && lanCount <= 1 {
		return errors.New("至少保留一个内网（LAN），否则将失去管理入口")
	}
	return nil
}
