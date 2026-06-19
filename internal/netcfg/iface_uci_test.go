package netcfg

import (
	"strings"
	"testing"
)

const sampleNetIfaceShow = `network.loopback=interface
network.loopback.ipaddr='127.0.0.1'
network.lan=interface
network.lan.proto='static'
network.lan.device='br-lan'
network.lan.ipaddr='192.168.1.1'
network.lan.netmask='255.255.255.0'
network.dev_lan=device
network.dev_lan.type='bridge'
network.dev_lan.name='br-lan'
network.dev_lan.ports='eth1' 'eth2'
network.wan=interface
network.wan.proto='pppoe'
network.wan.device='eth0'
network.wan.username='user@isp'
network.wan.password='secret'
network.wan.mtu='1480'
`

func TestNetIfacesParsing(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": sampleNetIfaceShow}}
	be := newTestUCI(t, f)
	ifaces, err := be.NetIfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(ifaces) != 2 {
		t.Fatalf("want 2 ifaces (lan,wan), got %d: %+v", len(ifaces), ifaces)
	}
	byID := map[string]NetIface{}
	for _, x := range ifaces {
		byID[x.ID] = x
	}
	lan := byID["lan"]
	if lan.Role != RoleLAN || lan.Proto != ProtoStatic || lan.IPAddr != "192.168.1.1" {
		t.Errorf("lan = %+v", lan)
	}
	if len(lan.Ports) != 2 || lan.Ports[0] != "eth1" || lan.Ports[1] != "eth2" {
		t.Errorf("lan ports = %v", lan.Ports)
	}
	wan := byID["wan"]
	if wan.Role != RoleWAN || wan.Proto != ProtoPPPoE || wan.Username != "user@isp" || wan.Device != "eth0" || wan.MTU != 1480 {
		t.Errorf("wan = %+v", wan)
	}
}

func TestNetIfacesSkipsIPv6Companion(t *testing.T) {
	// A real ImmortalWrt config: lan (static) + lan6 (dhcpv6) on the same eth0.
	// 内外网设置 must list only lan, and eth0 must bind to lan (not lan6).
	show := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='eth0'\nnetwork.lan.ipaddr='192.168.2.11/19'\n" +
		"network.lan6=interface\nnetwork.lan6.proto='dhcpv6'\nnetwork.lan6.device='eth0'\n" +
		"network.docker=interface\nnetwork.docker.proto='none'\nnetwork.docker.device='docker0'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show}}
	be := newTestUCI(t, f)
	ifaces, err := be.NetIfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(ifaces) != 1 || ifaces[0].ID != "lan" {
		t.Fatalf("want only [lan], got %+v", ifaces)
	}
	if ifaces[0].IPAddr != "192.168.2.11" || ifaces[0].Netmask != "255.255.224.0" {
		t.Errorf("lan addressing = %s/%s", ifaces[0].IPAddr, ifaces[0].Netmask)
	}
	if m := be.deviceToIface(); m["eth0"].iface != "lan" {
		t.Errorf("eth0 should bind to lan, got %+v", m["eth0"])
	}
}

func TestNICsBoundMapping(t *testing.T) {
	// deviceToIface: eth1/eth2 → lan (bridge members), eth0 → wan.
	f := &fakeRunner{show: map[string]string{"network": sampleNetIfaceShow}}
	be := &uciBackend{run: f}
	m := be.deviceToIface()
	if m["eth1"].iface != "lan" || m["eth1"].role != RoleLAN {
		t.Errorf("eth1 → %+v", m["eth1"])
	}
	if m["eth0"].iface != "wan" || m["eth0"].role != RoleWAN {
		t.Errorf("eth0 → %+v", m["eth0"])
	}
	if m["br-lan"].iface != "lan" {
		t.Errorf("br-lan → %+v", m["br-lan"])
	}
}

func TestSaveNetIfaceWANPPPoE(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "wan", Role: RoleWAN, Proto: ProtoPPPoE, Device: "eth0",
		Username: "u@isp", Password: "p", MTU: 1480, DefaultGW: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"set network.wan=interface",
		"set network.wan.proto='pppoe'",
		"set network.wan.username='u@isp'",
		"set network.wan.password='p'",
		"set network.wan.device='eth0'",
		"set network.wan.mtu='1480'",
		"delete network.wan.defaultroute", // DefaultGW true → default route on
		"commit network",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("wan batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestSaveNetIfacePortExclusivity(t *testing.T) {
	show := "network.lan=interface\nnetwork.lan.device='br-lan'\n" +
		"network.dev_lan=device\nnetwork.dev_lan.type='bridge'\nnetwork.dev_lan.name='br-lan'\nnetwork.dev_lan.ports='eth1'\n" +
		"network.dev_lan2=device\nnetwork.dev_lan2.type='bridge'\nnetwork.dev_lan2.name='br-lan2'\nnetwork.dev_lan2.ports='eth2' 'eth3'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show}}
	be := newTestUCI(t, f)
	// Bind eth1+eth2+eth3 to lan → eth2,eth3 must be detached from br-lan2.
	if err := be.SaveNetIface(NetIface{ID: "lan", Role: RoleLAN, Device: "br-lan", IPAddr: "192.168.1.1", Netmask: "255.255.255.0", Ports: []string{"eth1", "eth2", "eth3"}}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"add_list network.dev_lan.ports='eth2'",
		"del_list network.dev_lan2.ports='eth2'",
		"del_list network.dev_lan2.ports='eth3'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("exclusivity batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestSaveNetIfaceLANSingleNIC(t *testing.T) {
	// Single-port LAN must bind the NIC directly — NO bridge section, and
	// crucially never a bridge named after the NIC (which would collide).
	show := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='eth0'\nnetwork.lan.ipaddr='192.168.2.11/19'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show}}
	be := newTestUCI(t, f)
	if err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "eth0",
		IPAddr: "192.168.2.11", Netmask: "255.255.224.0", Ports: []string{"eth0"},
	}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	if !strings.Contains(b, "set network.lan.device='eth0'") {
		t.Errorf("single-NIC LAN should bind eth0 directly\n%s", b)
	}
	for _, bad := range []string{"type='bridge'", "name='eth0'", "set network.dev_eth0"} {
		if strings.Contains(b, bad) {
			t.Errorf("single-NIC LAN must NOT create a bridge, found %q\n%s", bad, b)
		}
	}
}

func TestSaveNetIfaceMultiIP(t *testing.T) {
	// 旧接口是单 option ipaddr，升级为多 IP，必须先清残留再统一 list。
	show := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='br-lan'\nnetwork.lan.ipaddr='192.168.1.1'\nnetwork.lan.netmask='255.255.255.0'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show, "firewall": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "br-lan", Ports: []string{"eth1"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0",
		ExtraAddrs: []IfaceAddr{
			{Address: "10.0.0.1", Prefix: 24, Family: "ipv4", Enabled: true},
			{Address: "172.16.0.1", Prefix: 16, Family: "ipv4", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"delete network.lan.ipaddr",
		"delete network.lan.netmask",
		"add_list network.lan.ipaddr='192.168.1.1/24'",
		"add_list network.lan.ipaddr='10.0.0.1/24'",
		"add_list network.lan.ipaddr='172.16.0.1/16'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("multi-IP batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
	// 不能再写 option 形式
	if strings.Contains(b, "set network.lan.ipaddr=") || strings.Contains(b, "set network.lan.netmask=") {
		t.Errorf("must not write option ipaddr/netmask alongside list\n%s", b)
	}
}

func TestSaveNetIfaceFullFields(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	peer := false
	auto := true
	err := be.SaveNetIface(NetIface{
		ID: "wan", Role: RoleWAN, Proto: ProtoStatic, Device: "eth0",
		IPAddr: "1.1.1.2", Netmask: "255.255.255.0", Gateway: "1.1.1.1",
		Metric: 10, PeerDNS: &peer, Auto: &auto, Broadcast: "1.1.1.255",
		IP6Assign: 60, IP6Hint: "10", IP6Addr: "2001:db8::1/64", IP6Gw: "2001:db8::1",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"set network.wan.metric='10'",
		"set network.wan.peerdns='0'",
		"set network.wan.auto='1'",
		"set network.wan.broadcast='1.1.1.255'",
		"set network.wan.ip6assign='60'",
		"set network.wan.ip6hint='10'",
		"set network.wan.ip6addr='2001:db8::1/64'",
		"set network.wan.ip6gw='2001:db8::1'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("full-fields batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestSaveNetIfaceCloneMAC(t *testing.T) {
	// 单网卡直连（无 device 段）：建 dev_lan 承载 macaddr。
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "eth0", Ports: []string{"eth0"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0", CloneMAC: "AA:BB:CC:DD:EE:FF",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"set network.dev_lan=device",
		"set network.dev_lan.name='eth0'",
		"set network.dev_lan.macaddr='AA:BB:CC:DD:EE:FF'",
		"set network.dev_lan.managed_by='kwrt-net-manager'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("clone-mac batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestSaveNetIfaceCloneMACOnBridge(t *testing.T) {
	// 已是网桥（dev_lan 存在）：macaddr 写到现有 device 段，不新建。
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": sampleNetIfaceShow, "firewall": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "br-lan", Ports: []string{"eth1", "eth2"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0", CloneMAC: "AA:BB:CC:DD:EE:01",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	if !strings.Contains(b, "set network.dev_lan.macaddr='AA:BB:CC:DD:EE:01'") {
		t.Errorf("bridge clone-mac should write to dev_lan\n%s", b)
	}
}

func TestSaveNetIfaceLANBridge(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": sampleNetIfaceShow}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "br-lan",
		IPAddr: "192.168.9.1", Netmask: "255.255.255.0",
		Ports: []string{"eth1", "eth2", "eth3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"set network.lan.proto='static'",
		"set network.lan.device='br-lan'",
		"add_list network.lan.ipaddr='192.168.9.1/24'",
		"set network.dev_lan.type='bridge'", // reused existing bridge section
		"set network.dev_lan.name='br-lan'",
		"add_list network.dev_lan.ports='eth1'",
		"add_list network.dev_lan.ports='eth3'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("lan batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}
