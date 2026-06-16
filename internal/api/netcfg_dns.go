package api

import (
	"net/http"

	"github.com/mia-clark/kwrt-net-manager/internal/netcfg"
)

// ================= DNS 全局设置 / DoH（单例） =================

// GetDNSSettings GET /api/v1/dns/settings
func (h *NetcfgHandler) GetDNSSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.GetDNSSettings()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, s)
}

// UpdateDNSSettings PUT /api/v1/dns/settings
func (h *NetcfgHandler) UpdateDNSSettings(w http.ResponseWriter, r *http.Request) {
	var in netcfg.DNSSettings
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.SaveDNSSettings(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// GetDNSDoH GET /api/v1/dns/doh
func (h *NetcfgHandler) GetDNSDoH(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.GetDNSDoH()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, d)
}

// UpdateDNSDoH PUT /api/v1/dns/doh
func (h *NetcfgHandler) UpdateDNSDoH(w http.ResponseWriter, r *http.Request) {
	var in netcfg.DNSDoH
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.SaveDNSDoH(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// InstallDoHHandler POST /api/v1/dns/doh/install
func (h *NetcfgHandler) InstallDoHHandler(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.InstallDoH()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, "安装 DoH 组件失败："+err.Error(), map[string]any{"output": out})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"output": out})
}

// ================= DNS 自定义解析记录 =================

// ListDNSRecords GET /api/v1/dns/records
func (h *NetcfgHandler) ListDNSRecords(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListDNSRecords()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.DNSRecord{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreateDNSRecord POST /api/v1/dns/records
func (h *NetcfgHandler) CreateDNSRecord(w http.ResponseWriter, r *http.Request) {
	var in netcfg.DNSRecord
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreateDNSRecord(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// UpdateDNSRecord PUT /api/v1/dns/records/{id}
func (h *NetcfgHandler) UpdateDNSRecord(w http.ResponseWriter, r *http.Request) {
	var in netcfg.DNSRecord
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateDNSRecord(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// DeleteDNSRecord DELETE /api/v1/dns/records/{id}
func (h *NetcfgHandler) DeleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteDNSRecord(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": pathID(r)})
}

// ToggleDNSRecord POST /api/v1/dns/records/{id}/toggle {enabled}
func (h *NetcfgHandler) ToggleDNSRecord(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetDNSRecordEnabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// BatchDNSRecords POST /api/v1/dns/records/batch {action, ids}
func (h *NetcfgHandler) BatchDNSRecords(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchDNSRecords(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ================= DNS 域名分流 =================

// ListDNSDomainRoutes GET /api/v1/dns/domain-routes
func (h *NetcfgHandler) ListDNSDomainRoutes(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListDNSDomainRoutes()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.DNSDomainRoute{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreateDNSDomainRoute POST /api/v1/dns/domain-routes
func (h *NetcfgHandler) CreateDNSDomainRoute(w http.ResponseWriter, r *http.Request) {
	var in netcfg.DNSDomainRoute
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreateDNSDomainRoute(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// UpdateDNSDomainRoute PUT /api/v1/dns/domain-routes/{id}
func (h *NetcfgHandler) UpdateDNSDomainRoute(w http.ResponseWriter, r *http.Request) {
	var in netcfg.DNSDomainRoute
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateDNSDomainRoute(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// DeleteDNSDomainRoute DELETE /api/v1/dns/domain-routes/{id}
func (h *NetcfgHandler) DeleteDNSDomainRoute(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteDNSDomainRoute(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": pathID(r)})
}

// ToggleDNSDomainRoute POST /api/v1/dns/domain-routes/{id}/toggle {enabled}
func (h *NetcfgHandler) ToggleDNSDomainRoute(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetDNSDomainRouteEnabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// BatchDNSDomainRoutes POST /api/v1/dns/domain-routes/batch {action, ids}
func (h *NetcfgHandler) BatchDNSDomainRoutes(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchDNSDomainRoutes(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ================= 运行态 / 维护 =================

// DNSCacheStats GET /api/v1/dns/cache-stats
func (h *NetcfgHandler) DNSCacheStats(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.DNSCacheStats()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, st)
}

// FlushDNSCache POST /api/v1/dns/cache/flush
func (h *NetcfgHandler) FlushDNSCache(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.FlushDNSCache(); err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, "清空 DNS 缓存失败："+err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DNSService GET /api/v1/dns/service — DNS 能力探测（filter-AAAA / DoH 安装情况）。
func (h *NetcfgHandler) DNSService(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.DNSServiceInfo()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, info)
}
