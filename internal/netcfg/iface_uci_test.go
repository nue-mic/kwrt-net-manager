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
		"set network.lan.ipaddr='192.168.9.1'",
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
