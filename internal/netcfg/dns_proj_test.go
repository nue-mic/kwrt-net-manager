package netcfg

import (
	"strings"
	"testing"
)

func TestSplitUpstreams(t *testing.T) {
	got := splitUpstreams("8.8.8.8, 1.1.1.1  9.9.9.9\n2.2.2.2")
	want := []string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "2.2.2.2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("splitUpstreams=%v want %v", got, want)
	}
	if len(splitUpstreams("  ,  ")) != 0 {
		t.Error("全空白应得空")
	}
}

func TestDesiredDNSServers_MultiUpstream(t *testing.T) {
	st := DNSSettings{Enabled: true, DNSPrimary: "223.5.5.5"}
	routes := []DNSDomainRoute{
		{Domain: "*.google.com", Server: "8.8.8.8, 1.1.1.1", Enabled: true, Managed: true},
		{Domain: "intra.lan", Server: "192.168.1.1#5353", OutIface: "wan", Enabled: true, Managed: true},
		{Domain: "off.com", Server: "9.9.9.9", Enabled: false, Managed: true}, // 停用不投射
	}
	got := desiredDNSServers(st, DNSDoH{}, routes)
	joined := strings.Join(got, "|")
	for _, want := range []string{"223.5.5.5", "/google.com/8.8.8.8", "/google.com/1.1.1.1", "/intra.lan/192.168.1.1#5353@wan"} {
		if !strings.Contains(joined, want) {
			t.Errorf("缺少 %q\n%v", want, got)
		}
	}
	if strings.Contains(joined, "9.9.9.9") {
		t.Error("停用的分流不应投射")
	}
}

func TestDesiredRebindDomains(t *testing.T) {
	// 未托管 → 空。
	if d := desiredRebindDomains(DNSSettings{Enabled: false, RebindDomains: []string{"a.com"}}); len(d) != 0 {
		t.Errorf("未托管应空，得 %v", d)
	}
	// 托管 → 去空白去重。
	got := desiredRebindDomains(DNSSettings{Enabled: true, RebindDomains: []string{"a.com", " a.com ", "", "b.com"}})
	if strings.Join(got, ",") != "a.com,b.com" {
		t.Errorf("去重去空白错：%v", got)
	}
}
