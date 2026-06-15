package api

import (
	"net/http"

	"github.com/mia-clark/kwrt-net-manager/internal/netcfg"
)

// ListNICs GET /api/v1/nics — physical NIC inventory (网卡列表).
func (h *NetcfgHandler) ListNICs(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListNICs()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// NetOverview GET /api/v1/netcfg/overview — 内外网设置 dashboard summary.
func (h *NetcfgHandler) NetOverview(w http.ResponseWriter, r *http.Request) {
	ov, err := h.svc.NetOverview()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, ov)
}

// ListNetIfaces GET /api/v1/ifaces — configured LAN/WAN interfaces.
func (h *NetcfgHandler) ListNetIfaces(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListNetIfaces()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetNetIface GET /api/v1/ifaces/{id}
func (h *NetcfgHandler) GetNetIface(w http.ResponseWriter, r *http.Request) {
	ni, err := h.svc.GetNetIface(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, ni)
}

// CreateNetIface POST /api/v1/ifaces
func (h *NetcfgHandler) CreateNetIface(w http.ResponseWriter, r *http.Request) {
	var in netcfg.NetIface
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.SaveNetIface(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// UpdateNetIface PUT /api/v1/ifaces/{id}
func (h *NetcfgHandler) UpdateNetIface(w http.ResponseWriter, r *http.Request) {
	var in netcfg.NetIface
	if !decodeJSON(w, r, &in) {
		return
	}
	in.ID = pathID(r)
	out, err := h.svc.SaveNetIface(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// DeleteNetIface DELETE /api/v1/ifaces/{id}
func (h *NetcfgHandler) DeleteNetIface(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteNetIface(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": pathID(r)})
}

// DHCPService GET /api/v1/dhcp/service — which DHCP daemon is installed/running.
func (h *NetcfgHandler) DHCPService(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.DHCPServiceInfo()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, info)
}

// InstallDHCP POST /api/v1/dhcp/install — 一键安装 dnsmasq.
func (h *NetcfgHandler) InstallDHCP(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.InstallDHCP()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), map[string]any{"output": out})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out})
}

// IfaceAction POST /api/v1/ifaces/{id}/action {action: connect|disconnect|restart}
func (h *NetcfgHandler) IfaceAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.WANAction(pathID(r), body.Action); err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
