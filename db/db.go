package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
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
	TimeZone     string `json:"time_zone"`
	Token        string `json:"token"`
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

// GetAll returns all profiles.
func (s *Store) GetAll() []Profile {
	m, _ := s.readAll()
	list := make([]Profile, 0, len(m))
	for _, p := range m {
		list = append(list, p)
	}
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

	// Auto-generate token if empty (must be before device ID derivation)
	if p.Portal.Token == "" {
		p.Portal.Token = randomToken()
	}

	// Auto-append API endpoint
	p.Portal.URL = normalizeURL(p.Portal.URL)

	// Auto-derive device IDs from device_id + token
	if p.Portal.DeviceID2 == "" && p.Portal.DeviceID != "" {
		h := sha256.New()
		h.Write([]byte(p.Portal.DeviceID + ":device_id:" + p.Portal.Token))
		p.Portal.DeviceID2 = hex.EncodeToString(h.Sum(nil))
	}
	if p.Portal.Signature == "" && p.Portal.DeviceID != "" {
		h := sha256.New()
		h.Write([]byte(p.Portal.DeviceID + ":signature"))
		p.Portal.Signature = hex.EncodeToString(h.Sum(nil))
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
