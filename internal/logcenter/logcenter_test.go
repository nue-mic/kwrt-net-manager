package logcenter

import (
	"strings"
	"testing"
	"time"
)

type fakeRunner struct{ out map[string]string }

func (f fakeRunner) Run(name string, args ...string) (string, error) {
	return f.out[name], nil
}

const sampleLogread = `Tue Jun 16 22:05:20 2026 daemon.err kwrtmgrd[20160]: started ok
Tue Jun 16 22:05:21 2026 daemon.info dnsmasq-dhcp[1234]: DHCPACK(br-lan) 192.168.1.100 aa:bb:cc:dd:ee:ff myhost
Tue Jun 16 22:05:22 2026 daemon.notice pppd[555]: PPP session established
this is a malformed line that should be skipped`

func newTestCenter(t *testing.T) *Center {
	t.Helper()
	c := New(t.TempDir(), nil)
	c.run = fakeRunner{out: map[string]string{"logread": sampleLogread, "date": "+0800"}}
	c.loc = time.FixedZone("local", 8*3600)
	return c
}

func TestParseSyslogLine(t *testing.T) {
	loc := time.FixedZone("local", 8*3600)
	e, ok := parseSyslogLine("Tue Jun 16 22:05:20 2026 daemon.err kwrtmgrd[20160]: hello world", loc)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if e.Time != "2026-06-16 22:05:20" {
		t.Errorf("time=%q", e.Time)
	}
	if e.Level != "err" {
		t.Errorf("level=%q want err", e.Level)
	}
	if e.Proc != "kwrtmgrd" {
		t.Errorf("proc=%q want kwrtmgrd", e.Proc)
	}
	if e.Message != "hello world" {
		t.Errorf("msg=%q", e.Message)
	}
	if _, ok := parseSyslogLine("garbage", loc); ok {
		t.Error("garbage line should not parse")
	}
}

func TestEnrichDHCP(t *testing.T) {
	e := Entry{Message: "DHCPACK(br-lan) 192.168.1.100 aa:bb:cc:dd:ee:ff myhost"}
	enrichDHCP(&e)
	if e.Type != "DHCPACK" {
		t.Errorf("type=%q", e.Type)
	}
	if e.Iface != "br-lan" {
		t.Errorf("iface=%q", e.Iface)
	}
	if e.IP != "192.168.1.100" {
		t.Errorf("ip=%q", e.IP)
	}
	if e.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("mac=%q", e.MAC)
	}
}

func TestQuerySources(t *testing.T) {
	c := newTestCenter(t)

	sys, err := c.Query(SourceSystem, Filter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if sys.Total != 3 { // 3 valid lines, malformed skipped
		t.Errorf("system total=%d want 3", sys.Total)
	}
	// 倒序：最新（22:05:22 pppd）在前。
	if len(sys.Items) > 0 && sys.Items[0].Proc != "pppd" {
		t.Errorf("first item proc=%q want pppd (newest first)", sys.Items[0].Proc)
	}

	dhcp, _ := c.Query(SourceDHCP, Filter{Page: 1, PageSize: 10})
	if dhcp.Total != 1 || dhcp.Items[0].Type != "DHCPACK" {
		t.Errorf("dhcp filter wrong: total=%d", dhcp.Total)
	}

	dial, _ := c.Query(SourceDialup, Filter{Page: 1, PageSize: 10})
	if dial.Total != 1 || dial.Items[0].Proc != "pppd" {
		t.Errorf("dialup filter wrong: total=%d", dial.Total)
	}

	if _, err := c.Query("bogus", Filter{}); err == nil {
		t.Error("bogus source should error")
	}
}

// 多线路拨号日志：两条不同接口(pppoe-wan1 / pppoe-wan2) + 一条抽不到接口名的行。
const dialLogread = `Tue Jun 16 22:05:20 2026 daemon.info pppd[1]: pppoe-wan1: Send PPPOE Discovery
Tue Jun 16 22:05:21 2026 daemon.info pppd[2]: pppoe-wan2: Send PPPOE Discovery
Tue Jun 16 22:05:22 2026 daemon.notice pppd[3]: PPP session established`

func TestDialupIfaceFilter(t *testing.T) {
	c := New(t.TempDir(), nil)
	c.loc = time.FixedZone("local", 8*3600)
	c.run = fakeRunner{out: map[string]string{"date": "+0800", "logread": dialLogread}}

	all, _ := c.Query(SourceDialup, Filter{Page: 1, PageSize: 50})
	if all.Total != 3 {
		t.Fatalf("dialup all total=%d want 3", all.Total)
	}

	// 选 wan1：滤掉 wan2，保留 wan1 + 无接口名行(established)。
	w1, _ := c.Query(SourceDialup, Filter{Iface: "wan1", Page: 1, PageSize: 50})
	if w1.Total != 2 {
		t.Fatalf("dialup wan1 total=%d want 2 (wan1 + 无接口行)", w1.Total)
	}
	for _, e := range w1.Items {
		if e.Iface != "" && !strings.Contains(e.Iface, "wan1") {
			t.Errorf("wan1 过滤泄漏 iface=%q", e.Iface)
		}
	}

	// 线路过滤只对 dialup 生效：system 源忽略 Iface。
	sys, _ := c.Query(SourceSystem, Filter{Iface: "wan1", Page: 1, PageSize: 50})
	if sys.Total != 3 {
		t.Errorf("system 源不应受 Iface 影响, total=%d want 3", sys.Total)
	}
}

func TestKeywordFilter(t *testing.T) {
	c := newTestCenter(t)
	res, _ := c.Query(SourceSystem, Filter{Keyword: "dhcpack", Page: 1, PageSize: 10})
	if res.Total != 1 {
		t.Errorf("keyword total=%d want 1", res.Total)
	}
}

func TestOperationRecordAndClear(t *testing.T) {
	c := newTestCenter(t)
	c.Record(OperationEntry{User: "admin", ClientIP: "1.2.3.4", Module: "DHCP服务端", Action: "新增规则"})
	res, _ := c.Query(SourceOperation, Filter{Page: 1, PageSize: 10})
	if res.Total != 1 || res.Items[0].Module != "DHCP服务端" {
		t.Fatalf("operation record not found: %+v", res)
	}
	if err := c.Clear(SourceOperation); err != nil {
		t.Fatal(err)
	}
	res2, _ := c.Query(SourceOperation, Filter{Page: 1, PageSize: 10})
	if res2.Total != 0 {
		t.Errorf("after clear total=%d want 0", res2.Total)
	}
	if err := c.Clear(SourceSystem); err == nil {
		t.Error("clearing system log should be rejected")
	}
}
