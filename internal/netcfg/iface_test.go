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
