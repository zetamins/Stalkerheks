package stalker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/erkexzcx/stalkerhek/db"
)

// A profile written by an older build (or shipped in the app) has no cdn_mac.
// LoadProfile must backfill a distinct one and persist it, so the 458 bypass
// works without re-creating the profile.
func TestLoadProfileBackfillsCDNMac(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "stalkerhek.db")

	// Raw profile as written before cdn_mac existed — note: no cdn_mac field.
	raw := `{"old":{"name":"old","portal":{` +
		`"model":"MAG254","serial_number":"SN","device_id":"DEV","device_id2":"DEV2",` +
		`"signature":"SIG","mac":"A0:BB:3E:02:3C:B6","url":"http://example.com/c/portal.php",` +
		`"time_zone":"Europe/Vilnius"},` +
		`"services":{"proxy_bind":"0.0.0.0:8888","hls_bind":"0.0.0.0:9999"}}}`
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
	if c.Portal.CDNMac == "" {
		t.Fatal("LoadProfile should backfill cdn_mac for a legacy profile")
	}
	if c.Portal.CDNMac == c.Portal.MAC {
		t.Errorf("cdn_mac %q must differ from auth MAC %q", c.Portal.CDNMac, c.Portal.MAC)
	}

	// Must be persisted so it stays stable across restarts.
	got, ok := store.Get("old")
	if !ok {
		t.Fatal("profile vanished")
	}
	if got.Portal.CDNMac != c.Portal.CDNMac {
		t.Errorf("backfilled cdn_mac not persisted: stored %q, loaded %q", got.Portal.CDNMac, c.Portal.CDNMac)
	}

	// A second load must reuse the persisted value, not generate a new one.
	c2, err := LoadProfile(store, "old")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Portal.CDNMac != c.Portal.CDNMac {
		t.Errorf("cdn_mac not stable across loads: %q then %q", c.Portal.CDNMac, c2.Portal.CDNMac)
	}
}
