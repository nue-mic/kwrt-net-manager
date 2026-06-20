package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mia-clark/kwrt-net-manager/internal/speedtest"
)

// SpeedtestHandler 服务「线路测速」端点（OpenWrt speedtest-go）。
type SpeedtestHandler struct{ svc *speedtest.Service }

// NewSpeedtestHandler 装配。
func NewSpeedtestHandler(svc *speedtest.Service) *SpeedtestHandler {
	return &SpeedtestHandler{svc: svc}
}

func registerSpeedtestRoutes(r chi.Router, svc *speedtest.Service) {
	if svc == nil {
		return
	}
	h := NewSpeedtestHandler(svc)
	r.Get("/api/v1/speedtest/status", h.Status)
	r.Get("/api/v1/speedtest/servers", h.Servers)
	r.Post("/api/v1/speedtest/run", h.Run)
	r.Get("/api/v1/speedtest/service", h.Service)
	r.Post("/api/v1/speedtest/install", h.Install)
	r.Get("/api/v1/speedtest/history", h.History)
	r.Post("/api/v1/speedtest/history/clear", h.ClearHistory)
}

// Status GET /api/v1/speedtest/status
func (h *SpeedtestHandler) Status(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, h.svc.Status())
}

// Run POST /api/v1/speedtest/run {server_ids?:[]} — 起一次多节点后台测速。
// server_ids 为空=后端自动挑默认节点；未装会自动安装。
func (h *SpeedtestHandler) Run(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerIDs []string `json:"server_ids"`
	}
	// body 可空（兼容“全自动”调用）；有 body 才解析（ContentLength<=0 含空/分块体，视为自动）。
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &body) {
			return
		}
	}
	if err := h.svc.Run(body.ServerIDs); err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, h.svc.Status())
}

// Servers GET /api/v1/speedtest/servers — 附近节点（含 recommended）。未装返回 installed=false。
func (h *SpeedtestHandler) Servers(w http.ResponseWriter, r *http.Request) {
	info := h.svc.ServiceInfo()
	if !info.Installed {
		WriteJSON(w, http.StatusOK, map[string]any{"installed": false, "items": []speedtest.Server{}, "isp": ""})
		return
	}
	servers, isp, err := h.svc.Servers()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if servers == nil {
		servers = []speedtest.Server{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"installed": true, "items": servers, "isp": isp})
}

// History GET /api/v1/speedtest/history — 历史记录（最新在前）。
func (h *SpeedtestHandler) History(w http.ResponseWriter, r *http.Request) {
	items := h.svc.History()
	if items == nil {
		items = []speedtest.HistoryEntry{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ClearHistory POST /api/v1/speedtest/history/clear — 清空历史。
func (h *SpeedtestHandler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ClearHistory(); err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Service GET /api/v1/speedtest/service — 组件探测。
func (h *SpeedtestHandler) Service(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, h.svc.ServiceInfo())
}

// Install POST /api/v1/speedtest/install — 一键安装 speedtest-go。
func (h *SpeedtestHandler) Install(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Install()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, "安装测速组件失败："+err.Error(), map[string]any{"output": out})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"output": out})
}
