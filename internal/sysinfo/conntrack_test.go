package sysinfo

import "testing"

func TestParseConntrackLine(t *testing.T) {
	line := "ipv4     2 tcp      6 431999 ESTABLISHED src=192.168.1.219 dst=192.168.1.12 sport=1254 dport=8443 packets=415 bytes=931000 src=192.168.1.12 dst=192.168.1.219 sport=8443 dport=1254 packets=300 bytes=50000 [ASSURED] mark=0 use=1"
	f, ok := parseConntrackLine(line)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if f.Family != "ipv4" || f.Proto != "tcp" {
		t.Errorf("family/proto = %s/%s", f.Family, f.Proto)
	}
	if f.Src != "192.168.1.219:1254" || f.Dst != "192.168.1.12:8443" {
		t.Errorf("src/dst = %s -> %s", f.Src, f.Dst)
	}
	if f.Packets != 715 { // 415 + 300（双向）
		t.Errorf("packets = %d want 715", f.Packets)
	}
	if f.Bytes != 981000 { // 931000 + 50000
		t.Errorf("bytes = %d want 981000", f.Bytes)
	}
}

func TestParseConntrackLineIPv6NoAcct(t *testing.T) {
	// 无 packets/bytes（未开 acct）、IPv6、udp。
	line := "ipv6     10 udp      17 29 src=fe80::1 dst=ff02::1 sport=5353 dport=5353 [UNREPLIED] src=ff02::1 dst=fe80::1 sport=5353 dport=5353 mark=0 use=1"
	f, ok := parseConntrackLine(line)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if f.Family != "ipv6" || f.Proto != "udp" {
		t.Errorf("family/proto = %s/%s", f.Family, f.Proto)
	}
	if f.Src != "[fe80::1]:5353" || f.Dst != "[ff02::1]:5353" {
		t.Errorf("src/dst = %s -> %s", f.Src, f.Dst)
	}
	if f.Bytes != 0 {
		t.Errorf("bytes = %d want 0", f.Bytes)
	}
}

func TestParseConntrackLineGarbage(t *testing.T) {
	if _, ok := parseConntrackLine("not a conntrack line"); ok {
		t.Error("garbage should not parse")
	}
}
