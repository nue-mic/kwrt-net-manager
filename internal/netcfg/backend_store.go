package netcfg

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/mia-clark/kwrt-net-manager/pkg/netutil"
)

// storeBackend is the development / non-OpenWrt backend. It persists the managed
// State to a JSON file and synthesizes a plausible lease table, interface list
// and routing table so every page works end-to-end without an OpenWrt host.
type storeBackend struct {
	path string
	log  *slog.Logger
	mu   sync.Mutex
	st   State
}

func newStoreBackend(path string, log *slog.Logger) (*storeBackend, error) {
	b := &storeBackend{path: path, log: log}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if e := json.Unmarshal(raw, &b.st); e != nil {
			return nil, fmt.Errorf("parse netcfg.json: %w", e)
		}
	case os.IsNotExist(err):
		b.st = seedState()
		if e := b.flushLocked(); e != nil {
			return nil, e
		}
	default:
		return nil, err
	}
	b.normalize()
	return b, nil
}

func (b *storeBackend) normalize() {
	if b.st.ACL.Mode == "" {
		b.st.ACL.Mode = ACLBlacklist
	}
	if b.st.ACL.Entries == nil {
		b.st.ACL.Entries = []ACLEntry{}
	}
}

func (b *storeBackend) Kind() string { return KindStore }

func (b *storeBackend) flushLocked() error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(b.st, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}

// ---- interfaces / status ----

func (b *storeBackend) Interfaces() ([]Interface, error) {
	return []Interface{
		{Name: "lan", IPv4: "192.168.1.1", Netmask: "255.255.255.0", Prefix: 24, Up: true},
		{Name: "lan2", IPv4: "192.168.2.1", Netmask: "255.255.255.0", Prefix: 24, Up: true},
		{Name: "wan", IPv4: "192.168.0.10", Netmask: "255.255.255.0", Prefix: 24, Up: true},
	}, nil
}

func (b *storeBackend) Status() (Status, error) {
	return Status{Backend: KindStore, DHCPOK: true, Pending: false, Message: "模拟后端（开发/测试）"}, nil
}

// ---- DHCP servers ----

func (b *storeBackend) DHCPServers() ([]DHCPServer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]DHCPServer(nil), b.st.DHCPServers...), nil
}

func (b *storeBackend) SaveDHCPServers(list []DHCPServer) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.DHCPServers = append([]DHCPServer(nil), list...)
	return b.flushLocked()
}

func (b *storeBackend) RestartDHCP() error {
	if b.log != nil {
		b.log.Info("netcfg(store): RestartDHCP (no-op simulation)")
	}
	return nil
}

// ---- statics ----

func (b *storeBackend) Statics() ([]StaticLease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]StaticLease(nil), b.st.Statics...), nil
}

func (b *storeBackend) ARPBind() (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.st.ARPBind, nil
}

func (b *storeBackend) SaveStatics(list []StaticLease, arpBind bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.Statics = append([]StaticLease(nil), list...)
	b.st.ARPBind = arpBind
	return b.flushLocked()
}

// ---- ACL ----

func (b *storeBackend) ACL() (ACL, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return ACL{Mode: b.st.ACL.Mode, Entries: append([]ACLEntry(nil), b.st.ACL.Entries...)}, nil
}

func (b *storeBackend) SaveACL(acl ACL) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.ACL = ACL{Mode: acl.Mode, Entries: append([]ACLEntry(nil), acl.Entries...)}
	return b.flushLocked()
}

// ---- routes ----

func (b *storeBackend) Routes() ([]Route, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Route(nil), b.st.Routes...), nil
}

func (b *storeBackend) SaveRoutes(list []Route) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.Routes = append([]Route(nil), list...)
	return b.flushLocked()
}

// ---- simulated leases ----

// fakeDevices seeds deterministic hostnames for synthesized dynamic leases.
var fakeDevices = []string{
	"iPhone", "MacBook-Air", "Redmi-Note", "ThinkPad-X1", "iPad-Pro",
	"Xiaomi-TV", "PS5", "OnePlus-12", "Switch", "Echo-Dot",
	"DESKTOP-7K2L", "HUAWEI-Mate", "Pixel-8", "SmartCam-01", "Printer-HP",
}

// Leases synthesizes an active-lease table: every static reservation appears as
// a "static" lease, plus a deterministic spread of "dynamic" leases inside each
// enabled pool. Deterministic so dev/test output is stable across reads.
func (b *storeBackend) Leases() ([]Lease, error) {
	b.mu.Lock()
	servers := append([]DHCPServer(nil), b.st.DHCPServers...)
	statics := append([]StaticLease(nil), b.st.Statics...)
	b.mu.Unlock()

	used := map[string]bool{}
	out := []Lease{}

	// Static reservations → static leases (long remaining).
	staticByMAC := map[string]StaticLease{}
	for _, s := range statics {
		staticByMAC[s.MAC] = s
		if !s.Enabled {
			continue
		}
		used[s.IP] = true
		out = append(out, Lease{
			Hostname: orHostname(s.Hostname, "static-"+lastOctet(s.IP)),
			IP:       s.IP, MAC: s.MAC, Expiry: 0, RemainingSeconds: 0,
			Interface: s.Interface, Static: true, Remark: s.Remark,
		})
	}

	// Dynamic leases: a few per enabled pool at spread offsets.
	di := 0
	for _, srv := range servers {
		if !srv.Enabled {
			continue
		}
		startU, ok := netutil.IPv4ToUint32(srv.IPStart)
		if !ok {
			continue
		}
		total, _ := netutil.RangeCount(srv.IPStart, srv.IPEnd)
		n := 6
		if total < n {
			n = total
		}
		step := uint32(7)
		for k := 0; k < n; k++ {
			ipU := startU + uint32(k)*step + 3
			if !netutil.IPInRange(netutil.Uint32ToIPv4(ipU), srv.IPStart, srv.IPEnd) {
				break
			}
			ip := netutil.Uint32ToIPv4(ipU)
			if used[ip] {
				continue
			}
			used[ip] = true
			mac := fmt.Sprintf("02:00:%02X:%02X:%02X:%02X", byte(ipU>>24), byte(ipU>>16), byte(ipU>>8), byte(ipU))
			host := fakeDevices[di%len(fakeDevices)]
			di++
			out = append(out, Lease{
				Hostname: host, IP: ip, MAC: mac,
				Expiry:           1893456000, // fixed future epoch (2030) for stable display
				RemainingSeconds: int64(3600 + k*420),
				Interface:        srv.Interface, Static: false, Remark: "",
			})
		}
	}
	return out, nil
}

// ---- simulated route table ----

func (b *storeBackend) RouteTable(family string) ([]RouteEntry, error) {
	b.mu.Lock()
	routes := append([]Route(nil), b.st.Routes...)
	b.mu.Unlock()

	out := []RouteEntry{}
	if family == FamilyIPv4 {
		// Direct + default routes for the synthetic interfaces.
		out = append(out,
			RouteEntry{Interface: "wan", Target: "0.0.0.0", Netmask: "0.0.0.0", Gateway: "192.168.0.1", Metric: 0},
			RouteEntry{Interface: "lan", Target: "192.168.1.0", Netmask: "255.255.255.0", Gateway: "0.0.0.0", Metric: 0},
			RouteEntry{Interface: "lan2", Target: "192.168.2.0", Netmask: "255.255.255.0", Gateway: "0.0.0.0", Metric: 0},
		)
	}
	for _, r := range routes {
		if !r.Enabled || r.Family != family {
			continue
		}
		dev := r.Interface
		if dev == "auto" || dev == "" {
			dev = "lan"
		}
		mask := r.Netmask
		if family == FamilyIPv6 {
			mask = fmt.Sprintf("/%d", r.Prefix)
		}
		out = append(out, RouteEntry{Interface: dev, Target: r.Target, Netmask: mask, Gateway: r.Gateway, Metric: r.Metric})
	}
	return out, nil
}

// ---- seed ----

func seedState() State {
	return State{
		DHCPServers: []DHCPServer{{
			ID: "dhcp_seedlan", Interface: "lan", Enabled: true,
			IPStart: "192.168.1.100", IPEnd: "192.168.1.200", Netmask: "255.255.255.0",
			Gateway: "192.168.1.1", DNSPrimary: "223.5.5.5", DNSSecondary: "114.114.114.114",
			LeaseMinutes: 120, Exclude: []string{}, ExpiredKeepHours: 0, CheckIP: true,
			RelayOnly: false, AssocInterface: "all", CustomOptions: []CustomOption{},
		}},
		Statics: []StaticLease{{
			ID: "host_seed1", Hostname: "demo-pc", IP: "192.168.1.50", MAC: "AA:BB:CC:00:00:01",
			Gateway: "192.168.1.1", Interface: "lan", DNSPrimary: "223.5.5.5", Remark: "示例：固定办公电脑", Enabled: true,
		}},
		ARPBind: false,
		ACL:     ACL{Mode: ACLBlacklist, Entries: []ACLEntry{}},
		Routes: []Route{{
			ID: "route_seed1", Family: FamilyIPv4, Interface: "auto", Target: "10.0.0.0",
			Netmask: "255.255.255.0", Prefix: 24, Gateway: "192.168.1.2", Metric: 1,
			Remark: "示例：到 10.0.0.0/24 的静态路由", Enabled: true,
		}},
	}
}

func orHostname(h, fallback string) string {
	if h == "" {
		return fallback
	}
	return h
}

func lastOctet(ip string) string {
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == '.' {
			return ip[i+1:]
		}
	}
	return ip
}
