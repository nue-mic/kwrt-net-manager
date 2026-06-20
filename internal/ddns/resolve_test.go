package ddns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 目标终端 MAC 00:0c:29:11:22:33 的 EUI-64 SLAAC IID = 020c:29ff:fe11:2233。
const (
	devMAC      = "00:0c:29:11:22:33"
	euiGUA      = "2408:8240:1:2:20c:29ff:fe11:2233" // EUI-64 稳定 SLAAC（可由 MAC 校验）
	tempGUA     = "2408:8240:1:2:dead:beef:cafe:1"   // 隐私/临时地址（非 EUI-64）
	dhcpGUA     = "2408:8240:1:2::abcd"              // DHCPv6 分配（最稳定）
	otherMACGUA = "2408:8240:1:2:20c:29ff:feaa:bbcc" // 另一台设备
)

func neighShow() string {
	return strings.Join([]string{
		euiGUA + " dev br-lan lladdr " + devMAC + " router REACHABLE",
		"fe80::20c:29ff:fe11:2233 dev br-lan lladdr " + devMAC + " STALE", // 链路本地，应排除
		tempGUA + " dev br-lan lladdr " + devMAC + " STALE",
		"fd00::1234 dev br-lan lladdr " + devMAC + " STALE", // ULA，应排除
		otherMACGUA + " dev br-lan lladdr 00:0c:29:aa:bb:cc STALE",
		"2408:8240:1:2:: dev br-lan  FAILED", // 无 lladdr，应跳过
	}, "\n")
}

// DUID-LL 0003 0001 + 000c29112233 → MAC 00:0c:29:11:22:33
func leases6JSON() string {
	return `{"device":{"br-lan":{"leases":[` +
		`{"duid":"00030001000c29112233","iaid":1,"hostname":"nas","valid":3600,"ipv6-addr":[{"address":"` + dhcpGUA + `"}]}` +
		`]}}}`
}

func TestPickStableGUA_PrefersEUI64OverTemporary(t *testing.T) {
	cands := parseNeighborCands(neighShow())
	ip, src := pickStableGUA(devMAC, cands)
	if ip != "2408:8240:1:2:20c:29ff:fe11:2233" {
		t.Fatalf("应选 EUI-64 稳定地址，得到 %q (src=%s)", ip, src)
	}
	if src != "slaac" {
		t.Errorf("source 应为 slaac，得到 %q", src)
	}
}

func TestPickStableGUA_DHCPv6Wins(t *testing.T) {
	cands := append(parseLeaseCandsV6(leases6JSON()), parseNeighborCands(neighShow())...)
	ip, src := pickStableGUA(devMAC, cands)
	if ip != "2408:8240:1:2::abcd" {
		t.Fatalf("DHCPv6 地址应最优先，得到 %q (src=%s)", ip, src)
	}
	if src != "dhcpv6" {
		t.Errorf("source 应为 dhcpv6，得到 %q", src)
	}
}

func TestPickStableGUA_ExcludesNonGlobal(t *testing.T) {
	// 只喂链路本地 + ULA，应解析不到。
	cands := parseNeighborCands(strings.Join([]string{
		"fe80::20c:29ff:fe11:2233 dev br-lan lladdr " + devMAC + " STALE",
		"fd00::1234 dev br-lan lladdr " + devMAC + " STALE",
	}, "\n"))
	if ip, _ := pickStableGUA(devMAC, cands); ip != "" {
		t.Errorf("链路本地/ULA 不应被选中，得到 %q", ip)
	}
}

func TestResolveDeviceGUA(t *testing.T) {
	f := &fakeRunner{neigh: neighShow(), leases6: leases6JSON()}
	ip, src, err := resolveDeviceGUA(f, devMAC)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "2408:8240:1:2::abcd" || src != "dhcpv6" {
		t.Fatalf("解析结果 ip=%q src=%q，期望 DHCPv6 地址", ip, src)
	}
}

func TestResolveDeviceGUA_NotFound(t *testing.T) {
	f := &fakeRunner{neigh: neighShow(), leases6: leases6JSON()}
	if _, _, err := resolveDeviceGUA(f, "aa:aa:aa:aa:aa:aa"); err == nil {
		t.Error("未知 MAC 应返回错误")
	}
}

func TestListDevices(t *testing.T) {
	f := &fakeRunner{neigh: neighShow(), leases6: leases6JSON()}
	devs := listDevices(f, filepath.Join(t.TempDir(), "nonexistent.leases"))
	var got *Device
	for i := range devs {
		if strings.EqualFold(devs[i].MAC, devMAC) {
			got = &devs[i]
		}
	}
	if got == nil {
		t.Fatalf("目标设备未出现在候选列表：%+v", devs)
	}
	if got.IPv6 != "2408:8240:1:2::abcd" {
		t.Errorf("候选设备 GUA=%q，期望 DHCPv6 地址", got.IPv6)
	}
}

func TestRefreshDeviceWritesCache(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{neigh: neighShow(), leases6: leases6JSON()}
	svc := New(f, filepath.Join(dir, "ddns.json"), func() string { return "ddns_dev1" })
	svc.scriptDir = dir
	e := Entry{ID: "ddns_dev1", IPSource: "device", MAC: devMAC, RecordType: "AAAA", Enabled: true}
	ip, changed, err := svc.refreshDevice(e)
	if err != nil || !changed || ip != "2408:8240:1:2::abcd" {
		t.Fatalf("refreshDevice ip=%q changed=%v err=%v", ip, changed, err)
	}
	b, err := os.ReadFile(svc.deviceIPPath("ddns_dev1"))
	if err != nil || strings.TrimSpace(string(b)) != "2408:8240:1:2::abcd" {
		t.Fatalf("缓存文件内容=%q err=%v", string(b), err)
	}
	// 二次刷新：无变化。
	if _, changed, _ := svc.refreshDevice(e); changed {
		t.Error("IP 未变时不应报告 changed")
	}
}

func TestApplyDeviceProjection(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{neigh: neighShow(), leases6: leases6JSON()}
	svc := New(f, filepath.Join(dir, "ddns.json"), func() string { return "ddns_devp" })
	svc.scriptDir = dir
	if _, err := svc.Create(Entry{
		Provider: "cloudflare.com", Domain: "nas.example.com", AuthMode: "token",
		Password: "secret", IPSource: "device", MAC: devMAC, RecordType: "AAAA",
	}); err != nil {
		t.Fatal(err)
	}
	b := f.uciBatch
	wants := []string{
		"ip_source='script'",
		"ip_script='" + svc.deviceScriptPath("ddns_devp") + "'",
		"service_name='cloudflare.com-v6'",
		"use_ipv6='1'",
		"check_interval='2'",
	}
	for _, w := range wants {
		if !strings.Contains(b, w) {
			t.Errorf("device 投射缺少 %q\n--- batch ---\n%s", w, b)
		}
	}
	if strings.Contains(b, "ip_source='device'") {
		t.Error("device 不应直接写成 ip_source='device'（ddns-scripts 不认）")
	}
	// 生成的脚本与缓存文件应存在。
	if _, err := os.Stat(svc.deviceScriptPath("ddns_devp")); err != nil {
		t.Errorf("ip_script 脚本未生成：%v", err)
	}
	if c, err := os.ReadFile(svc.deviceIPPath("ddns_devp")); err != nil || strings.TrimSpace(string(c)) != "2408:8240:1:2::abcd" {
		t.Errorf("缓存 IP 文件内容=%q err=%v", string(c), err)
	}
}

func TestValidateDeviceSource(t *testing.T) {
	good := Entry{Provider: "cloudflare.com", Domain: "a.b.com", Password: "t", IPSource: "device", MAC: devMAC, RecordType: "AAAA"}
	if err := validate(good); err != nil {
		t.Fatalf("合法 device 条目被拒：%v", err)
	}
	bad := []Entry{
		{Provider: "p", Domain: "a", Password: "t", IPSource: "device", MAC: "", RecordType: "AAAA"},   // 缺 MAC
		{Provider: "p", Domain: "a", Password: "t", IPSource: "device", MAC: "zz", RecordType: "AAAA"}, // 非法 MAC
		{Provider: "p", Domain: "a", Password: "t", IPSource: "device", MAC: devMAC, RecordType: "A"},  // device 必须 AAAA
	}
	for i, e := range bad {
		if err := validate(e); err == nil {
			t.Errorf("非法 device 条目 %d 应被拒", i)
		}
	}
}
