package speedtest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// archRunner 只回答 `uname -m`，其余命令返回空（二进制直装只需要架构）。
type archRunner struct{ m string }

func (a *archRunner) Run(_, name string, _ ...string) (string, error) {
	if name == "uname" {
		return a.m + "\n", nil
	}
	return "", nil
}

func TestAssetArch(t *testing.T) {
	cases := map[string]string{
		"aarch64":     "arm64", // 本项目实测机（ImmortalWrt aarch64_generic）
		"x86_64":      "x86_64",
		"armv7l":      "armv7",
		"mips":        "mips_softfloat", // 软浮点：硬浮点机器也能跑，反之会 SIGILL
		"mipsel":      "mipsle_softfloat",
		"mips64el":    "mips64le_hardfloat",
		"riscv64":     "riscv64",
		"  AArch64\n": "arm64", // 容忍大小写与换行
		"sparc64":     "",      // 官方没发布 → 明确报错而不是瞎拼 URL
	}
	for in, want := range cases {
		if got := assetArch(in); got != want {
			t.Errorf("assetArch(%q) = %q want %q", in, got, want)
		}
	}
}

// tarGz 打一个内含 speedtest-go 可执行文件的包，模拟官方发布资产。
func tarGz(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ n, c string }{{"README.md", "doc"}, {name, content}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.n, Mode: 0o755, Size: int64(len(f.c)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.c)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallBinaryFallsBackAcrossMirrors(t *testing.T) {
	pkg := tarGz(t, "speedtest-go", "#!/bin/sh\necho ok\n")
	// 第一个源（直连）返回 502 模拟被墙，第二个（代理）才成功 —— 必须自动换源。
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write(pkg)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "speedtest-go")
	oldBase, oldPath, oldMirrors := binDownloadBase, binInstallPath, binMirrors
	binDownloadBase, binInstallPath, binMirrors = srv.URL+"/dl", dst, []string{"", ""}
	defer func() { binDownloadBase, binInstallPath, binMirrors = oldBase, oldPath, oldMirrors }()

	svc := New(&archRunner{m: "aarch64"}, t.TempDir())
	logs, err := svc.installBinary()
	if err != nil {
		t.Fatalf("installBinary: %v\n%s", err, logs)
	}
	if hits != 2 {
		t.Errorf("应在首个源失败后换第二个源重试，实际请求 %d 次", hits)
	}
	b, err := os.ReadFile(dst)
	if err != nil || !strings.Contains(string(b), "echo ok") {
		t.Fatalf("二进制未落地: %v %q", err, string(b))
	}
	// 执行位在 Windows 上无法表达（chmod 只映射只读位），只在类 Unix 上校验。
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(dst); fi != nil && fi.Mode().Perm()&0o111 == 0 {
			t.Errorf("安装后应可执行，实际权限 %v", fi.Mode().Perm())
		}
	}
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Error("临时文件未清理")
	}
}

func TestInstallBinaryRejectsNonGzip(t *testing.T) {
	// 公共代理常在失败时返回 HTML 错误页（HTTP 200），不能把它当二进制装进 /usr/bin。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>404 not found</html>"))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "speedtest-go")
	oldBase, oldPath, oldMirrors := binDownloadBase, binInstallPath, binMirrors
	binDownloadBase, binInstallPath, binMirrors = srv.URL+"/dl", dst, []string{""}
	defer func() { binDownloadBase, binInstallPath, binMirrors = oldBase, oldPath, oldMirrors }()

	svc := New(&archRunner{m: "aarch64"}, t.TempDir())
	if _, err := svc.installBinary(); err == nil {
		t.Fatal("非 gzip 响应必须报错")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("失败时不应留下任何文件")
	}
}
