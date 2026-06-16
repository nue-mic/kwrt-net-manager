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
	r.Get("/api/v1/dhcp/service", h.DHCPService)
	r.Post("/api/v1/dhcp/install", h.InstallDHCP)

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

	// 网卡列表 + 内外网设置（LAN/WAN 接口 + 物理网卡）。
	r.Get("/api/v1/nics", h.ListNICs)
	r.Get("/api/v1/netcfg/overview", h.NetOverview)
	r.Get("/api/v1/ifaces", h.ListNetIfaces)
	r.Post("/api/v1/ifaces", h.CreateNetIface)
	r.Get("/api/v1/ifaces/{id}", h.GetNetIface)
	r.Put("/api/v1/ifaces/{id}", h.UpdateNetIface)
	r.Delete("/api/v1/ifaces/{id}", h.DeleteNetIface)
	r.Post("/api/v1/ifaces/{id}/action", h.IfaceAction)

	// IPv6（爱快 IPv6 菜单全套）。
	// 外网（WANv6，odhcp6c 客户端侧）。
	r.Get("/api/v1/ipv6/wan", h.ListWANv6)
	r.Post("/api/v1/ipv6/wan", h.CreateWANv6)
	r.Post("/api/v1/ipv6/wan/batch", h.BatchWANv6)
	r.Get("/api/v1/ipv6/wan/{id}", h.GetWANv6)
	r.Put("/api/v1/ipv6/wan/{id}", h.UpdateWANv6)
	r.Delete("/api/v1/ipv6/wan/{id}", h.DeleteWANv6)
	r.Post("/api/v1/ipv6/wan/{id}/toggle", h.ToggleWANv6)
	r.Post("/api/v1/ipv6/wan/{id}/duid", h.RegenWANv6DUID)
	r.Get("/api/v1/ipv6/transition-pkg", h.TransitionPkgV6)
	// 内网（LANv6，odhcpd RA/DHCPv6 服务端侧）。
	r.Get("/api/v1/ipv6/lan", h.ListLANv6)
	r.Post("/api/v1/ipv6/lan", h.CreateLANv6)
	r.Post("/api/v1/ipv6/lan/batch", h.BatchLANv6)
	r.Get("/api/v1/ipv6/lan/{id}", h.GetLANv6)
	r.Put("/api/v1/ipv6/lan/{id}", h.UpdateLANv6)
	r.Delete("/api/v1/ipv6/lan/{id}", h.DeleteLANv6)
	r.Post("/api/v1/ipv6/lan/{id}/toggle", h.ToggleLANv6)
	// DHCPv6 终端（只读）。
	r.Get("/api/v1/ipv6/leases", h.ListLeasesV6)
	// 前缀静态分配。
	r.Get("/api/v1/ipv6/prefix-static", h.ListPrefixStaticsV6)
	r.Post("/api/v1/ipv6/prefix-static", h.CreatePrefixStaticV6)
	r.Post("/api/v1/ipv6/prefix-static/batch", h.BatchPrefixStaticsV6)
	r.Put("/api/v1/ipv6/prefix-static/{id}", h.UpdatePrefixStaticV6)
	r.Delete("/api/v1/ipv6/prefix-static/{id}", h.DeletePrefixStaticV6)
	r.Post("/api/v1/ipv6/prefix-static/{id}/toggle", h.TogglePrefixStaticV6)
	// DHCPv6 接入控制（黑白名单）。
	r.Get("/api/v1/ipv6/acl", h.GetACLv6)
	r.Put("/api/v1/ipv6/acl/mode", h.SetACLv6Mode)
	r.Post("/api/v1/ipv6/acl/entries", h.AddACLv6Entry)
	r.Put("/api/v1/ipv6/acl/entries/{id}", h.UpdateACLv6Entry)
	r.Delete("/api/v1/ipv6/acl/entries/{id}", h.DeleteACLv6Entry)
	r.Post("/api/v1/ipv6/acl/entries/{id}/toggle", h.ToggleACLv6Entry)
	// 邻居列表 / 线路详情 / 服务信息。
	r.Get("/api/v1/ipv6/neighbors", h.ListNeighborsV6)
	r.Delete("/api/v1/ipv6/neighbors", h.DeleteNeighborV6)
	r.Post("/api/v1/ipv6/neighbors/flush", h.FlushNeighborsV6)
	r.Get("/api/v1/ipv6/lines", h.ListLinesV6)
	r.Get("/api/v1/ipv6/service", h.DHCPv6Service)
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
