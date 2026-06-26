package netcfg

import (
	"errors"
	"strings"

	"github.com/nue-mic/kwrt-net-manager/pkg/netutil"
)

// dnsmasq 默认编译把 min/max-cache-ttl 钳到 1 小时（3600 秒）。
const dnsMaxCacheTTLCap = 3600

// validateDNSSettings 校验 + 规整全局 DNS 设置。所有语义校验在 commit 之前。
func validateDNSSettings(s *DNSSettings) error {
	s.DNSPrimary = strings.TrimSpace(s.DNSPrimary)
	s.DNSSecondary = strings.TrimSpace(s.DNSSecondary)
	if s.DNSPrimary != "" && !netutil.IsIP(s.DNSPrimary) {
		return errors.New("首选 DNS 不是合法的 IP 地址")
	}
	if s.DNSSecondary != "" && !netutil.IsIP(s.DNSSecondary) {
		return errors.New("备选 DNS 不是合法的 IP 地址")
	}
	// 开 noresolv（仅用指定上游）前必须有可达上游，否则路由器自身也会断 DNS。
	if s.NoResolv && s.DNSPrimary == "" && s.DNSSecondary == "" {
		return errors.New("启用「仅用指定上游」前必须至少填写一个上游 DNS，否则路由器自身也无法解析")
	}
	if s.CacheSize < 0 {
		s.CacheSize = 0
	}
	if s.LocalTTL < 0 {
		s.LocalTTL = 0
	}
	// min/max cache ttl 钳到 3600（超出 dnsmasq 默认编译会静默截断）。
	if s.MinCacheTTL < 0 {
		s.MinCacheTTL = 0
	}
	if s.MinCacheTTL > dnsMaxCacheTTLCap {
		s.MinCacheTTL = dnsMaxCacheTTLCap
	}
	if s.MaxCacheTTL < 0 {
		s.MaxCacheTTL = 0
	}
	if s.MaxCacheTTL > dnsMaxCacheTTLCap {
		s.MaxCacheTTL = dnsMaxCacheTTLCap
	}
	return nil
}

// validateDNSDoH 校验 DoH 配置。
func validateDNSDoH(d *DNSDoH) error {
	d.ResolverURL = strings.TrimSpace(d.ResolverURL)
	if d.Enabled {
		if !strings.HasPrefix(d.ResolverURL, "https://") {
			return errors.New("DoH 请求地址必须以 https:// 开头")
		}
	}
	if d.ListenPort == 0 {
		d.ListenPort = 5053
	}
	if d.ListenPort == 53 || d.ListenPort < 1 || d.ListenPort > 65535 {
		return errors.New("DoH 监听端口必须是 1-65535 且不能为 53（会与 dnsmasq 冲突）")
	}
	d.BootstrapDNS = strings.TrimSpace(d.BootstrapDNS)
	if d.BootstrapDNS != "" && !netutil.IsIP(strings.SplitN(d.BootstrapDNS, ",", 2)[0]) {
		return errors.New("引导 DNS 不是合法的 IP 地址")
	}
	return nil
}

// validateDNSRecord 校验 + 规整一条自定义解析记录。
func validateDNSRecord(r *DNSRecord) error {
	r.Domain = strings.TrimSpace(r.Domain)
	r.Address = strings.TrimSpace(r.Address)
	if r.Domain == "" {
		return errors.New("域名不能为空")
	}
	// *. 前缀视为通配。
	if strings.HasPrefix(r.Domain, "*.") {
		r.Wildcard = true
	}
	bare := strings.TrimPrefix(r.Domain, "*.")
	if !isPlausibleDomain(bare) {
		return errors.New("域名格式不合法")
	}
	if !netutil.IsIP(r.Address) {
		return errors.New("解析地址不是合法的 IP 地址")
	}
	// 解析类型由地址族决定（A=IPv4，AAAA=IPv6）。
	if netutil.IsIPv4(r.Address) {
		r.RecordType = DNSRecordA
	} else {
		r.RecordType = DNSRecordAAAA
	}
	return nil
}

// validateDNSDomainRoute 校验 + 规整一条域名分流 DNS。
func validateDNSDomainRoute(r *DNSDomainRoute) error {
	r.Domain = strings.TrimSpace(r.Domain)
	r.Server = strings.TrimSpace(r.Server)
	r.OutIface = strings.TrimSpace(r.OutIface)
	if !isPlausibleDomain(r.Domain) {
		return errors.New("域名格式不合法")
	}
	// 支持一个域配多个上游（逗号/空格分隔），逐个校验。
	ups := splitUpstreams(r.Server)
	if len(ups) == 0 {
		return errors.New("请至少填写一个上游 DNS")
	}
	for _, up := range ups {
		host := up
		if i := strings.IndexByte(host, '#'); i >= 0 { // 去掉 #port 再校验 IP
			host = host[:i]
		}
		if !netutil.IsIP(host) {
			return errors.New("上游 DNS「" + up + "」不是合法的 IP 地址（可带 #端口）")
		}
	}
	return nil
}

// isPlausibleDomain 做宽松的域名字面量校验（字母数字、连字符、点；不含空格）。
func isPlausibleDomain(d string) bool {
	if d == "" || len(d) > 253 || strings.ContainsAny(d, " \t/'\"") {
		return false
	}
	for _, r := range d {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '.', r == '_':
		default:
			return false
		}
	}
	return true
}
