package api

import (
	"net/http"

	"github.com/nue-mic/kwrt-net-manager/internal/netcfg"
)

// IPv6 子树的 HTTP handler（爱快 IPv6 菜单全套）。请求/响应体一律 snake_case，
// decodeJSON 启用 DisallowUnknownFields；编辑用 Omit 只读字段的输入类型。

type v6ModeReq struct {
	Mode string `json:"mode"`
}
type neighborDelReq struct {
	Addr string `json:"addr"`
	Dev  string `json:"dev"`
}
type neighborFlushReq struct {
	Dev string `json:"dev"`
}

// ---- WANv6（IPv6 外网） ----

func (h *NetcfgHandler) ListWANv6(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListWANv6()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.WANv6{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *NetcfgHandler) GetWANv6(w http.ResponseWriter, r *http.Request) {
	item, err := h.svc.GetWANv6(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, item)
}

func (h *NetcfgHandler) CreateWANv6(w http.ResponseWriter, r *http.Request) {
	var in netcfg.WANv6
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreateWANv6(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) UpdateWANv6(w http.ResponseWriter, r *http.Request) {
	var in netcfg.WANv6
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateWANv6(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) DeleteWANv6(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteWANv6(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) ToggleWANv6(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetWANv6Enabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) BatchWANv6(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchWANv6(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) RegenWANv6DUID(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.RegenWANv6DUID(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) TransitionPkgV6(w http.ResponseWriter, r *http.Request) {
	ok, pkg, err := h.svc.TransitionPkg(r.URL.Query().Get("proto"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"installed": ok, "pkg": pkg})
}

// ---- LANv6（IPv6 内网） ----

func (h *NetcfgHandler) ListLANv6(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListLANv6()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.LANv6{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *NetcfgHandler) GetLANv6(w http.ResponseWriter, r *http.Request) {
	item, err := h.svc.GetLANv6(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, item)
}

func (h *NetcfgHandler) CreateLANv6(w http.ResponseWriter, r *http.Request) {
	var in netcfg.LANv6
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreateLANv6(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) UpdateLANv6(w http.ResponseWriter, r *http.Request) {
	var in netcfg.LANv6
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateLANv6(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) DeleteLANv6(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteLANv6(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) ToggleLANv6(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetLANv6Enabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) BatchLANv6(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchLANv6(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- DHCPv6 终端（只读） ----

func (h *NetcfgHandler) ListLeasesV6(w http.ResponseWriter, r *http.Request) {
	f := netcfg.LeaseFilter{
		Interface: r.URL.Query().Get("interface"),
		Query:     r.URL.Query().Get("query"),
	}
	items, err := h.svc.ListLeasesV6(f)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.LeaseV6{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "family": "ipv6"})
}

// ---- 前缀静态分配 ----

func (h *NetcfgHandler) ListPrefixStaticsV6(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPrefixStaticsV6()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.PrefixStaticV6{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *NetcfgHandler) CreatePrefixStaticV6(w http.ResponseWriter, r *http.Request) {
	var in netcfg.PrefixStaticV6
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreatePrefixStaticV6(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) UpdatePrefixStaticV6(w http.ResponseWriter, r *http.Request) {
	var in netcfg.PrefixStaticV6
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdatePrefixStaticV6(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) DeletePrefixStaticV6(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeletePrefixStaticV6(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) TogglePrefixStaticV6(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetPrefixStaticV6Enabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) BatchPrefixStaticsV6(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchPrefixStaticsV6(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- DHCPv6 接入控制（黑白名单） ----

func (h *NetcfgHandler) GetACLv6(w http.ResponseWriter, r *http.Request) {
	acl, err := h.svc.GetACLv6()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, acl)
}

func (h *NetcfgHandler) SetACLv6Mode(w http.ResponseWriter, r *http.Request) {
	var body v6ModeReq
	if !decodeJSON(w, r, &body) {
		return
	}
	acl, err := h.svc.SetACLv6Mode(body.Mode)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, acl)
}

func (h *NetcfgHandler) AddACLv6Entry(w http.ResponseWriter, r *http.Request) {
	var in netcfg.ACLv6Entry
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.AddACLv6Entry(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) UpdateACLv6Entry(w http.ResponseWriter, r *http.Request) {
	var in netcfg.ACLv6Entry
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateACLv6Entry(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *NetcfgHandler) DeleteACLv6Entry(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteACLv6Entry(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) ToggleACLv6Entry(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ToggleACLv6Entry(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// ---- 邻居列表 / 线路详情 / 服务信息 ----

func (h *NetcfgHandler) ListNeighborsV6(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListNeighborsV6()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.NeighborV6{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *NetcfgHandler) DeleteNeighborV6(w http.ResponseWriter, r *http.Request) {
	var body neighborDelReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.DeleteNeighborV6(body.Addr, body.Dev); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) FlushNeighborsV6(w http.ResponseWriter, r *http.Request) {
	var body neighborFlushReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.FlushNeighborsV6(body.Dev); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *NetcfgHandler) ListLinesV6(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListLinesV6()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.LineV6{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *NetcfgHandler) DHCPv6Service(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.DHCPv6ServiceInfo()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, info)
}
