package speedtest

// 二进制直装 —— 包管理器安装 speedtest-go 全失败后的最后一级回退。
//
// 为什么需要：精简/自编译固件的软件源里常常根本没有 speedtest-go 这个包，此时
// 「默认源 → 国内镜像 → 官方源」三级回退全都是白费；而 speedtest-go 官方就发布
// 静态编译的单文件二进制，按 CPU 架构拉下来丢进 /usr/bin 即可用。
//
// 下载走 Go 自己的 http（不依赖机器上的 curl/wget/uclient-fetch —— busybox wget
// 在不少固件上没有可用的 TLS 证书链），并复用本项目其它模块同款的 GitHub 公共代理
// 兜底，直连失败时逐个换代理重试。

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// 内置的 speedtest-go 版本（资产名形如 speedtest-go_1.8.3_Linux_arm64.tar.gz）。
	// 升级只需改这一行。
	binVersion = "1.8.3"
	binName    = "speedtest-go"
	// 单个镜像的下载超时；三个镜像串起来仍远小于前端 180s 的请求超时。
	binHTTPTimeout = 45 * time.Second
	maxBinBytes    = 32 << 20 // 解包时的单文件上限，防坏包撑爆内存/磁盘
)

// 可被测试替换的接缝。
var (
	binDownloadBase = "https://github.com/showwin/speedtest-go/releases/download"
	binInstallPath  = "/usr/bin/" + binName
	// 直连优先，失败再走公共代理（与 selfupdate / kwrtmgrd-fetch 同一批）。
	binMirrors = []string{"", "https://gh-proxy.com/", "https://ghfast.top/"}
	// 二进制直装只对 Linux（OpenWrt）有意义：在 Windows/macOS 开发机上拉一个 Linux
	// 二进制既跑不了，"/usr/bin/..." 还会落到盘符根目录去。
	binInstallEnabled = runtime.GOOS == "linux"
)

// assetArch 把 `uname -m` 的输出映射成 speedtest-go 发布资产里的架构名。
// 返回 "" 表示该架构官方没有发布二进制。mips 系按软浮点取（路由器 SoC 多无 FPU，
// 软浮点二进制在硬浮点机器上同样能跑，反过来会 SIGILL）。
func assetArch(unameM string) string {
	switch strings.ToLower(strings.TrimSpace(unameM)) {
	case "aarch64", "arm64", "armv8l":
		return "arm64"
	case "x86_64", "amd64":
		return "x86_64"
	case "i386", "i486", "i586", "i686", "x86":
		return "i386"
	case "armv7l", "armv7":
		return "armv7"
	case "armv6l", "armv6":
		return "armv6"
	case "armv5l", "armv5tel", "armv5":
		return "armv5"
	case "mips":
		return "mips_softfloat"
	case "mipsel", "mipsle":
		return "mipsle_softfloat"
	case "mips64":
		return "mips64_hardfloat"
	case "mips64el", "mips64le":
		return "mips64le_hardfloat"
	case "riscv64":
		return "riscv64"
	case "loongarch64", "loong64":
		return "loong64"
	}
	return ""
}

// installBinary 按本机架构拉 speedtest-go 静态二进制装到 /usr/bin。
// 返回过程日志（成功/失败都带，供 UI 展示）。
func (s *Service) installBinary() (string, error) {
	unameM, _ := s.run.Run("", "uname", "-m")
	arch := assetArch(unameM)
	if arch == "" {
		return "", fmt.Errorf("speedtest-go 官方未发布该 CPU 架构（uname -m = %q）的二进制", strings.TrimSpace(unameM))
	}
	asset := fmt.Sprintf("%s_%s_Linux_%s.tar.gz", binName, binVersion, arch)
	url := fmt.Sprintf("%s/v%s/%s", binDownloadBase, binVersion, asset)

	var logs strings.Builder
	fmt.Fprintf(&logs, "按架构 %s 直接下载 %s v%s 二进制\n", arch, binName, binVersion)
	for _, m := range binMirrors {
		src := m + url
		if m == "" {
			src = url
		}
		fmt.Fprintf(&logs, "\n--- 下载 %s ---\n", src)
		err := downloadBinaryTo(src, binInstallPath)
		if err == nil {
			logs.WriteString("安装成功 -> " + binInstallPath + "\n")
			if out, err := s.run.Run("", binInstallPath, "--version"); err == nil {
				logs.WriteString(strings.TrimSpace(out) + "\n")
			}
			return logs.String(), nil
		}
		logs.WriteString(err.Error() + "\n")
	}
	return logs.String(), errors.New("直连与全部公共代理均下载失败：请检查路由器能否访问外网")
}

// downloadBinaryTo 下载 tar.gz、取出其中的 speedtest-go 可执行文件、原子替换到 dst。
func downloadBinaryTo(url, dst string) error {
	client := &http.Client{Timeout: binHTTPTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("解压失败（不是 gzip 包，多半是代理返回了错误页）：%v", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("包内没有 " + binName + " 可执行文件")
		}
		if err != nil {
			return fmt.Errorf("解包失败：%v", err)
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != binName {
			continue
		}
		return writeExecutable(dst, io.LimitReader(tr, maxBinBytes))
	}
}

// writeExecutable 把内容写成可执行文件：先写同目录临时文件再 rename，避免把
// 正在运行/半截的文件留在 /usr/bin。
func writeExecutable(dst string, src io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建目录失败：%v", err)
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("写入失败：%v", err)
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("写入失败：%v", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("写入失败：%v", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("赋可执行权限失败：%v", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("安装到 %s 失败：%v", dst, err)
	}
	return nil
}
