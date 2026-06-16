// Package pkgmgr 提供「一键安装 OpenWrt 软件包」的自愈安装：先用机器现有源，失败则按
// /etc/os-release 推导「国内镜像 USTC → 官方源」依次写一次性临时源重试，装完即删，绝不改
// 用户 distfeeds。供 DDNS / 线路测速等「需要时才装的可选组件」共用。
//
// 与 DoH 自愈安装同策略（国内优先：官方 downloads.* 在国内常极慢/超时，USTC 实测 <1s）。
package pkgmgr

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner 抽象命令执行（便于单测）。
type Runner interface {
	Run(stdin, name string, args ...string) (string, error)
}

// RealRunner 真正 shell 调用，stdout+stderr 合并；供 DDNS/测速等子系统复用。
type RealRunner struct{}

// Run 执行命令；stdin 非空时喂入。
func (RealRunner) Run(stdin, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// Install 安装 pkgs（空格分隔）。返回合并输出与最终错误。
func Install(run Runner, pkgs string) (string, error) {
	pm := pkgManager(run)
	if pm != "opkg" && pm != "apk" {
		return "", errors.New("未检测到 opkg/apk 包管理器，无法自动安装组件")
	}
	out, err := runInstall(run, pm, pkgs, "")
	if err == nil {
		return out, nil
	}
	logs := strings.TrimSpace(out)
	groups := fallbackGroups(run, pm)
	if len(groups) == 0 {
		return logs, errors.New(hint(out) + "（系统版本/架构未知，无法推导回退源）")
	}
	for _, g := range groups {
		out2, err2 := runInstall(run, pm, pkgs, g.feed)
		logs += "\n\n=== 默认源失败，已回退「" + g.name + "」重试 ===\n" + strings.TrimSpace(out2)
		if err2 == nil {
			return logs, nil
		}
	}
	return logs, errors.New("默认源与全部回退镜像均安装失败：" + hint(logs))
}

// Installed 报告 initd 服务脚本是否存在（即组件是否已装）。
func Installed(run Runner, initdName string) bool {
	out, err := run.Run("", "test", "-x", "/etc/init.d/"+initdName)
	return err == nil && strings.TrimSpace(out) == ""
}

// PkgManager 暴露探测结果（"opkg"|"apk"|""）。
func PkgManager(run Runner) string { return pkgManager(run) }

func pkgManager(run Runner) string {
	if _, err := run.Run("", "sh", "-c", "command -v apk"); err == nil {
		// 25.x 起为 apk；但 opkg 可能并存——优先 apk 仅当无 opkg。
		if _, e := run.Run("", "sh", "-c", "command -v opkg"); e != nil {
			return "apk"
		}
	}
	if _, err := run.Run("", "sh", "-c", "command -v opkg"); err == nil {
		return "opkg"
	}
	if _, err := run.Run("", "sh", "-c", "command -v apk"); err == nil {
		return "apk"
	}
	return ""
}

func runInstall(run Runner, pm, pkgs, feed string) (string, error) {
	updateInstall := "opkg update; opkg install " + pkgs
	path := "/etc/opkg/zzz-kwrt-pkg.conf"
	if pm == "apk" {
		updateInstall = "apk update; apk add " + pkgs
		path = "/etc/apk/repositories.d/zzz-kwrt-pkg.list"
	}
	if feed == "" {
		return run.Run("", "sh", "-c", updateInstall)
	}
	cmd := "cat > " + path + " <<'KWRTFEED'\n" + feed + "KWRTFEED\n" +
		updateInstall + "; rc=$?; rm -f " + path + "; exit $rc"
	return run.Run("", "sh", "-c", cmd)
}

type group struct{ name, feed string }

func fallbackGroups(run Runner, pm string) []group {
	osr, _ := run.Run("", "cat", "/etc/os-release")
	id := osField(osr, "ID")
	ver := osField(osr, "VERSION_ID")
	arch := osField(osr, "OPENWRT_ARCH")
	if ver == "" || arch == "" || strings.EqualFold(ver, "snapshot") {
		return nil
	}
	distro := "openwrt"
	if id == "immortalwrt" {
		distro = "immortalwrt"
	}
	defs := []struct{ tag, name, root string }{
		{"ustc", "国内镜像 USTC", fmt.Sprintf("https://mirrors.ustc.edu.cn/%s/releases/%s/packages/%s", distro, ver, arch)},
		{"off", "官方源", fmt.Sprintf("https://downloads.%s.org/releases/%s/packages/%s", distro, ver, arch)},
	}
	out := make([]group, 0, len(defs))
	for _, d := range defs {
		var sb strings.Builder
		for _, feed := range []string{"base", "packages", "luci"} {
			if pm == "apk" {
				fmt.Fprintf(&sb, "%s/%s\n", d.root, feed)
			} else {
				fmt.Fprintf(&sb, "src/gz kwrtpkg_%s_%s %s/%s\n", d.tag, feed, d.root, feed)
			}
		}
		out = append(out, group{name: d.name, feed: sb.String()})
	}
	return out
}

func osField(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") {
			return strings.Trim(strings.TrimPrefix(line, key+"="), "\"'")
		}
	}
	return ""
}

func hint(out string) string {
	lo := strings.ToLower(out)
	switch {
	case strings.Contains(lo, "could not lock") || strings.Contains(lo, "opkg.lock") || strings.Contains(lo, "resource temporarily unavailable"):
		return "另一个软件安装/更新正在进行（opkg 被占用），请稍候再点一次"
	case strings.Contains(lo, "ssl error") || strings.Contains(lo, "certificate") || strings.Contains(lo, "handshake"):
		return "软件源 HTTPS 握手失败（多为缺 ca 证书或系统时间不对）：请更新 ca-bundle/ca-certificates 或校正时间后重试"
	case strings.Contains(lo, "failed to download") || strings.Contains(lo, "wget returned") || strings.Contains(lo, "could not") || strings.Contains(lo, "resolve"):
		return "无法连接软件源（网络/镜像不可达）：请确认路由器可访问软件源后重试"
	case strings.Contains(lo, "unknown package") || strings.Contains(lo, "not found"):
		return "软件源里找不到该组件（包列表未更新成功，多为上面的网络/SSL 问题导致）"
	default:
		return "安装失败：" + lastLine(out)
	}
}

func lastLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			if len(s) > 200 {
				s = s[:200]
			}
			return s
		}
	}
	return "（无输出）"
}
