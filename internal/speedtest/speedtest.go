// Package speedtest 用 OpenWrt 原生包 speedtest-go 实现「线路测速」(仿爱快 应用工具>线路测速)。
//
// 增强：多 speedtest.net 节点挨个测 + 历史趋势 + 全自动（未装自动安装再测）。
//   - 节点发现：speedtest-go --list（自带每节点延迟探测）。
//   - 单节点测：speedtest-go --server <id> --json（dl/ul 为 bps，latency/jitter 为纳秒）。
//   - 一个 job goroutine 串行跑选中节点，逐个更新状态；同一时刻仅一个任务。
//   - 历史落旁车 DATA_DIR/speedtest_history.json（最近 50 次）。
package speedtest

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nue-mic/kwrt-net-manager/internal/pkgmgr"
)

// Runner 抽象命令执行。
type Runner interface {
	Run(stdin, name string, args ...string) (string, error)
}

// Server 是一个候选 speedtest.net 节点。
type Server struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`    // 城市(国家)
	Sponsor     string  `json:"sponsor"` // 运营商
	DistanceKm  float64 `json:"distance_km"`
	PingMs      float64 `json:"ping_ms"`     // --list 自带延迟；-1=Timeout
	Reachable   bool    `json:"reachable"`   // 非 Timeout
	Recommended bool    `json:"recommended"` // 智能默认勾选
}

// NodeResult 是一个节点的测速结果（含进行态）。
type NodeResult struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Sponsor      string  `json:"sponsor"`
	DistanceKm   float64 `json:"distance_km"`
	Status       string  `json:"status"` // pending | testing | done | failed
	DownloadMbps float64 `json:"download_mbps"`
	UploadMbps   float64 `json:"upload_mbps"`
	PingMs       float64 `json:"ping_ms"`
	JitterMs     float64 `json:"jitter_ms"`
	Error        string  `json:"error,omitempty"`
}

// Status 是测速任务运行态。
type Status struct {
	Phase      string       `json:"phase"` // idle|installing|listing|testing|done|error
	Running    bool         `json:"running"`
	Message    string       `json:"message"`
	Nodes      []NodeResult `json:"nodes"`
	ISP        string       `json:"isp,omitempty"`
	StartedAt  string       `json:"started_at,omitempty"`
	FinishedAt string       `json:"finished_at,omitempty"`
	Error      string       `json:"error,omitempty"`
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
	gen       int // job 代次：每次 Run 自增；卡死的老 job 解挂后凭它把状态写入变 no-op
	startedAt time.Time
	loc       *time.Location
	histPath  string
	histMu    sync.Mutex
}

// New 构造。dataDir 用于历史持久化。
func New(run Runner, dataDir string) *Service {
	return &Service{
		run:      run,
		loc:      detectLoc(run),
		histPath: filepath.Join(dataDir, "speedtest_history.json"),
		st:       Status{Phase: "idle"},
	}
}

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

// Status 返回当前测速运行态（深拷贝 Nodes，避免与 job 写并发）。
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.st
	out.Nodes = append([]NodeResult(nil), s.st.Nodes...)
	return out
}

// ServiceInfo 探测 speedtest-go 是否已装。
func (s *Service) ServiceInfo() SvcInfo {
	pm := pkgmgr.PkgManager(s.run)
	return SvcInfo{Installed: s.installed(), CanInstall: pm != "", PkgManager: pm}
}

func (s *Service) installed() bool {
	_, err := s.run.Run("", "sh", "-c", "command -v speedtest-go")
	return err == nil
}

// Install 一键安装 speedtest-go（自愈回退源）。
func (s *Service) Install() (string, error) {
	return pkgmgr.Install(s.run, "speedtest-go")
}

// Servers 列出附近节点并标记智能默认勾选（供前端节点选择器）。未装则报错。
func (s *Service) Servers() ([]Server, string, error) {
	if !s.installed() {
		return nil, "", errors.New("未安装 speedtest-go")
	}
	servers, isp, err := s.listServers()
	if err != nil {
		return nil, "", err
	}
	pickRecommended(servers, defaultPick)
	return servers, isp, nil
}

func (s *Service) listServers() ([]Server, string, error) {
	out, err := s.run.Run("", "sh", "-c", "speedtest-go --list 2>&1")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, "", err
	}
	servers := parseServerList(out)
	if len(servers) == 0 {
		return nil, "", errors.New("未发现可用节点：" + lastLine(out))
	}
	return servers, parseISP(out), nil
}

const (
	defaultPick = 3 // 智能默认勾选节点数
	maxNodes    = 8 // 单次最多节点（防总耗时过长）
)

// Run 起一次后台多节点测速。serverIDs 为空=后端自动挑默认。未装会先自动安装。
func (s *Service) Run(serverIDs []string) error {
	s.mu.Lock()
	if s.st.Running && time.Since(s.startedAt) < staleGuard(len(serverIDs)) {
		s.mu.Unlock()
		return errors.New("测速正在进行中，请稍候")
	}
	if len(serverIDs) > maxNodes {
		serverIDs = serverIDs[:maxNodes]
	}
	s.gen++
	gen := s.gen
	s.st = Status{Phase: "starting", Running: true, Message: "准备测速…", StartedAt: s.now()}
	s.startedAt = time.Now()
	s.mu.Unlock()

	go s.runJob(gen, serverIDs)
	return nil
}

// staleGuard 卡死兜底时长：节点越多越久，至少 5 分钟。
func staleGuard(n int) time.Duration {
	if n < defaultPick {
		n = defaultPick
	}
	d := time.Duration(n) * 90 * time.Second
	if d < 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

func (s *Service) runJob(gen int, serverIDs []string) {
	// 1) 未装则自动安装。
	if !s.installed() {
		if !s.setPhase(gen, "installing", "正在安装测速组件…") {
			return
		}
		if out, err := s.Install(); err != nil {
			s.fail(gen, "安装测速组件失败："+strings.TrimSpace(lastLine(out)+" "+err.Error()))
			return
		}
	}
	// 2) 取节点列表（含用户 ISP）。
	if !s.setPhase(gen, "listing", "正在获取节点列表…") {
		return
	}
	servers, isp, err := s.listServers()
	if err != nil {
		s.fail(gen, "获取节点列表失败："+errMsg(err))
		return
	}
	byID := make(map[string]Server, len(servers))
	for _, sv := range servers {
		byID[sv.ID] = sv
	}
	// 3) 解析目标节点（空=自动挑默认）。
	ids := serverIDs
	if len(ids) == 0 {
		pickRecommended(servers, defaultPick)
		for _, sv := range servers {
			if sv.Recommended {
				ids = append(ids, sv.ID)
			}
		}
	}
	if len(ids) == 0 {
		s.fail(gen, "没有可用节点")
		return
	}
	nodes := make([]NodeResult, 0, len(ids))
	for _, id := range ids {
		sv := byID[id]
		nodes = append(nodes, NodeResult{
			ID: id, Name: sv.Name, Sponsor: sv.Sponsor, DistanceKm: sv.DistanceKm, Status: "pending",
		})
	}
	if !s.withGen(gen, func() { s.st.ISP = isp; s.st.Nodes = nodes }) {
		return
	}

	// 4) 逐个测。
	for i := range nodes {
		if !s.updateNode(gen, i, func(n *NodeResult) { n.Status = "testing" }) {
			return
		}
		s.setPhase(gen, "testing", fmt.Sprintf("正在测试 %s（%d/%d）…", nodeLabel(nodes[i]), i+1, len(nodes)))
		out, runErr := s.run.Run("", "sh", "-c", "speedtest-go --server "+nodes[i].ID+" --json 2>/dev/null")
		nr, perr := parseNodeJSON(out)
		if !s.updateNode(gen, i, func(n *NodeResult) {
			if perr != nil || (nr.DownloadMbps == 0 && nr.UploadMbps == 0) {
				n.Status = "failed"
				n.Error = "节点不可达或被限速"
				if runErr != nil && strings.TrimSpace(out) == "" {
					n.Error = runErr.Error()
				}
				return
			}
			n.Status = "done"
			n.DownloadMbps = nr.DownloadMbps
			n.UploadMbps = nr.UploadMbps
			n.PingMs = nr.PingMs
			n.JitterMs = nr.JitterMs
		}) {
			return
		}
	}
	s.finish(gen)
}

// withGen 在持锁且 gen 仍为当前代次时执行 fn；代次已变（被新 job 取代）返回 false，调用方应退出。
func (s *Service) withGen(gen int, fn func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return false
	}
	fn()
	return true
}

func (s *Service) setPhase(gen int, phase, msg string) bool {
	return s.withGen(gen, func() {
		s.st.Phase = phase
		s.st.Message = msg
	})
}

func (s *Service) updateNode(gen, i int, fn func(*NodeResult)) bool {
	return s.withGen(gen, func() {
		if i >= 0 && i < len(s.st.Nodes) {
			fn(&s.st.Nodes[i])
		}
	})
}

func (s *Service) fail(gen int, msg string) {
	s.withGen(gen, func() {
		s.st.Running = false
		s.st.Phase = "error"
		s.st.Error = msg
		s.st.Message = "测速失败"
		s.st.FinishedAt = s.now()
	})
}

func (s *Service) finish(gen int) {
	var nodes []NodeResult
	ok := s.withGen(gen, func() {
		s.st.Running = false
		s.st.Phase = "done"
		s.st.Message = "测速完成"
		s.st.FinishedAt = s.now()
		nodes = append([]NodeResult(nil), s.st.Nodes...)
	})
	if ok {
		s.appendHistory(nodes)
	}
}

func nodeLabel(n NodeResult) string {
	if n.Sponsor != "" {
		return n.Name + " · " + n.Sponsor
	}
	return n.Name
}

func errMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
