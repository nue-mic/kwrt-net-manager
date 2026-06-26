package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nue-mic/kwrt-net-manager/internal/appcfg"
	"github.com/nue-mic/kwrt-net-manager/internal/eventbus"
	"github.com/nue-mic/kwrt-net-manager/internal/netcfg"
	"github.com/nue-mic/kwrt-net-manager/internal/store"
)

const testToken = "test-token"

func setupAPI(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &appcfg.Config{
		APIToken:          testToken,
		DataDir:           dir,
		CORSOrigins:       []string{"*"},
		LogLevel:          "info",
		DocsEnabled:       true,
		SelfUpdateEnabled: true,
	}
	st, err := store.New(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New(64)
	nbe, err := netcfg.NewBackend("store", dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	nsvc := netcfg.NewService(nbe, bus, logger)
	exp := NewExportSource(st, nsvc, logger)
	return NewRouter(Deps{Cfg: cfg, Logger: logger, Store: st, Bus: bus, Export: exp, Net: nsvc})
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+testToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return m
}

func TestAPIAuthRequired(t *testing.T) {
	h := setupAPI(t)
	req := httptest.NewRequest("GET", "/api/v1/dhcp/servers", nil) // no token
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestAPIStatusAndInterfaces(t *testing.T) {
	h := setupAPI(t)
	w := do(t, h, "GET", "/api/v1/netcfg/status", nil)
	if w.Code != 200 {
		t.Fatalf("status code %d: %s", w.Code, w.Body)
	}
	if decode(t, w)["backend"] != "store" {
		t.Errorf("backend = %v", decode(t, w)["backend"])
	}

	w = do(t, h, "GET", "/api/v1/interfaces", nil)
	items := decode(t, w)["items"].([]any)
	if len(items) == 0 {
		t.Error("expected interfaces")
	}
}

func TestAPIDHCPServerLifecycle(t *testing.T) {
	h := setupAPI(t)

	// Seed list has one server.
	w := do(t, h, "GET", "/api/v1/dhcp/servers", nil)
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	items := decode(t, w)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("seed servers = %d", len(items))
	}

	// Create.
	w = do(t, h, "POST", "/api/v1/dhcp/servers", map[string]any{
		"interface": "lan2", "enabled": true,
		"ip_start": "192.168.2.10", "ip_end": "192.168.2.100", "netmask": "255.255.255.0",
		"gateway": "192.168.2.1", "dns_primary": "1.1.1.1", "dns_secondary": "",
		"lease_minutes": 60, "exclude": []string{}, "custom_options": []any{},
	})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	id, _ := decode(t, w)["id"].(string)
	if id == "" {
		t.Fatal("no id returned")
	}

	// Toggle off.
	w = do(t, h, "POST", "/api/v1/dhcp/servers/"+id+"/toggle", map[string]any{"enabled": false})
	if w.Code != 200 {
		t.Fatalf("toggle: %d %s", w.Code, w.Body)
	}

	// Delete.
	w = do(t, h, "DELETE", "/api/v1/dhcp/servers/"+id, nil)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}

	// Get deleted → 404.
	w = do(t, h, "GET", "/api/v1/dhcp/servers/"+id, nil)
	if w.Code != 404 {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestAPIRejectsUnknownFieldAndInvalid(t *testing.T) {
	h := setupAPI(t)
	// Unknown field → 400 (DisallowUnknownFields).
	w := do(t, h, "POST", "/api/v1/dhcp/servers", map[string]any{
		"interface": "lan", "ip_start": "192.168.1.10", "ip_end": "192.168.1.20",
		"netmask": "255.255.255.0", "lease_minutes": 60, "bogus_field": "x",
	})
	if w.Code != 400 {
		t.Errorf("unknown field want 400, got %d: %s", w.Code, w.Body)
	}
	// Invalid IP → 400 (validation).
	w = do(t, h, "POST", "/api/v1/dhcp/servers", map[string]any{
		"interface": "lan", "ip_start": "not-an-ip", "ip_end": "192.168.1.20",
		"netmask": "255.255.255.0", "lease_minutes": 60,
	})
	if w.Code != 400 {
		t.Errorf("invalid ip want 400, got %d: %s", w.Code, w.Body)
	}
}

func TestAPIStaticAndLeaseActions(t *testing.T) {
	h := setupAPI(t)

	// Static list carries the arp_bind flag.
	w := do(t, h, "GET", "/api/v1/dhcp/statics", nil)
	body := decode(t, w)
	if _, ok := body["arp_bind"]; !ok {
		t.Error("statics response missing arp_bind")
	}

	// Leases populated by the store simulation.
	w = do(t, h, "GET", "/api/v1/dhcp/leases", nil)
	leases := decode(t, w)["items"].([]any)
	if len(leases) == 0 {
		t.Fatal("expected simulated leases")
	}
	first := leases[0].(map[string]any)

	// Reserve a lease → becomes a static.
	w = do(t, h, "POST", "/api/v1/dhcp/leases/reserve", map[string]any{
		"ip": first["ip"], "mac": first["mac"], "hostname": first["hostname"], "interface": first["interface"],
	})
	if w.Code != 201 {
		t.Fatalf("reserve: %d %s", w.Code, w.Body)
	}

	// ARP-bind toggle.
	w = do(t, h, "PUT", "/api/v1/dhcp/statics/arp-bind", map[string]any{"enabled": true})
	if w.Code != 200 || decode(t, w)["arp_bind"] != true {
		t.Errorf("arp-bind: %d %s", w.Code, w.Body)
	}
}

func TestAPIACLLifecycle(t *testing.T) {
	h := setupAPI(t)
	w := do(t, h, "POST", "/api/v1/dhcp/acl/entries", map[string]any{"mac": "aa:bb:cc:dd:ee:ff", "remark": "x", "enabled": true})
	if w.Code != 201 {
		t.Fatalf("add acl: %d %s", w.Code, w.Body)
	}
	id := decode(t, w)["id"].(string)
	w = do(t, h, "POST", "/api/v1/dhcp/acl/entries/"+id+"/toggle", nil)
	if w.Code != 200 {
		t.Fatalf("toggle acl: %d %s", w.Code, w.Body)
	}
	w = do(t, h, "PUT", "/api/v1/dhcp/acl/mode", map[string]any{"mode": "whitelist"})
	if w.Code != 200 || decode(t, w)["mode"] != "whitelist" {
		t.Errorf("acl mode: %d %s", w.Code, w.Body)
	}
	w = do(t, h, "DELETE", "/api/v1/dhcp/acl/entries/"+id, nil)
	if w.Code != 200 {
		t.Errorf("delete acl: %d %s", w.Code, w.Body)
	}
}

func TestAPIRouteLifecycleAndTable(t *testing.T) {
	h := setupAPI(t)

	w := do(t, h, "POST", "/api/v1/routes", map[string]any{
		"family": "ipv4", "interface": "auto", "target": "172.16.0.0", "netmask": "255.255.0.0",
		"prefix": 0, "gateway": "192.168.1.2", "metric": 5, "remark": "test", "enabled": true,
	})
	if w.Code != 201 {
		t.Fatalf("create route: %d %s", w.Code, w.Body)
	}
	id := decode(t, w)["id"].(string)

	// Duplicate.
	w = do(t, h, "POST", "/api/v1/routes/"+id+"/duplicate", nil)
	if w.Code != 201 {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body)
	}

	// Route table.
	w = do(t, h, "GET", "/api/v1/route-table?family=ipv4", nil)
	if w.Code != 200 {
		t.Fatalf("route-table: %d %s", w.Code, w.Body)
	}
	if len(decode(t, w)["items"].([]any)) == 0 {
		t.Error("expected route table rows")
	}

	// Batch delete.
	w = do(t, h, "POST", "/api/v1/routes/batch", map[string]any{"action": "delete", "ids": []string{id}})
	if w.Code != 200 {
		t.Errorf("batch: %d %s", w.Code, w.Body)
	}
}

func TestAPIExportImportZip(t *testing.T) {
	h := setupAPI(t)
	// Export all → a zip blob.
	w := do(t, h, "GET", "/api/v1/export/all", nil)
	if w.Code != 200 {
		t.Fatalf("export: %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("content-type = %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Error("empty export")
	}
}
