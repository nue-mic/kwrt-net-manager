package netcfg

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// IPv6 各类型的语义校验，全部在 Go 层、commit 之前（odhcpd/netifd 不校验，坏值
// 会让 RA/DHCPv6 不生效或接口拉不起来）。错误信息中文，对齐既有 validate.go 风格。

// validateWANv6 校验并规范化一条 IPv6 外网线路。
func validateWANv6(w *WANv6) error {
	w.ID = strings.TrimSpace(w.ID)
	if w.Name == "" {
		w.Name = w.ID
	}
	switch w.Proto {
	case ProtoDHCPv6, ProtoStatic6, Proto6in4, Proto6to4, Proto6rd:
	case "":
		w.Proto = ProtoDHCPv6
	default:
		return fmt.Errorf("不支持的 IPv6 接入方式：%s", w.Proto)
	}
	switch w.Proto {
	case ProtoDHCPv6:
		if rp := strings.TrimSpace(w.ReqPrefix); rp != "" && rp != "auto" && rp != "no" {
			if p := atoiSafe(rp); p < 1 || p > 64 {
				return errors.New("请求前缀长度应为 1-64、auto 或 no")
			}
		}
		if w.FixedPrefix != "" && !isIPv6CIDR(w.FixedPrefix) {
			return errors.New("尝试固定前缀必须是合法的 IPv6 前缀（如 2001:db8::/60）")
		}
		if w.ClientID != "" && !isHexBytes(w.ClientID) {
			return errors.New("客户端 DUID 必须是十六进制串")
		}
	case ProtoStatic6:
		if !isIPv6CIDR(w.StaticIP6) {
			return errors.New("静态 IPv6 地址必须带前缀（如 2001:db8::1/64）")
		}
		if w.StaticGW != "" && !netutil.IsIPv6(w.StaticGW) {
			return errors.New("IPv6 网关不是合法的 IPv6 地址")
		}
	case Proto6in4, Proto6rd:
		if w.PeerAddr != "" && !netutil.IsIPv4(w.PeerAddr) {
			return errors.New("隧道对端地址不是合法的 IPv4 地址")
		}
		if w.TunPrefix != "" && !isIPv6CIDR(w.TunPrefix) {
			return errors.New("隧道前缀必须是合法的 IPv6 前缀")
		}
	}
	if !w.PeerDNS {
		if w.DNSPrimary != "" && !netutil.IsIPv6(w.DNSPrimary) {
			return errors.New("首选 IPv6 DNS 不合法")
		}
		if w.DNSSecondary != "" && !netutil.IsIPv6(w.DNSSecondary) {
			return errors.New("备选 IPv6 DNS 不合法")
		}
	}
	if w.MTU != 0 && (w.MTU < 1280 || w.MTU > 9200) {
		return errors.New("IPv6 MTU 应为 0 或 1280-9200")
	}
	return nil
}

// validateLANv6 校验并规范化一条 IPv6 内网。
func validateLANv6(l *LANv6) error {
	l.Interface = strings.TrimSpace(l.Interface)
	if l.Interface == "" {
		return errors.New("内网接口不能为空")
	}
	if l.ID == "" {
		l.ID = l.Interface
	}
	switch l.ConfigType {
	case ConfigTypeAuto, ConfigTypeStatic:
	case "":
		l.ConfigType = ConfigTypeAuto
	default:
		return fmt.Errorf("不支持的配置类型：%s", l.ConfigType)
	}
	switch l.DHCPv6Mode {
	case DHCPv6Stateless, DHCPv6Stateful, DHCPv6StatefulOnly:
	case "":
		l.DHCPv6Mode = DHCPv6Stateful
	default:
		return fmt.Errorf("不支持的 DHCPv6 模式：%s", l.DHCPv6Mode)
	}
	if l.PrefixAssignLen != 0 && (l.PrefixAssignLen < 1 || l.PrefixAssignLen > 64) {
		return errors.New("前缀分配长度应为 1-64")
	}
	if l.ConfigType == ConfigTypeStatic && !isIPv6CIDR(l.StaticIP6) {
		return errors.New("静态模式下内网 IPv6 必须带前缀（如 fd00::1/64）")
	}
	for _, d := range l.DNSServers {
		if d = strings.TrimSpace(d); d != "" && !netutil.IsIPv6(d) {
			return fmt.Errorf("IPv6 DNS %q 不合法", d)
		}
	}
	if l.LeaseMinutes < 0 {
		return errors.New("租期不能为负")
	}
	if l.RAMTUEnabled && (l.RAMTU < 1280 || l.RAMTU > 9200) {
		return errors.New("RA MTU 应为 1280-9200")
	}
	return nil
}

// validatePrefixStaticV6 校验前缀静态分配（DHCPv6 host 保留 IID）。
func validatePrefixStaticV6(p *PrefixStaticV6) error {
	p.DUID = strings.TrimSpace(p.DUID)
	p.MAC = netutil.NormalizeMAC(p.MAC)
	if p.DUID == "" && p.MAC == "" {
		return errors.New("前缀静态分配需提供 DUID 或 MAC 之一作为匹配键")
	}
	if p.DUID != "" && !isHexBytes(p.DUID) {
		return errors.New("DUID 必须是十六进制串")
	}
	if p.LocalLink != "" && !netutil.IsIPv6(p.LocalLink) {
		return errors.New("终端本地链接地址不是合法的 IPv6 地址")
	}
	if p.HostID != "" && !isValidHostID(p.HostID) {
		return errors.New("接口 ID 必须是 hex（如 ::1234 或 1234）")
	}
	return nil
}

// validateACLv6Entry 校验 DHCPv6 接入控制条目。
func validateACLv6Entry(e *ACLv6Entry) error {
	switch e.Method {
	case ACLv6MethodDUID, "":
		e.Method = ACLv6MethodDUID
	case ACLv6MethodL2:
		// 诚实降级：odhcpd 无按 MAC 拒发能力，且 SLAAC 可绕过 L2 过滤。第一版不
		// 接纳 L2 方法（绝不存一条对系统无效的规则冒充已拦截），仅提供 DUID 拒发。
		return errors.New("「按 MAC L2 拦截」为实验功能、暂未实现（odhcpd 原生不支持、SLAAC 可绕过），请使用「按 DUID 拒发」")
	default:
		return fmt.Errorf("不支持的接入控制方法：%s", e.Method)
	}
	e.DUID = strings.TrimSpace(e.DUID)
	if e.DUID == "" || !isHexBytes(e.DUID) {
		return errors.New("按 DUID 拒发需提供合法的十六进制 DUID")
	}
	return nil
}

// ---- IPv6 校验小工具 ----

// isIPv6CIDR 检查 "<ipv6>/<prefix>" 形式。
func isIPv6CIDR(s string) bool {
	addr, prefix, ok := strings.Cut(s, "/")
	if !ok {
		return false
	}
	if !netutil.IsIPv6(addr) {
		return false
	}
	p := atoiSafe(prefix)
	return p >= 1 && p <= 128 && prefix == fmt.Sprintf("%d", p)
}

// isHexBytes 检查是否为十六进制串（允许冒号分隔，长度去冒号后为偶数）。
func isHexBytes(s string) bool {
	s = strings.ReplaceAll(strings.TrimSpace(s), ":", "")
	if s == "" || len(s)%2 != 0 {
		return false
	}
	for _, r := range s {
		if !isHexRune(r) {
			return false
		}
	}
	return true
}

// isHexRune reports whether r is a hexadecimal digit.
func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// isValidHostID 检查 odhcpd hostid：可是 "::1234" 或裸 hex "1234"。
func isValidHostID(s string) bool {
	s = strings.TrimPrefix(strings.TrimSpace(s), "::")
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isHexRune(r) && r != ':' {
			return false
		}
	}
	return true
}
