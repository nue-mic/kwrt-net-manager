package netcfg

import "testing"

func TestParseAddrLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want NICAddr
		ok   bool
	}{
		{
			name: "ipv4 global",
			line: `2: eth0    inet 192.168.1.12/24 brd 192.168.1.255 scope global eth0\       valid_lft forever preferred_lft forever`,
			want: NICAddr{Family: FamilyIPv4, Address: "192.168.1.12", Prefix: 24, Scope: "global"},
			ok:   true,
		},
		{
			name: "ipv6 link",
			line: `3: br-lan    inet6 fe80::7e2b:e1ff:fe13:e45a/64 scope link \       valid_lft forever preferred_lft forever`,
			want: NICAddr{Family: FamilyIPv6, Address: "fe80::7e2b:e1ff:fe13:e45a", Prefix: 64, Scope: "link"},
			ok:   true,
		},
		{
			name: "ipv6 global no scope token defaults global",
			line: `4: br-lan    inet6 fd00::1/60`,
			want: NICAddr{Family: FamilyIPv6, Address: "fd00::1", Prefix: 60, Scope: "global"},
			ok:   true,
		},
		{
			name: "loopback v4 skipped",
			line: `1: lo    inet 127.0.0.1/8 scope host lo`,
			ok:   false,
		},
		{
			name: "loopback v6 skipped",
			line: `1: lo    inet6 ::1/128 scope host`,
			ok:   false,
		},
		{
			name: "garbage skipped",
			line: `2: eth0    link/ether 7c:2b:e1:13:e4:59 brd ff:ff:ff:ff:ff:ff`,
			ok:   false,
		},
		{
			name: "empty",
			line: ``,
			ok:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseAddrLine(c.line)
			if ok != c.ok {
				t.Fatalf("ok=%v want %v (got %+v)", ok, c.ok, got)
			}
			if !c.ok {
				return
			}
			if got != c.want {
				t.Errorf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestParseVlanLine(t *testing.T) {
	out := `5: eth0.10@eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP mode DEFAULT group default qlen 1000
    link/ether 7c:2b:e1:13:e4:59 brd ff:ff:ff:ff:ff:ff promiscuity 0
    vlan protocol 802.1Q id 10 <REORDER_HDR> addrgenmode eui64`
	id, proto, ok := parseVlanLine(out)
	if !ok || id != 10 || proto != "802.1Q" {
		t.Fatalf("got id=%d proto=%q ok=%v, want 10/802.1Q/true", id, proto, ok)
	}

	// Non-VLAN device → ok=false.
	plain := `2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP mode DEFAULT group default qlen 1000
    link/ether 7c:2b:e1:13:e4:59 brd ff:ff:ff:ff:ff:ff`
	if _, _, ok := parseVlanLine(plain); ok {
		t.Errorf("plain device should not parse as vlan")
	}
}

func TestParseEthtool(t *testing.T) {
	out := `Settings for eth0:
	Supported ports: [ TP MII ]
	Supported link modes:   10baseT/Half 10baseT/Full
	                        100baseT/Half 100baseT/Full
	                        1000baseT/Full
	Supported pause frame use: Symmetric Receive-only
	Supports auto-negotiation: Yes
	Advertised link modes:  10baseT/Half 10baseT/Full
	                        100baseT/Half 100baseT/Full
	                        1000baseT/Full
	Advertised pause frame use: Symmetric
	Advertised auto-negotiation: Yes
	Speed: 1000Mb/s
	Duplex: Full
	Port: Twisted Pair
	PHYAD: 0
	Transceiver: internal
	Auto-negotiation: on
	MDI-X: Unknown
	Link detected: yes`
	e := parseEthtool(out)
	if e.speedMb != 1000 {
		t.Errorf("speedMb=%d want 1000", e.speedMb)
	}
	if e.duplex != "full" {
		t.Errorf("duplex=%q want full", e.duplex)
	}
	if e.autoneg != "on" {
		t.Errorf("autoneg=%q want on", e.autoneg)
	}
	if e.port != "Twisted Pair" {
		t.Errorf("port=%q want Twisted Pair", e.port)
	}
	if e.linkDetected == nil || !*e.linkDetected {
		t.Errorf("linkDetected=%v want true", e.linkDetected)
	}
	// Supported modes: 5 tokens across 3 lines.
	wantSup := []string{"10baseT/Half", "10baseT/Full", "100baseT/Half", "100baseT/Full", "1000baseT/Full"}
	if !eqStrs(e.supported, wantSup) {
		t.Errorf("supported=%v want %v", e.supported, wantSup)
	}
	if !eqStrs(e.advertised, wantSup) {
		t.Errorf("advertised=%v want %v", e.advertised, wantSup)
	}
}

func TestParseEthtoolEmpty(t *testing.T) {
	// Missing/garbage output must yield a zero-value info, never panic.
	e := parseEthtool("")
	if e.speedMb != 0 || e.duplex != "" || e.linkDetected != nil || len(e.supported) != 0 {
		t.Errorf("empty parse should be zero, got %+v", e)
	}
}

func TestParseEthtoolDriver(t *testing.T) {
	out := `driver: r8169
version: 6.6.30
firmware-version: rtl8168h-2_0.0.2 02/26/15
expansion-rom-version:
bus-info: 0000:01:00.0
supports-statistics: yes
supports-test: no`
	d := parseEthtoolDriver(out)
	if d.driver != "r8169" {
		t.Errorf("driver=%q", d.driver)
	}
	if d.version != "6.6.30" {
		t.Errorf("version=%q", d.version)
	}
	if d.firmware != "rtl8168h-2_0.0.2 02/26/15" {
		t.Errorf("firmware=%q", d.firmware)
	}
	if d.busInfo != "0000:01:00.0" {
		t.Errorf("busInfo=%q", d.busInfo)
	}
}

func TestParseEthtoolPerm(t *testing.T) {
	if got := parseEthtoolPerm("Permanent address: 7c:2b:e1:13:e4:59"); got != "7C:2B:E1:13:E4:59" {
		t.Errorf("perm=%q want normalized 7C:2B:E1:13:E4:59", got)
	}
	if got := parseEthtoolPerm(""); got != "" {
		t.Errorf("empty perm=%q want empty", got)
	}
}

func TestStoreNICDetail(t *testing.T) {
	be := &storeBackend{}
	d, err := be.NICDetail("br-lan")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "br-lan" || d.Kind != NICBridge {
		t.Errorf("got name=%q kind=%q", d.Name, d.Kind)
	}
	if d.Operstate != "up" || !d.Carrier {
		t.Errorf("br-lan should be up, got operstate=%q carrier=%v", d.Operstate, d.Carrier)
	}
	if len(d.BridgePorts) == 0 {
		t.Errorf("bridge should report ports")
	}
	if d.Stats.RxBytes != d.RxBytes {
		t.Errorf("stats rx_bytes %d should mirror nic rx_bytes %d", d.Stats.RxBytes, d.RxBytes)
	}
	// Addrs synthesized from simulated IPAddrs (v4 + v6).
	var v4, v6 int
	for _, a := range d.Addrs {
		switch a.Family {
		case FamilyIPv4:
			v4++
		case FamilyIPv6:
			v6++
		}
	}
	if v4 != 1 || v6 != 1 {
		t.Errorf("want 1 v4 + 1 v6 addr, got %d/%d (%+v)", v4, v6, d.Addrs)
	}

	if _, err := be.NICDetail("does-not-exist"); err != ErrNotFound {
		t.Errorf("missing NIC should be ErrNotFound, got %v", err)
	}
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
