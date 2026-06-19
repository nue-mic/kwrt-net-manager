package netcfg

import "testing"

func TestCloneIfaceDeepCopy(t *testing.T) {
	tr := true
	orig := NetIface{
		ID: "lan", Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0",
		ExtraAddrs: []IfaceAddr{{Address: "10.0.0.1", Prefix: 24, Family: "ipv4", Remark: "nas", Enabled: true}},
		PeerDNS:    &tr,
	}
	c := cloneIface(orig)
	c.ExtraAddrs[0].Remark = "changed"
	*c.PeerDNS = false
	if orig.ExtraAddrs[0].Remark != "nas" {
		t.Errorf("ExtraAddrs not deep-copied: %q", orig.ExtraAddrs[0].Remark)
	}
	if orig.PeerDNS == nil || *orig.PeerDNS != true {
		t.Errorf("PeerDNS pointer aliased")
	}
}

func TestValidateNetIfaceExtraAddrs(t *testing.T) {
	base := func() NetIface {
		return NetIface{Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"}
	}
	// 合法附加 IP
	ok := base()
	ok.ExtraAddrs = []IfaceAddr{{Address: "10.0.0.1", Prefix: 24, Family: "ipv4", Enabled: true}}
	if err := validateNetIface(&ok); err != nil {
		t.Errorf("valid extra addr rejected: %v", err)
	}
	// 非法附加 IP
	bad := base()
	bad.ExtraAddrs = []IfaceAddr{{Address: "999.1.1.1", Prefix: 24, Family: "ipv4", Enabled: true}}
	if err := validateNetIface(&bad); err == nil {
		t.Error("invalid extra IP accepted")
	}
	// prefix 越界
	bp := base()
	bp.ExtraAddrs = []IfaceAddr{{Address: "10.0.0.1", Prefix: 33, Family: "ipv4", Enabled: true}}
	if err := validateNetIface(&bp); err == nil {
		t.Error("prefix 33 accepted")
	}
	// 与主 IP 重复
	dup := base()
	dup.ExtraAddrs = []IfaceAddr{{Address: "192.168.1.1", Prefix: 24, Family: "ipv4", Enabled: true}}
	if err := validateNetIface(&dup); err == nil {
		t.Error("duplicate of primary IP accepted")
	}
	// 非法 clone_mac
	mac := base()
	mac.CloneMAC = "zz:zz"
	if err := validateNetIface(&mac); err == nil {
		t.Error("invalid MAC accepted")
	}
}

func TestCheckIfaceRelations(t *testing.T) {
	existing := []NetIface{
		{ID: "lan", Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"},
	}
	servers := []DHCPServer{
		{ID: "d_lan", Interface: "lan", IPStart: "192.168.1.100", IPEnd: "192.168.1.200"},
	}
	// 跨接口 IP 冲突
	conflict := NetIface{ID: "lan2", Role: RoleLAN, IPAddr: "192.168.1.1", Netmask: "255.255.255.0"}
	if err := checkIfaceRelations(conflict, existing, servers); err == nil {
		t.Error("cross-iface duplicate IP accepted")
	}
	// 改子网导致绑定的 DHCP 池越界（G8）
	moved := NetIface{ID: "lan", Role: RoleLAN, IPAddr: "10.9.9.1", Netmask: "255.255.255.0"}
	if err := checkIfaceRelations(moved, existing, servers); err == nil {
		t.Error("subnet change orphaning DHCP pool accepted")
	}
	// 正常新增
	ok := NetIface{ID: "lan3", Role: RoleLAN, IPAddr: "192.168.5.1", Netmask: "255.255.255.0"}
	if err := checkIfaceRelations(ok, existing, servers); err != nil {
		t.Errorf("valid iface rejected: %v", err)
	}
}

func TestCanDeleteLastLAN(t *testing.T) {
	one := []NetIface{{ID: "lan", Role: RoleLAN}}
	if err := canDeleteNetIface("lan", one); err == nil {
		t.Error("deleting the only LAN was allowed")
	}
	two := []NetIface{{ID: "lan", Role: RoleLAN}, {ID: "lan2", Role: RoleLAN}}
	if err := canDeleteNetIface("lan2", two); err != nil {
		t.Errorf("deleting one of two LANs rejected: %v", err)
	}
	withWan := []NetIface{{ID: "lan", Role: RoleLAN}, {ID: "wan", Role: RoleWAN}}
	if err := canDeleteNetIface("wan", withWan); err != nil {
		t.Errorf("deleting WAN rejected: %v", err)
	}
}
