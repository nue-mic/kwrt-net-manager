package netcfg

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// Backend is the persistence + apply layer the Service drives. Reads return the
// current configuration; the Save* methods replace a whole collection and apply
// it (commit + reload on uci; write JSON on store). Keeping writes
// whole-collection lets the uci backend regenerate its managed sections
// idempotently without tracking per-item deltas.
type Backend interface {
	// Kind reports "uci" or "store".
	Kind() string

	// Interfaces lists L3 interfaces usable as DHCP/route targets.
	Interfaces() ([]Interface, error)
	// Status summarizes service health (committed-but-not-reloaded → pending).
	Status() (Status, error)

	// DHCP servers.
	DHCPServers() ([]DHCPServer, error)
	SaveDHCPServers([]DHCPServer) error
	RestartDHCP() error

	// Static reservations + the global ARP-bind toggle.
	Statics() ([]StaticLease, error)
	ARPBind() (bool, error)
	SaveStatics(list []StaticLease, arpBind bool) error

	// Active leases (read-only).
	Leases() ([]Lease, error)

	// MAC access-control list.
	ACL() (ACL, error)
	SaveACL(ACL) error

	// Static routes.
	Routes() ([]Route, error)
	SaveRoutes([]Route) error

	// Live kernel routing table for family "ipv4" | "ipv6" (read-only).
	RouteTable(family string) ([]RouteEntry, error)

	// Physical NIC inventory (网卡列表, read-only).
	NICs() ([]NIC, error)
	// Configured LAN/WAN interfaces (内外网设置).
	NetIfaces() ([]NetIface, error)
	SaveNetIface(NetIface) error
	DeleteNetIface(id string) error
	// WANAction runs a runtime action on an interface: "connect" (ifup),
	// "disconnect" (ifdown) or "restart".
	WANAction(id, action string) error

	// DHCPServiceInfo reports which DHCP daemon is installed/running.
	DHCPServiceInfo() (DHCPSvcInfo, error)
	// InstallDHCP installs dnsmasq (一键安装) via the system package manager,
	// returning the combined command output.
	InstallDHCP() (string, error)
}

// NewBackend selects and constructs the network-config backend. kind is one of
// "uci", "store" or "auto" (detect). dataDir is where the store backend keeps
// netcfg.json.
func NewBackend(kind, dataDir string, logger *slog.Logger) (Backend, error) {
	if logger == nil {
		logger = slog.Default()
	}
	sidecar := filepath.Join(dataDir, "netcfg.json")
	switch kind {
	case KindUCI:
		return newUCIBackend(realRunner{}, sidecar, logger)
	case KindStore:
		return newStoreBackend(sidecar, logger, true)
	default: // "auto"
		if uciAvailable() {
			logger.Info("netcfg backend: detected OpenWrt UCI")
			return newUCIBackend(realRunner{}, sidecar, logger)
		}
		logger.Info("netcfg backend: no UCI detected, using store (JSON + simulated leases)")
		return newStoreBackend(sidecar, logger, true)
	}
}

// uciAvailable reports whether this host looks like an OpenWrt system with a
// working uci CLI and a dnsmasq DHCP config to manage.
func uciAvailable() bool {
	if _, err := exec.LookPath("uci"); err != nil {
		return false
	}
	if _, err := os.Stat("/etc/config/dhcp"); err != nil {
		return false
	}
	return true
}
