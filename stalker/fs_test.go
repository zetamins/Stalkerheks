package stalker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/erkexzcx/stalkerhek/db"
)

// cdn_mac is opt-in. A profile with no cdn_mac must load with an EMPTY cdn_mac
// (cdnMAC() then falls back to the real auth MAC, which is what actually
// plays). LoadProfile must NOT auto-generate a random streaming MAC — doing so
// broke live playback on portals whose CDN validates mac=. An explicitly
// configured cdn_mac must be loaded and used verbatim.
func TestLoadProfileDoesNotBackfillCDNMac(t *testing.T) {
	dir := t.TempDir()

	// Profile with no cdn_mac field — must stay empty after load.
	raw := `{"old":{"name":"old","portal":{` +
		`"model":"MAG254","serial_number":"SN","device_id":"DEV","device_id2":"DEV2",` +
		`"signature":"SIG","mac":"A0:BB:3E:02:3C:B6","url":"http://example.com/c/portal.php",` +
		`"time_zone":"Europe/Vilnius"},` +
		`"services":{"proxy_bind":"0.0.0.0:8888","hls_bind":"0.0.0.0:9999"}}}`
	dbPath := filepath.Join(dir, "stalkerhek.db")
	if err := os.WriteFile(dbPath, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	c, err := LoadProfile(store, "old")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if c.Portal.CDNMac != "" {
		t.Errorf("cdn_mac should stay empty (opt-in), got %q", c.Portal.CDNMac)
	}

	// An explicitly configured cdn_mac must be loaded as-is.
	store2, err := db.Open(filepath.Join(dir, "two.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store2.Save(db.Profile{
		Name: "set",
		Portal: db.PortalConfig{
			Model: "MAG254", SerialNumber: "SN", DeviceID: "DEV", DeviceID2: "DEV2",
			MAC: "A0:BB:3E:02:3C:B6", CDNMac: "00:1A:79:AA:BB:CC",
			URL: "http://example.com/c/portal.php", TimeZone: "Europe/Vilnius",
		},
	}); err != nil {
		t.Fatal(err)
	}
	cs, err := LoadProfile(store2, "set")
	if err != nil {
		t.Fatalf("LoadProfile(set): %v", err)
	}
	if cs.Portal.CDNMac != "00:1A:79:AA:BB:CC" {
		t.Errorf("explicit cdn_mac = %q, want 00:1A:79:AA:BB:CC", cs.Portal.CDNMac)
	}
}
