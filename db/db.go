package db

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// PortalConfig holds all connection parameters for a stalker portal profile.
type PortalConfig struct {
	Model        string `json:"model"`
	SerialNumber string `json:"serial_number"`
	DeviceID     string `json:"device_id"`
	DeviceID2    string `json:"device_id2"`
	Signature    string `json:"signature"`
	MAC          string `json:"mac"`

	// CDNMac, when set, is the MAC embedded in the mac= query parameter of
	// CDN/stream play URLs (live.php etc.) — distinct from MAC, which is used
	// for portal auth. The portal flags the account's real MAC for
	// anti-sharing and returns HTTP 458 on stream requests carrying it, but
	// the play_token is not bound to the MAC, so a different CDNMac gets the
	// same stream through. Leave empty to use MAC (original behavior).
	CDNMac string `json:"cdn_mac,omitempty"`

	URL string `json:"url"`
	URL2         string `json:"url2,omitempty"` // fallback portal URL (real STBs are provisioned with portal1/portal2 and fail over)
	TimeZone     string `json:"time_zone"`
	Token        string `json:"token"`

	// UIDSecret is the software stand-in for the Hardware Unique Key a real
	// STB's Trustonic TEE secure element would hold — the root secret
	// device_id2/signature are derived from (see stalker.Portal.GetUID).
	// Auto-generated once if empty, like Token.
	UIDSecret string `json:"uid_secret,omitempty"`
}

// ServiceConfig holds bind addresses for HLS and proxy.
type ServiceConfig struct {
	ProxyBind string `json:"proxy_bind"`
	HLSBind   string `json:"hls_bind"`
}

// DashboardConfig holds dashboard settings.
type DashboardConfig struct {
	Bind        string `json:"bind"`
	ProfilesDir string `json:"profiles_dir"`
}

// Profile is a complete stalkerhek profile.
type Profile struct {
	Name      string          `json:"name"`
	Portal    PortalConfig    `json:"portal"`
	Services  ServiceConfig   `json:"services"`
	Dashboard DashboardConfig `json:"dashboard,omitempty"`
}

// Store is a JSON file-based profile database.
type Store struct {
	mu   sync.RWMutex
	path string
}

var defaultStore *Store

// Open opens (or creates) the profile database at the given path.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)
	s := &Store{path: path}
	// Create empty DB if not exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := s.writeAll(map[string]Profile{}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// DefaultStore opens the default database at the given directory.
func DefaultStore(dir string) (*Store, error) {
	if defaultStore != nil {
		return defaultStore, nil
	}
	var err error
	defaultStore, err = Open(filepath.Join(dir, "stalkerhek.db"))
	return defaultStore, err
}

func (s *Store) readAll() (map[string]Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readAllLocked()
}

// readAllLocked is readAll without locking. Callers that perform a
// read-modify-write (Save, Delete) hold s.mu across the whole sequence and use
// this, so a concurrent writer can't slip in between the read and the write
// and clobber the update.
func (s *Store) readAllLocked() (map[string]Profile, error) {
	data, err := ioutil.ReadFile(s.path)
	if err != nil {
		return make(map[string]Profile), nil
	}
	var m map[string]Profile
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]Profile), nil
	}
	return m, nil
}

func (s *Store) writeAll(m map[string]Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeAllLocked(m)
}

// writeAllLocked is writeAll without locking — see readAllLocked.
func (s *Store) writeAllLocked(m map[string]Profile) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(s.path, data, 0644)
}

// Get retrieves a profile by name.
func (s *Store) Get(name string) (Profile, bool) {
	m, _ := s.readAll()
	p, ok := m[name]
	return p, ok
}

// GetAll returns all profiles, sorted by name. The Android/JNI bridge
// derives positional integer IDs from this order (since profiles are
// otherwise keyed only by name) — without a stable sort, those IDs would
// shuffle on every call, since Go map iteration order is randomized.
func (s *Store) GetAll() []Profile {
	m, _ := s.readAll()
	list := make([]Profile, 0, len(m))
	for _, p := range m {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// Save creates or updates a profile. Auto-derives device_id2 and signature
// from device_id if empty, normalizes the portal URL, and generates a token
// if none is provided.
func (s *Store) Save(p Profile) error {
	// Apply defaults first
	if p.Portal.Model == "" {
		p.Portal.Model = "MAG254"
	}
	if p.Portal.TimeZone == "" {
		p.Portal.TimeZone = "Europe/Vilnius"
	}
	if p.Services.ProxyBind == "" {
		p.Services.ProxyBind = "0.0.0.0:8888"
	}
	if p.Services.HLSBind == "" {
		p.Services.HLSBind = "0.0.0.0:9999"
	}
	if p.Dashboard.Bind == "" {
		p.Dashboard.Bind = "0.0.0.0:8080"
	}

	// Auto-generate token if empty
	if p.Portal.Token == "" {
		p.Portal.Token = randomToken()
	}

	// Auto-generate the root secret device_id2/signature derive from (the
	// software stand-in for a real STB's TEE-held Hardware Unique Key) if
	// empty. device_id2 and signature themselves are deliberately NOT
	// derived/stored here: device_id2 only needs device_id+token (both
	// already known) so stalker.LoadProfile resolves it on load instead of
	// duplicating that logic here; signature is keyed by the handshake's
	// random nonce, which doesn't exist yet at save time, so it's always
	// computed fresh per-handshake in stalker (see Portal.signature()).
	if p.Portal.UIDSecret == "" {
		p.Portal.UIDSecret = randomUIDSecret()
	}

	// Auto-generate a CDN MAC (distinct from the auth MAC) if none was set.
	// The portal flags the account's auth MAC for anti-sharing and returns
	// HTTP 458 on stream requests carrying it, but the play_token isn't bound
	// to the MAC — so streams play when the play URL's mac= is a different
	// value. A user-supplied cdn_mac is respected; otherwise one is generated
	// here. See stalker.Portal.cdnMAC.
	if p.Portal.CDNMac == "" {
		p.Portal.CDNMac = randomCDNMac(p.Portal.MAC)
	}

	// Auto-append API endpoint
	p.Portal.URL = normalizeURL(p.Portal.URL)
	if p.Portal.URL2 != "" {
		p.Portal.URL2 = normalizeURL(p.Portal.URL2)
	}

	// Hold the write lock across the read-modify-write so two concurrent Saves
	// (e.g. two dashboard POSTs) can't both read, then both write, losing one
	// of the updates.
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.readAllLocked()
	m[p.Name] = p
	return s.writeAllLocked(m)
}

// Delete removes a profile by name.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.readAllLocked()
	delete(m, name)
	return s.writeAllLocked(m)
}

// normalizeURL ensures the URL points to the portal API endpoint.
func normalizeURL(raw string) string {
	raw = strings.TrimRight(raw, "/")
	if strings.HasSuffix(raw, ".php") {
		return raw
	}
	return raw + "/portal.php"
}

// randomToken returns a 32-char uppercase-hex token. Uses crypto/rand rather
// than math/rand: on go1.13 the global math/rand source is not auto-seeded
// (that's go1.20+), so the old version handed out the *same* token sequence on
// every process start — predictable, and collision-prone across devices.
func randomToken() string {
	b := make([]byte, 16) // 16 bytes -> 32 hex chars
	if _, err := cryptorand.Read(b); err != nil {
		// fallback in the astronomically unlikely event of crypto/rand failure
		for i := range b {
			b[i] = byte(i ^ 0xA5)
		}
	}
	return strings.ToUpper(hex.EncodeToString(b))
}

// randomCDNMac returns a random MAC for CDN/stream play URLs, distinct from
// authMAC and using an Infomir OUI (00:1A:79) so it still resembles a real MAG
// STB to the portal. Used to populate cdn_mac when a profile is saved without
// one, so the per-MAC 458 anti-sharing bypass works out of the box.
func randomCDNMac(authMAC string) string {
	b := make([]byte, 3)
	for {
		if _, err := cryptorand.Read(b); err != nil {
			// astronomically unlikely; deterministic fallback
			for i := range b {
				b[i] = byte(i ^ 0x3C)
			}
		}
		mac := fmt.Sprintf("00:1A:79:%02X:%02X:%02X", b[0], b[1], b[2])
		if !strings.EqualFold(mac, strings.TrimSpace(authMAC)) {
			return mac
		}
	}
}

// randomUIDSecret generates a root secret for stalker.Portal.GetUID — the
// software stand-in for a real STB's TEE-held Hardware Unique Key.
// Uses crypto/rand rather than randomToken's math/rand: this value is used
// as actual HMAC key material, not just an opaque session token, so it
// needs to be unpredictable rather than just unique-looking.
func randomUIDSecret() string {
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		// fallback in the astronomically unlikely event of crypto/rand failure
		for i := range b {
			b[i] = byte(i ^ 0x5A)
		}
	}
	return hex.EncodeToString(b)
}
