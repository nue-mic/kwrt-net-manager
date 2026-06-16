// Package speedtest 用 OpenWrt 原生包 speedtest-go 实现「线路测速」(仿爱快 应用工具>线路测速)。
//
// 第一准则——只迁移 OpenWrt 能原生实现的：
//   - speedtest-go 纯 Go 单二进制，走 speedtest.net 自动选最近服务器（国内有大量节点），输出
//     下载/上传 Mbps 与延迟。一键安装走 pkgmgr 自愈回退源。
//   - **降级**：爱快是自带测速程序+逐秒实时仪表盘；speedtest-go 给最终结果（下载/上传/延迟/
//     服务器），前端展示「测速中…」+最终结果，非逐秒指针；多线路指定出接口能力有限，先测默认线路。
//
// 异步：Run() 起后台测速，Status() 轮询。同一时刻只允许一个测速。
package speedtest

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mia-clark/kwrt-net-manager/internal/pkgmgr"
)

// Runner 抽象命令执行。
type Runner interface {
	Run(stdin, name string, args ...string) (string, error)
}

// Result 是一次测速结果。
type Result struct {
	DownloadMbps float64 `json:"download_mbps"`
	UploadMbps   float64 `json:"upload_mbps"`
	PingMs       float64 `json:"ping_ms"`
	Server       string  `json:"server"`
	ISP          string  `json:"isp"`
}

// Status 是测速运行态。
type Status struct {
	Running    bool    `json:"running"`
	Result     *Result `json:"result,omitempty"`
	Error      string  `json:"error,omitempty"`
	StartedAt  string  `json:"started_at,omitempty"`
	FinishedAt string  `json:"finished_at,omitempty"`
}

// SvcInfo 报告组件状态。
type SvcInfo struct {
	Installed  bool   `json:"installed"`
	CanInstall bool   `json:"can_install"`
	PkgManager string `json:"pkg_manager"`
}

// Service 是线路测速服务。
type Service struct {
	run       Runner
	mu        sync.Mutex
	st        Status
	startedAt time.Time
	loc       *time.Location
}

// New 构造。
func New(run Runner) *Service { return &Service{run: run, loc: detectLoc(run)} }

// detectLoc 用 `date +%z` 取系统时区（守护进程常跑 UTC，须按系统时区显示时间）。
func detectLoc(run Runner) *time.Location {
	out, _ := run.Run("", "date", "+%z")
	z := strings.TrimSpace(out)
	if len(z) != 5 || (z[0] != '+' && z[0] != '-') {
		return time.Local
	}
	sign := 1
	if z[0] == '-' {
		sign = -1
	}
	off := sign * ((int(z[1]-'0')*10+int(z[2]-'0'))*3600 + (int(z[3]-'0')*10+int(z[4]-'0'))*60)
	if off == 0 {
		return time.UTC
	}
	return time.FixedZone("local", off)
}

func (s *Service) now() string { return time.Now().In(s.loc).Format("2006-01-02 15:04:05") }

// Status 返回当前测速运行态（拷贝）。
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// ServiceInfo 探测 speedtest-go 是否已装。
func (s *Service) ServiceInfo() SvcInfo {
	pm := pkgmgr.PkgManager(s.run)
	_, err := s.run.Run("", "sh", "-c", "command -v speedtest-go")
	return SvcInfo{Installed: err == nil, CanInstall: pm != "", PkgManager: pm}
}

// Install 一键安装 speedtest-go（自愈回退源）。
func (s *Service) Install() (string, error) {
	return pkgmgr.Install(s.run, "speedtest-go")
}

// Run 起一次后台测速。已在测速中则报错（超过 5 分钟视为卡死的旧任务，允许重跑）。
func (s *Service) Run() error {
	s.mu.Lock()
	if s.st.Running && time.Since(s.startedAt) < 5*time.Minute {
		s.mu.Unlock()
		return errors.New("测速正在进行中，请稍候")
	}
	if _, err := s.run.Run("", "sh", "-c", "command -v speedtest-go"); err != nil {
		s.mu.Unlock()
		return errors.New("未安装 speedtest-go，请先点「一键安装测速组件」")
	}
	s.st = Status{Running: true, StartedAt: s.now()}
	s.startedAt = time.Now()
	s.mu.Unlock()

	go func() {
		// speedtest-go 自动选最近服务器并测下载+上传，内部已有网络超时；本机 busybox 无 timeout
		// 命令，故不外包 timeout（卡死任务由 Run 的 5 分钟陈旧判定兜底）。
		out, err := s.run.Run("", "sh", "-c", "speedtest-go 2>&1")
		s.mu.Lock()
		defer s.mu.Unlock()
		s.st.Running = false
		s.st.FinishedAt = s.now()
		if err != nil && strings.TrimSpace(out) == "" {
			s.st.Error = "测速失败：" + err.Error()
			return
		}
		r := parseSpeedtest(out)
		if r.DownloadMbps == 0 && r.UploadMbps == 0 {
			s.st.Error = "测速未取得有效结果（服务器不可达或被限速）：" + lastLine(out)
			return
		}
		s.st.Result = &r
		s.st.Error = ""
	}()
	return nil
}

// 解析 speedtest-go v1.7.x 文本输出（真机实测格式）：
//
//	✓ ISP: 1.2.3.4 (China Unicom) [..,..]
//	✓ Test Server: [43752] 461.09km Beijing (China) by BJ Unicom
//	✓ Latency: 20.79ms Jitter: ...
//	✓ Download: 542.09 Mbps (Used: ...)
//	✓ Upload: 54.74 Mbps (Used: ...)
var (
	reDL   = regexp.MustCompile(`(?i)download[:\s]+([\d.]+)\s*Mbps`)
	reUL   = regexp.MustCompile(`(?i)upload[:\s]+([\d.]+)\s*Mbps`)
	rePing = regexp.MustCompile(`(?i)latency[:\s]+([\d.]+)\s*ms`)
	reSrv  = regexp.MustCompile(`(?i)Test Server[:\s]+(.+)`)
	reISP  = regexp.MustCompile(`(?i)\bISP:[^(]*\(([^)]+)\)`)
)

func parseSpeedtest(out string) Result {
	var r Result
	if m := reDL.FindStringSubmatch(out); m != nil {
		r.DownloadMbps, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reUL.FindStringSubmatch(out); m != nil {
		r.UploadMbps, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := rePing.FindStringSubmatch(out); m != nil {
		r.PingMs, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reSrv.FindStringSubmatch(out); m != nil {
		r.Server = strings.TrimSpace(m[1])
	}
	if m := reISP.FindStringSubmatch(out); m != nil {
		r.ISP = strings.TrimSpace(m[1])
	}
	return r
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			if len(s) > 200 {
				s = s[:200]
			}
			return s
		}
	}
	return ""
}
