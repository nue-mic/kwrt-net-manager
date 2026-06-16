package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mia-clark/kwrt-net-manager/internal/ddns"
)

// DDNSHandler 服务「动态域名」端点（OpenWrt ddns-scripts）。
type DDNSHandler struct{ svc *ddns.Service }

// NewDDNSHandler 装配。
func NewDDNSHandler(svc *ddns.Service) *DDNSHandler { return &DDNSHandler{svc: svc} }

func registerDDNSRoutes(r chi.Router, svc *ddns.Service) {
	if svc == nil {
		return
	}
	h := NewDDNSHandler(svc)
	r.Get("/api/v1/ddns", h.List)
	r.Post("/api/v1/ddns", h.Create)
	r.Post("/api/v1/ddns/batch", h.Batch)
	r.Put("/api/v1/ddns/{id}", h.Update)
	r.Delete("/api/v1/ddns/{id}", h.Delete)
	r.Post("/api/v1/ddns/{id}/toggle", h.Toggle)
	r.Get("/api/v1/ddns/service", h.Service)
	r.Post("/api/v1/ddns/install", h.Install)
}

func (h *DDNSHandler) writeErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ddns.ErrNotFound()) {
		WriteError(w, http.StatusNotFound, CodeNotFound, "资源不存在", nil)
		return
	}
	WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
}

// List GET /api/v1/ddns
func (h *DDNSHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []ddns.Entry{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Create POST /api/v1/ddns
func (h *DDNSHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in ddns.Entry
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.Create(in)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// Update PUT /api/v1/ddns/{id}
func (h *DDNSHandler) Update(w http.ResponseWriter, r *http.Request) {
	var in ddns.Entry
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.Update(pathID(r), in)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// Delete DELETE /api/v1/ddns/{id}
func (h *DDNSHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(pathID(r)); err != nil {
		h.writeErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Toggle POST /api/v1/ddns/{id}/toggle
func (h *DDNSHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.Toggle(pathID(r), body.Enabled); err != nil {
		h.writeErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Batch POST /api/v1/ddns/batch
func (h *DDNSHandler) Batch(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.Batch(body.Action, body.IDs); err != nil {
		h.writeErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Service GET /api/v1/ddns/service — 组件探测（是否已装 / 服务商列表）。
func (h *DDNSHandler) Service(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, h.svc.ServiceInfo())
}

// Install POST /api/v1/ddns/install — 一键安装 ddns-scripts。
func (h *DDNSHandler) Install(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Install()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, "安装 DDNS 组件失败："+err.Error(), map[string]any{"output": out})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"output": out})
}
