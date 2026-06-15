package api

import (
	"net/http"

	"github.com/mia-clark/kwrt-net-manager/internal/netcfg"
)

// ================= DHCP servers =================

// ListServers GET /api/v1/dhcp/servers
func (h *NetcfgHandler) ListServers(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListDHCPServers()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.DHCPServer{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetServer GET /api/v1/dhcp/servers/{id}
func (h *NetcfgHandler) GetServer(w http.ResponseWriter, r *http.Request) {
	srv, err := h.svc.GetDHCPServer(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, srv)
}

// CreateServer POST /api/v1/dhcp/servers
func (h *NetcfgHandler) CreateServer(w http.ResponseWriter, r *http.Request) {
	var in netcfg.DHCPServer
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreateDHCPServer(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// UpdateServer PUT /api/v1/dhcp/servers/{id}
func (h *NetcfgHandler) UpdateServer(w http.ResponseWriter, r *http.Request) {
	var in netcfg.DHCPServer
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateDHCPServer(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// DeleteServer DELETE /api/v1/dhcp/servers/{id}
func (h *NetcfgHandler) DeleteServer(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteDHCPServer(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": pathID(r)})
}

// ToggleServer POST /api/v1/dhcp/servers/{id}/toggle {enabled}
func (h *NetcfgHandler) ToggleServer(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetDHCPServerEnabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// BatchServers POST /api/v1/dhcp/servers/batch {action, ids}
func (h *NetcfgHandler) BatchServers(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchDHCPServers(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RestartDHCP POST /api/v1/dhcp/restart
func (h *NetcfgHandler) RestartDHCP(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RestartDHCP(); err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, "重启 DHCP 服务失败："+err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ================= static reservations =================

// ListStatics GET /api/v1/dhcp/statics
func (h *NetcfgHandler) ListStatics(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListStatics()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.StaticLease{}
	}
	arp, _ := h.svc.GetARPBind()
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "arp_bind": arp})
}

// CreateStatic POST /api/v1/dhcp/statics
func (h *NetcfgHandler) CreateStatic(w http.ResponseWriter, r *http.Request) {
	var in netcfg.StaticLease
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreateStatic(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// UpdateStatic PUT /api/v1/dhcp/statics/{id}
func (h *NetcfgHandler) UpdateStatic(w http.ResponseWriter, r *http.Request) {
	var in netcfg.StaticLease
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateStatic(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// DeleteStatic DELETE /api/v1/dhcp/statics/{id}
func (h *NetcfgHandler) DeleteStatic(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteStatic(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": pathID(r)})
}

// ToggleStatic POST /api/v1/dhcp/statics/{id}/toggle {enabled}
func (h *NetcfgHandler) ToggleStatic(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetStaticEnabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// BatchStatics POST /api/v1/dhcp/statics/batch {action, ids}
func (h *NetcfgHandler) BatchStatics(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchStatics(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// SetARPBind PUT /api/v1/dhcp/statics/arp-bind {enabled}
func (h *NetcfgHandler) SetARPBind(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetARPBind(body.Enabled); err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"arp_bind": body.Enabled})
}

// ================= leases (terminal list) =================

// ListLeases GET /api/v1/dhcp/leases?interface=&status=&q=
func (h *NetcfgHandler) ListLeases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := h.svc.ListLeases(netcfg.LeaseFilter{
		Interface: q.Get("interface"),
		Status:    q.Get("status"),
		Query:     q.Get("q"),
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.Lease{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ReserveLease POST /api/v1/dhcp/leases/reserve {ip,mac,hostname,interface}
func (h *NetcfgHandler) ReserveLease(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IP        string `json:"ip"`
		MAC       string `json:"mac"`
		Hostname  string `json:"hostname"`
		Interface string `json:"interface"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := h.svc.ReserveLease(body.IP, body.MAC, body.Hostname, body.Interface)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// BlacklistLease POST /api/v1/dhcp/leases/blacklist {mac,remark}
func (h *NetcfgHandler) BlacklistLease(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MAC    string `json:"mac"`
		Remark string `json:"remark"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := h.svc.BlacklistMAC(body.MAC, body.Remark)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// FixSubnet POST /api/v1/dhcp/leases/fix-subnet {interface}
func (h *NetcfgHandler) FixSubnet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface string `json:"interface"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	added, err := h.svc.FixSubnet(body.Interface)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"added": added})
}

// ================= MAC ACL =================

// GetACL GET /api/v1/dhcp/acl
func (h *NetcfgHandler) GetACL(w http.ResponseWriter, r *http.Request) {
	acl, err := h.svc.GetACL()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, acl)
}

// SetACLMode PUT /api/v1/dhcp/acl/mode {mode}
func (h *NetcfgHandler) SetACLMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	acl, err := h.svc.SetACLMode(body.Mode)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, acl)
}

// AddACLEntry POST /api/v1/dhcp/acl/entries
func (h *NetcfgHandler) AddACLEntry(w http.ResponseWriter, r *http.Request) {
	var in netcfg.ACLEntry
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.AddACLEntry(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// UpdateACLEntry PUT /api/v1/dhcp/acl/entries/{id}
func (h *NetcfgHandler) UpdateACLEntry(w http.ResponseWriter, r *http.Request) {
	var in netcfg.ACLEntry
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateACLEntry(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// DeleteACLEntry DELETE /api/v1/dhcp/acl/entries/{id}
func (h *NetcfgHandler) DeleteACLEntry(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteACLEntry(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": pathID(r)})
}

// ToggleACLEntry POST /api/v1/dhcp/acl/entries/{id}/toggle
func (h *NetcfgHandler) ToggleACLEntry(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ToggleACLEntry(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}
