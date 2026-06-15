package netcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every command and returns canned output, so the uci
// backend's command generation can be asserted with no OpenWrt host.
type fakeRunner struct {
	calls      []fakeCall
	show       map[string]string // `uci show <config>` output
	get        map[string]string // `uci get <key>` value
	routeOut   string            // `ip route show` output
	failReload bool
}

type fakeCall struct {
	stdin string
	name  string
	args  []string
}

func (f *fakeRunner) Run(stdin, name string, args ...string) (string, error) {
	f.calls = append(f.calls, fakeCall{stdin, name, append([]string(nil), args...)})
	switch {
	case name == "uci" && len(args) >= 1 && args[0] == "batch":
		return "", nil
	case name == "uci" && len(args) >= 3 && args[1] == "get":
		if v, ok := f.get[args[2]]; ok {
			return v, nil
		}
		return "", fmt.Errorf("uci: Entry not found")
	case name == "uci" && len(args) >= 3 && args[1] == "show":
		return f.show[args[2]], nil
	case name == "ip":
		return f.routeOut, nil
	case strings.Contains(name, "init.d"):
		if f.failReload && containsStr(args, "reload") {
			return "", fmt.Errorf("reload failed")
		}
		return "", nil
	}
	return "", nil
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// lastBatch returns the stdin of the most recent `uci batch` call containing marker.
func (f *fakeRunner) batchContaining(marker string) string {
	for i := len(f.calls) - 1; i >= 0; i-- {
		c := f.calls[i]
		if c.name == "uci" && len(c.args) >= 1 && c.args[0] == "batch" && strings.Contains(c.stdin, marker) {
			return c.stdin
		}
	}
	return ""
}

func newTestUCI(t *testing.T, f *fakeRunner) *uciBackend {
	t.Helper()
	dir := t.TempDir()
	be, err := newUCIBackend(f, filepath.Join(dir, "netcfg.json"), nil)
	if err != nil {
		t.Fatalf("newUCIBackend: %v", err)
	}
	return be
}

func TestUCIApplyGeneratesManagedSections(t *testing.T) {
	f := &fakeRunner{
		get: map[string]string{"network.lan.ipaddr": "192.168.1.1"},
	}
	_ = newTestUCI(t, f) // construction triggers initial apply on the seed state

	dhcp := f.batchContaining("commit dhcp")
	if dhcp == "" {
		t.Fatal("no dhcp batch generated")
	}
	wantDHCP := []string{
		"set dhcp.nm_dhcp_seedlan=dhcp",
		"set dhcp.nm_dhcp_seedlan.managed_by='kwrt-net-manager'",
		"set dhcp.nm_dhcp_seedlan.interface='lan'",
		"set dhcp.nm_dhcp_seedlan.start='100'",
		"set dhcp.nm_dhcp_seedlan.limit='101'",
		"set dhcp.nm_dhcp_seedlan.leasetime='120m'",
		"add_list dhcp.nm_dhcp_seedlan.dhcp_option='3,192.168.1.1'",
		"add_list dhcp.nm_dhcp_seedlan.dhcp_option='6,223.5.5.5,114.114.114.114'",
		"set dhcp.nm_host_seed1=host",
		"set dhcp.nm_host_seed1.mac='AA:BB:CC:00:00:01'",
		"set dhcp.nm_host_seed1.ip='192.168.1.50'",
		"set dhcp.nm_host_seed1.name='demo-pc'",
		"commit dhcp",
	}
	for _, w := range wantDHCP {
		if !strings.Contains(dhcp, w) {
			t.Errorf("dhcp batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}

	net := f.batchContaining("commit network")
	wantNet := []string{
		"set network.nm_route_seed1=route",
		"set network.nm_route_seed1.managed_by='kwrt-net-manager'",
		"set network.nm_route_seed1.target='10.0.0.0'",
		"set network.nm_route_seed1.netmask='255.255.255.0'",
		"set network.nm_route_seed1.gateway='192.168.1.2'",
		"set network.nm_route_seed1.metric='1'",
		"commit network",
	}
	for _, w := range wantNet {
		if !strings.Contains(net, w) {
			t.Errorf("network batch missing %q\n--- batch ---\n%s", w, net)
		}
	}
	// interface=auto must NOT emit an interface line.
	if strings.Contains(net, "nm_route_seed1.interface=") {
		t.Error("auto route should not set an interface")
	}
}

func TestUCIDisabledItemsNotRendered(t *testing.T) {
	f := &fakeRunner{}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)

	// A disabled route must not appear in the network batch (version-safe: we
	// never rely on `option disabled`, we just omit it).
	if _, err := svc.CreateRoute(Route{Family: "ipv4", Interface: "auto", Target: "172.16.0.0", Netmask: "255.255.0.0", Gateway: "192.168.1.9", Metric: 2, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	net := f.batchContaining("commit network")
	if strings.Contains(net, "172.16.0.0") {
		t.Errorf("disabled route should not be rendered:\n%s", net)
	}
}

func TestUCIMarkerScopedDeletes(t *testing.T) {
	f := &fakeRunner{
		show: map[string]string{
			// One managed section + one stock section.
			"dhcp": "dhcp.nm_old=host\n" +
				"dhcp.nm_old.managed_by='kwrt-net-manager'\n" +
				"dhcp.lan=dhcp\n" +
				"dhcp.lan.interface='lan'\n",
		},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	// Trigger an apply.
	_, _ = svc.CreateStatic(StaticLease{IP: "192.168.1.60", MAC: "aa:bb:cc:dd:ee:99", Interface: "lan", Enabled: true})

	dhcp := f.batchContaining("commit dhcp")
	if !strings.Contains(dhcp, "delete dhcp.nm_old") {
		t.Error("should delete the managed section")
	}
	if strings.Contains(dhcp, "delete dhcp.lan") {
		t.Error("must NOT delete the stock (unmarked) section")
	}
}

func TestUCIReloadFailureSetsPending(t *testing.T) {
	f := &fakeRunner{failReload: true}
	be := newTestUCI(t, f)
	st, _ := be.Status()
	if !st.Pending {
		t.Error("reload failure should set pending")
	}
	if st.Backend != KindUCI {
		t.Errorf("backend = %s", st.Backend)
	}
}

func TestUCILeaseParsingAndAnnotation(t *testing.T) {
	dir := t.TempDir()
	leasePath := filepath.Join(dir, "dhcp.leases")
	// One dynamic, one matching the seed static (AA:BB:CC:00:00:01 / .50).
	content := "1893456000 12:34:56:78:9a:bc 192.168.1.150 laptop *\n" +
		"0 aa:bb:cc:00:00:01 192.168.1.50 demo-pc *\n"
	if err := os.WriteFile(leasePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{get: map[string]string{"dhcp.@dnsmasq[0].leasefile": leasePath}}
	be := newTestUCI(t, f)

	leases, err := be.Leases()
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 2 {
		t.Fatalf("want 2 leases, got %d", len(leases))
	}
	var staticSeen, dynSeen bool
	for _, l := range leases {
		if l.IP == "192.168.1.50" {
			if !l.Static {
				t.Error("seed reservation should be marked static")
			}
			if l.Interface != "lan" {
				t.Errorf("interface attribution = %q", l.Interface)
			}
			staticSeen = true
		}
		if l.IP == "192.168.1.150" && !l.Static {
			dynSeen = true
		}
	}
	if !staticSeen || !dynSeen {
		t.Errorf("static=%v dyn=%v", staticSeen, dynSeen)
	}
}

func TestParseIPRoute(t *testing.T) {
	out := "default via 192.168.0.1 dev eth0 metric 0\n" +
		"10.0.0.0/24 via 192.168.1.2 dev br-lan metric 1\n" +
		"192.168.1.0/24 dev br-lan scope link\n"
	rows := parseIPRoute(out, FamilyIPv4)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].Target != "0.0.0.0" || rows[0].Netmask != "0.0.0.0" || rows[0].Gateway != "192.168.0.1" {
		t.Errorf("default row = %+v", rows[0])
	}
	if rows[1].Target != "10.0.0.0" || rows[1].Netmask != "255.255.255.0" || rows[1].Gateway != "192.168.1.2" || rows[1].Metric != 1 || rows[1].Interface != "br-lan" {
		t.Errorf("route row = %+v", rows[1])
	}
	if rows[2].Target != "192.168.1.0" || rows[2].Gateway != "" {
		t.Errorf("direct row = %+v", rows[2])
	}
}

func TestManagedSectionsParsing(t *testing.T) {
	show := "dhcp.nm_a=host\n" +
		"dhcp.nm_a.managed_by='kwrt-net-manager'\n" +
		"dhcp.nm_a.mac='AA:BB'\n" +
		"dhcp.stock=dhcp\n" +
		"dhcp.nm_b=host\n" +
		"dhcp.nm_b.managed_by='kwrt-net-manager'\n"
	got := managedSections(show, "dhcp")
	if len(got) != 2 || got[0] != "nm_a" || got[1] != "nm_b" {
		t.Errorf("managedSections = %v", got)
	}
}
