package netcfg

import (
	"encoding/json"
	"errors"
	"fmt"
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

// 要安装的 DoH 组件包名。
const dohPkgs = "https-dns-proxy luci-app-https-dns-proxy"

// InstallDoH 一键安装 https-dns-proxy。先用机器现有源；若失败（最常见是默认镜像不可达/SSL 坏，
// 与本包无关），按 /etc/os-release 推导「国内镜像(USTC) → 官方源」依次写一次性临时源重试，装完即删，
// 绝不改用户 distfeeds。国内优先：官方 downloads.* 在国内常极慢甚至超时（实测 15s 仅拉到 32KB），
// USTC 实测 <1s —— 分组逐个尝试，任一成功即止，最大化「不同环境都能装上」。
func (b *uciBackend) InstallDoH() (string, error) {
	pm := b.pkgManager()
	if pm != "opkg" && pm != "apk" {
		return "", errors.New("未检测到 opkg/apk 包管理器，无法自动安装 DoH 组件")
	}
	// 0) 先用机器现有源。
	out, err := b.runPkgInstall(pm, "")
	if err == nil {
		return out, nil
	}
	logs := strings.TrimSpace(out)
	// 1) 现有源失败 → 依次回退到国内/官方镜像，任一成功即返回。
	groups := b.fallbackMirrorGroups(pm)
	if len(groups) == 0 {
		return logs, errors.New(dohInstallHint(out) + "（系统版本/架构未知，无法推导回退源）")
	}
	for _, g := range groups {
		out2, err2 := b.runPkgInstall(pm, g.feed)
		logs += "\n\n=== 默认源失败，已回退「" + g.name + "」重试 ===\n" + strings.TrimSpace(out2)
		if err2 == nil {
			return logs, nil
		}
	}
	return logs, errors.New("默认源与全部回退镜像均安装失败：" + dohInstallHint(logs))
}

// runPkgInstall 安装 DoH 组件。feed 非空则先写为一次性临时源（装完即删），不动用户既有 distfeeds。
func (b *uciBackend) runPkgInstall(pm, feed string) (string, error) {
	updateInstall := "opkg update; opkg install " + dohPkgs
	path := "/etc/opkg/zzz-kwrt-doh.conf"
	if pm == "apk" {
		updateInstall = "apk update; apk add " + dohPkgs
		path = "/etc/apk/repositories.d/zzz-kwrt-doh.list"
	}
	if feed == "" {
		return b.run.Run("", "sh", "-c", updateInstall)
	}
	cmd := "cat > " + path + " <<'KWRTFEED'\n" + feed + "KWRTFEED\n" +
		updateInstall + "; rc=$?; rm -f " + path + "; exit $rc"
	return b.run.Run("", "sh", "-c", cmd)
}

// fallbackMirror 一个回退镜像组：name 展示名，feed 已渲染好的临时源内容。
type fallbackMirror struct{ name, feed string }

// fallbackMirrorGroups 据 /etc/os-release 推导回退镜像组，顺序＝国内(USTC) 优先、官方兜底。
// 镜像分发的是原始签名包，能通过 opkg check_signature。pm 取 "opkg" | "apk"。
func (b *uciBackend) fallbackMirrorGroups(pm string) []fallbackMirror {
	osr, _ := b.run.Run("", "cat", "/etc/os-release")
	id := osReleaseField(osr, "ID")
	ver := osReleaseField(osr, "VERSION_ID")
	arch := osReleaseField(osr, "OPENWRT_ARCH")
	if ver == "" || arch == "" || strings.EqualFold(ver, "snapshot") {
		return nil // 仅支持正式版本；arch/版本缺失则放弃回退
	}
	distro := "openwrt"
	if id == "immortalwrt" {
		distro = "immortalwrt"
	}
	defs := []struct{ tag, name, root string }{
		{"ustc", "国内镜像 USTC", fmt.Sprintf("https://mirrors.ustc.edu.cn/%s/releases/%s/packages/%s", distro, ver, arch)},
		{"off", "官方源", fmt.Sprintf("https://downloads.%s.org/releases/%s/packages/%s", distro, ver, arch)},
	}
	out := make([]fallbackMirror, 0, len(defs))
	for _, d := range defs {
		var sb strings.Builder
		for _, feed := range []string{"base", "packages", "luci"} {
			if pm == "apk" {
				fmt.Fprintf(&sb, "%s/%s\n", d.root, feed)
			} else {
				fmt.Fprintf(&sb, "src/gz kwrt_%s_%s %s/%s\n", d.tag, feed, d.root, feed)
			}
		}
		out = append(out, fallbackMirror{name: d.name, feed: sb.String()})
	}
	return out
}

// osReleaseField 解析 /etc/os-release 的 KEY=value（去引号）。
func osReleaseField(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") {
			return strings.Trim(strings.TrimPrefix(line, key+"="), "\"'")
		}
	}
	return ""
}

// dohInstallHint 把 opkg/apk 输出归纳为一句可读原因。
func dohInstallHint(out string) string {
	lo := strings.ToLower(out)
	switch {
	case strings.Contains(lo, "could not lock") || strings.Contains(lo, "opkg.lock") || strings.Contains(lo, "resource temporarily unavailable"):
		return "另一个软件安装/更新正在进行（opkg 被占用），请稍候再点一次"
	case strings.Contains(lo, "ssl error") || strings.Contains(lo, "certificate") || strings.Contains(lo, "handshake"):
		return "软件源 HTTPS 握手失败（多为缺 ca 证书或系统时间不对）：请更新 ca-bundle/ca-certificates 或校正系统时间后重试"
	case strings.Contains(lo, "failed to download") || strings.Contains(lo, "wget returned") || strings.Contains(lo, "could not") || strings.Contains(lo, "resolve"):
		return "无法连接软件源（网络/镜像不可达）：请确认路由器可访问 opkg 源后重试"
	case strings.Contains(lo, "unknown package") || strings.Contains(lo, "not found"):
		return "软件源里找不到 https-dns-proxy（包列表未更新成功，多为上面的网络/SSL 问题导致）"
	default:
		return "安装失败：" + lastNonEmptyLine(out)
	}
}

// lastNonEmptyLine 取输出里最后一条非空行（截断到 200 字符）。
func lastNonEmptyLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		s := strings.TrimSpace(lines[i])
		if s != "" {
			if len(s) > 200 {
				s = s[:200]
			}
			return s
		}
	}
	return "（无输出）"
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
