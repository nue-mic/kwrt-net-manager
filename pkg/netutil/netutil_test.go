package netutil

import "testing"

func TestIPv4Uint32RoundTrip(t *testing.T) {
	cases := []struct {
		ip string
		u  uint32
	}{
		{"0.0.0.0", 0},
		{"127.0.0.1", 0x7F000001},
		{"192.168.1.1", 0xC0A80101},
		{"255.255.255.255", 0xFFFFFFFF},
	}
	for _, c := range cases {
		got, ok := IPv4ToUint32(c.ip)
		if !ok || got != c.u {
			t.Errorf("IPv4ToUint32(%q) = %#x,%v want %#x", c.ip, got, ok, c.u)
		}
		if back := Uint32ToIPv4(c.u); back != c.ip {
			t.Errorf("Uint32ToIPv4(%#x) = %q want %q", c.u, back, c.ip)
		}
	}
}

func TestIPv4Invalid(t *testing.T) {
	for _, s := range []string{"", "256.1.1.1", "1.2.3", "::1", "abc", "1.2.3.4.5"} {
		if _, ok := IPv4ToUint32(s); ok {
			t.Errorf("IPv4ToUint32(%q) unexpectedly ok", s)
		}
	}
}

func TestIsIPv6(t *testing.T) {
	if !IsIPv6("fe80::1") {
		t.Error("fe80::1 should be IPv6")
	}
	if IsIPv6("192.168.1.1") {
		t.Error("192.168.1.1 should not be IPv6")
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := []struct{ in, out string }{
		{"aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF"},
		{"AA-BB-CC-DD-EE-FF", "AA:BB:CC:DD:EE:FF"},
		{"14:9b:77:68:c5:1c", "14:9B:77:68:C5:1C"},
		{"", ""},
		{"gg:bb:cc:dd:ee:ff", ""},
		{"aa:bb:cc:dd:ee", ""},
		{"not a mac", ""},
	}
	for _, c := range cases {
		if got := NormalizeMAC(c.in); got != c.out {
			t.Errorf("NormalizeMAC(%q) = %q want %q", c.in, got, c.out)
		}
	}
}

func TestMaskPrefixRoundTrip(t *testing.T) {
	cases := []struct {
		mask   string
		prefix int
	}{
		{"0.0.0.0", 0},
		{"255.0.0.0", 8},
		{"255.255.224.0", 19},
		{"255.255.255.0", 24},
		{"255.255.255.255", 32},
	}
	for _, c := range cases {
		p, ok := MaskToPrefix(c.mask)
		if !ok || p != c.prefix {
			t.Errorf("MaskToPrefix(%q) = %d,%v want %d", c.mask, p, ok, c.prefix)
		}
		if m := PrefixToMask(c.prefix); m != c.mask {
			t.Errorf("PrefixToMask(%d) = %q want %q", c.prefix, m, c.mask)
		}
	}
}

func TestMaskInvalid(t *testing.T) {
	// 255.255.0.255 has a 1 after a 0 → not contiguous.
	for _, m := range []string{"255.255.0.255", "0.255.0.0", "abc", "255.0.255.0"} {
		if IsValidNetmask(m) {
			t.Errorf("IsValidNetmask(%q) should be false", m)
		}
	}
}

func TestPrefixToMaskRange(t *testing.T) {
	if PrefixToMask(-1) != "" || PrefixToMask(33) != "" {
		t.Error("out-of-range prefix should yield empty mask")
	}
}

func TestNetworkBaseAndSameSubnet(t *testing.T) {
	base, ok := NetworkBase("192.168.31.200", "255.255.224.0")
	if !ok || base != "192.168.0.0" {
		t.Errorf("NetworkBase = %q,%v want 192.168.0.0", base, ok)
	}
	if !SameSubnet("192.168.1.31", "192.168.31.254", "255.255.224.0") {
		t.Error("addresses should be in same /19 subnet")
	}
	if SameSubnet("192.168.1.1", "192.168.40.1", "255.255.224.0") {
		t.Error("192.168.40.1 is outside the /19")
	}
}

func TestRangeCount(t *testing.T) {
	n, ok := RangeCount("192.168.1.31", "192.168.31.254")
	if !ok || n != (31-1)*256+(254-31)+1 {
		t.Errorf("RangeCount = %d,%v", n, ok)
	}
	if _, ok := RangeCount("192.168.1.10", "192.168.1.1"); ok {
		t.Error("reversed range should be invalid")
	}
	n, ok = RangeCount("10.0.0.5", "10.0.0.5")
	if !ok || n != 1 {
		t.Errorf("single-address range count = %d,%v want 1", n, ok)
	}
}

func TestIPInRange(t *testing.T) {
	if !IPInRange("192.168.1.50", "192.168.1.1", "192.168.1.100") {
		t.Error("should be in range")
	}
	if IPInRange("192.168.1.200", "192.168.1.1", "192.168.1.100") {
		t.Error("should be out of range")
	}
}

func TestDHCPStartLimit(t *testing.T) {
	// iface 192.168.1.1/19 (base 192.168.0.0), pool .1.31 – .31.254.
	start, limit, ok := DHCPStartLimit("192.168.1.1", "255.255.224.0", "192.168.1.31", "192.168.31.254")
	if !ok {
		t.Fatal("expected ok")
	}
	wantStart := (1*256 + 31) // host offset of .1.31 from .0.0
	wantLimit := (31-1)*256 + (254 - 31) + 1
	if start != wantStart || limit != wantLimit {
		t.Errorf("DHCPStartLimit = %d,%d want %d,%d", start, limit, wantStart, wantLimit)
	}
	// Pool outside subnet → not ok.
	if _, _, ok := DHCPStartLimit("192.168.1.1", "255.255.255.0", "192.168.2.10", "192.168.2.20"); ok {
		t.Error("pool outside /24 should be rejected")
	}
}

func TestParseExcludeLine(t *testing.T) {
	s, e, ok := ParseExcludeLine("192.168.1.5")
	if !ok || s != "192.168.1.5" || e != "192.168.1.5" {
		t.Errorf("single = %q,%q,%v", s, e, ok)
	}
	s, e, ok = ParseExcludeLine(" 192.168.1.10 - 192.168.1.20 ")
	if !ok || s != "192.168.1.10" || e != "192.168.1.20" {
		t.Errorf("range = %q,%q,%v", s, e, ok)
	}
	if _, _, ok := ParseExcludeLine("192.168.1.20-192.168.1.10"); ok {
		t.Error("reversed exclude range should fail")
	}
	if _, _, ok := ParseExcludeLine("garbage"); ok {
		t.Error("garbage should fail")
	}
}

func TestParseLeaseLine(t *testing.T) {
	pl, ok := ParseLeaseLine("1718000000 14:9b:77:68:c5:1c 192.168.1.220 MacMini 01:14:9b:77:68:c5:1c")
	if !ok {
		t.Fatal("expected ok")
	}
	if pl.Expiry != 1718000000 || pl.MAC != "14:9B:77:68:C5:1C" || pl.IP != "192.168.1.220" ||
		pl.Hostname != "MacMini" || pl.ClientID != "01:14:9b:77:68:c5:1c" {
		t.Errorf("parsed = %+v", pl)
	}
	// "*" hostname → empty.
	pl, ok = ParseLeaseLine("0 aa:bb:cc:dd:ee:ff 10.0.0.5 * *")
	if !ok || pl.Hostname != "" || pl.ClientID != "" || pl.Expiry != 0 {
		t.Errorf("wildcard parse = %+v,%v", pl, ok)
	}
	for _, bad := range []string{"", "x", "notnum aa:bb:cc:dd:ee:ff 10.0.0.1", "100 zz 10.0.0.1"} {
		if _, ok := ParseLeaseLine(bad); ok {
			t.Errorf("ParseLeaseLine(%q) should fail", bad)
		}
	}
}
