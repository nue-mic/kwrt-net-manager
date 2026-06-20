// Package logcenter 实现「日志中心」(仿爱快)：把 OpenWrt 原生日志源(logread / ddns-scripts
// 日志 / ip neigh)与本工具自管的审计/ARP 日志，统一成可过滤、分页、导出的查询接口。
//
// 第一准则——只迁移 OpenWrt 能原生实现的：
//   - system/dhcp/dialup ← `logread`（OpenWrt logd 全量，按来源过滤+解析）；
//   - ddns              ← ddns-scripts 的 /var/log/ddns/*.log；
//   - operation         ← 本工具审计（每个写操作 + 模块/动作/客户端IP，落 JSONL）；
//   - arp               ← 轮询 `ip neigh` 差分，记录某 IP 的 MAC 变化(疑似 ARP 欺骗/冲突)。
//
// 非 OpenWrt(开发/CI)上 logread/ip neigh 不存在 → 对应源返回空，operation/arp 仍按文件工作。
package logcenter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// 已支持的日志源。
const (
	SourceSystem    = "system"    // 系统日志（logread 全量）
	SourceDHCP      = "dhcp"      // DHCP日志（dnsmasq-dhcp / odhcpd）
	SourceDialup    = "dialup"    // 外网拨号日志（pppd/netifd/udhcpc）
	SourceDDNS      = "ddns"      // 动态域名日志（ddns-scripts）
	SourceOperation = "operation" // 操作日志（本工具审计）
	SourceARP       = "arp"       // ARP日志（ip neigh 差分，疑似欺骗）
)

// selfManaged 报告该源是否由本工具落盘（可清空/可记录）。
func selfManaged(source string) bool { return source == SourceOperation || source == SourceARP }

// maxFileLines 是自管 JSONL 每源的环形上限，防止撑爆 tmpfs。
const maxFileLines = 5000

// Runner 抽象命令执行，便于单测注入假实现。
type Runner interface {
	Run(name string, args ...string) (string, error)
}

// StreamRunner 是 Runner 的可选能力：持续读取一个长驻命令(如 `logread -f`)的 stdout 行，
// 用于「拨号实时日志」。返回的 channel 在进程退出或 ctx 取消时关闭。Runner 未实现它的后端
// 不提供实时拨号流（仅快照）。
type StreamRunner interface {
	Stream(ctx context.Context, name string, args ...string) (<-chan string, error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// Stream 起一个长驻进程并把它的 stdout 按行投递到 channel。ctx 取消即 kill 进程
// （exec.CommandContext 负责 SIGKILL，musl/busybox 下亦可靠）。binary 不存在时
// Start 立即报错——非 OpenWrt(无 logread)由此自然走不通，调用方据此判定。
func (execRunner) Stream(ctx context.Context, name string, args ...string) (<-chan string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			select {
			case ch <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
		_ = cmd.Wait()
	}()
	return ch, nil
}

// Entry 是统一日志行；不同源按需填充对应字段（前端按源渲染列）。
type Entry struct {
	Time     string `json:"time"`                // "2006-01-02 15:04:05"
	TS       int64  `json:"ts"`                  // unix 秒，用于排序/过滤
	Level    string `json:"level,omitempty"`     // syslog 级别
	Proc     string `json:"proc,omitempty"`      // 进程/标识
	Message  string `json:"message,omitempty"`   // 事件描述
	Type     string `json:"type,omitempty"`      // dhcp 报文类型 / arp 事件类型
	Iface    string `json:"iface,omitempty"`     // 接口
	MAC      string `json:"mac,omitempty"`       // MAC
	IP       string `json:"ip,omitempty"`        // IP
	User     string `json:"user,omitempty"`      // operation：用户名
	ClientIP string `json:"client_ip,omitempty"` // operation：客户端 IP
	Module   string `json:"module,omitempty"`    // operation：功能模块
	Action   string `json:"action,omitempty"`    // operation：动作

	// 拨号诊断（仅 dialup 源 / 实时拨号流富化；其余源为空，omitempty 不影响）。
	Phase     string `json:"phase,omitempty"`      // discovery|auth|ipcp|established|teardown|other
	DialState string `json:"dial_state,omitempty"` // connecting|connected|failed（与接口三态语义一致）
	Severity  string `json:"severity,omitempty"`   // info|success|warning|error
	Diagnosis string `json:"diagnosis,omitempty"`  // 中文诊断（命中模式表才有）
	Advice    string `json:"advice,omitempty"`     // 中文处置建议
	Seq       uint64 `json:"seq,omitempty"`         // 实时流内单调序号，前端去重/续传
}

// Filter 是查询过滤条件。
type Filter struct {
	Start    int64  // unix 秒，0=不限
	End      int64  // unix 秒，0=不限
	Keyword  string // 在所有文本字段里子串匹配
	Page     int    // 1-based
	PageSize int
}

// Result 是分页查询结果。
type Result struct {
	Items []Entry `json:"items"`
	Total int     `json:"total"`
}

// Center 是日志中心。
type Center struct {
	run      Runner
	dir      string // DATA_DIR/logs
	log      *slog.Logger
	loc      *time.Location // 机器本地时区（守护进程常跑在 UTC，须按系统时区显示/换算）
	mu       sync.Mutex
	arpSeen  map[string]string // ip -> last mac（ARP 差分基线）
	simulate bool              // true=拨号流推送脚本化模拟序列（非 OpenWrt/store 后端演示用）
	dial     *dialStream       // 拨号实时日志：单例 logread -f + 环形缓冲 + 多订阅广播
}

// New 构造 Center。dataDir 为数据根目录（自管日志落 dataDir/logs）。
func New(dataDir string, log *slog.Logger) *Center {
	if log == nil {
		log = slog.Default()
	}
	c := &Center{run: execRunner{}, dir: filepath.Join(dataDir, "logs"), log: log, arpSeen: map[string]string{}}
	c.loc = detectLoc(c.run)
	c.dial = newDialStream(c)
	_ = os.MkdirAll(c.dir, 0o755)
	return c
}

// SetSimulate 开/关拨号流模拟。守护进程在 netcfg 后端为 store(非 OpenWrt)时置 true，
// 让 Windows/CI 也能演示「拨号实时日志 + 诊断」整条链路。
func (c *Center) SetSimulate(b bool) { c.simulate = b }

// detectLoc 用 `date +%z` 取系统当前 UTC 偏移，构造固定时区。失败回退 time.Local。
// 目的：logread 打印的是系统本地时间，而守护进程多跑在 UTC——统一按系统时区解析/落盘，
// 保证「系统日志」与「操作/ARP 日志」时间一致、时间范围过滤正确。
func detectLoc(run Runner) *time.Location {
	out, err := run.Run("date", "+%z")
	z := strings.TrimSpace(out)
	if err != nil || len(z) != 5 {
		return time.Local
	}
	sign := 1
	if z[0] == '-' {
		sign = -1
	} else if z[0] != '+' {
		return time.Local
	}
	hh := int(z[1]-'0')*10 + int(z[2]-'0')
	mm := int(z[3]-'0')*10 + int(z[4]-'0')
	off := sign * (hh*3600 + mm*60)
	if off == 0 {
		return time.UTC
	}
	return time.FixedZone("local", off)
}

// ---- 查询 ----

// Query 按源返回过滤+分页后的日志（按时间倒序，新在前）。
func (c *Center) Query(source string, f Filter) (Result, error) {
	var all []Entry
	switch source {
	case SourceOperation, SourceARP:
		all = c.readJSONL(source)
	case SourceDDNS:
		all = c.readDDNS()
	case SourceSystem, SourceDHCP, SourceDialup:
		all = c.readSyslog(source)
	default:
		return Result{}, fmt.Errorf("未知日志源: %s", source)
	}

	kw := strings.ToLower(strings.TrimSpace(f.Keyword))
	filtered := make([]Entry, 0, len(all))
	for _, e := range all {
		if f.Start > 0 && e.TS > 0 && e.TS < f.Start {
			continue
		}
		if f.End > 0 && e.TS > 0 && e.TS > f.End {
			continue
		}
		if kw != "" && !entryMatch(e, kw) {
			continue
		}
		filtered = append(filtered, e)
	}
	// 倒序（新在前）：syslog/jsonl 都是旧→新追加。
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].TS > filtered[j].TS })

	total := len(filtered)
	page, size := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	start := (page - 1) * size
	if start >= total {
		return Result{Items: []Entry{}, Total: total}, nil
	}
	end := start + size
	if end > total {
		end = total
	}
	return Result{Items: filtered[start:end], Total: total}, nil
}

func entryMatch(e Entry, kw string) bool {
	for _, s := range []string{e.Message, e.Proc, e.Type, e.Iface, e.MAC, e.IP, e.User, e.ClientIP, e.Module, e.Action, e.Level} {
		if s != "" && strings.Contains(strings.ToLower(s), kw) {
			return true
		}
	}
	return false
}

// Export 把某源当前过滤结果（全部，不分页）拼成纯文本，供下载。
func (c *Center) Export(source string, f Filter) (string, error) {
	f.Page, f.PageSize = 1, 1<<30
	res, err := c.Query(source, f)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, e := range res.Items {
		sb.WriteString(e.Time)
		for _, kv := range []string{e.Level, e.Proc, e.Module, e.Action, e.User, e.ClientIP, e.Type, e.Iface, e.MAC, e.IP} {
			if kv != "" {
				sb.WriteString("\t")
				sb.WriteString(kv)
			}
		}
		if e.Message != "" {
			sb.WriteString("\t")
			sb.WriteString(e.Message)
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// Clear 清空某源——仅对本工具自管(operation/arp)有效；系统/DHCP 等由 OpenWrt logd 维护，不支持。
func (c *Center) Clear(source string) error {
	if !selfManaged(source) {
		return fmt.Errorf("%s 由 OpenWrt 系统日志维护，本工具不支持清空", source)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return os.Remove(c.path(source))
}

// ---- 写入：审计 + ARP ----

// OperationEntry 是一条操作审计。
type OperationEntry struct {
	User     string
	ClientIP string
	Module   string
	Action   string
	Detail   string
}

// Record 追加一条操作审计（供 HTTP 审计中间件调用）。
func (c *Center) Record(op OperationEntry) {
	now := time.Now()
	c.appendEntry(SourceOperation, Entry{
		Time: now.In(c.loc).Format("2006-01-02 15:04:05"), TS: now.Unix(),
		User: op.User, ClientIP: op.ClientIP, Module: op.Module, Action: op.Action, Message: op.Detail,
	})
}

// StartARPMonitor 起后台轮询 `ip neigh`，记录某 IP 的 MAC 变化(疑似 ARP 欺骗/冲突)。
// 首轮仅建立基线不记录。非 OpenWrt(ip neigh 不存在)上自动空转。
func (c *Center) StartARPMonitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	go func() {
		c.scanARP(true) // 基线
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.scanARP(false)
			}
		}
	}()
}

var reNeigh = regexp.MustCompile(`^(\S+)\s+dev\s+(\S+)\s+lladdr\s+([0-9a-fA-F:]{17})\s+(\S+)`)

func (c *Center) scanARP(seed bool) {
	out, err := c.run.Run("ip", "neigh")
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		m := reNeigh.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		ip, dev, mac := m[1], m[2], strings.ToLower(m[3])
		c.mu.Lock()
		prev, seen := c.arpSeen[ip]
		c.arpSeen[ip] = mac
		c.mu.Unlock()
		if seed || !seen || prev == mac {
			continue // 只记录「同一 IP 的 MAC 发生变化」＝疑似欺骗/冲突，与爱快一致
		}
		now := time.Now()
		c.appendEntry(SourceARP, Entry{
			Time: now.In(c.loc).Format("2006-01-02 15:04:05"), TS: now.Unix(),
			Type: "疑似ARP欺骗", Iface: dev, IP: ip, MAC: mac,
			Message: fmt.Sprintf("检测到一个 ARP 地址变化在接口(%s): %s %s -> %s", dev, ip, prev, mac),
		})
	}
}

// ---- 文件读写（JSONL，环形截断）----

func (c *Center) path(source string) string { return filepath.Join(c.dir, source+".jsonl") }

func (c *Center) appendEntry(source string, e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lines := c.readLinesLocked(source)
	b, _ := json.Marshal(e)
	lines = append(lines, string(b))
	if len(lines) > maxFileLines {
		lines = lines[len(lines)-maxFileLines:]
	}
	_ = os.WriteFile(c.path(source), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func (c *Center) readLinesLocked(source string) []string {
	f, err := os.Open(c.path(source))
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if s := strings.TrimSpace(sc.Text()); s != "" {
			lines = append(lines, s)
		}
	}
	return lines
}

func (c *Center) readJSONL(source string) []Entry {
	c.mu.Lock()
	lines := c.readLinesLocked(source)
	c.mu.Unlock()
	out := make([]Entry, 0, len(lines))
	for _, ln := range lines {
		var e Entry
		if json.Unmarshal([]byte(ln), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}
