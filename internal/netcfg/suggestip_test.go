package netcfg

import (
	"strconv"
	"testing"

	"github.com/nue-mic/kwrt-net-manager/pkg/netutil"
)

func usedSet(ips ...string) map[uint32]bool {
	m := map[uint32]bool{}
	for _, ip := range ips {
		if u, ok := netutil.IPv4ToUint32(ip); ok {
			m[u] = true
		}
	}
	return m
}

func TestNextFreeIP(t *testing.T) {
	pool := DHCPServer{IPStart: "192.168.1.100", IPEnd: "192.168.1.110"}

	// .100/.101 已用、.102 空 → 应返回 .102。
	if got := nextFreeIP(pool, usedSet("192.168.1.100", "192.168.1.101")); got != "192.168.1.102" {
		t.Errorf("应返回首个空闲 .102，得到 %q", got)
	}
	// 起始处空闲 → 返回 .100。
	if got := nextFreeIP(pool, usedSet("192.168.1.105")); got != "192.168.1.100" {
		t.Errorf("起始空闲应返回 .100，得到 %q", got)
	}
	// 池满 → 返回 ""。
	all := []string{}
	for i := 100; i <= 110; i++ {
		all = append(all, "192.168.1."+strconv.Itoa(i))
	}
	if got := nextFreeIP(pool, usedSet(all...)); got != "" {
		t.Errorf("池满应返回空，得到 %q", got)
	}
	// 非法池 → 返回 ""。
	if got := nextFreeIP(DHCPServer{IPStart: "bad", IPEnd: "x"}, nil); got != "" {
		t.Errorf("非法池应返回空，得到 %q", got)
	}
	// start>end → 返回 ""。
	if got := nextFreeIP(DHCPServer{IPStart: "192.168.1.110", IPEnd: "192.168.1.100"}, nil); got != "" {
		t.Errorf("start>end 应返回空，得到 %q", got)
	}
}
