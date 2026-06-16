package netcfg

import (
	"strings"
	"testing"
)

// 上游 server + filter_aaaa + 缓存标量投射到 stock @dnsmasq[0]。
func TestUCIDNSSettingsProjection(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	if _, err := svc.SaveDNSSettings(DNSSettings{
		Enabled: true, DNSPrimary: "223.5.5.5", DNSSecondary: "114.114.114.114",
		FilterAAAA: true, MinCacheTTL: 600, CacheSize: 8000,
	}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.@dnsmasq[0].filter_aaaa='1'",
		"set dhcp.@dnsmasq[0].min_cache_ttl='600'",
		"set dhcp.@dnsmasq[0].cachesize='8000'",
		"add_list dhcp.@dnsmasq[0].server='223.5.5.5'",
		"add_list dhcp.@dnsmasq[0].server='114.114.114.114'",
	} {
		if !strings.Contains(dhcp, w) {
			t.Errorf("settings batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
}

// 禁用托管后回写 stock 旧值（SavedStock）。
func TestUCIDNSDisableRestores(t *testing.T) {
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": ""},
		get:  map[string]string{"dhcp.@dnsmasq[0].cachesize": "8000", "dhcp.@dnsmasq[0].min_cache_ttl": "3600"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	// 先接管（快照旧值 8000/3600）。
	if _, err := svc.SaveDNSSettings(DNSSettings{Enabled: true, DNSPrimary: "1.1.1.1", CacheSize: 100}); err != nil {
		t.Fatal(err)
	}
	// 再关闭托管 → 应回写旧值。
	if _, err := svc.SaveDNSSettings(DNSSettings{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.@dnsmasq[0].cachesize='8000'",      // 回写快照
		"set dhcp.@dnsmasq[0].min_cache_ttl='3600'",  // 回写快照
		"del_list dhcp.@dnsmasq[0].server='1.1.1.1'", // 撤掉自己写过的上游
	} {
		if !strings.Contains(dhcp, w) {
			t.Errorf("disable-restore batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
}

// 自定义解析精确域 → 独立 config hostrecord 具名节 + DNS marker。
func TestUCIDNSRecordHostrecord(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_t" }
	if _, err := svc.CreateDNSRecord(DNSRecord{Domain: "nas.lan", Address: "192.168.1.50", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.dns_t=hostrecord",
		"set dhcp.dns_t.managed_by='kwrt-net-manager-dns'",
		"set dhcp.dns_t.name='nas.lan'",
		"set dhcp.dns_t.ip='192.168.1.50'",
	} {
		if !strings.Contains(dhcp, w) {
			t.Errorf("hostrecord batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
}

// 通配域 → @dnsmasq[0] address 列表。
func TestUCIDNSWildcardAddress(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_w" }
	if _, err := svc.CreateDNSRecord(DNSRecord{Domain: "*.demo.lan", Address: "192.168.1.9", Wildcard: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	if !strings.Contains(dhcp, "add_list dhcp.@dnsmasq[0].address='/demo.lan/192.168.1.9'") {
		t.Errorf("wildcard address missing\n--- batch ---\n%s", dhcp)
	}
}

// 域名分流 → @dnsmasq[0] server '/域名/上游'。
func TestUCIDNSDomainRoute(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_d" }
	if _, err := svc.CreateDNSDomainRoute(DNSDomainRoute{Domain: "example.com", Server: "8.8.8.8", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	if !strings.Contains(dhcp, "add_list dhcp.@dnsmasq[0].server='/example.com/8.8.8.8'") {
		t.Errorf("domain route server missing\n--- batch ---\n%s", dhcp)
	}
}

// 隔离：applyDNS 只删自己的 DNS marker 具名节，绝不删 v4(kwrt-net-manager)/v6(kwrt-net-manager-v6) 节。
func TestDNSIsolationFromV4V6(t *testing.T) {
	show := `dhcp.dhcp_v4=dhcp
dhcp.dhcp_v4.managed_by='kwrt-net-manager'
dhcp.host_v6=host
dhcp.host_v6.managed_by='kwrt-net-manager-v6'
dhcp.dns_old=hostrecord
dhcp.dns_old.managed_by='kwrt-net-manager-dns'
`
	f := &fakeRunner{show: map[string]string{"dhcp": show, "network": ""}}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	// 保存空记录集 → 触发 applyDNS 的孤儿 GC。
	if err := svc.be.SaveDNSRecords([]DNSRecord{}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	if !strings.Contains(dhcp, "delete dhcp.dns_old") {
		t.Errorf("应删除自己的孤儿 DNS 节 dns_old\n%s", dhcp)
	}
	if strings.Contains(dhcp, "delete dhcp.dhcp_v4") {
		t.Errorf("绝不能删 v4 托管节 dhcp_v4\n%s", dhcp)
	}
	if strings.Contains(dhcp, "delete dhcp.host_v6") {
		t.Errorf("绝不能删 v6 托管节 host_v6\n%s", dhcp)
	}
}
