package netcfg

import (
	"encoding/json"
	"errors"
	"strings"
)

// DNS 领域的 uci 后端只读 / 探测 / 维护实现。

// DNSSettings 反射：未接管(Enabled=false)时回填当前 stock 现状，便于前端展示真实状态，
// 用户「保存」即以现状为基线接管，避免误清。已接管则返回旁车权威值。
func (b *uciBackend) DNSSettings() (DNSSettings, error) {
	st, err := b.storeBackend.DNSSettings()
	if err != nil {
		return st, err
	}
	if !st.Enabled {
		st.FilterAAAA = b.uciGetBool(dnsmasqSec + ".filter_aaaa")
		st.CacheSize = b.uciGetInt(dnsmasqSec + ".cachesize")
		st.LocalTTL = b.uciGetInt(dnsmasqSec + ".local_ttl")
		st.MinCacheTTL = b.uciGetInt(dnsmasqSec + ".min_cache_ttl")
		st.MaxCacheTTL = b.uciGetInt(dnsmasqSec + ".max_cache_ttl")
		st.NoResolv = b.uciGetBool(dnsmasqSec + ".noresolv")
		var ups []string
		for _, s := range b.uciGetList(dnsmasqSec + ".server") {
			if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "127.0.0.1") {
				continue // 跳过域名分流 / DoH 本地代理条目
			}
			ups = append(ups, s)
		}
		if len(ups) > 0 {
			st.DNSPrimary = ups[0]
		}
		if len(ups) > 1 {
			st.DNSSecondary = ups[1]
		}
	}
	return st, nil
}

// DNSCacheStats 读 dnsmasq 运行态统计。真机无 dig、busybox nslookup 不支持 CHAOS，
// 且 dnsmasq 跑在 ujail 里——改走 dnsmasq 的 ubus metrics（编译含 UBus），最稳。
// 命中≈本地/缓存应答(dns_local_answered+stale+auth)，未命中=转发上游(dns_queries_forwarded)。
func (b *uciBackend) DNSCacheStats() (DNSCacheStats, error) {
	out, err := b.run.Run("", "ubus", "call", "dnsmasq", "metrics")
	if err != nil || strings.TrimSpace(out) == "" {
		return DNSCacheStats{Supported: false}, nil
	}
	var m struct {
		Inserted  int64 `json:"dns_cache_inserted"`
		Freed     int64 `json:"dns_cache_live_freed"`
		Forwarded int64 `json:"dns_queries_forwarded"`
		Local     int64 `json:"dns_local_answered"`
		Auth      int64 `json:"dns_auth_answered"`
		Stale     int64 `json:"dns_stale_answered"`
	}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return DNSCacheStats{Supported: false}, nil
	}
	hits := m.Local + m.Stale + m.Auth
	misses := m.Forwarded
	st := DNSCacheStats{
		Supported:  true,
		CacheSize:  int64(b.uciGetInt(dnsmasqSec + ".cachesize")),
		Insertions: m.Inserted,
		Evictions:  m.Freed,
		Hits:       hits,
		Misses:     misses,
	}
	if hits+misses > 0 {
		st.HitRatio = float64(hits) / float64(hits+misses)
	}
	return st, nil
}

// FlushDNSCache 清空 dnsmasq 缓存（SIGHUP，轻量不中断）。
func (b *uciBackend) FlushDNSCache() error {
	_, err := b.run.Run("", "killall", "-HUP", "dnsmasq")
	return err
}

// DNSServiceInfo 探测 DNS 能力：filter-AAAA 是否支持、DoH 是否已装、包管理器。
func (b *uciBackend) DNSServiceInfo() (DNSSvcInfo, error) {
	pm := b.pkgManager()
	return DNSSvcInfo{
		Backend:             KindUCI,
		FilterAAAASupported: b.filterAAAASupported(),
		DoHInstalled:        initdExists("https-dns-proxy"),
		PkgManager:          pm,
		CanInstall:          pm != "",
	}, nil
}

// InstallDoH 一键安装 https-dns-proxy（按 opkg/apk 分支）。
func (b *uciBackend) InstallDoH() (string, error) {
	switch b.pkgManager() {
	case "apk":
		return b.run.Run("", "sh", "-c", "apk update; apk add https-dns-proxy luci-app-https-dns-proxy")
	case "opkg":
		return b.run.Run("", "sh", "-c", "opkg update; opkg install https-dns-proxy luci-app-https-dns-proxy")
	}
	return "", errors.New("未检测到 opkg/apk 包管理器，无法自动安装 DoH 组件")
}

// filterAAAASupported 探测本机 dnsmasq 是否支持 --filter-AAAA（精简构建不支持）。
func (b *uciBackend) filterAAAASupported() bool {
	out, _ := b.run.Run("", "dnsmasq", "--filter-AAAA", "--test")
	return !strings.Contains(strings.ToLower(out), "bad option") &&
		!strings.Contains(strings.ToLower(out), "bad command")
}

func (b *uciBackend) uciGetInt(key string) int   { v, _ := b.uciGet(key); return atoiSafe(v) }
func (b *uciBackend) uciGetBool(key string) bool { v, _ := b.uciGet(key); return v == "1" }
func (b *uciBackend) uciGetList(key string) []string {
	v, _ := b.uciGet(key)
	return strings.Fields(v)
}
