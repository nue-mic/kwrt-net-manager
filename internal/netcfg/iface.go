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
	if in.ID == "" {
		in.ID = s.nextIfaceID(in.Role)
		in.Name = in.ID
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
	if in.DNSPrimary != "" && !netutil.IsIPv4(in.DNSPrimary) {
		return errors.New("首选 DNS 不合法")
	}
	if in.DNSSecondary != "" && !netutil.IsIPv4(in.DNSSecondary) {
		return errors.New("备选 DNS 不合法")
	}
	return nil
}
