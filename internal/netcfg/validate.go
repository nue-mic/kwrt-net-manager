package netcfg

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// validateDHCPServer checks a DHCP pool definition. All semantic validation
// lives here (the Go layer), before anything is persisted/applied, because uci
// itself does not validate and a bad value crashes dnsmasq on reload.
func validateDHCPServer(s *DHCPServer) error {
	s.Interface = strings.TrimSpace(s.Interface)
	if s.Interface == "" {
		return errors.New("服务接口不能为空")
	}
	if !netutil.IsIPv4(s.IPStart) || !netutil.IsIPv4(s.IPEnd) {
		return errors.New("客户端地址起止必须是合法的 IPv4 地址")
	}
	if n, ok := netutil.RangeCount(s.IPStart, s.IPEnd); !ok || n <= 0 {
		return errors.New("客户端地址：结束地址必须不小于起始地址")
	}
	if s.Netmask != "" && !netutil.IsValidNetmask(s.Netmask) {
		return errors.New("子网掩码不合法")
	}
	if s.Netmask != "" && !netutil.SameSubnet(s.IPStart, s.IPEnd, s.Netmask) {
		return errors.New("客户端地址起止必须在同一子网内")
	}
	if s.Gateway != "" && !netutil.IsIPv4(s.Gateway) {
		return errors.New("网关不是合法的 IPv4 地址")
	}
	if s.DNSPrimary != "" && !netutil.IsIPv4(s.DNSPrimary) {
		return errors.New("首选 DNS 不是合法的 IPv4 地址")
	}
	if s.DNSSecondary != "" && !netutil.IsIPv4(s.DNSSecondary) {
		return errors.New("备选 DNS 不是合法的 IPv4 地址")
	}
	if s.LeaseMinutes <= 0 {
		return errors.New("租期（分钟）必须大于 0")
	}
	if s.ExpiredKeepHours < 0 {
		return errors.New("过期地址保留时间不能为负")
	}
	for _, line := range s.Exclude {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, _, ok := netutil.ParseExcludeLine(line); !ok {
			return fmt.Errorf("排除地址格式错误：%q（应为 IP 或 IP-IP，每行一条）", line)
		}
	}
	for _, o := range s.CustomOptions {
		if o.Code < 1 || o.Code > 254 {
			return fmt.Errorf("自定义 DHCP 选项号 %d 越界（应为 1-254）", o.Code)
		}
	}
	return nil
}

// validateStatic checks a DHCP reservation. The MAC is normalized in place.
func validateStatic(s *StaticLease) error {
	mac := netutil.NormalizeMAC(s.MAC)
	if mac == "" {
		return errors.New("绑定 MAC 不是合法的 MAC 地址")
	}
	s.MAC = mac
	if !netutil.IsIPv4(s.IP) {
		return errors.New("绑定 IP 不是合法的 IPv4 地址")
	}
	// 绑定接口对 dnsmasq host 而言是全局保留，不写入接口字段；此处仅作分组/展示，
	// 允许留空（与导入的既有 host 一致），不强制。
	s.Interface = strings.TrimSpace(s.Interface)
	if s.Gateway != "" && !netutil.IsIPv4(s.Gateway) {
		return errors.New("网关不是合法的 IPv4 地址")
	}
	if s.DNSPrimary != "" && !netutil.IsIPv4(s.DNSPrimary) {
		return errors.New("首选 DNS 不是合法的 IPv4 地址")
	}
	if s.DNSSecondary != "" && !netutil.IsIPv4(s.DNSSecondary) {
		return errors.New("备选 DNS 不是合法的 IPv4 地址")
	}
	return nil
}

// validateRoute checks a static route, normalizing family + netmask/prefix.
func validateRoute(r *Route) error {
	switch r.Family {
	case "", FamilyIPv4:
		r.Family = FamilyIPv4
	case FamilyIPv6:
	default:
		return errors.New("协议栈必须是 ipv4 或 ipv6")
	}
	r.Interface = strings.TrimSpace(r.Interface)
	if r.Interface == "" {
		r.Interface = "auto"
	}
	if r.Family == FamilyIPv4 {
		if !netutil.IsIPv4(r.Target) {
			return errors.New("目的地址不是合法的 IPv4 地址")
		}
		// Accept either a dotted netmask or a prefix; keep both in sync.
		if r.Netmask != "" {
			p, ok := netutil.MaskToPrefix(r.Netmask)
			if !ok {
				return errors.New("子网掩码不合法")
			}
			r.Prefix = p
		} else {
			if r.Prefix < 0 || r.Prefix > 32 {
				return errors.New("子网掩码/前缀不合法（0-32）")
			}
			r.Netmask = netutil.PrefixToMask(r.Prefix)
		}
		if r.Gateway != "" && !netutil.IsIPv4(r.Gateway) {
			return errors.New("网关不是合法的 IPv4 地址")
		}
	} else {
		if !netutil.IsIPv6(r.Target) {
			return errors.New("目的地址不是合法的 IPv6 地址")
		}
		if r.Prefix < 0 || r.Prefix > 128 {
			return errors.New("前缀长度不合法（0-128）")
		}
		r.Netmask = ""
		if r.Gateway != "" && !netutil.IsIP(r.Gateway) {
			return errors.New("网关不是合法的 IP 地址")
		}
	}
	if r.Metric < 0 {
		return errors.New("优先级不能为负")
	}
	return nil
}

// validateACLEntry checks + normalizes a MAC ACL entry.
func validateACLEntry(e *ACLEntry) error {
	mac := netutil.NormalizeMAC(e.MAC)
	if mac == "" {
		return errors.New("MAC 地址不合法")
	}
	e.MAC = mac
	return nil
}
