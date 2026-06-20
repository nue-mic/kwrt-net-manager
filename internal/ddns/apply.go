package ddns

import (
	"fmt"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/internal/pkgmgr"
)

// SvcInfo 报告 DDNS 组件状态，供前端决定是否提示「一键安装」。
type SvcInfo struct {
	Installed  bool     `json:"installed"`
	CanInstall bool     `json:"can_install"`
	PkgManager string   `json:"pkg_manager"`
	Providers  []string `json:"providers"` // 本机支持的服务商（下拉源）
}

// 内置服务商兜底（未装 ddns-scripts 时给前端一个可选清单；装后以本机实际为准）。
var fallbackProviders = []string{
	"cloudflare.com", "dnspod.cn", "aliyun.com", "duckdns.org", "no-ip.com",
	"dyndns.org", "freedns.afraid.org", "godaddy.com", "namecheap.com", "he.net",
}

// ServiceInfo 探测。
func (s *Service) ServiceInfo() SvcInfo {
	pm := pkgmgr.PkgManager(runAdapter{s.run})
	installed := pkgmgr.Installed(runAdapter{s.run}, "ddns")
	providers := providerFiles()
	if len(providers) == 0 {
		providers = fallbackProviders
	}
	return SvcInfo{Installed: installed, CanInstall: pm != "", PkgManager: pm, Providers: providers}
}

// Install 一键安装 ddns-scripts 及常用服务商扩展（自愈回退源）。
// 2.8.x 把服务商拆成独立包：services 是通用定义集合，再带上 cloudflare/国内常用三家。
func (s *Service) Install() (string, error) {
	return pkgmgr.Install(runAdapter{s.run},
		"ddns-scripts ddns-scripts-services ddns-scripts-cloudflare ddns-scripts-dnspod ddns-scripts-aliyun ddns-scripts-noip")
}

// runAdapter 把本包 Runner 适配成 pkgmgr.Runner（签名相同）。
type runAdapter struct{ r Runner }

func (a runAdapter) Run(stdin, name string, args ...string) (string, error) {
	return a.r.Run(stdin, name, args...)
}

// apply 把旁车条目投射进 /etc/config/ddns（仅本工具 marker 节）并重启 ddns。
func (s *Service) apply(items []Entry) error {
	if !pkgmgr.Installed(runAdapter{s.run}, "ddns") {
		return nil // 未装 ddns-scripts：仅存旁车，装好后下次保存即生效
	}
	var b strings.Builder
	keep := map[string]bool{}
	anyEnabled := false
	for _, e := range items {
		id := e.ID
		keep[id] = true
		useV6 := "0"
		if e.RecordType == "AAAA" {
			useV6 = "1"
		}
		svcName := e.Provider
		if !strings.HasSuffix(svcName, "-v4") && !strings.HasSuffix(svcName, "-v6") {
			if useV6 == "1" {
				svcName += "-v6"
			} else {
				svcName += "-v4"
			}
		}
		iface := e.Interface
		if iface == "" {
			if e.IPSource == "device" {
				iface = "lan" // device 触发接口在 LAN 侧
			} else {
				iface = "wan"
			}
		}
		set := func(k, v string) { fmt.Fprintf(&b, "set ddns.%s.%s='%s'\n", id, k, uciEsc(v)) }
		fmt.Fprintf(&b, "set ddns.%s=service\n", id)
		set(markerOpt, markerDDNS)
		if e.Enabled {
			set("enabled", "1")
			anyEnabled = true
		} else {
			set("enabled", "0")
		}
		set("service_name", svcName)
		set("lookup_host", e.Domain)
		set("domain", e.Domain)
		if e.Username != "" {
			set("username", e.Username)
		} else {
			fmt.Fprintf(&b, "delete ddns.%s.username\n", id)
		}
		set("password", e.Password)
		set("use_ipv6", useV6)
		checkInterval := "10"
		switch e.IPSource {
		case "device":
			// device：ddns-scripts 无 MAC 解析能力 → 用 ip_source='script' 读 kwrtmgrd
			// 解析并缓存的 GUA（ip_script=生成脚本，内容仅 cat 缓存文件）。
			set("ip_source", "script")
			set("ip_script", s.deviceScriptPath(id))
			fmt.Fprintf(&b, "delete ddns.%s.ip_network\n", id)
			_ = s.ensureDeviceScript(id) // 生成可执行脚本
			if e.Enabled {
				_, _, _ = s.refreshDevice(e) // 立即解析一次填充缓存（best-effort）
			}
			checkInterval = "2" // device IP 变化要更快被 ddns-scripts 拾取
		case "network":
			set("ip_source", "network")
			set("ip_network", iface)
		default:
			set("ip_source", e.IPSource)
			fmt.Fprintf(&b, "delete ddns.%s.ip_network\n", id)
		}
		set("interface", iface)
		// 通用稳健项：走 HTTPS + 系统 CA、定时检查/强制刷新。
		set("use_https", "1")
		set("cacert", "/etc/ssl/certs")
		set("check_interval", checkInterval)
		set("check_unit", "minutes")
		set("force_interval", "72")
		set("force_unit", "hours")
	}
	// GC：删掉本工具 marker 下、已不在 keep 的节。
	for _, name := range s.managedNames() {
		if !keep[name] {
			fmt.Fprintf(&b, "delete ddns.%s\n", name)
		}
	}
	b.WriteString("commit ddns\n")
	if out, err := s.run.Run(b.String(), "uci", "batch"); err != nil {
		return fmt.Errorf("uci batch ddns: %v (%s)", err, strings.TrimSpace(out))
	}
	// 生效：enable + 重启 ddns 守护。restart 会同步派生各服务的更新脚本（联网更新可能耗时几秒），
	// 故后台执行，避免阻塞保存请求；结果可在「动态域名日志」查看。
	_, _ = s.run.Run("", "/etc/init.d/ddns", "enable")
	if anyEnabled {
		_, _ = s.run.Run("", "sh", "-c", "/etc/init.d/ddns restart >/dev/null 2>&1 &")
	} else {
		_, _ = s.run.Run("", "sh", "-c", "/etc/init.d/ddns stop >/dev/null 2>&1 &")
	}
	return nil
}

// managedNames 列出 ddns 配置里带本工具 marker 的具名节。
func (s *Service) managedNames() []string {
	show, err := s.run.Run("", "uci", "show", "ddns")
	if err != nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	suffix := "." + markerOpt + "='" + markerDDNS + "'"
	for _, line := range strings.Split(show, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ddns.") || !strings.HasSuffix(line, suffix) {
			continue
		}
		rest := strings.TrimPrefix(line, "ddns.")
		if i := strings.IndexByte(rest, '.'); i > 0 {
			name := rest[:i]
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// uciEsc 转义单引号，防止 uci batch 注入/断裂。
func uciEsc(v string) string { return strings.ReplaceAll(v, "'", "'\\''") }
