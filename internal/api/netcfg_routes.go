package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mia-clark/kwrt-net-manager/internal/netcfg"
)

// NetcfgHandler serves the DHCP + static-routing endpoints on top of the
// netcfg.Service. All request/response bodies are snake_case, matching the Go
// structs field-for-field (decodeJSON rejects unknown keys).
type NetcfgHandler struct {
	svc *netcfg.Service
	log *slog.Logger
}

// NewNetcfgHandler wires the handler.
func NewNetcfgHandler(svc *netcfg.Service, log *slog.Logger) *NetcfgHandler {
	return &NetcfgHandler{svc: svc, log: log}
}

// registerNetcfgRoutes mounts the DHCP + static-routing endpoints on the
// authenticated subtree.
func registerNetcfgRoutes(r chi.Router, d Deps) {
	if d.Net == nil {
		return
	}
	h := NewNetcfgHandler(d.Net, d.Logger)

	// Dropdowns + service status.
	r.Get("/api/v1/interfaces", h.Interfaces)
	r.Get("/api/v1/netcfg/status", h.Status)

	// DHCP servers.
	r.Get("/api/v1/dhcp/servers", h.ListServers)
	r.Post("/api/v1/dhcp/servers", h.CreateServer)
	r.Post("/api/v1/dhcp/servers/batch", h.BatchServers)
	r.Get("/api/v1/dhcp/servers/{id}", h.GetServer)
	r.Put("/api/v1/dhcp/servers/{id}", h.UpdateServer)
	r.Delete("/api/v1/dhcp/servers/{id}", h.DeleteServer)
	r.Post("/api/v1/dhcp/servers/{id}/toggle", h.ToggleServer)
	r.Post("/api/v1/dhcp/restart", h.RestartDHCP)

	// Static reservations.
	r.Get("/api/v1/dhcp/statics", h.ListStatics)
	r.Post("/api/v1/dhcp/statics", h.CreateStatic)
	r.Post("/api/v1/dhcp/statics/batch", h.BatchStatics)
	r.Put("/api/v1/dhcp/statics/arp-bind", h.SetARPBind)
	r.Put("/api/v1/dhcp/statics/{id}", h.UpdateStatic)
	r.Delete("/api/v1/dhcp/statics/{id}", h.DeleteStatic)
	r.Post("/api/v1/dhcp/statics/{id}/toggle", h.ToggleStatic)

	// Active leases (terminal list) + actions.
	r.Get("/api/v1/dhcp/leases", h.ListLeases)
	r.Post("/api/v1/dhcp/leases/reserve", h.ReserveLease)
	r.Post("/api/v1/dhcp/leases/blacklist", h.BlacklistLease)
	r.Post("/api/v1/dhcp/leases/fix-subnet", h.FixSubnet)

	// MAC access-control list.
	r.Get("/api/v1/dhcp/acl", h.GetACL)
	r.Put("/api/v1/dhcp/acl/mode", h.SetACLMode)
	r.Post("/api/v1/dhcp/acl/entries", h.AddACLEntry)
	r.Put("/api/v1/dhcp/acl/entries/{id}", h.UpdateACLEntry)
	r.Delete("/api/v1/dhcp/acl/entries/{id}", h.DeleteACLEntry)
	r.Post("/api/v1/dhcp/acl/entries/{id}/toggle", h.ToggleACLEntry)

	// Static routes.
	r.Get("/api/v1/routes", h.ListRoutes)
	r.Post("/api/v1/routes", h.CreateRoute)
	r.Post("/api/v1/routes/batch", h.BatchRoutes)
	r.Get("/api/v1/routes/{id}", h.GetRoute)
	r.Put("/api/v1/routes/{id}", h.UpdateRoute)
	r.Delete("/api/v1/routes/{id}", h.DeleteRoute)
	r.Post("/api/v1/routes/{id}/toggle", h.ToggleRoute)
	r.Post("/api/v1/routes/{id}/duplicate", h.DuplicateRoute)

	// Live kernel routing table.
	r.Get("/api/v1/route-table", h.RouteTable)
}

// writeNetErr maps netcfg errors to HTTP responses: ErrNotFound → 404, any
// other (validation) error → 400 with the message.
func (h *NetcfgHandler) writeNetErr(w http.ResponseWriter, err error) {
	if errors.Is(err, netcfg.ErrNotFound) {
		WriteError(w, http.StatusNotFound, CodeNotFound, "资源不存在", nil)
		return
	}
	WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
}

// ---- common: interfaces / status / route table ----

// Interfaces GET /api/v1/interfaces — dropdown source for 服务接口 / 线路.
func (h *NetcfgHandler) Interfaces(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Interfaces()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Status GET /api/v1/netcfg/status — backend kind + DHCP health + pending flag.
func (h *NetcfgHandler) Status(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Status()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, st)
}

// RouteTable GET /api/v1/route-table?family=ipv4|ipv6 — live kernel routes.
func (h *NetcfgHandler) RouteTable(w http.ResponseWriter, r *http.Request) {
	family := r.URL.Query().Get("family")
	if family == "" {
		family = netcfg.FamilyIPv4
	}
	items, err := h.svc.RouteTable(family)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal, err.Error(), nil)
		return
	}
	if items == nil {
		items = []netcfg.RouteEntry{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "family": family})
}

// ---- shared request bodies ----

type toggleReq struct {
	Enabled bool `json:"enabled"`
}

type batchReq struct {
	Action string   `json:"action"` // enable | disable | delete
	IDs    []string `json:"ids"`
}
