package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nue-mic/kwrt-net-manager/internal/logcenter"
)

// LogHandler 服务「日志中心」端点（系统/DHCP/拨号/DDNS/操作/ARP）。
type LogHandler struct{ c *logcenter.Center }

// NewLogHandler 装配。
func NewLogHandler(c *logcenter.Center) *LogHandler { return &LogHandler{c: c} }

// registerLogRoutes 在鉴权子树挂载日志中心端点。
func registerLogRoutes(r chi.Router, c *logcenter.Center) {
	if c == nil {
		return
	}
	h := NewLogHandler(c)
	r.Get("/api/v1/logs/{source}", h.Query)
	r.Get("/api/v1/logs/{source}/export", h.Export)
	r.Post("/api/v1/logs/{source}/clear", h.Clear)
}

func (h *LogHandler) filter(r *http.Request) logcenter.Filter {
	q := r.URL.Query()
	atoi := func(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	return logcenter.Filter{
		Start:    atoi(q.Get("start")),
		End:      atoi(q.Get("end")),
		Keyword:  q.Get("keyword"),
		Page:     page,
		PageSize: size,
	}
}

// Query GET /api/v1/logs/{source}?start&end&keyword&page&page_size
func (h *LogHandler) Query(w http.ResponseWriter, r *http.Request) {
	res, err := h.c.Query(chi.URLParam(r, "source"), h.filter(r))
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, res)
}

// Export GET /api/v1/logs/{source}/export — 纯文本下载（应用当前过滤，不分页）。
func (h *LogHandler) Export(w http.ResponseWriter, r *http.Request) {
	source := chi.URLParam(r, "source")
	text, err := h.c.Export(source, h.filter(r))
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+source+"-log.txt\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(text))
}

// Clear POST /api/v1/logs/{source}/clear — 仅本工具自管(operation/arp)可清空。
func (h *LogHandler) Clear(w http.ResponseWriter, r *http.Request) {
	if err := h.c.Clear(chi.URLParam(r, "source")); err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- 操作审计中间件 ----

// auditMiddleware 记录鉴权子树里的「写操作」(POST/PUT/DELETE 且 2xx)，落操作日志。
// 读操作(GET)、日志中心自身、导出/下载不记。
func auditMiddleware(c *logcenter.Center) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}
			rec := &auditRecorder{ResponseWriter: w, status: 0}
			next.ServeHTTP(rec, r)
			if rec.status >= 200 && rec.status < 400 && !skipAudit(r.URL.Path) {
				module, action := deriveAudit(r.Method, r.URL.Path)
				c.Record(logcenter.OperationEntry{
					User:     "admin",
					ClientIP: clientIP(r),
					Module:   module,
					Action:   action,
					Detail:   r.Method + " " + r.URL.Path,
				})
			}
		})
	}
}

type auditRecorder struct {
	http.ResponseWriter
	status int
}

func (a *auditRecorder) WriteHeader(code int) { a.status = code; a.ResponseWriter.WriteHeader(code) }
func (a *auditRecorder) Write(b []byte) (int, error) {
	if a.status == 0 {
		a.status = http.StatusOK
	}
	return a.ResponseWriter.Write(b)
}

func skipAudit(path string) bool {
	return strings.HasPrefix(path, "/api/v1/logs/")
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// 路径片段 → 中文功能模块名（操作日志「功能」列）。
var auditModules = []struct{ seg, name string }{
	{"/dhcp/servers", "DHCP服务端"},
	{"/dhcp/statics", "DHCP静态分配"},
	{"/dhcp/leases", "DHCP终端列表"},
	{"/dhcp/acl", "DHCP黑白名单"},
	{"/dhcp/route-push", "DHCP服务端"},
	{"/dhcp/restart", "DHCP服务端"},
	{"/dhcp/install", "DHCP服务端"},
	{"/routes", "静态路由"},
	{"/dns/records", "自定义解析"},
	{"/dns/domain-routes", "域名分流"},
	{"/dns/doh", "DNS设置"},
	{"/dns/cache", "DNS设置"},
	{"/dns/settings", "DNS设置"},
	{"/ddns", "动态域名"},
	{"/speedtest", "线路测速"},
	{"/ipv6/", "IPv6设置"},
	{"/ifaces", "内外网设置"},
	{"/nics", "内外网设置"},
	{"/backup", "定时备份"},
	{"/import", "导入导出"},
	{"/export", "导入导出"},
	{"/system/config", "系统设置"},
	{"/system/update", "系统更新"},
	{"/ui/branding", "品牌设置"},
}

// deriveAudit 从 method+path 推导(功能模块, 动作)。
func deriveAudit(method, path string) (module, action string) {
	module = "系统"
	for _, m := range auditModules {
		if strings.Contains(path, m.seg) {
			module = m.name
			break
		}
	}
	switch {
	case strings.HasSuffix(path, "/toggle"):
		action = "启用/停用规则"
	case strings.HasSuffix(path, "/batch"):
		action = "批量操作"
	case strings.HasSuffix(path, "/restart"):
		action = "重启服务"
	case strings.HasSuffix(path, "/install"):
		action = "安装组件"
	case strings.HasSuffix(path, "/run"):
		action = "执行命令"
	case strings.HasSuffix(path, "/flush"):
		action = "清空"
	case strings.HasSuffix(path, "/duplicate"):
		action = "复制规则"
	case method == http.MethodPost:
		action = "新增规则"
	case method == http.MethodPut:
		action = "修改配置"
	case method == http.MethodDelete:
		action = "删除规则"
	default:
		action = "操作"
	}
	return module, action
}
