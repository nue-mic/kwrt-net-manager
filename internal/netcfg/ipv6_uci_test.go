package netcfg

import (
	"strings"
	"testing"
)

// IPv6 投射 / 导入 / 解析 / 隔离 的 fake-exec 单测，无需真机锁定行为。

func TestImportIPv6(t *testing.T) {
	// 本机式配置：lan6(dhcpv6 客户端) + lan.ip6assign；dhcp 无 v6 服务 → 不导入 LANv6。
	netShow := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.ip6assign='60'\n" +
		"network.lan6=interface\nnetwork.lan6.proto='dhcpv6'\nnetwork.lan6.device='eth0'\nnetwork.lan6.reqaddress='force'\nnetwork.lan6.reqprefix='48'\nnetwork.lan6.norelease='1'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": netShow}}
	be := newTestUCI(t, f)
	wans, _ := be.storeBackend.WANv6s() // 旁车，绕过 ubus 富化
	if len(wans) != 1 || wans[0].ID != "lan6" {
		t.Fatalf("want imported [lan6], got %+v", wans)
	}
	w := wans[0]
	if w.Proto != ProtoDHCPv6 || !w.ForcePrefix || w.ReqPrefix != "48" || !w.NoRelease || w.Managed {
		t.Errorf("lan6 import = %+v", w)
	}
	lans, _ := be.storeBackend.LANv6s()
	if len(lans) != 0 {
		t.Errorf("dhcp 无 v6 服务，不应导入 LANv6，got %+v", lans)
	}
}

func TestSaveWANv6DHCPv6(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	err := be.SaveWANv6s([]WANv6{{
		ID: "wan6", Name: "wan6", WANIface: "wan", Proto: ProtoDHCPv6, Enabled: true, Managed: true,
		ReqPrefix: "60", ForcePrefix: true, ClientID: "0004abcd", NoRelease: true,
		PeerDNS: false, DNSPrimary: "2606:4700:4700::1111", MTU: 1492,
	}})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"set network.wan6=interface",
		"set network.wan6.managed_by='kwrt-net-manager-v6'",
		"set network.wan6.device='@wan'",
		"set network.wan6.proto='dhcpv6'",
		"set network.wan6.reqprefix='60'",
		"set network.wan6.reqaddress='force'",
		"set network.wan6.clientid='0004abcd'",
		"set network.wan6.norelease='1'",
		"set network.wan6.peerdns='0'",
		"add_list network.wan6.dns='2606:4700:4700::1111'",
		"set network.wan6.mtu='1492'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("wan6 batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestSaveWANv6ProtoSwitch(t *testing.T) {
	// 切到 static6 时，必须清空 dhcpv6/隧道 的专属选项，避免残留。
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	if err := be.SaveWANv6s([]WANv6{{
		ID: "wan6", Proto: ProtoStatic6, Enabled: true, Managed: true,
		StaticIP6: "2001:db8::1/64", StaticGW: "2001:db8::1",
	}}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"delete network.wan6.reqprefix",
		"delete network.wan6.reqaddress",
		"delete network.wan6.clientid",
		"delete network.wan6.peerdns",
		"delete network.wan6.peeraddr",
		"delete network.wan6.tunlink",
		"set network.wan6.proto='static'",
		"set network.wan6.ip6addr='2001:db8::1/64'",
		"set network.wan6.ip6gw='2001:db8::1'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("static6 proto-switch batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestSaveLANv6Server(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	err := be.SaveLANv6s([]LANv6{{
		ID: "lan", Interface: "lan", Managed: true, Enabled: true,
		DHCPv6Enabled: true, DHCPv6Mode: DHCPv6Stateful, PrefixAssignLen: 64,
		LeaseMinutes: 120, BindWAN: "wan6", RAMTUEnabled: true, RAMTU: 1492,
		IPv6DNSEnabled: true, DNSServers: []string{"2606:4700:4700::1111"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	nb := f.batchContaining("commit network")
	for _, w := range []string{"set network.lan.ip6assign='64'", "add_list network.lan.ip6class='wan6'"} {
		if !strings.Contains(nb, w) {
			t.Errorf("lan network batch missing %q\n%s", w, nb)
		}
	}
	db := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.lan=dhcp",
		"set dhcp.lan.interface='lan'",
		"set dhcp.lan.managed_v6='1'",
		"set dhcp.lan.dhcpv6='server'",
		"set dhcp.lan.ra='server'",
		"set dhcp.lan.ra_management='1'",
		"set dhcp.lan.leasetime='120m'",
		"set dhcp.lan.ra_mtu='1492'",
		"add_list dhcp.lan.dns='2606:4700:4700::1111'",
	} {
		if !strings.Contains(db, w) {
			t.Errorf("lan dhcp batch missing %q\n%s", w, db)
		}
	}
}

func TestSaveLANv6DisabledMode(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	// 停用 DHCPv6 → 写 disabled，不删节、不动 stock ip6assign。
	if err := be.SaveLANv6s([]LANv6{{ID: "lan", Interface: "lan", Managed: true, Enabled: true, DHCPv6Enabled: false}}); err != nil {
		t.Fatal(err)
	}
	db := f.batchContaining("commit dhcp")
	for _, w := range []string{"set dhcp.lan.dhcpv6='disabled'", "set dhcp.lan.ra='disabled'"} {
		if !strings.Contains(db, w) {
			t.Errorf("disabled batch missing %q\n%s", w, db)
		}
	}
}

func TestSaveACLv6DUID(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	err := be.SaveACLv6(ACLv6{Mode: ACLBlacklist, Entries: []ACLv6Entry{
		{ID: "aclv6_1", Method: ACLv6MethodDUID, DUID: "00030001aabbccddeeff", Enabled: true, Managed: true},
		{ID: "aclv6_2", Method: ACLv6MethodL2, MAC: "11:22:33:44:55:66", Enabled: true, Managed: true}, // L2 不投射
	}})
	if err != nil {
		t.Fatal(err)
	}
	db := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.aclv6_1=host",
		"set dhcp.aclv6_1.managed_by='kwrt-net-manager-v6'",
		"set dhcp.aclv6_1.duid='00030001aabbccddeeff'",
		"set dhcp.aclv6_1.hostid='ignore'",
	} {
		if !strings.Contains(db, w) {
			t.Errorf("aclv6 batch missing %q\n%s", w, db)
		}
	}
	if strings.Contains(db, "aclv6_2") {
		t.Error("L2 方法不应投射到 UCI（OpenWrt 原生不支持），但 batch 含 aclv6_2")
	}
}

func TestSavePrefixStaticV6(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	err := be.SavePrefixStaticsV6([]PrefixStaticV6{{
		ID: "ps6_1", DUID: "00030001aabbccddeeff", HostID: "::1234", Remark: "NAS", Enabled: true, Managed: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	db := f.batchContaining("commit dhcp")
	for _, w := range []string{
		"set dhcp.ps6_1=host",
		"set dhcp.ps6_1.managed_by='kwrt-net-manager-v6'",
		"set dhcp.ps6_1.duid='00030001aabbccddeeff'",
		"set dhcp.ps6_1.hostid='1234'",
		"set dhcp.ps6_1.name='NAS'",
	} {
		if !strings.Contains(db, w) {
			t.Errorf("prefix-static batch missing %q\n%s", w, db)
		}
	}
}

// TestV6IsolationFromV4 验证关键设计：IPv4 apply 与 IPv6 applyIPv6 用不同的
// managed 标记，互不删除对方的具名节。
func TestV6IsolationFromV4(t *testing.T) {
	netShow := "network.r1=route\nnetwork.r1.managed_by='kwrt-net-manager'\nnetwork.r1.target='10.0.0.0'\n" +
		"network.wan6=interface\nnetwork.wan6.managed_by='kwrt-net-manager-v6'\nnetwork.wan6.proto='dhcpv6'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": netShow}}
	be := newTestUCI(t, f)

	// IPv4 apply（清空路由）只删 v4 managed 节 r1，绝不删 v6 节 wan6。
	if err := be.SaveRoutes([]Route{}); err != nil {
		t.Fatal(err)
	}
	v4 := f.batchContaining("commit network")
	if !strings.Contains(v4, "delete network.r1") {
		t.Errorf("v4 apply 应删除 r1\n%s", v4)
	}
	if strings.Contains(v4, "delete network.wan6") {
		t.Errorf("v4 apply 不应删除 v6 节 wan6\n%s", v4)
	}

	// IPv6 applyIPv6（清空 WANv6）只删 v6 节 wan6，绝不删 v4 节 r1。
	if err := be.SaveWANv6s([]WANv6{}); err != nil {
		t.Fatal(err)
	}
	v6 := f.batchContaining("commit network")
	if !strings.Contains(v6, "delete network.wan6") {
		t.Errorf("v6 apply 应删除 wan6\n%s", v6)
	}
	if strings.Contains(v6, "delete network.r1") {
		t.Errorf("v6 apply 不应删除 v4 节 r1\n%s", v6)
	}
}

func TestDuidToMAC(t *testing.T) {
	cases := map[string]string{
		"00030001f42d064fe3fb":           "f4:2d:06:4f:e3:fb", // DUID-LL
		"000100015abc1234aabbccddeeff":   "aa:bb:cc:dd:ee:ff", // DUID-LLT
		"0004aabbccddeeff00112233445566": "",                  // DUID-UUID → 无 MAC
		"":                               "",
	}
	for in, want := range cases {
		if got := duidToMAC(in); got != want {
			t.Errorf("duidToMAC(%q) = %q want %q", in, got, want)
		}
	}
}

func TestNeighborsV6Parse(t *testing.T) {
	out := "fe80::5 dev br-lan lladdr f4:2d:06:4f:e3:fb router STALE\n" +
		"2408::d0a dev br-lan lladdr 00:0c:29:06:da:3a REACHABLE\n" +
		"fe80::9 dev br-lan  FAILED\n"
	f := &fakeRunner{cmd: map[string]string{"ip -6 neighbor show": out}}
	be := &uciBackend{run: f}
	ns, err := be.NeighborsV6()
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 2 { // FAILED 无 lladdr → 跳过
		t.Fatalf("want 2 neighbors, got %d: %+v", len(ns), ns)
	}
	if ns[0].IPv6 != "fe80::5" || ns[0].MAC != "F4:2D:06:4F:E3:FB" || ns[0].State != "STALE" || !ns[0].Router {
		t.Errorf("neighbor0 = %+v", ns[0])
	}
	if ns[1].State != "REACHABLE" || ns[1].Router {
		t.Errorf("neighbor1 = %+v", ns[1])
	}
}

func TestLeasesV6Parse(t *testing.T) {
	ubusOut := `{"device":{"br-lan":{"leases":[{"duid":"00030001aabbccddeeff","iaid":4660,"hostname":"pc","valid":3600,"ipv6-addr":[{"address":"2408::c53"}]}]}}}`
	f := &fakeRunner{
		show: map[string]string{"dhcp": "", "network": ""},
		cmd:  map[string]string{"ubus call dhcp ipv6leases": ubusOut},
	}
	be := newTestUCI(t, f)
	ls, err := be.LeasesV6()
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 1 {
		t.Fatalf("want 1 lease, got %d: %+v", len(ls), ls)
	}
	l := ls[0]
	if l.MAC != "AA:BB:CC:DD:EE:FF" || l.IPv6Addr != "2408::c53" || l.Hostname != "pc" || l.ValidSeconds != 3600 || l.Interface != "br-lan" {
		t.Errorf("lease = %+v", l)
	}
}
