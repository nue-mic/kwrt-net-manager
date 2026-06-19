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
		"add_list network.wan.ip6addr='2001:db8::1/64'", // 主 IPv6 现以 list 形式投射
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

func TestSaveNetIfaceJoinsFirewallZone(t *testing.T) {
	fw := "firewall.lanzone=zone\nfirewall.lanzone.name='lan'\nfirewall.lanzone.network='lan'\n" +
		"firewall.wanzone=zone\nfirewall.wanzone.name='wan'\nfirewall.wanzone.network='wan' 'wan6'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": fw}}
	be := newTestUCI(t, f)
	// 新建 lan2 → 自动进 lan zone
	if err := be.SaveNetIface(NetIface{ID: "lan2", Role: RoleLAN, Device: "eth3", Ports: []string{"eth3"}, IPAddr: "192.168.5.1", Netmask: "255.255.255.0"}); err != nil {
		t.Fatal(err)
	}
	fwb := f.batchContaining("commit firewall")
	if !strings.Contains(fwb, "add_list firewall.lanzone.network='lan2'") {
		t.Errorf("lan2 should join lan zone\n%s", fwb)
	}
}

func TestSaveMainLANSkipsZone(t *testing.T) {
	fw := "firewall.lanzone=zone\nfirewall.lanzone.name='lan'\nfirewall.lanzone.network='lan'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": fw}}
	be := newTestUCI(t, f)
	if err := be.SaveNetIface(NetIface{ID: "lan", Role: RoleLAN, Device: "eth1", Ports: []string{"eth1"}, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"}); err != nil {
		t.Fatal(err)
	}
	// main lan 已在默认 zone：不应发起任何 firewall commit（batchContaining 未命中返回空串）。
	if f.batchContaining("commit firewall") != "" {
		t.Error("main lan already in zone, must not touch firewall")
	}
}

func TestDeleteNetIfaceCleansUp(t *testing.T) {
	// 真机：多网卡桥 lan2 的 device='br-lan2'，writeBridge 建的 device 段名是
	// uciName("dev_br-lan2") = dev_br_lan2（不是 dev_lan2）。删除必须按接口当前
	// device 选项指向的托管段精确回收，否则猜名 dev_lan2 漏删 → 孤儿桥段残留。
	net := "network.lan2=interface\nnetwork.lan2.proto='static'\nnetwork.lan2.device='br-lan2'\n" +
		"network.dev_br_lan2=device\nnetwork.dev_br_lan2.type='bridge'\nnetwork.dev_br_lan2.name='br-lan2'\nnetwork.dev_br_lan2.managed_by='kwrt-net-manager'\nnetwork.dev_br_lan2.ports='eth3'\n"
	fw := "firewall.lanzone=zone\nfirewall.lanzone.name='lan'\nfirewall.lanzone.network='lan' 'lan2'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": net, "firewall": fw}}
	be := newTestUCI(t, f)
	if err := be.DeleteNetIface("lan2"); err != nil {
		t.Fatal(err)
	}
	netb := f.batchContaining("commit network")
	if !strings.Contains(netb, "delete network.lan2") {
		t.Errorf("should delete interface\n%s", netb)
	}
	if !strings.Contains(netb, "delete network.dev_br_lan2") {
		t.Errorf("should delete managed device section dev_br_lan2 (真机段名)\n%s", netb)
	}
	fwb := f.batchContaining("commit firewall")
	if !strings.Contains(fwb, "del_list firewall.lanzone.network='lan2'") {
		t.Errorf("should leave firewall zone\n%s", fwb)
	}
}

func TestSaveNetIfaceTopologySwitchCleansOldDevice(t *testing.T) {
	// 危险场景：单网卡直连 lan（device=eth0）曾用 clone_mac 建了托管段
	// dev_lan(name=eth0, macaddr)；用户改成多网卡桥接（Ports=eth1,eth2,Device=br-lan）。
	// writeBridge 会另建 dev_br_lan，旧的 dev_lan 必须被回收，否则 eth0 上克隆 MAC
	// 仍生效（ISP 绑 MAC 会断网）。
	net := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='eth0'\nnetwork.lan.ipaddr='192.168.1.1'\nnetwork.lan.netmask='255.255.255.0'\n" +
		"network.dev_lan=device\nnetwork.dev_lan.name='eth0'\nnetwork.dev_lan.macaddr='AA:BB:CC:DD:EE:FF'\nnetwork.dev_lan.managed_by='kwrt-net-manager'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": net, "firewall": ""}}
	be := newTestUCI(t, f)
	if err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "br-lan",
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0",
		Ports: []string{"eth1", "eth2"},
	}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	if !strings.Contains(b, "delete network.dev_lan") {
		t.Errorf("topology switch should reclaim old managed device dev_lan\n--- batch ---\n%s", b)
	}
	// 新桥段 dev_br_lan 必须被建出来（不能误删）
	if !strings.Contains(b, "set network.dev_br_lan.name='br-lan'") {
		t.Errorf("new bridge device dev_br_lan should be created\n--- batch ---\n%s", b)
	}
}

func TestSaveNetIfaceSamePortNoDelete(t *testing.T) {
	// 常见场景：单网卡 lan（device=eth0，托管 dev_lan 承载 clone_mac）仅改 IP，
	// 仍 device=eth0 / Ports=[eth0]。device 段没换，绝不能误删 dev_lan。
	net := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='eth0'\nnetwork.lan.ipaddr='192.168.1.1'\nnetwork.lan.netmask='255.255.255.0'\n" +
		"network.dev_lan=device\nnetwork.dev_lan.name='eth0'\nnetwork.dev_lan.macaddr='AA:BB:CC:DD:EE:FF'\nnetwork.dev_lan.managed_by='kwrt-net-manager'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": net, "firewall": ""}}
	be := newTestUCI(t, f)
	if err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "eth0",
		IPAddr: "192.168.1.99", Netmask: "255.255.255.0", Ports: []string{"eth0"},
		CloneMAC: "AA:BB:CC:DD:EE:FF",
	}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	if strings.Contains(b, "delete network.dev_lan\n") {
		t.Errorf("same-device IP-only change must NOT delete dev_lan\n--- batch ---\n%s", b)
	}
}

func TestNetIfacesReadsMultiIP(t *testing.T) {
	show := "network.lan=interface\nnetwork.lan.proto='static'\nnetwork.lan.device='br-lan'\n" +
		"network.lan.ipaddr='192.168.1.1/24' '10.0.0.1/24'\n" +
		"network.lan.metric='5'\nnetwork.lan.peerdns='0'\nnetwork.lan.ip6assign='60'\n" +
		"network.dev_lan=device\nnetwork.dev_lan.type='bridge'\nnetwork.dev_lan.name='br-lan'\nnetwork.dev_lan.macaddr='AA:BB:CC:DD:EE:FF'\nnetwork.dev_lan.ports='eth1'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show}}
	be := newTestUCI(t, f)
	ifaces, err := be.NetIfaces()
	if err != nil {
		t.Fatal(err)
	}
	lan := ifaces[0]
	if lan.IPAddr != "192.168.1.1" || lan.Netmask != "255.255.255.0" {
		t.Errorf("primary = %s/%s", lan.IPAddr, lan.Netmask)
	}
	if len(lan.ExtraAddrs) != 1 || lan.ExtraAddrs[0].Address != "10.0.0.1" || lan.ExtraAddrs[0].Prefix != 24 {
		t.Errorf("extra addrs = %+v", lan.ExtraAddrs)
	}
	if lan.Metric != 5 || lan.PeerDNS == nil || *lan.PeerDNS != false || lan.IP6Assign != 60 {
		t.Errorf("full fields = metric:%d peerdns:%v ip6assign:%d", lan.Metric, lan.PeerDNS, lan.IP6Assign)
	}
	if lan.CloneMAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("clone_mac from device = %q", lan.CloneMAC)
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

func TestSaveNetIfaceMultiIPv6(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "eth0", Ports: []string{"eth0"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0",
		IP6Addr: "2001:db8::1/64",
		ExtraAddrs: []IfaceAddr{
			{Address: "10.0.0.1", Prefix: 24, Family: "ipv4", Enabled: true},
			{Address: "fd00::1", Prefix: 64, Family: "ipv6", Enabled: true},
			{Address: "fd11::1", Prefix: 64, Family: "ipv6", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"delete network.lan.ip6addr",
		"add_list network.lan.ip6addr='2001:db8::1/64'",
		"add_list network.lan.ip6addr='fd00::1/64'",
		"add_list network.lan.ip6addr='fd11::1/64'",
		"add_list network.lan.ipaddr='192.168.1.1/24'",
		"add_list network.lan.ipaddr='10.0.0.1/24'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("ipv6 batch missing %q\n%s", w, b)
		}
	}
	// 不得再写 option ip6addr
	if strings.Contains(b, "set network.lan.ip6addr=") {
		t.Errorf("must not write option ip6addr with list\n%s", b)
	}
}

func TestSaveNetIfacePPPoEv6Keepalive(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	v6 := true
	if err := be.SaveNetIface(NetIface{ID: "wan", Role: RoleWAN, Proto: ProtoPPPoE, Device: "eth1",
		Username: "u", Password: "p", Keepalive: "5 25", PPPoEv6: &v6}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{"set network.wan.keepalive='5 25'", "set network.wan.ipv6='1'"} {
		if !strings.Contains(b, w) {
			t.Errorf("pppoe batch missing %q\n%s", w, b)
		}
	}
}
