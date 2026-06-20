package netutil

import "testing"

func TestVendor(t *testing.T) {
	cases := []struct {
		mac  string
		want string
	}{
		{"00:0c:29:11:22:33", "VMware"},     // 测试机就是 VMware 虚机
		{"00:0C:29:AA:BB:CC", "VMware"},     // 大小写不敏感
		{"52:54:00:12:34:56", "QEMU/KVM"},   // 虚拟化
		{"08:00:27:00:00:01", "VirtualBox"}, // 虚拟化
		{"b8:27:eb:00:00:01", "Raspberry Pi"},
		{"3c:07:54:00:00:01", "Apple"},
		{"ff:ff:ff:00:00:00", ""}, // 未知前缀
		{"", ""},                  // 空
		{"not-a-mac", ""},         // 非法
	}
	for _, c := range cases {
		if got := Vendor(c.mac); got != c.want {
			t.Errorf("Vendor(%q)=%q, want %q", c.mac, got, c.want)
		}
	}
}

func TestVendorTableLoaded(t *testing.T) {
	// 触发加载并抽样确认表非空、虚拟化条目齐全。
	if Vendor("00:0c:29:00:00:00") == "" {
		t.Fatal("OUI 表未加载或 VMware 缺失")
	}
	if len(ouiTable) < 80 {
		t.Errorf("OUI 表条目数 %d 偏少，疑似解析异常", len(ouiTable))
	}
}
