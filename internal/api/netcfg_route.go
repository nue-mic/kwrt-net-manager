package api

import (
	"net/http"

	"github.com/nue-mic/kwrt-net-manager/internal/netcfg"
)

// ListRoutes GET /api/v1/routes
func (h *NetcfgHandler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListRoutes()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.Route{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetRoute GET /api/v1/routes/{id}
func (h *NetcfgHandler) GetRoute(w http.ResponseWriter, r *http.Request) {
	rt, err := h.svc.GetRoute(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, rt)
}

// CreateRoute POST /api/v1/routes
func (h *NetcfgHandler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	var in netcfg.Route
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.CreateRoute(in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// UpdateRoute PUT /api/v1/routes/{id}
func (h *NetcfgHandler) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	var in netcfg.Route
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateRoute(pathID(r), in)
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// DeleteRoute DELETE /api/v1/routes/{id}
func (h *NetcfgHandler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteRoute(pathID(r)); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": pathID(r)})
}

// ToggleRoute POST /api/v1/routes/{id}/toggle {enabled}
func (h *NetcfgHandler) ToggleRoute(w http.ResponseWriter, r *http.Request) {
	var body toggleReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.SetRouteEnabled(pathID(r), body.Enabled); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DuplicateRoute POST /api/v1/routes/{id}/duplicate
func (h *NetcfgHandler) DuplicateRoute(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.DuplicateRoute(pathID(r))
	if err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, out)
}

// BatchRoutes POST /api/v1/routes/batch {action, ids}
func (h *NetcfgHandler) BatchRoutes(w http.ResponseWriter, r *http.Request) {
	var body batchReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.svc.BatchRoutes(body.Action, body.IDs); err != nil {
		h.writeNetErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
