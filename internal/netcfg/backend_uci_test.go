package netcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every command and returns canned output, so the uci
// backend's import + command generation can be asserted with no OpenWrt host.
type fakeRunner struct {
	calls []fakeCall
	show  map[string]string // `uci -q show <config>` output
	get   map[string]string // `uci -q get <key>` value
	route string            // `ip route show`
	cmd   map[string]string // 任意命令 "name args..." → 输出（优先于内置 case，用于 ubus/ip -6 neigh）
}

type fakeCall struct {
	stdin string
	name  string
	args  []string
}

func (f *fakeRunner) Run(stdin, name string, args ...string) (string, error) {
	f.calls = append(f.calls, fakeCall{stdin, name, append([]string(nil), args...)})
	if f.cmd != nil {
		if v, ok := f.cmd[strings.TrimSpace(name+" "+strings.Join(args, " "))]; ok {
			return v, nil
		}
	}
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
		return f.route, nil
	}
	return "", nil
}

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

const sampleDHCPShow = `dhcp.@dnsmasq[0]=dnsmasq
dhcp.@dnsmasq[0].leasefile='/tmp/dhcp.leases'
dhcp.lan=dhcp
dhcp.lan.interface='lan'
dhcp.lan.start='100'
dhcp.lan.limit='150'
dhcp.lan.leasetime='12h'
dhcp.lan.dhcpv4='server'
dhcp.lan.ignore='1'
dhcp.lan.dhcp_option='6,1.1.1.1'
dhcp.pc=host
dhcp.pc.name='laptop'
dhcp.pc.mac='aa:bb:cc:dd:ee:ff'
dhcp.pc.ip='192.168.1.50'
`

const sampleNetShow = `network.loopback=interface
network.loopback.ipaddr='127.0.0.1'
network.lan=interface
network.lan.ipaddr='192.168.1.1'
network.lan.netmask='255.255.255.0'
network.r1=route
network.r1.target='10.0.0.0'
network.r1.netmask='255.255.255.0'
network.r1.gateway='192.168.1.2'
network.r1.metric='1'
`

func TestUCIImportsExistingConfig(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": sampleDHCPShow, "network": sampleNetShow}}
	be := newTestUCI(t, f) // fresh → imports

	servers, _ := be.DHCPServers()
	if len(servers) != 1 || servers[0].ID != "lan" {
		t.Fatalf("imported servers = %+v", servers)
	}
	s := servers[0]
	if s.Enabled { // ignore='1' → disabled
		t.Error("lan dhcp should be imported as disabled (ignore=1)")
	}
	if s.IPStart != "192.168.1.100" || s.IPEnd != "192.168.1.249" {
		t.Errorf("range = %s-%s want .100-.249", s.IPStart, s.IPEnd)
	}
	if s.LeaseMinutes != 720 { // 12h
		t.Errorf("lease = %d want 720", s.LeaseMinutes)
	}
	if s.DNSPrimary != "1.1.1.1" {
		t.Errorf("dns = %q", s.DNSPrimary)
	}

	statics, _ := be.Statics()
	if len(statics) != 1 || statics[0].IP != "192.168.1.50" || statics[0].Hostname != "laptop" {
		t.Fatalf("imported statics = %+v", statics)
	}

	routes, _ := be.Routes()
	if len(routes) != 1 || routes[0].Target != "10.0.0.0" || routes[0].Gateway != "192.168.1.2" {
		t.Fatalf("imported routes = %+v", routes)
	}
	if routes[0].Interface != "auto" {
		t.Errorf("route interface = %q want auto", routes[0].Interface)
	}
}

func TestUCIApplyModifiesInPlace(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_t1" } // deterministic id

	if _, err := svc.CreateStatic(StaticLease{Hostname: "x", IP: "192.168.1.77", MAC: "12:34:56:78:9a:bc", Interface: "lan", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	want := []string{
		"set dhcp.host_t1=host",
		"set dhcp.host_t1.managed_by='kwrt-net-manager'",
		"set dhcp.host_t1.mac='12:34:56:78:9A:BC'",
		"set dhcp.host_t1.ip='192.168.1.77'",
		"set dhcp.host_t1.name='x'",
		"commit dhcp",
	}
	for _, w := range want {
		if !strings.Contains(dhcp, w) {
			t.Errorf("dhcp batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
	// modify-in-place: must NOT delete-recreate (no blanket section delete of host_t1).
	if strings.Contains(dhcp, "delete dhcp.host_t1\n") {
		t.Error("apply should not delete the section it is creating")
	}
}

func TestUCIServiceDetectionPending(t *testing.T) {
	// On the test host /etc/init.d/dnsmasq doesn't exist → dhcpService()=="".
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	if _, err := svc.CreateRoute(Route{Family: "ipv4", Target: "172.16.0.0", Netmask: "255.255.0.0", Gateway: "192.168.1.2", Metric: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	st, _ := be.Status()
	if !st.Pending || !strings.Contains(st.Message, "未检测到 DHCP 服务") {
		t.Errorf("expected pending+no-service message, got %+v", st)
	}
	// RestartDHCP must give a friendly error, not a raw fork/exec one.
	if err := be.RestartDHCP(); err == nil || !strings.Contains(err.Error(), "未检测到 DHCP 服务") {
		t.Errorf("RestartDHCP error = %v", err)
	}
}

func TestUCINoSeedingOnFreshUCI(t *testing.T) {
	// A fresh uci backend with empty config must NOT fabricate demo servers.
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	if servers, _ := be.DHCPServers(); len(servers) != 0 {
		t.Errorf("fresh uci backend should have 0 servers, got %d", len(servers))
	}
}

func TestUCIForceEnableOnOdhcpd(t *testing.T) {
	old := dhcpService
	dhcpService = func() string { return "odhcpd" }
	defer func() { dhcpService = old }()
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": ""},
		get:  map[string]string{"dhcp.odhcpd": "odhcpd", "network.lan.ipaddr": "192.168.1.1"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_e" }
	if _, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan", Enabled: true, IPStart: "192.168.1.100", IPEnd: "192.168.1.200",
		Netmask: "255.255.255.0", Gateway: "192.168.1.1", LeaseMinutes: 120,
	}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"delete dhcp.dhcp_e.ignore",
		"set dhcp.dhcp_e.dhcpv4='server'",
		"set dhcp.odhcpd.maindhcp='1'", // odhcpd flipped into main DHCP on enable
	} {
		if !strings.Contains(dhcp, w) {
			t.Errorf("force-enable batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
}

func TestUCIDnsmasqForcesMaindhcpOff(t *testing.T) {
	// dnsmasq present → it must stay the DHCPv4 server; a stale odhcpd.maindhcp=1
	// silently kills dnsmasq's dhcp-range, so projection must force it back to 0.
	old := dhcpService
	dhcpService = func() string { return "dnsmasq" }
	defer func() { dhcpService = old }()
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": ""},
		get:  map[string]string{"dhcp.odhcpd": "odhcpd", "network.lan.ipaddr": "192.168.1.1"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_e" }
	if _, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan", Enabled: true, IPStart: "192.168.1.100", IPEnd: "192.168.1.200",
		Netmask: "255.255.255.0", Gateway: "192.168.1.1", LeaseMinutes: 120,
	}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	if !strings.Contains(dhcp, "set dhcp.odhcpd.maindhcp='0'") {
		t.Errorf("dnsmasq box must force maindhcp=0\n--- batch ---\n%s", dhcp)
	}
	if strings.Contains(dhcp, "set dhcp.odhcpd.maindhcp='1'") {
		t.Errorf("dnsmasq box must never set maindhcp=1\n--- batch ---\n%s", dhcp)
	}
}

func TestUCIStaticPerHostGatewayDNSViaTag(t *testing.T) {
	// Per-host gateway/DNS must project natively via a `config tag` carrying
	// option 3/6, with the host pointed at that tag.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.1", "network.lan.netmask": "255.255.255.0", "network.lan.device": "br-lan"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_g" }
	if _, err := svc.CreateStatic(StaticLease{
		IP: "192.168.1.50", MAC: "aa:bb:cc:dd:ee:01", Interface: "lan",
		Gateway: "192.168.1.254", DNSPrimary: "1.1.1.1", DNSSecondary: "8.8.8.8", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.host_g=host",
		"set dhcp.host_g_t=tag",
		"add_list dhcp.host_g_t.dhcp_option='3,192.168.1.254'",
		"add_list dhcp.host_g_t.dhcp_option='6,1.1.1.1,8.8.8.8'",
		"set dhcp.host_g.tag='host_g_t'",
	} {
		if !strings.Contains(dhcp, w) {
			t.Errorf("per-host tag batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
}

func TestUCIPoolExcludeAsReservations(t *testing.T) {
	// Excluded addresses must project as placeholder host reservations (dnsmasq
	// keeps reserved IPs out of the dynamic pool); ranges expand per IP.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.1", "network.lan.netmask": "255.255.255.0"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_p" }
	if _, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan", Enabled: true, IPStart: "192.168.1.100", IPEnd: "192.168.1.200",
		LeaseMinutes: 120, Exclude: []string{"192.168.1.150", "192.168.1.160-192.168.1.161"},
	}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.dhcp_p_x_192_168_1_150=host",
		"set dhcp.dhcp_p_x_192_168_1_150.ip='192.168.1.150'",
		"set dhcp.dhcp_p_x_192_168_1_150.mac='02:00:c0:a8:01:96'", // 192.168.1.150
		"set dhcp.dhcp_p_x_192_168_1_160.ip='192.168.1.160'",
		"set dhcp.dhcp_p_x_192_168_1_161.ip='192.168.1.161'",
	} {
		if !strings.Contains(dhcp, w) {
			t.Errorf("exclude reservation batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
}

func TestUCIBlacklistIgnoreHost(t *testing.T) {
	// Blacklist must use the native `option ip 'ignore'` (config host `ignore` is a
	// no-op in OpenWrt's dnsmasq init, and a mac-only host is dropped).
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.1", "network.lan.netmask": "255.255.255.0"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_b" }
	if _, err := svc.AddACLEntry(ACLEntry{MAC: "aa:bb:cc:dd:ee:0b", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	for _, w := range []string{"set dhcp.acl_b=host", "set dhcp.acl_b.mac='AA:BB:CC:DD:EE:0B'", "set dhcp.acl_b.ip='ignore'"} {
		if !strings.Contains(dhcp, w) {
			t.Errorf("blacklist batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
}

func TestUCIWhitelistDynamicDHCPOff(t *testing.T) {
	// Whitelist mode: every pool gets dynamicdhcp='0' (serve only known hosts) and
	// each allowed MAC is reserved a free IP from the pool. Blacklist deletes it.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.1", "network.lan.netmask": "255.255.255.0"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_w" }
	if _, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan", Enabled: true, IPStart: "192.168.1.100", IPEnd: "192.168.1.200", LeaseMinutes: 120,
	}); err != nil {
		t.Fatal(err)
	}
	if dhcp := f.batchContaining("commit dhcp"); !strings.Contains(dhcp, "delete dhcp.dhcp_w.dynamicdhcp") {
		t.Errorf("blacklist mode must delete dynamicdhcp\n%s", dhcp)
	}
	if _, err := svc.SetACLMode(ACLWhitelist); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddACLEntry(ACLEntry{MAC: "aa:bb:cc:dd:ee:02", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.dhcp_w.dynamicdhcp='0'",
		"set dhcp.acl_w=host",
		"set dhcp.acl_w.mac='AA:BB:CC:DD:EE:02'",
		"set dhcp.acl_w.ip='192.168.1.100'", // first free IP in the pool
	} {
		if !strings.Contains(dhcp, w) {
			t.Errorf("whitelist batch missing %q\n--- batch ---\n%s", w, dhcp)
		}
	}
}

func TestUCIARPBindNeigh(t *testing.T) {
	// With ARP-bind on, managed reservations install static neigh entries.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.1", "network.lan.netmask": "255.255.255.0", "network.lan.device": "br-lan"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_a" }
	if _, err := svc.CreateStatic(StaticLease{IP: "192.168.1.51", MAC: "aa:bb:cc:dd:ee:03", Interface: "lan", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetARPBind(true); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.calls {
		if c.name == "ip" && len(c.args) >= 5 && c.args[0] == "neigh" && c.args[1] == "replace" && c.args[2] == "192.168.1.51" {
			found = true
		}
	}
	if !found {
		t.Error("ARP-bind on should issue `ip neigh replace 192.168.1.51 ...`")
	}
}

func TestUCIStaticUnsafeHostnameNotProjected(t *testing.T) {
	// A device named in Chinese (or with spaces/dots) must NOT be written as the
	// dnsmasq host name — that crashes dnsmasq with "bad DHCP host name". The
	// reservation (mac+ip) must still be projected; the name is just dropped.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.1", "network.lan.netmask": "255.255.255.0"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_h" }
	if _, err := svc.CreateStatic(StaticLease{
		Hostname: "IP14-慕容-5G", IP: "192.168.1.104", MAC: "16:cd:03:ca:2b:30", Interface: "lan", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	if !strings.Contains(dhcp, "set dhcp.host_h.ip='192.168.1.104'") {
		t.Errorf("reservation IP must still project\n%s", dhcp)
	}
	if strings.Contains(dhcp, "慕容") || strings.Contains(dhcp, "set dhcp.host_h.name=") {
		t.Errorf("unsafe hostname must NOT be projected to dnsmasq\n%s", dhcp)
	}
	if !strings.Contains(dhcp, "delete dhcp.host_h.name") {
		t.Errorf("unsafe hostname should delete the name option\n%s", dhcp)
	}
}

func TestDNSSafeHostname(t *testing.T) {
	cases := map[string]string{
		"laptop-1": "laptop-1", "my_pc": "my_pc", "ABC123": "ABC123",
		"IP14-慕容-5G": "", "测试机": "", "a b": "", "host.local": "",
		"-bad": "", "bad-": "", "": "",
	}
	for in, want := range cases {
		if got := dnsSafeHostname(in); got != want {
			t.Errorf("dnsSafeHostname(%q) = %q want %q", in, got, want)
		}
	}
}

func TestUCIPoolForceProjection(t *testing.T) {
	// Force on → set force='1' (skip dnsmasq's dhcp_check "another server" probe);
	// off → delete force.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.1", "network.lan.netmask": "255.255.255.0"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_f" }
	srv, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan", Enabled: true, Force: true,
		IPStart: "192.168.1.100", IPEnd: "192.168.1.150", LeaseMinutes: 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dhcp := f.batchContaining("commit dhcp"); !strings.Contains(dhcp, "set dhcp.dhcp_f.force='1'") {
		t.Errorf("force on must set force='1'\n%s", dhcp)
	}
	srv.Force = false
	if _, err := svc.UpdateDHCPServer(srv.ID, srv); err != nil {
		t.Fatal(err)
	}
	if dhcp := f.batchContaining("commit dhcp"); !strings.Contains(dhcp, "delete dhcp.dhcp_f.force") {
		t.Errorf("force off must delete force\n%s", dhcp)
	}
}

func TestUCIPoolRejectsOutOfSubnetRange(t *testing.T) {
	// The Service must reject a pool range outside the bound interface subnet
	// (the old silent &0xFF truncation bug) with a clear error.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.1", "network.lan.netmask": "255.255.255.0"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	_, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan", Enabled: true, IPStart: "192.168.2.100", IPEnd: "192.168.2.200", LeaseMinutes: 120,
	})
	if err == nil || !strings.Contains(err.Error(), "不在接口") {
		t.Errorf("expected out-of-subnet rejection, got %v", err)
	}
}

func TestUCIRoutePushAllPoolLevel(t *testing.T) {
	// "all" mode: pushed IPv4 routes go to every pool client via option 121/249,
	// auto-including the default route (via pool gateway) and next-hop = bypass IP.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.12", "network.lan.netmask": "255.255.255.0"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_rp" }
	if _, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan", Enabled: true, IPStart: "192.168.1.100", IPEnd: "192.168.1.150",
		Gateway: "192.168.1.1", LeaseMinutes: 120,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRoute(Route{
		Family: "ipv4", Interface: "auto", Target: "10.8.8.0", Netmask: "255.255.255.0",
		Gateway: "192.168.1.254", Metric: 1, Enabled: true, PushToClients: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRoutePushMode(RoutePushAll); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	want := "add_list dhcp.dhcp_rp.dhcp_option='121,0.0.0.0/0,192.168.1.1,10.8.8.0/24,192.168.1.12'"
	if !strings.Contains(dhcp, want) {
		t.Errorf("missing pool-level 121\nwant: %s\n--- batch ---\n%s", want, dhcp)
	}
	if !strings.Contains(dhcp, "dhcp.dhcp_rp.dhcp_option='249,0.0.0.0/0,192.168.1.1,10.8.8.0/24,192.168.1.12'") {
		t.Errorf("missing 249 compat option\n%s", dhcp)
	}
}

func TestUCIRoutePushTaggedHostLevel(t *testing.T) {
	// "tagged" mode: pushed routes go ONLY to reserved devices with RoutePush, via
	// the host's tag; pools do NOT push to everyone.
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": sampleNetShow},
		get:  map[string]string{"network.lan.ipaddr": "192.168.1.12", "network.lan.netmask": "255.255.255.0", "network.lan.device": "br-lan"},
	}
	be := newTestUCI(t, f)
	svc := NewService(be, nil, nil)
	svc.idFn = func(p string) string { return p + "_t1" }
	if _, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan", Enabled: true, IPStart: "192.168.1.100", IPEnd: "192.168.1.150",
		Gateway: "192.168.1.1", LeaseMinutes: 120,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRoute(Route{
		Family: "ipv4", Interface: "auto", Target: "10.8.8.0", Netmask: "255.255.255.0",
		Gateway: "192.168.1.254", Metric: 1, Enabled: true, PushToClients: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateStatic(StaticLease{
		IP: "192.168.1.77", MAC: "16:cd:03:ca:2b:31", Interface: "lan", Enabled: true, RoutePush: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRoutePushMode(RoutePushTagged); err != nil {
		t.Fatal(err)
	}
	dhcp := f.batchContaining("commit dhcp")
	if !strings.Contains(dhcp, "dhcp.host_t1_t.dhcp_option='121,0.0.0.0/0,192.168.1.1,10.8.8.0/24,192.168.1.12'") {
		t.Errorf("tagged host missing 121\n--- batch ---\n%s", dhcp)
	}
	if !strings.Contains(dhcp, "set dhcp.host_t1.tag='host_t1_t'") {
		t.Errorf("host not pointed at its tag\n%s", dhcp)
	}
	if strings.Contains(dhcp, "dhcp.dhcp_t1.dhcp_option='121,") {
		t.Errorf("pool must NOT push to everyone in tagged mode\n%s", dhcp)
	}
}

func TestLeasetimeToMin(t *testing.T) {
	cases := map[string]int{"12h": 720, "120m": 120, "1d": 1440, "infinite": 0, "": -1, "30s": 1, "3600": 60}
	for in, want := range cases {
		if got := leasetimeToMin(in); got != want {
			t.Errorf("leasetimeToMin(%q) = %d want %d", in, got, want)
		}
	}
}

func TestStartLimitToRange(t *testing.T) {
	s, e := startLimitToRange("192.168.1.1", "255.255.255.0", 100, 101)
	if s != "192.168.1.100" || e != "192.168.1.200" {
		t.Errorf("range = %s-%s", s, e)
	}
	if s, e := startLimitToRange("bad", "x", 1, 1); s != "" || e != "" {
		t.Errorf("invalid → %q-%q", s, e)
	}
}

func TestUciName(t *testing.T) {
	cases := map[string]string{"lan": "lan", "dhcp_ab12": "dhcp_ab12", "9foo": "nm_9foo", "a.b-c": "a_b_c"}
	for in, want := range cases {
		if got := uciName(in); got != want {
			t.Errorf("uciName(%q) = %q want %q", in, got, want)
		}
	}
}

func TestParseUciValues(t *testing.T) {
	got := parseUciValues("'3,1.2.3.4' '6,8.8.8.8'")
	if len(got) != 2 || got[0] != "3,1.2.3.4" || got[1] != "6,8.8.8.8" {
		t.Errorf("parseUciValues = %v", got)
	}
}

func TestUCILeaseParsingAndAnnotation(t *testing.T) {
	dir := t.TempDir()
	leasePath := filepath.Join(dir, "dhcp.leases")
	content := "1893456000 12:34:56:78:9a:bc 192.168.1.150 laptop *\n"
	if err := os.WriteFile(leasePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": ""},
		get:  map[string]string{"dhcp.@dnsmasq[0].leasefile": leasePath},
	}
	be := newTestUCI(t, f)
	leases, err := be.Leases()
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].IP != "192.168.1.150" || leases[0].MAC != "12:34:56:78:9A:BC" {
		t.Fatalf("leases = %+v", leases)
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
	if rows[0].Target != "0.0.0.0" || rows[0].Gateway != "192.168.0.1" {
		t.Errorf("default row = %+v", rows[0])
	}
	if rows[1].Target != "10.0.0.0" || rows[1].Netmask != "255.255.255.0" || rows[1].Interface != "br-lan" {
		t.Errorf("route row = %+v", rows[1])
	}
}

func TestManagedSectionsParsing(t *testing.T) {
	show := "dhcp.a=host\ndhcp.a.managed_by='kwrt-net-manager'\ndhcp.stock=dhcp\ndhcp.b=host\ndhcp.b.managed_by='kwrt-net-manager'\n"
	got := managedSections(show, "dhcp")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("managedSections = %v", got)
	}
}
