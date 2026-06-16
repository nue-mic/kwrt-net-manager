package netcfg

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/mia-clark/kwrt-net-manager/internal/eventbus"
	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// ErrNotFound is returned when an id does not resolve to an item.
var ErrNotFound = errors.New("not found")

// Service is the network-config domain API the HTTP layer drives. It adds
// validation, id generation and event publishing on top of a Backend, and
// serializes every read-modify-write so two concurrent edits cannot clobber
// each other (mirroring the uci staging caveat from the research).
type Service struct {
	be   Backend
	bus  *eventbus.Bus
	log  *slog.Logger
	mu   sync.Mutex
	idFn func(prefix string) string
}

// NewService wires a Service. bus/log may be nil.
func NewService(be Backend, bus *eventbus.Bus, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{be: be, bus: bus, log: log, idFn: cryptoID}
}

// Backend kind passthrough.
func (s *Service) Kind() string { return s.be.Kind() }

func cryptoID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}

func (s *Service) publish(t eventbus.EventType, action string, count int) {
	if s.bus != nil {
		s.bus.Publish(t, "", eventbus.NetChangeData{Action: action, Count: count})
	}
}

// ---- interfaces / status ----

// Interfaces lists the L3 interfaces usable as DHCP/route targets.
func (s *Service) Interfaces() ([]Interface, error) { return s.be.Interfaces() }

// Status reports backend kind + health.
func (s *Service) Status() (Status, error) { return s.be.Status() }

// ================= DHCP servers =================

// ListDHCPServers returns every pool with its computed remaining-address count.
func (s *Service) ListDHCPServers() ([]DHCPServer, error) {
	servers, err := s.be.DHCPServers()
	if err != nil {
		return nil, err
	}
	leases, _ := s.be.Leases()
	for i := range servers {
		servers[i].Remaining = remainingAddrs(servers[i], leases)
	}
	return servers, nil
}

// remainingAddrs = pool size minus the leases currently inside the pool range.
func remainingAddrs(srv DHCPServer, leases []Lease) int {
	total, ok := netutil.RangeCount(srv.IPStart, srv.IPEnd)
	if !ok {
		return 0
	}
	used := 0
	for _, l := range leases {
		if netutil.IPInRange(l.IP, srv.IPStart, srv.IPEnd) {
			used++
		}
	}
	if used > total {
		used = total
	}
	return total - used
}

// GetDHCPServer returns one pool by id.
func (s *Service) GetDHCPServer(id string) (DHCPServer, error) {
	servers, err := s.ListDHCPServers()
	if err != nil {
		return DHCPServer{}, err
	}
	for _, srv := range servers {
		if srv.ID == id {
			return srv, nil
		}
	}
	return DHCPServer{}, ErrNotFound
}

// checkPoolSubnet enforces that a pool's address range lies inside its bound
// interface's subnet, and fills in the authoritative (interface) netmask. A
// dnsmasq pool has no independent netmask — start/limit are always offsets into
// the interface network — so an out-of-subnet range would otherwise be silently
// truncated. If the interface can't be resolved (e.g. the store/dev backend),
// the check is skipped rather than blocking.
func (s *Service) checkPoolSubnet(in *DHCPServer) error {
	ifaces, err := s.be.Interfaces()
	if err != nil {
		return nil
	}
	for _, i := range ifaces {
		if i.Name != in.Interface {
			continue
		}
		if i.IPv4 == "" || i.Netmask == "" {
			return nil
		}
		if !netutil.SameSubnet(in.IPStart, i.IPv4, i.Netmask) || !netutil.SameSubnet(in.IPEnd, i.IPv4, i.Netmask) {
			return fmt.Errorf("客户端地址 %s-%s 不在接口 %s 的子网（%s / %s）内：DHCP 池必须落在接口网段内；如需更换网段，请先到「内外网设置」修改该接口的 IP/掩码",
				in.IPStart, in.IPEnd, in.Interface, i.IPv4, i.Netmask)
		}
		in.Netmask = i.Netmask // 接口掩码为权威值，回填以保持一致
		return nil
	}
	return nil
}

// CreateDHCPServer validates + persists a new pool.
func (s *Service) CreateDHCPServer(in DHCPServer) (DHCPServer, error) {
	if err := validateDHCPServer(&in); err != nil {
		return DHCPServer{}, err
	}
	if err := s.checkPoolSubnet(&in); err != nil {
		return DHCPServer{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	servers, err := s.be.DHCPServers()
	if err != nil {
		return DHCPServer{}, err
	}
	in.ID = s.idFn("dhcp")
	in.Managed = true
	in.Remaining = 0
	servers = append(servers, in)
	if err := s.be.SaveDHCPServers(servers); err != nil {
		return DHCPServer{}, err
	}
	s.publish(eventbus.TypeDHCPChanged, "create", len(servers))
	return in, nil
}

// UpdateDHCPServer replaces an existing pool.
func (s *Service) UpdateDHCPServer(id string, in DHCPServer) (DHCPServer, error) {
	if err := validateDHCPServer(&in); err != nil {
		return DHCPServer{}, err
	}
	if err := s.checkPoolSubnet(&in); err != nil {
		return DHCPServer{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	servers, err := s.be.DHCPServers()
	if err != nil {
		return DHCPServer{}, err
	}
	idx := indexByID(len(servers), func(i int) string { return servers[i].ID }, id)
	if idx < 0 {
		return DHCPServer{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	servers[idx] = in
	if err := s.be.SaveDHCPServers(servers); err != nil {
		return DHCPServer{}, err
	}
	s.publish(eventbus.TypeDHCPChanged, "update", len(servers))
	return in, nil
}

// DeleteDHCPServer removes a pool by id.
func (s *Service) DeleteDHCPServer(id string) error {
	return s.mutateDHCP("delete", func(servers []DHCPServer) ([]DHCPServer, error) {
		out, removed := dropByID(servers, func(x DHCPServer) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

// SetDHCPServerEnabled toggles a pool's enabled flag.
func (s *Service) SetDHCPServerEnabled(id string, on bool) error {
	return s.mutateDHCP("toggle", func(servers []DHCPServer) ([]DHCPServer, error) {
		idx := indexByID(len(servers), func(i int) string { return servers[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		servers[idx].Enabled = on
		servers[idx].Managed = true
		return servers, nil
	})
}

// BatchDHCPServers applies enable/disable/delete to many ids at once.
func (s *Service) BatchDHCPServers(action string, ids []string) error {
	set := toSet(ids)
	return s.mutateDHCP(action, func(servers []DHCPServer) ([]DHCPServer, error) {
		switch action {
		case "enable", "disable":
			on := action == "enable"
			for i := range servers {
				if set[servers[i].ID] {
					servers[i].Enabled = on
					servers[i].Managed = true
				}
			}
			return servers, nil
		case "delete":
			out := servers[:0:0]
			for _, x := range servers {
				if !set[x.ID] {
					out = append(out, x)
				}
			}
			return out, nil
		default:
			return nil, errors.New("不支持的批量操作")
		}
	})
}

func (s *Service) mutateDHCP(action string, fn func([]DHCPServer) ([]DHCPServer, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	servers, err := s.be.DHCPServers()
	if err != nil {
		return err
	}
	next, err := fn(servers)
	if err != nil {
		return err
	}
	if err := s.be.SaveDHCPServers(next); err != nil {
		return err
	}
	s.publish(eventbus.TypeDHCPChanged, action, len(next))
	return nil
}

// RestartDHCP restarts the DHCP service (iKuai 重启DHCP服务).
func (s *Service) RestartDHCP() error {
	if err := s.be.RestartDHCP(); err != nil {
		return err
	}
	s.publish(eventbus.TypeDHCPChanged, "apply", 0)
	return nil
}

// ================= static reservations =================

// ListStatics returns all reservations.
func (s *Service) ListStatics() ([]StaticLease, error) { return s.be.Statics() }

// GetARPBind reports the global ARP-bind toggle.
func (s *Service) GetARPBind() (bool, error) { return s.be.ARPBind() }

// GetStatic returns one reservation by id.
func (s *Service) GetStatic(id string) (StaticLease, error) {
	list, err := s.be.Statics()
	if err != nil {
		return StaticLease{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return StaticLease{}, ErrNotFound
}

// CreateStatic validates + persists a new reservation. IP and MAC must be unique
// across reservations (a duplicate would crash dnsmasq on the uci backend).
func (s *Service) CreateStatic(in StaticLease) (StaticLease, error) {
	if err := validateStatic(&in); err != nil {
		return StaticLease{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, arp, err := s.staticsAndARP()
	if err != nil {
		return StaticLease{}, err
	}
	if err := ensureStaticUnique(list, in, ""); err != nil {
		return StaticLease{}, err
	}
	in.ID = s.idFn("host")
	in.Managed = true
	list = append(list, in)
	if err := s.be.SaveStatics(list, arp); err != nil {
		return StaticLease{}, err
	}
	s.publish(eventbus.TypeStaticChanged, "create", len(list))
	return in, nil
}

// UpdateStatic replaces a reservation.
func (s *Service) UpdateStatic(id string, in StaticLease) (StaticLease, error) {
	if err := validateStatic(&in); err != nil {
		return StaticLease{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, arp, err := s.staticsAndARP()
	if err != nil {
		return StaticLease{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return StaticLease{}, ErrNotFound
	}
	if err := ensureStaticUnique(list, in, id); err != nil {
		return StaticLease{}, err
	}
	in.ID = id
	in.Managed = true
	list[idx] = in
	if err := s.be.SaveStatics(list, arp); err != nil {
		return StaticLease{}, err
	}
	s.publish(eventbus.TypeStaticChanged, "update", len(list))
	return in, nil
}

// DeleteStatic removes a reservation by id.
func (s *Service) DeleteStatic(id string) error {
	return s.mutateStatics("delete", func(list []StaticLease) ([]StaticLease, error) {
		out, removed := dropByID(list, func(x StaticLease) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

// SetStaticEnabled toggles a reservation.
func (s *Service) SetStaticEnabled(id string, on bool) error {
	return s.mutateStatics("toggle", func(list []StaticLease) ([]StaticLease, error) {
		idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		list[idx].Enabled = on
		list[idx].Managed = true
		return list, nil
	})
}

// BatchStatics applies enable/disable/delete to many ids.
func (s *Service) BatchStatics(action string, ids []string) error {
	set := toSet(ids)
	return s.mutateStatics(action, func(list []StaticLease) ([]StaticLease, error) {
		switch action {
		case "enable", "disable":
			on := action == "enable"
			for i := range list {
				if set[list[i].ID] {
					list[i].Enabled = on
					list[i].Managed = true
				}
			}
			return list, nil
		case "delete":
			out := list[:0:0]
			for _, x := range list {
				if !set[x.ID] {
					out = append(out, x)
				}
			}
			return out, nil
		default:
			return nil, errors.New("不支持的批量操作")
		}
	})
}

// SetARPBind sets the global ARP-bind toggle (keeps reservations unchanged).
func (s *Service) SetARPBind(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, _, err := s.staticsAndARP()
	if err != nil {
		return err
	}
	if err := s.be.SaveStatics(list, on); err != nil {
		return err
	}
	s.publish(eventbus.TypeStaticChanged, "update", len(list))
	return nil
}

func (s *Service) staticsAndARP() ([]StaticLease, bool, error) {
	list, err := s.be.Statics()
	if err != nil {
		return nil, false, err
	}
	arp, err := s.be.ARPBind()
	if err != nil {
		return nil, false, err
	}
	return list, arp, nil
}

func (s *Service) mutateStatics(action string, fn func([]StaticLease) ([]StaticLease, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, arp, err := s.staticsAndARP()
	if err != nil {
		return err
	}
	next, err := fn(list)
	if err != nil {
		return err
	}
	if err := s.be.SaveStatics(next, arp); err != nil {
		return err
	}
	s.publish(eventbus.TypeStaticChanged, action, len(next))
	return nil
}

// ensureStaticUnique rejects a reservation whose IP or MAC collides with another
// (excluding the one with id excludeID, for updates).
func ensureStaticUnique(list []StaticLease, in StaticLease, excludeID string) error {
	for _, x := range list {
		if x.ID == excludeID {
			continue
		}
		if strings.EqualFold(x.MAC, in.MAC) {
			return errors.New("该 MAC 已被其它静态分配占用")
		}
		if x.IP == in.IP {
			return errors.New("该 IP 已被其它静态分配占用")
		}
	}
	return nil
}

// ================= leases (read-only + actions) =================

// LeaseFilter narrows the terminal list.
type LeaseFilter struct {
	Interface string // "" = all
	Status    string // "static" | "dynamic" | ""
	Query     string // substring over hostname/ip/mac/remark
}

// ListLeases returns active leases, annotated (static/remark) and filtered.
func (s *Service) ListLeases(f LeaseFilter) ([]Lease, error) {
	leases, err := s.be.Leases()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	out := leases[:0:0]
	for _, l := range leases {
		if f.Interface != "" && l.Interface != f.Interface {
			continue
		}
		if f.Status == "static" && !l.Static {
			continue
		}
		if f.Status == "dynamic" && l.Static {
			continue
		}
		if q != "" && !leaseMatches(l, q) {
			continue
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out, nil
}

func leaseMatches(l Lease, q string) bool {
	return strings.Contains(strings.ToLower(l.Hostname), q) ||
		strings.Contains(strings.ToLower(l.IP), q) ||
		strings.Contains(strings.ToLower(l.MAC), q) ||
		strings.Contains(strings.ToLower(l.Remark), q)
}

// ReserveLease promotes an active lease to a static reservation (iKuai 加入静态分配).
func (s *Service) ReserveLease(ip, mac, hostname, iface string) (StaticLease, error) {
	in := StaticLease{Hostname: hostname, IP: ip, MAC: mac, Interface: iface, Enabled: true}
	return s.CreateStatic(in)
}

// BlacklistMAC adds a MAC to the DHCP ACL (iKuai 加入MAC黑名单).
func (s *Service) BlacklistMAC(mac, remark string) (ACLEntry, error) {
	e := ACLEntry{MAC: mac, Remark: remark, Enabled: true}
	return s.AddACLEntry(e)
}

// FixSubnet reserves every currently-dynamic lease on an interface (iKuai
// 一键固定同网段). Returns the number of reservations added.
func (s *Service) FixSubnet(iface string) (int, error) {
	leases, err := s.be.Leases()
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, arp, err := s.staticsAndARP()
	if err != nil {
		return 0, err
	}
	added := 0
	for _, l := range leases {
		if l.Static || (iface != "" && l.Interface != iface) {
			continue
		}
		cand := StaticLease{Hostname: l.Hostname, IP: l.IP, MAC: l.MAC, Interface: l.Interface, Remark: l.Remark, Enabled: true}
		if validateStatic(&cand) != nil {
			continue
		}
		if ensureStaticUnique(list, cand, "") != nil {
			continue
		}
		cand.ID = s.idFn("host")
		cand.Managed = true
		list = append(list, cand)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	if err := s.be.SaveStatics(list, arp); err != nil {
		return 0, err
	}
	s.publish(eventbus.TypeStaticChanged, "create", len(list))
	return added, nil
}

// ================= ACL =================

// GetACL returns the MAC access-control list.
func (s *Service) GetACL() (ACL, error) {
	acl, err := s.be.ACL()
	if err != nil {
		return ACL{}, err
	}
	if acl.Mode == "" {
		acl.Mode = ACLBlacklist
	}
	if acl.Entries == nil {
		acl.Entries = []ACLEntry{}
	}
	return acl, nil
}

// SetACLMode switches between blacklist and whitelist.
func (s *Service) SetACLMode(mode string) (ACL, error) {
	if mode != ACLBlacklist && mode != ACLWhitelist {
		return ACL{}, errors.New("模式必须是 blacklist 或 whitelist")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACL()
	if err != nil {
		return ACL{}, err
	}
	acl.Mode = mode
	if err := s.be.SaveACL(acl); err != nil {
		return ACL{}, err
	}
	s.publish(eventbus.TypeACLChanged, "update", len(acl.Entries))
	return acl, nil
}

// AddACLEntry appends a MAC entry.
func (s *Service) AddACLEntry(in ACLEntry) (ACLEntry, error) {
	if err := validateACLEntry(&in); err != nil {
		return ACLEntry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACL()
	if err != nil {
		return ACLEntry{}, err
	}
	for _, e := range acl.Entries {
		if strings.EqualFold(e.MAC, in.MAC) {
			return ACLEntry{}, errors.New("该 MAC 已在名单中")
		}
	}
	in.ID = s.idFn("acl")
	in.Managed = true
	acl.Entries = append(acl.Entries, in)
	if err := s.be.SaveACL(acl); err != nil {
		return ACLEntry{}, err
	}
	s.publish(eventbus.TypeACLChanged, "create", len(acl.Entries))
	return in, nil
}

// UpdateACLEntry replaces a MAC entry.
func (s *Service) UpdateACLEntry(id string, in ACLEntry) (ACLEntry, error) {
	if err := validateACLEntry(&in); err != nil {
		return ACLEntry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACL()
	if err != nil {
		return ACLEntry{}, err
	}
	idx := indexByID(len(acl.Entries), func(i int) string { return acl.Entries[i].ID }, id)
	if idx < 0 {
		return ACLEntry{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	acl.Entries[idx] = in
	if err := s.be.SaveACL(acl); err != nil {
		return ACLEntry{}, err
	}
	s.publish(eventbus.TypeACLChanged, "update", len(acl.Entries))
	return in, nil
}

// DeleteACLEntry removes a MAC entry.
func (s *Service) DeleteACLEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACL()
	if err != nil {
		return err
	}
	out, removed := dropByID(acl.Entries, func(x ACLEntry) string { return x.ID }, id)
	if !removed {
		return ErrNotFound
	}
	acl.Entries = out
	if err := s.be.SaveACL(acl); err != nil {
		return err
	}
	s.publish(eventbus.TypeACLChanged, "delete", len(acl.Entries))
	return nil
}

// ToggleACLEntry flips a MAC entry's enabled flag.
func (s *Service) ToggleACLEntry(id string) (ACLEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACL()
	if err != nil {
		return ACLEntry{}, err
	}
	idx := indexByID(len(acl.Entries), func(i int) string { return acl.Entries[i].ID }, id)
	if idx < 0 {
		return ACLEntry{}, ErrNotFound
	}
	acl.Entries[idx].Enabled = !acl.Entries[idx].Enabled
	acl.Entries[idx].Managed = true
	if err := s.be.SaveACL(acl); err != nil {
		return ACLEntry{}, err
	}
	s.publish(eventbus.TypeACLChanged, "toggle", len(acl.Entries))
	return acl.Entries[idx], nil
}

// ================= static routes =================

// ListRoutes returns all static routes.
func (s *Service) ListRoutes() ([]Route, error) { return s.be.Routes() }

// GetRoutePushMode returns the global "push static routes to DHCP clients" mode.
func (s *Service) GetRoutePushMode() (string, error) {
	m, err := s.be.RoutePushMode()
	if err != nil {
		return "", err
	}
	if m == "" {
		m = RoutePushOff
	}
	return m, nil
}

// SetRoutePushMode sets the global push mode ("off" | "all" | "tagged").
func (s *Service) SetRoutePushMode(mode string) error {
	switch mode {
	case RoutePushOff, RoutePushAll, RoutePushTagged:
	default:
		return errors.New("路由下发模式必须是 off / all / tagged")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.be.SaveRoutePushMode(mode); err != nil {
		return err
	}
	s.publish(eventbus.TypeRouteChanged, "update", 0)
	return nil
}

// GetRoute returns one route by id.
func (s *Service) GetRoute(id string) (Route, error) {
	list, err := s.be.Routes()
	if err != nil {
		return Route{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return Route{}, ErrNotFound
}

// CreateRoute validates + persists a new route.
func (s *Service) CreateRoute(in Route) (Route, error) {
	if err := validateRoute(&in); err != nil {
		return Route{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.Routes()
	if err != nil {
		return Route{}, err
	}
	in.ID = s.idFn("route")
	in.Managed = true
	list = append(list, in)
	if err := s.be.SaveRoutes(list); err != nil {
		return Route{}, err
	}
	s.publish(eventbus.TypeRouteChanged, "create", len(list))
	return in, nil
}

// UpdateRoute replaces a route.
func (s *Service) UpdateRoute(id string, in Route) (Route, error) {
	if err := validateRoute(&in); err != nil {
		return Route{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.Routes()
	if err != nil {
		return Route{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return Route{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	list[idx] = in
	if err := s.be.SaveRoutes(list); err != nil {
		return Route{}, err
	}
	s.publish(eventbus.TypeRouteChanged, "update", len(list))
	return in, nil
}

// DeleteRoute removes a route by id.
func (s *Service) DeleteRoute(id string) error {
	return s.mutateRoutes("delete", func(list []Route) ([]Route, error) {
		out, removed := dropByID(list, func(x Route) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

// SetRouteEnabled toggles a route.
func (s *Service) SetRouteEnabled(id string, on bool) error {
	return s.mutateRoutes("toggle", func(list []Route) ([]Route, error) {
		idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		list[idx].Enabled = on
		list[idx].Managed = true
		return list, nil
	})
}

// DuplicateRoute copies a route (iKuai 复制), appending a new disabled-by-default? no — copy as-is.
func (s *Service) DuplicateRoute(id string) (Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.Routes()
	if err != nil {
		return Route{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return Route{}, ErrNotFound
	}
	cp := list[idx]
	cp.ID = s.idFn("route")
	cp.Managed = true
	if cp.Remark != "" {
		cp.Remark += "（副本）"
	}
	list = append(list, cp)
	if err := s.be.SaveRoutes(list); err != nil {
		return Route{}, err
	}
	s.publish(eventbus.TypeRouteChanged, "create", len(list))
	return cp, nil
}

// BatchRoutes applies enable/disable/delete to many ids.
func (s *Service) BatchRoutes(action string, ids []string) error {
	set := toSet(ids)
	return s.mutateRoutes(action, func(list []Route) ([]Route, error) {
		switch action {
		case "enable", "disable":
			on := action == "enable"
			for i := range list {
				if set[list[i].ID] {
					list[i].Enabled = on
					list[i].Managed = true
				}
			}
			return list, nil
		case "delete":
			out := list[:0:0]
			for _, x := range list {
				if !set[x.ID] {
					out = append(out, x)
				}
			}
			return out, nil
		default:
			return nil, errors.New("不支持的批量操作")
		}
	})
}

func (s *Service) mutateRoutes(action string, fn func([]Route) ([]Route, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.Routes()
	if err != nil {
		return err
	}
	next, err := fn(list)
	if err != nil {
		return err
	}
	if err := s.be.SaveRoutes(next); err != nil {
		return err
	}
	s.publish(eventbus.TypeRouteChanged, action, len(next))
	return nil
}

// RouteTable returns the live kernel routing table for family "ipv4"|"ipv6".
func (s *Service) RouteTable(family string) ([]RouteEntry, error) {
	if family == "" {
		family = FamilyIPv4
	}
	return s.be.RouteTable(family)
}

// ================= export / import (satisfies api.NetExporter) =================

// ExportJSON serializes the full managed configuration.
func (s *Service) ExportJSON() ([]byte, error) {
	st := State{}
	var err error
	if st.DHCPServers, err = s.be.DHCPServers(); err != nil {
		return nil, err
	}
	if st.Statics, err = s.be.Statics(); err != nil {
		return nil, err
	}
	if st.ARPBind, err = s.be.ARPBind(); err != nil {
		return nil, err
	}
	if st.ACL, err = s.be.ACL(); err != nil {
		return nil, err
	}
	if st.Routes, err = s.be.Routes(); err != nil {
		return nil, err
	}
	if st.WANv6s, err = s.be.WANv6s(); err != nil {
		return nil, err
	}
	if st.LANv6s, err = s.be.LANv6s(); err != nil {
		return nil, err
	}
	if st.PrefixStaticsV6, err = s.be.PrefixStaticsV6(); err != nil {
		return nil, err
	}
	if st.ACLv6, err = s.be.ACLv6(); err != nil {
		return nil, err
	}
	if st.DNS, err = s.be.DNSSettings(); err != nil {
		return nil, err
	}
	if st.DNSDoH, err = s.be.DNSDoH(); err != nil {
		return nil, err
	}
	if st.DNSRecords, err = s.be.DNSRecords(); err != nil {
		return nil, err
	}
	if st.DNSDomainRoutes, err = s.be.DNSDomainRoutes(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(st, "", "  ")
}

// ImportJSON replaces the managed configuration from an export blob, validating
// every item first. A single invalid item aborts the whole import.
func (s *Service) ImportJSON(raw []byte) error {
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return err
	}
	for i := range st.DHCPServers {
		if err := validateDHCPServer(&st.DHCPServers[i]); err != nil {
			return err
		}
		if st.DHCPServers[i].ID == "" {
			st.DHCPServers[i].ID = s.idFn("dhcp")
		}
	}
	for i := range st.Statics {
		if err := validateStatic(&st.Statics[i]); err != nil {
			return err
		}
		if st.Statics[i].ID == "" {
			st.Statics[i].ID = s.idFn("host")
		}
	}
	for i := range st.Routes {
		if err := validateRoute(&st.Routes[i]); err != nil {
			return err
		}
		if st.Routes[i].ID == "" {
			st.Routes[i].ID = s.idFn("route")
		}
	}
	for i := range st.ACL.Entries {
		if err := validateACLEntry(&st.ACL.Entries[i]); err != nil {
			return err
		}
		if st.ACL.Entries[i].ID == "" {
			st.ACL.Entries[i].ID = s.idFn("acl")
		}
	}
	if st.ACL.Mode == "" {
		st.ACL.Mode = ACLBlacklist
	}
	for i := range st.WANv6s {
		if err := validateWANv6(&st.WANv6s[i]); err != nil {
			return err
		}
		if st.WANv6s[i].ID == "" {
			st.WANv6s[i].ID = s.idFn("wan6")
		}
		st.WANv6s[i].Managed = true
	}
	for i := range st.LANv6s {
		if err := validateLANv6(&st.LANv6s[i]); err != nil {
			return err
		}
		st.LANv6s[i].Managed = true
	}
	for i := range st.PrefixStaticsV6 {
		if err := validatePrefixStaticV6(&st.PrefixStaticsV6[i]); err != nil {
			return err
		}
		if st.PrefixStaticsV6[i].ID == "" {
			st.PrefixStaticsV6[i].ID = s.idFn("ps6")
		}
		st.PrefixStaticsV6[i].Managed = true
	}
	for i := range st.ACLv6.Entries {
		if err := validateACLv6Entry(&st.ACLv6.Entries[i]); err != nil {
			return err
		}
		if st.ACLv6.Entries[i].ID == "" {
			st.ACLv6.Entries[i].ID = s.idFn("aclv6")
		}
		st.ACLv6.Entries[i].Managed = true
	}
	if st.ACLv6.Mode == "" {
		st.ACLv6.Mode = ACLBlacklist
	}

	if err := validateDNSSettings(&st.DNS); err != nil {
		return err
	}
	if err := validateDNSDoH(&st.DNSDoH); err != nil {
		return err
	}
	for i := range st.DNSRecords {
		if err := validateDNSRecord(&st.DNSRecords[i]); err != nil {
			return err
		}
		if st.DNSRecords[i].ID == "" {
			st.DNSRecords[i].ID = s.idFn("dns")
		}
		st.DNSRecords[i].Managed = true
	}
	for i := range st.DNSDomainRoutes {
		if err := validateDNSDomainRoute(&st.DNSDomainRoutes[i]); err != nil {
			return err
		}
		if st.DNSDomainRoutes[i].ID == "" {
			st.DNSDomainRoutes[i].ID = s.idFn("dnsr")
		}
		st.DNSDomainRoutes[i].Managed = true
	}

	for i := range st.DHCPServers {
		st.DHCPServers[i].Managed = true
	}
	for i := range st.Statics {
		st.Statics[i].Managed = true
	}
	for i := range st.Routes {
		st.Routes[i].Managed = true
	}
	for i := range st.ACL.Entries {
		st.ACL.Entries[i].Managed = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.be.SaveDHCPServers(st.DHCPServers); err != nil {
		return err
	}
	if err := s.be.SaveStatics(st.Statics, st.ARPBind); err != nil {
		return err
	}
	if err := s.be.SaveACL(st.ACL); err != nil {
		return err
	}
	if err := s.be.SaveRoutes(st.Routes); err != nil {
		return err
	}
	if err := s.be.SaveWANv6s(st.WANv6s); err != nil {
		return err
	}
	if err := s.be.SaveLANv6s(st.LANv6s); err != nil {
		return err
	}
	if err := s.be.SavePrefixStaticsV6(st.PrefixStaticsV6); err != nil {
		return err
	}
	if err := s.be.SaveACLv6(st.ACLv6); err != nil {
		return err
	}
	if err := s.be.SaveDNSRecords(st.DNSRecords); err != nil {
		return err
	}
	if err := s.be.SaveDNSDomainRoutes(st.DNSDomainRoutes); err != nil {
		return err
	}
	if err := s.be.SaveDNSDoH(st.DNSDoH); err != nil {
		return err
	}
	if err := s.be.SaveDNSSettings(st.DNS); err != nil {
		return err
	}
	s.publish(eventbus.TypeDHCPChanged, "apply", len(st.DHCPServers))
	s.publish(eventbus.TypeRouteChanged, "apply", len(st.Routes))
	s.publish(eventbus.TypeIPv6Changed, "apply", len(st.WANv6s)+len(st.LANv6s))
	return nil
}

// ---- small generic helpers ----

func indexByID(n int, idAt func(int) string, id string) int {
	for i := 0; i < n; i++ {
		if idAt(i) == id {
			return i
		}
	}
	return -1
}

func dropByID[T any](list []T, idOf func(T) string, id string) ([]T, bool) {
	out := list[:0:0]
	removed := false
	for _, x := range list {
		if idOf(x) == id {
			removed = true
			continue
		}
		out = append(out, x)
	}
	return out, removed
}

func toSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
