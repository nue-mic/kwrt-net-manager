// Package ddns 用 OpenWrt 原生 ddns-scripts 实现「动态域名」(仿爱快 高级应用>动态域名)。
//
// 第一准则——只迁移 OpenWrt 能原生实现的：
//   - 解析设置=外网线路/出口IP → ddns-scripts ip_source='web'（探测公网出口 IP）；
//   - 解析设置=接口IP        → ip_source='network' + ip_network=<wan>；
//   - 记录类型 A/AAAA        → use_ipv6 0/1；
//   - 服务商                 → service_name（本机 /usr/share/ddns/default 实际支持的）。
//   - **降级**：爱快「解析设置=终端MAC（把某 LAN 设备的 IP 顶到域名）」ddns-scripts 无原生
//     能力，不做（前端说明只支持外网线路/出口IP/接口IP）。
//
// 安全：旁车 JSON 为权威，投射时只增删带本工具 marker 的 ddns 具名节，绝不碰用户/LuCI 配置。
package ddns

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	markerDDNS = "kwrt-net-manager-ddns"
	markerOpt  = "managed_by"
)

// Runner 抽象命令执行（与 pkgmgr 同签名，便于复用安装器与单测）。
type Runner interface {
	Run(stdin, name string, args ...string) (string, error)
}

// Entry 是一条动态域名配置（旁车权威）。
type Entry struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`    // service_name 基名，如 cloudflare.com
	Domain     string `json:"domain"`      // 要更新的完整记录，如 home.example.com
	AuthMode   string `json:"auth_mode"`   // token | userpass
	Username   string `json:"username"`    // zone/账号（token 模式可空或填 zone）
	Password   string `json:"password"`    // API Token/Key 或密码（密文）
	IPSource   string `json:"ip_source"`   // web（出口IP）| network（接口IP）
	Interface  string `json:"interface"`   // ip_network / 触发接口，如 wan
	RecordType string `json:"record_type"` // A | AAAA
	Enabled    bool   `json:"enabled"`
	Remark     string `json:"remark"`

	// 只读运行态。
	LastResult string `json:"last_result,omitempty"` // 成功 | 失败 | 等待更新 | 已停用
	CurrentIP  string `json:"current_ip,omitempty"`
	LastUpdate string `json:"last_update,omitempty"`
}

// Service 是 DDNS 领域服务。
type Service struct {
	run     Runner
	sidecar string
	mu      sync.Mutex
	idFn    func() string
}

// New 构造。sidecar 为旁车 JSON 路径（DATA_DIR/ddns.json）。idFn 可空。
func New(run Runner, sidecar string, idFn func() string) *Service {
	if idFn == nil {
		idFn = defaultID
	}
	return &Service{run: run, sidecar: sidecar, idFn: idFn}
}

// ---- 读 ----

// List 返回全部条目并富化运行态。
func (s *Service) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.load()
	for i := range items {
		s.enrich(&items[i])
	}
	return items, nil
}

func (s *Service) enrich(e *Entry) {
	if !e.Enabled {
		e.LastResult = "已停用"
		return
	}
	// ddns-scripts 把当前已登记 IP 写到 /var/run/ddns/<id>.ip。
	ipFile := "/var/run/ddns/" + e.ID + ".ip"
	if b, err := os.ReadFile(ipFile); err == nil {
		ip := strings.TrimSpace(string(b))
		if ip != "" {
			e.CurrentIP = ip
			e.LastResult = "成功"
			if fi, err := os.Stat(ipFile); err == nil {
				e.LastUpdate = fi.ModTime().Format("2006-01-02 15:04:05")
			}
			return
		}
	}
	e.LastResult = "等待更新"
}

// ---- 写 ----

// Create 新增。
func (s *Service) Create(in Entry) (Entry, error) {
	if err := validate(in); err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.load()
	in.ID = s.idFn()
	in.Enabled = true
	items = append(items, in)
	return in, s.saveAndApply(items)
}

// Update 改。
func (s *Service) Update(id string, in Entry) (Entry, error) {
	if err := validate(in); err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.load()
	for i := range items {
		if items[i].ID == id {
			in.ID = id
			in.Enabled = items[i].Enabled
			items[i] = in
			return in, s.saveAndApply(items)
		}
	}
	return Entry{}, errNotFound
}

// Delete 删。
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.load()
	out := items[:0]
	found := false
	for _, e := range items {
		if e.ID == id {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return errNotFound
	}
	return s.saveAndApply(out)
}

// Toggle 启停。
func (s *Service) Toggle(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.load()
	for i := range items {
		if items[i].ID == id {
			items[i].Enabled = enabled
			return s.saveAndApply(items)
		}
	}
	return errNotFound
}

// Batch 批量 enable|disable|delete。
func (s *Service) Batch(action string, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idset := map[string]bool{}
	for _, id := range ids {
		idset[id] = true
	}
	items := s.load()
	switch action {
	case "enable", "disable":
		on := action == "enable"
		for i := range items {
			if idset[items[i].ID] {
				items[i].Enabled = on
			}
		}
	case "delete":
		out := items[:0]
		for _, e := range items {
			if !idset[e.ID] {
				out = append(out, e)
			}
		}
		items = out
	default:
		return fmt.Errorf("未知批量动作: %s", action)
	}
	return s.saveAndApply(items)
}

var errNotFound = errors.New("not found")

// ErrNotFound 暴露给 API 层判定 404。
func ErrNotFound() error { return errNotFound }

func validate(e Entry) error {
	if strings.TrimSpace(e.Provider) == "" {
		return errors.New("服务商不能为空")
	}
	if strings.TrimSpace(e.Domain) == "" {
		return errors.New("域名不能为空")
	}
	if strings.TrimSpace(e.Password) == "" {
		return errors.New("API Token/密码不能为空")
	}
	if e.IPSource != "web" && e.IPSource != "network" {
		return errors.New("解析方式只能为 出口IP(web) 或 接口IP(network)")
	}
	if e.IPSource == "network" && strings.TrimSpace(e.Interface) == "" {
		return errors.New("接口IP 模式必须指定解析网卡")
	}
	if e.RecordType != "A" && e.RecordType != "AAAA" {
		return errors.New("记录类型只能为 A 或 AAAA")
	}
	return nil
}

// ---- 旁车持久化 ----

func (s *Service) load() []Entry {
	b, err := os.ReadFile(s.sidecar)
	if err != nil {
		return nil
	}
	var items []Entry
	_ = json.Unmarshal(b, &items)
	return items
}

func (s *Service) saveAndApply(items []Entry) error {
	b, _ := json.MarshalIndent(items, "", "  ")
	if err := os.WriteFile(s.sidecar, b, 0o600); err != nil {
		return err
	}
	return s.apply(items)
}

func defaultID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "ddns_" + hex.EncodeToString(b[:])
}

// path 工具：返回 ddns service 文件目录里的服务商列表（去 -v4/-v6/.json 后去重）。
func providerFiles() []string {
	files, _ := filepath.Glob("/usr/share/ddns/default/*")
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		name := filepath.Base(f)
		name = strings.TrimSuffix(name, ".json")
		name = strings.TrimSuffix(name, "-v4")
		name = strings.TrimSuffix(name, "-v6")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
