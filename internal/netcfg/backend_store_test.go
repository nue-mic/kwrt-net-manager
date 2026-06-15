package netcfg

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	be, err := newStoreBackend(filepath.Join(dir, "netcfg.json"), nil)
	if err != nil {
		t.Fatalf("newStoreBackend: %v", err)
	}
	return NewService(be, nil, nil)
}

func TestStoreSeedAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "netcfg.json")
	be, err := newStoreBackend(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("seed file not written: %v", statErr)
	}
	servers, _ := be.DHCPServers()
	if len(servers) != 1 || servers[0].Interface != "lan" {
		t.Fatalf("seed servers = %+v", servers)
	}
	// Reopen → state persists.
	be2, err := newStoreBackend(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s2, _ := be2.DHCPServers(); len(s2) != 1 {
		t.Fatalf("reopened servers = %+v", s2)
	}
}

func TestDHCPServerCRUD(t *testing.T) {
	svc := newTestService(t)

	created, err := svc.CreateDHCPServer(DHCPServer{
		Interface: "lan2", Enabled: true,
		IPStart: "192.168.2.10", IPEnd: "192.168.2.100", Netmask: "255.255.255.0",
		Gateway: "192.168.2.1", DNSPrimary: "1.1.1.1", LeaseMinutes: 60,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated id")
	}

	list, _ := svc.ListDHCPServers()
	if len(list) != 2 {
		t.Fatalf("want 2 servers, got %d", len(list))
	}
	// Remaining is computed (pool of 91 minus simulated leases inside it).
	got, err := svc.GetDHCPServer(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Remaining <= 0 || got.Remaining > 91 {
		t.Errorf("remaining out of range: %d", got.Remaining)
	}

	got.LeaseMinutes = 240
	if _, err := svc.UpdateDHCPServer(got.ID, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	re, _ := svc.GetDHCPServer(got.ID)
	if re.LeaseMinutes != 240 {
		t.Errorf("update not persisted: %d", re.LeaseMinutes)
	}

	if err := svc.SetDHCPServerEnabled(got.ID, false); err != nil {
		t.Fatal(err)
	}
	re, _ = svc.GetDHCPServer(got.ID)
	if re.Enabled {
		t.Error("toggle off failed")
	}

	if err := svc.DeleteDHCPServer(got.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetDHCPServer(got.ID); err != ErrNotFound {
		t.Errorf("want ErrNotFound after delete, got %v", err)
	}
}

func TestDHCPServerValidation(t *testing.T) {
	svc := newTestService(t)
	bad := []DHCPServer{
		{Interface: "", IPStart: "192.168.1.1", IPEnd: "192.168.1.9", Netmask: "255.255.255.0", LeaseMinutes: 1},
		{Interface: "lan", IPStart: "x", IPEnd: "192.168.1.9", Netmask: "255.255.255.0", LeaseMinutes: 1},
		{Interface: "lan", IPStart: "192.168.1.9", IPEnd: "192.168.1.1", Netmask: "255.255.255.0", LeaseMinutes: 1},
		{Interface: "lan", IPStart: "192.168.1.1", IPEnd: "192.168.1.9", Netmask: "255.255.255.0", LeaseMinutes: 0},
		{Interface: "lan", IPStart: "192.168.1.1", IPEnd: "192.168.1.9", Netmask: "bad", LeaseMinutes: 1},
	}
	for i, b := range bad {
		if _, err := svc.CreateDHCPServer(b); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestStaticUniquenessAndCRUD(t *testing.T) {
	svc := newTestService(t)
	a, err := svc.CreateStatic(StaticLease{Hostname: "pc-a", IP: "192.168.1.51", MAC: "aa:bb:cc:dd:ee:01", Interface: "lan", Enabled: true})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	// Duplicate IP rejected.
	if _, err := svc.CreateStatic(StaticLease{IP: "192.168.1.51", MAC: "aa:bb:cc:dd:ee:02", Interface: "lan"}); err == nil {
		t.Error("expected duplicate IP rejection")
	}
	// Duplicate MAC rejected.
	if _, err := svc.CreateStatic(StaticLease{IP: "192.168.1.52", MAC: "AA:BB:CC:DD:EE:01", Interface: "lan"}); err == nil {
		t.Error("expected duplicate MAC rejection")
	}
	// MAC normalized to upper colon form.
	if a.MAC != "AA:BB:CC:DD:EE:01" {
		t.Errorf("mac not normalized: %s", a.MAC)
	}
	if err := svc.DeleteStatic(a.ID); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseSimulationAndReserve(t *testing.T) {
	svc := newTestService(t)
	leases, err := svc.ListLeases(LeaseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) == 0 {
		t.Fatal("expected simulated leases")
	}
	// There must be at least one dynamic lease (from the seeded pool).
	var dyn *Lease
	for i := range leases {
		if !leases[i].Static {
			dyn = &leases[i]
			break
		}
	}
	if dyn == nil {
		t.Fatal("expected a dynamic lease to reserve")
	}
	// Reserve it → becomes a static reservation.
	if _, err := svc.ReserveLease(dyn.IP, dyn.MAC, dyn.Hostname, dyn.Interface); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	statics, _ := svc.ListStatics()
	found := false
	for _, s := range statics {
		if s.IP == dyn.IP {
			found = true
		}
	}
	if !found {
		t.Error("reserved lease not found in statics")
	}

	// Filter by dynamic status returns only dynamic.
	dynList, _ := svc.ListLeases(LeaseFilter{Status: "dynamic"})
	for _, l := range dynList {
		if l.Static {
			t.Error("dynamic filter leaked a static lease")
		}
	}
}

func TestACLLifecycle(t *testing.T) {
	svc := newTestService(t)
	acl, _ := svc.GetACL()
	if acl.Mode != ACLBlacklist {
		t.Errorf("default mode = %s", acl.Mode)
	}
	e, err := svc.BlacklistMAC("aa:bb:cc:dd:ee:ff", "bad device")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddACLEntry(ACLEntry{MAC: "AA:BB:CC:DD:EE:FF"}); err == nil {
		t.Error("expected duplicate MAC rejection in ACL")
	}
	toggled, _ := svc.ToggleACLEntry(e.ID)
	if toggled.Enabled == e.Enabled {
		t.Error("toggle did not flip")
	}
	if _, err := svc.SetACLMode("whitelist"); err != nil {
		t.Fatal(err)
	}
	acl, _ = svc.GetACL()
	if acl.Mode != ACLWhitelist {
		t.Errorf("mode not switched: %s", acl.Mode)
	}
	if err := svc.DeleteACLEntry(e.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRouteCRUDAndValidation(t *testing.T) {
	svc := newTestService(t)
	r, err := svc.CreateRoute(Route{Family: "ipv4", Interface: "auto", Target: "172.16.0.0", Netmask: "255.255.0.0", Gateway: "192.168.1.2", Metric: 5, Enabled: true})
	if err != nil {
		t.Fatalf("create route: %v", err)
	}
	if r.Prefix != 16 {
		t.Errorf("prefix not derived from mask: %d", r.Prefix)
	}
	// IPv6 route via prefix.
	r6, err := svc.CreateRoute(Route{Family: "ipv6", Target: "2001:db8::", Prefix: 48, Gateway: "fe80::1", Enabled: true})
	if err != nil {
		t.Fatalf("create v6: %v", err)
	}
	if r6.Interface != "auto" {
		t.Errorf("default interface = %s", r6.Interface)
	}
	// Bad: ipv4 target with ipv6 family.
	if _, err := svc.CreateRoute(Route{Family: "ipv6", Target: "10.0.0.0", Prefix: 24}); err == nil {
		t.Error("expected ipv6 family + ipv4 target to fail")
	}
	// Duplicate (copy).
	cp, err := svc.DuplicateRoute(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cp.ID == r.ID {
		t.Error("duplicate should get a new id")
	}
	list, _ := svc.ListRoutes()
	// seed(1) + r + r6 + cp = 4
	if len(list) != 4 {
		t.Errorf("route count = %d want 4", len(list))
	}

	// Route table reflects enabled ipv4 routes.
	tbl, _ := svc.RouteTable("ipv4")
	if len(tbl) == 0 {
		t.Error("expected non-empty ipv4 route table")
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	svc := newTestService(t)
	_, _ = svc.CreateStatic(StaticLease{IP: "192.168.1.77", MAC: "12:34:56:78:9a:bc", Interface: "lan", Enabled: true})
	blob, err := svc.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	// Fresh service, import the blob.
	svc2 := newTestService(t)
	if err := svc2.ImportJSON(blob); err != nil {
		t.Fatalf("import: %v", err)
	}
	statics, _ := svc2.ListStatics()
	found := false
	for _, s := range statics {
		if s.IP == "192.168.1.77" {
			found = true
		}
	}
	if !found {
		t.Error("imported static not present")
	}
}
