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
