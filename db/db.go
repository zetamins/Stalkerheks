package db

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"math/rand"
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
	URL          string `json:"url"`
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
	Name      string        `json:"name"`
	Portal    PortalConfig  `json:"portal"`
	Services  ServiceConfig `json:"services"`
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
	m, _ := s.readAll()

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

	// Auto-append API endpoint
	p.Portal.URL = normalizeURL(p.Portal.URL)
	if p.Portal.URL2 != "" {
		p.Portal.URL2 = normalizeURL(p.Portal.URL2)
	}

	m[p.Name] = p
	return s.writeAll(m)
}

// Delete removes a profile by name.
func (s *Store) Delete(name string) error {
	m, _ := s.readAll()
	delete(m, name)
	return s.writeAll(m)
}

// normalizeURL ensures the URL points to the portal API endpoint.
func normalizeURL(raw string) string {
	raw = strings.TrimRight(raw, "/")
	if strings.HasSuffix(raw, ".php") {
		return raw
	}
	return raw + "/portal.php"
}

func randomToken() string {
	const charset = "ABCDEF0123456789"
	b := make([]byte, 32)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
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
