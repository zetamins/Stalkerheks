package db

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestOpenCreatesDB(t *testing.T) {
	dir, err := ioutil.TempDir("", "stalkerhek-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if store == nil {
		t.Fatal("Open returned nil store")
	}

	// Verify the file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("DB file was not created")
	}
}

func TestSaveAndGet(t *testing.T) {
	dir, err := ioutil.TempDir("", "stalkerhek-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	p := Profile{
		Name: "test-profile",
		Portal: PortalConfig{
			Model:        "MAG544",
			SerialNumber: "SN12345",
			DeviceID:     "DEVICEID123",
			MAC:          "00:1A:79:12:34:56",
			URL:          "http://example.com/stalker_portal/server/load.php",
			TimeZone:     "Europe/Berlin",
		},
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, ok := store.Get("test-profile")
	if !ok {
		t.Fatal("Get returned not found")
	}
	if got.Name != "test-profile" {
		t.Errorf("Name = %q, want %q", got.Name, "test-profile")
	}
	if got.Portal.Model != "MAG544" {
		t.Errorf("Model = %q", got.Portal.Model)
	}
	if got.Portal.Token == "" {
		t.Error("Token should be auto-generated")
	}
	if got.Portal.UIDSecret == "" {
		t.Error("UIDSecret should be auto-generated")
	}
}

func TestSaveAutoDefaults(t *testing.T) {
	dir, err := ioutil.TempDir("", "stalkerhek-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	// Minimal profile — should get defaults
	p := Profile{
		Name: "minimal",
		Portal: PortalConfig{
			SerialNumber: "SN",
			DeviceID:     "DEV",
			MAC:          "00:00:00:00:00:00",
			URL:          "http://example.com/",
			TimeZone:     "UTC/UTC",
		},
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, _ := store.Get("minimal")
	if got.Portal.Model != "MAG254" {
		t.Errorf("default Model = %q, want MAG254", got.Portal.Model)
	}
	if got.Portal.TimeZone != "UTC/UTC" {
		t.Errorf("TimeZone should be preserved")
	}
	if got.Services.ProxyBind != "0.0.0.0:8888" {
		t.Errorf("default ProxyBind = %q", got.Services.ProxyBind)
	}
	if got.Services.HLSBind != "0.0.0.0:9999" {
		t.Errorf("default HLSBind = %q", got.Services.HLSBind)
	}
	// cdn_mac is auto-generated, distinct from the auth MAC, valid format.
	if got.Portal.CDNMac == "" {
		t.Error("CDNMac should be auto-generated when empty")
	}
	if strings.EqualFold(got.Portal.CDNMac, got.Portal.MAC) {
		t.Errorf("CDNMac %q must differ from auth MAC %q", got.Portal.CDNMac, got.Portal.MAC)
	}
	if !regexp.MustCompile(`^[0-9A-F]{2}(:[0-9A-F]{2}){5}$`).MatchString(got.Portal.CDNMac) {
		t.Errorf("CDNMac %q is not a valid uppercase MAC", got.Portal.CDNMac)
	}
}

func TestSaveCDNMacRespectsUserValue(t *testing.T) {
	dir, err := ioutil.TempDir("", "stalkerhek-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	const userCDN = "11:22:33:44:55:66"
	p := Profile{
		Name: "explicit",
		Portal: PortalConfig{
			SerialNumber: "SN", DeviceID: "DEV", MAC: "00:00:00:00:00:00",
			URL: "http://example.com/", TimeZone: "UTC/UTC", CDNMac: userCDN,
		},
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, _ := store.Get("explicit")
	if got.Portal.CDNMac != userCDN {
		t.Errorf("user-set CDNMac = %q, want %q (must not be overwritten)", got.Portal.CDNMac, userCDN)
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com", "http://example.com/portal.php"},
		{"http://example.com/", "http://example.com/portal.php"},
		{"http://example.com/server/load.php", "http://example.com/server/load.php"},
		{"http://example.com/custom.php", "http://example.com/custom.php"},
	}
	for _, tc := range tests {
		result := normalizeURL(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeURL(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestRandomToken(t *testing.T) {
	t1 := randomToken()
	t2 := randomToken()
	if len(t1) != 32 {
		t.Errorf("token length = %d, want 32", len(t1))
	}
	if t1 == t2 {
		t.Errorf("two random tokens should not be equal: %s", t1)
	}
	// Should be uppercase hex
	for _, c := range t1 {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			t.Errorf("token contains non-hex char: %c", c)
		}
	}
}

func TestRandomUIDSecret(t *testing.T) {
	s1 := randomUIDSecret()
	s2 := randomUIDSecret()
	if len(s1) != 64 {
		t.Errorf("UIDSecret length = %d, want 64", len(s1))
	}
	if s1 == s2 {
		t.Errorf("two random UIDSecrets should not be equal")
	}
}

func TestGetAllSorted(t *testing.T) {
	dir, err := ioutil.TempDir("", "stalkerhek-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	store.Save(Profile{Name: "zulu", Portal: PortalConfig{MAC: "00:00:00:00:00:01", SerialNumber: "S", DeviceID: "D", URL: "http://x", TimeZone: "UTC/UTC"}})
	store.Save(Profile{Name: "alpha", Portal: PortalConfig{MAC: "00:00:00:00:00:02", SerialNumber: "S", DeviceID: "D", URL: "http://x", TimeZone: "UTC/UTC"}})
	store.Save(Profile{Name: "beta", Portal: PortalConfig{MAC: "00:00:00:00:00:03", SerialNumber: "S", DeviceID: "D", URL: "http://x", TimeZone: "UTC/UTC"}})

	list := store.GetAll()
	if len(list) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(list))
	}
	// Should be sorted by name
	if list[0].Name != "alpha" || list[1].Name != "beta" || list[2].Name != "zulu" {
		t.Errorf("GetAll not sorted: %v", []string{list[0].Name, list[1].Name, list[2].Name})
	}
}

func TestDelete(t *testing.T) {
	dir, err := ioutil.TempDir("", "stalkerhek-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	p := Profile{Name: "to-delete", Portal: PortalConfig{MAC: "00:00:00:00:00:00", SerialNumber: "S", DeviceID: "D", URL: "http://x", TimeZone: "UTC/UTC"}}
	store.Save(p)

	if _, ok := store.Get("to-delete"); !ok {
		t.Fatal("profile should exist before delete")
	}

	store.Delete("to-delete")

	if _, ok := store.Get("to-delete"); ok {
		t.Fatal("profile should not exist after delete")
	}
}

func TestLoadProfileValidationMAC(t *testing.T) {
	// Verify that profiles with various MAC formats are stored correctly.
	// Validation (MAC regex, timezone format) happens at load time in
	// stalker.LoadProfile, not at db save time — the db layer is a
	// pure data store. This test verifies that MAC values round-trip
	// through save/load correctly regardless of format.
	dir, err := ioutil.TempDir("", "stalkerhek-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		mac  string
	}{
		{"valid", "00:1A:79:12:34:56"},
		{"invalid-no-colons", "invalid"},
		{"lowercase", "00:1a:79:12:34:56"},
	}
	for _, tc := range tests {
		p := Profile{
			Name: "mac-" + tc.name,
			Portal: PortalConfig{
				Model:        "MAG544",
				SerialNumber: "SN",
				DeviceID:     "DEV",
				MAC:          tc.mac,
				URL:          "http://example.com",
				TimeZone:     "Europe/Berlin",
			},
		}
		store.Save(p)
		got, ok := store.Get("mac-" + tc.name)
		if !ok {
			t.Errorf("profile mac-%s not found after save", tc.name)
			continue
		}
		if got.Portal.MAC != tc.mac {
			t.Errorf("MAC round-trip: saved %q, got %q", tc.mac, got.Portal.MAC)
		}
	}
}
