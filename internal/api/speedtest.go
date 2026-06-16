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
	r.Post("/api/v1/speedtest/run", h.Run)
	r.Get("/api/v1/speedtest/service", h.Service)
	r.Post("/api/v1/speedtest/install", h.Install)
}

// Status GET /api/v1/speedtest/status
func (h *SpeedtestHandler) Status(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, h.svc.Status())
}

// Run POST /api/v1/speedtest/run — 起一次后台测速。
func (h *SpeedtestHandler) Run(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Run(); err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, h.svc.Status())
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
