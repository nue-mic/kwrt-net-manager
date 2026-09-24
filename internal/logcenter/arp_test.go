package logcenter

import (
	"strings"
	"testing"
	"time"
)

// arpCenter 造一个只喂 `ip neigh` 的 Center；neigh 可在轮次之间替换。
type arpRunner struct{ neigh string }

func (r *arpRunner) Run(name string, args ...string) (string, error) {
	if name == "ip" {
		return r.neigh, nil
	}
	return "", nil
}

func arpCenter(t *testing.T) (*Center, *arpRunner) {
	t.Helper()
	r := &arpRunner{}
	c := New(t.TempDir(), nil)
	c.run = r
	c.loc = time.FixedZone("local", 8*3600)
	return c, r
}

func arpEntries(t *testing.T, c *Center) []Entry {
	t.Helper()
	res, err := c.Query(SourceARP, Filter{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	return res.Items
}

func countType(items []Entry, typ string) int {
	n := 0
	for _, e := range items {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func TestARPIgnoresIPv6AndFailed(t *testing.T) {
	// ARP 只存在于 IPv4：fe80:: 是 NDP，记进 ARP 日志纯属噪声。
	// FAILED/INCOMPLETE 的 lladdr 是上一次的残留，拿它比对会产生假变化。
	c, r := arpCenter(t)
	r.neigh = `fe80::592c:9135:64d7:bc4b dev eth0 lladdr 42:e9:71:49:44:29 STALE
192.168.0.83 dev eth0 lladdr ce:13:e6:e1:46:94 STALE
192.168.0.90 dev eth0 lladdr aa:bb:cc:dd:ee:01 FAILED`
	c.scanARP(true) // 基线

	r.neigh = `fe80::592c:9135:64d7:bc4b dev eth0 lladdr 76:6b:8e:ca:14:f5 STALE
192.168.0.83 dev eth0 lladdr ce:13:e6:e1:46:94 STALE
192.168.0.90 dev eth0 lladdr aa:bb:cc:dd:ee:02 FAILED`
	c.scanARP(false)

	if items := arpEntries(t, c); len(items) != 0 {
		t.Errorf("IPv6 与 FAILED 条目不应产生日志，实际 %d 条: %+v", len(items), items)
	}
}

func TestARPChangeIsInfoNotSpoof(t *testing.T) {
	// 同一 IP 换 MAC：漫游/换设备/随机 MAC 都会如此，只能算信息级「地址变化」，
	// 叫「疑似ARP欺骗」会把用户吓一跳（实测一台家用路由 16 天刷出 170+ 条）。
	c, r := arpCenter(t)
	r.neigh = "192.168.0.86 dev eth0 lladdr 76:6b:8e:ca:14:f5 REACHABLE"
	c.scanARP(true)
	r.neigh = "192.168.0.86 dev eth0 lladdr 42:e9:71:49:44:29 REACHABLE"
	c.scanARP(false)

	items := arpEntries(t, c)
	if len(items) != 1 || items[0].Type != arpTypeChanged {
		t.Fatalf("应记 1 条「%s」，实际 %+v", arpTypeChanged, items)
	}
	if !strings.Contains(items[0].Message, "76:6b:8e:ca:14:f5 -> 42:e9:71:49:44:29") {
		t.Errorf("消息应含新旧 MAC: %q", items[0].Message)
	}
}

func TestARPDedupWithinWindow(t *testing.T) {
	// 邻居表在 REACHABLE/STALE/DELAY 间抖动时同一条变化会被反复看见 —— 窗口内只记一次。
	c, r := arpCenter(t)
	r.neigh = "192.168.0.86 dev eth0 lladdr 76:6b:8e:ca:14:f5 REACHABLE"
	c.scanARP(true)
	for i := 0; i < 5; i++ { // 来回横跳 5 轮
		r.neigh = "192.168.0.86 dev eth0 lladdr 42:e9:71:49:44:29 REACHABLE"
		c.scanARP(false)
		r.neigh = "192.168.0.86 dev eth0 lladdr 76:6b:8e:ca:14:f5 REACHABLE"
		c.scanARP(false)
	}
	// 两个方向各算一种事件，窗口内各只记一次 → 共 2 条，而不是 10 条
	if n := countType(arpEntries(t, c), arpTypeChanged); n != 2 {
		t.Errorf("窗口内应只剩 2 条地址变化，实际 %d 条", n)
	}
}

func TestARPMultiIPIsSpoof(t *testing.T) {
	// 一个 MAC 同时占多个 IPv4 才是 ARP 欺骗的典型特征 —— 原实现完全没检测这个。
	c, r := arpCenter(t)
	r.neigh = `192.168.0.221 dev eth0 lladdr 42:e9:71:49:44:29 REACHABLE
192.168.0.3 dev eth0 lladdr 42:e9:71:49:44:29 STALE
192.168.0.86 dev eth0 lladdr 76:6b:8e:ca:14:f5 STALE`
	c.scanARP(true)  // 基线轮不记
	c.scanARP(false) // 第二轮应报多 IP

	items := arpEntries(t, c)
	if n := countType(items, arpTypeMultiIP); n != 1 {
		t.Fatalf("应报 1 条「%s」，实际 %+v", arpTypeMultiIP, items)
	}
	for _, e := range items {
		if e.Type == arpTypeMultiIP {
			if e.MAC != "42:e9:71:49:44:29" || !strings.Contains(e.Message, "192.168.0.221") {
				t.Errorf("多 IP 告警内容不对: %+v", e)
			}
		}
	}
	// 窗口内重复扫描不再刷屏
	c.scanARP(false)
	if n := countType(arpEntries(t, c), arpTypeMultiIP); n != 1 {
		t.Errorf("窗口内多 IP 告警应只记一次，实际 %d 条", n)
	}
}

func TestARPStaticBindingConflict(t *testing.T) {
	// 实际在线 MAC 与面板静态分配绑定不符 → IP 被占用/冒用，值得告警。
	c, r := arpCenter(t)
	c.SetStaticBindings(func() map[string]string {
		return map[string]string{"192.168.0.221": "34:5a:60:3c:27:08"}
	})
	r.neigh = "192.168.0.221 dev eth0 lladdr 42:e9:71:49:44:29 REACHABLE"
	c.scanARP(true)
	c.scanARP(false)

	items := arpEntries(t, c)
	if n := countType(items, arpTypeConflict); n != 1 {
		t.Fatalf("应报 1 条「%s」，实际 %+v", arpTypeConflict, items)
	}
}

func TestARPNoConflictWhenBindingMatches(t *testing.T) {
	c, r := arpCenter(t)
	c.SetStaticBindings(func() map[string]string {
		return map[string]string{"192.168.0.221": "34:5a:60:3c:27:08"}
	})
	r.neigh = "192.168.0.221 dev eth0 lladdr 34:5a:60:3c:27:08 REACHABLE"
	c.scanARP(true)
	c.scanARP(false)

	if items := arpEntries(t, c); len(items) != 0 {
		t.Errorf("绑定一致时不应有任何告警: %+v", items)
	}
}
