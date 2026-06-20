package ddns

import (
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	uciBatch string // 捕获最后一次 uci batch 的 stdin
	show     string // `uci show ddns` 返回
	neigh    string // `ip -6 neighbor show` 返回
	leases6  string // `ubus call dhcp ipv6leases` 返回
}

func (f *fakeRunner) Run(stdin, name string, args ...string) (string, error) {
	switch {
	case name == "uci" && len(args) > 0 && args[0] == "batch":
		f.uciBatch = stdin
		return "", nil
	case name == "uci" && len(args) > 0 && args[0] == "show":
		return f.show, nil
	case name == "ip" && len(args) >= 2 && args[0] == "-6" && args[1] == "neighbor":
		return f.neigh, nil
	case name == "ubus" && len(args) >= 3 && args[0] == "call" && args[1] == "dhcp" && args[2] == "ipv6leases":
		return f.leases6, nil
	case name == "test": // pkgmgr.Installed: /etc/init.d/ddns 存在
		return "", nil
	}
	return "", nil
}

func TestValidate(t *testing.T) {
	ok := Entry{Provider: "cloudflare.com", Domain: "a.b.com", Password: "tok", IPSource: "web", RecordType: "A"}
	if err := validate(ok); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}
	bad := []Entry{
		{Provider: "", Domain: "a", Password: "t", IPSource: "web", RecordType: "A"},
		{Provider: "p", Domain: "", Password: "t", IPSource: "web", RecordType: "A"},
		{Provider: "p", Domain: "a", Password: "", IPSource: "web", RecordType: "A"},
		{Provider: "p", Domain: "a", Password: "t", IPSource: "bogus", RecordType: "A"},
		{Provider: "p", Domain: "a", Password: "t", IPSource: "network", Interface: "", RecordType: "A"},
		{Provider: "p", Domain: "a", Password: "t", IPSource: "web", RecordType: "X"},
	}
	for i, e := range bad {
		if err := validate(e); err == nil {
			t.Errorf("bad entry %d should be rejected", i)
		}
	}
}

func TestApplyProjection(t *testing.T) {
	f := &fakeRunner{}
	svc := New(f, filepath.Join(t.TempDir(), "ddns.json"), func() string { return "ddns_test1" })
	if _, err := svc.Create(Entry{
		Provider: "cloudflare.com", Domain: "home.example.com", AuthMode: "token",
		Password: "secret", IPSource: "web", Interface: "wan", RecordType: "AAAA",
	}); err != nil {
		t.Fatal(err)
	}
	b := f.uciBatch
	wants := []string{
		"set ddns.ddns_test1=service",
		"managed_by='kwrt-net-manager-ddns'",
		"service_name='cloudflare.com-v6'", // AAAA → -v6
		"lookup_host='home.example.com'",
		"password='secret'",
		"use_ipv6='1'",
		"ip_source='web'",
		"commit ddns",
	}
	for _, w := range wants {
		if !strings.Contains(b, w) {
			t.Errorf("uci batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestApplyGC(t *testing.T) {
	// show 里有一个本工具 marker 的旧节，但 keep 里没有 → 应被删除。
	f := &fakeRunner{show: "ddns.ddns_old=service\nddns.ddns_old.managed_by='kwrt-net-manager-ddns'\n"}
	svc := New(f, filepath.Join(t.TempDir(), "ddns.json"), func() string { return "ddns_new" })
	_, _ = svc.Create(Entry{Provider: "no-ip.com", Domain: "x.example", Password: "p", IPSource: "web", RecordType: "A"})
	if !strings.Contains(f.uciBatch, "delete ddns.ddns_old") {
		t.Errorf("GC should delete orphan managed section\n%s", f.uciBatch)
	}
}
