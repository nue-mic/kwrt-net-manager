package netcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 接口自定义 DNS（list dns / dns_metric / peerdns 联动）的命令生成、回读、往返、校验与
// 旧数据迁移，全部用 fake-exec 锁定，无需真机。

func TestSaveNetIfaceLANMultiDNS(t *testing.T) {
	// LAN(static) 现在必须投射 list dns（历史上完全不写），多条 + IPv6 通吃、保序。
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	if err := be.SaveNetIface(NetIface{
		ID: "lan", Role: RoleLAN, Device: "eth1", Ports: []string{"eth1"},
		IPAddr: "192.168.1.1", Netmask: "255.255.255.0",
		DNS: []string{"223.5.5.5", "114.114.114.114", "2400:3200::1"},
	}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"delete network.lan.dns",
		"add_list network.lan.dns='223.5.5.5'",
		"add_list network.lan.dns='114.114.114.114'",
		"add_list network.lan.dns='2400:3200::1'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("lan dns batch missing %q\n--- batch ---\n%s", w, b)
		}
	}
}

func TestSaveNetIfaceWANStaticMultiDNS(t *testing.T) {
	// WAN(static) 不再卡 2 条，全部 add_list。
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	if err := be.SaveNetIface(NetIface{
		ID: "wan", Role: RoleWAN, Proto: ProtoStatic, Device: "eth0",
		IPAddr: "1.1.1.2", Netmask: "255.255.255.0", Gateway: "1.1.1.1",
		DNS: []string{"8.8.8.8", "8.8.4.4", "1.1.1.1"},
	}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"add_list network.wan.dns='8.8.8.8'",
		"add_list network.wan.dns='8.8.4.4'",
		"add_list network.wan.dns='1.1.1.1'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("wan static dns batch missing %q\n%s", w, b)
		}
	}
}

func TestSaveNetIfaceWANDHCPCustomDNSPeerdns(t *testing.T) {
	// WAN(dhcp)：现在可配自定义 DNS（历史上直接删），并与 peerdns=0 联动「只用自定义」。
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	peer := false
	if err := be.SaveNetIface(NetIface{
		ID: "wan", Role: RoleWAN, Proto: ProtoDHCP, Device: "eth0",
		DNS: []string{"8.8.8.8", "1.1.1.1"}, PeerDNS: &peer,
	}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"set network.wan.proto='dhcp'",
		"set network.wan.peerdns='0'",
		"delete network.wan.dns",
		"add_list network.wan.dns='8.8.8.8'",
		"add_list network.wan.dns='1.1.1.1'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("wan dhcp custom dns batch missing %q\n%s", w, b)
		}
	}
}

func TestSaveNetIfacePPPoEClearsStaleDNS(t *testing.T) {
	// 历史 bug：PPPoE 分支从不清 dns。改用统一 writeDNSList 后：无自定义 DNS 时仅 delete、
	// 不残留旧 dns，也不新增。
	show := "network.wan=interface\nnetwork.wan.proto='static'\nnetwork.wan.device='eth1'\nnetwork.wan.dns='8.8.8.8' '8.8.4.4'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show, "firewall": ""}}
	be := newTestUCI(t, f)
	if err := be.SaveNetIface(NetIface{
		ID: "wan", Role: RoleWAN, Proto: ProtoPPPoE, Device: "eth1", Username: "u", Password: "p",
	}); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	if !strings.Contains(b, "delete network.wan.dns") {
		t.Errorf("pppoe must clear stale dns\n%s", b)
	}
	if strings.Contains(b, "add_list network.wan.dns=") {
		t.Errorf("pppoe with no custom dns must not add any\n%s", b)
	}
}

func TestSaveNetIfaceDNSMetric(t *testing.T) {
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": "", "firewall": ""}}
	be := newTestUCI(t, f)
	// 设了 dns_metric → set
	if err := be.SaveNetIface(NetIface{
		ID: "wan", Role: RoleWAN, Proto: ProtoStatic, Device: "eth0",
		IPAddr: "1.1.1.2", Netmask: "255.255.255.0", DNSMetric: 10, DNS: []string{"8.8.8.8"},
	}); err != nil {
		t.Fatal(err)
	}
	if b := f.batchContaining("commit network"); !strings.Contains(b, "set network.wan.dns_metric='10'") {
		t.Errorf("dns_metric set missing\n%s", b)
	}
	// 不设（0）→ delete 回归默认
	f.calls = nil
	if err := be.SaveNetIface(NetIface{
		ID: "wan", Role: RoleWAN, Proto: ProtoStatic, Device: "eth0",
		IPAddr: "1.1.1.2", Netmask: "255.255.255.0", DNSMetric: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if b := f.batchContaining("commit network"); !strings.Contains(b, "delete network.wan.dns_metric") {
		t.Errorf("dns_metric=0 should delete\n%s", b)
	}
}

func TestNetIfacesReadsMultiDNS(t *testing.T) {
	show := "network.wan=interface\nnetwork.wan.proto='static'\nnetwork.wan.device='eth0'\n" +
		"network.wan.ipaddr='1.1.1.2/24'\n" +
		"network.wan.dns='8.8.8.8' '8.8.4.4' '2606:4700:4700::1111'\n" +
		"network.wan.dns_metric='5'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show}}
	be := newTestUCI(t, f)
	ifaces, err := be.NetIfaces()
	if err != nil {
		t.Fatal(err)
	}
	wan := ifaces[0]
	if len(wan.DNS) != 3 || wan.DNS[0] != "8.8.8.8" || wan.DNS[1] != "8.8.4.4" || wan.DNS[2] != "2606:4700:4700::1111" {
		t.Errorf("dns = %v (want 3, 含 IPv6)", wan.DNS)
	}
	if wan.DNSMetric != 5 {
		t.Errorf("dns_metric = %d want 5", wan.DNSMetric)
	}
}

func TestNetIfaceDNSRoundTripSymmetric(t *testing.T) {
	// 读 3 条 list dns → DNS[] 3 条 → 原样写回应产出同样 3 条 add_list，且先 delete。
	show := "network.wan=interface\nnetwork.wan.proto='static'\nnetwork.wan.device='eth0'\n" +
		"network.wan.ipaddr='1.1.1.2/24'\n" +
		"network.wan.dns='8.8.8.8' '8.8.4.4' '2606:4700:4700::1111'\n"
	f := &fakeRunner{show: map[string]string{"dhcp": "", "network": show, "firewall": ""}}
	be := newTestUCI(t, f)
	ifaces, err := be.NetIfaces()
	if err != nil {
		t.Fatal(err)
	}
	if err := be.SaveNetIface(ifaces[0]); err != nil {
		t.Fatal(err)
	}
	b := f.batchContaining("commit network")
	for _, w := range []string{
		"delete network.wan.dns",
		"add_list network.wan.dns='8.8.8.8'",
		"add_list network.wan.dns='8.8.4.4'",
		"add_list network.wan.dns='2606:4700:4700::1111'",
	} {
		if !strings.Contains(b, w) {
			t.Errorf("round-trip dns missing %q\n%s", w, b)
		}
	}
}

func TestValidateNetIfaceDNS(t *testing.T) {
	// 多条 + IPv6 接受、空行过滤、原地写回归一化列表。
	in := NetIface{Role: RoleWAN, Proto: ProtoStatic, IPAddr: "1.1.1.2",
		DNS: []string{"8.8.8.8", "  ", "2606:4700:4700::1111"}}
	if err := validateNetIface(&in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(in.DNS) != 2 || in.DNS[0] != "8.8.8.8" || in.DNS[1] != "2606:4700:4700::1111" {
		t.Errorf("normalized dns = %v", in.DNS)
	}
	// 非法地址
	if err := validateNetIface(&NetIface{Role: RoleWAN, Proto: ProtoStatic, IPAddr: "1.1.1.2", DNS: []string{"not-an-ip"}}); err == nil {
		t.Error("want error for invalid dns")
	}
	// 重复（含 trim 后重复）
	if err := validateNetIface(&NetIface{Role: RoleWAN, Proto: ProtoStatic, IPAddr: "1.1.1.2", DNS: []string{"8.8.8.8", " 8.8.8.8 "}}); err == nil {
		t.Error("want error for duplicate dns")
	}
	// dns_metric 负数
	if err := validateNetIface(&NetIface{Role: RoleWAN, Proto: ProtoStatic, IPAddr: "1.1.1.2", DNSMetric: -1}); err == nil {
		t.Error("want error for negative dns_metric")
	}
}

func TestStoreMigratesLegacyIfaceDNS(t *testing.T) {
	// 旧 netcfg.json：接口 DNS 存成 dns_primary/dns_secondary 两个标量，加载后应折叠进 DNS[]。
	dir := t.TempDir()
	path := filepath.Join(dir, "netcfg.json")
	raw := `{"net_ifaces":[{"id":"lan","role":"lan","proto":"static","ipaddr":"192.168.1.1",` +
		`"dns_primary":"223.5.5.5","dns_secondary":"114.114.114.114"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	be, err := newStoreBackend(path, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	ifaces, _ := be.NetIfaces()
	if len(ifaces) != 1 {
		t.Fatalf("ifaces = %+v", ifaces)
	}
	got := ifaces[0]
	if len(got.DNS) != 2 || got.DNS[0] != "223.5.5.5" || got.DNS[1] != "114.114.114.114" {
		t.Errorf("migrated dns = %v", got.DNS)
	}
	if got.DNSPrimaryLegacy != "" || got.DNSSecondaryLegacy != "" {
		t.Errorf("legacy fields should be cleared, got %q/%q", got.DNSPrimaryLegacy, got.DNSSecondaryLegacy)
	}
}
