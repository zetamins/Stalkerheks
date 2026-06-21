package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ProfileConfig stores all portal connection parameters for a profile.
type ProfileConfig struct {
	Model        string `json:"model"`
	SerialNumber string `json:"serial_number"`
	DeviceID     string `json:"device_id"`
	DeviceID2    string `json:"device_id2"`
	Signature    string `json:"signature"`
	MAC          string `json:"mac"`
	URL          string `json:"url"`
	TimeZone     string `json:"time_zone"`
	Token        string `json:"token"`
	ProxyBind    string `json:"proxy_bind"`
	HLSBind      string `json:"hls_bind"`
}

// Profile represents a managed stalkerhek profile.
type Profile struct {
	Name      string        `json:"name"`
	Config    ProfileConfig `json:"config"`
	Status    string        `json:"status"`
	PID       int           `json:"pid"`
	Uptime    string        `json:"uptime"`
	StartedAt string        `json:"started_at"`
}

var (
	profilesDir string
	profilesMu  sync.Mutex
	processes   = make(map[string]*os.Process)
)

func profilesPath() string { return filepath.Join(profilesDir, "profiles.json") }

// Start initializes the dashboard HTTP server.
func Start(dir, bind string) {
	profilesDir = dir
	os.MkdirAll(profilesDir, 0755)

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveDashboard)
	mux.HandleFunc("/api/profiles", handleProfiles)
	mux.HandleFunc("/api/profiles/", handleProfileByID)
	mux.HandleFunc("/api/profiles/start", handleProfileStart)
	mux.HandleFunc("/api/profiles/stop", handleProfileStop)
	mux.HandleFunc("/api/profiles/logs", handleProfileLogs)

	log.Printf("Dashboard available at http://%s", bind)
	panic(http.ListenAndServe(bind, mux))
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

func loadAllProfiles() (map[string]Profile, error) {
	data, err := ioutil.ReadFile(profilesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]Profile), nil
		}
		return nil, err
	}
	var m map[string]Profile
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]Profile), nil
	}
	return m, nil
}

func saveAllProfiles(m map[string]Profile) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(profilesPath(), data, 0644)
}

func handleProfiles(w http.ResponseWriter, r *http.Request) {
	profilesMu.Lock()
	defer profilesMu.Unlock()

	all, _ := loadAllProfiles()

	if r.Method == "POST" {
		var p Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if p.Name == "" {
			http.Error(w, "name required", 400)
			return
		}
		// Auto-derive device_id2 and signature from device_id if not provided
		if p.Config.DeviceID2 == "" && p.Config.DeviceID != "" {
			h := sha256.New()
			h.Write([]byte(p.Config.DeviceID + ":device_id:" + p.Config.Token))
			p.Config.DeviceID2 = hex.EncodeToString(h.Sum(nil))
		}
		if p.Config.Signature == "" && p.Config.DeviceID != "" {
			h := sha256.New()
			h.Write([]byte(p.Config.DeviceID + ":signature"))
			p.Config.Signature = hex.EncodeToString(h.Sum(nil))
		}
		if p.Config.ProxyBind == "" {
			p.Config.ProxyBind = "0.0.0.0:8888"
		}
		if p.Config.HLSBind == "" {
			p.Config.HLSBind = "0.0.0.0:9999"
		}
		// Auto-append API endpoint: if URL doesn't end with .php,
		// append portal.php (strips trailing slash first)
		p.Config.URL = normalizePortalURL(p.Config.URL)
		all[p.Name] = p
		if err := saveAllProfiles(all); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("Created profile: %s", p.Name)
		writeJSON(w, map[string]string{"ok": "created"})
		return
	}

	// Merge running status
	for name := range all {
		p := all[name]
		if proc, ok := processes[name]; ok {
			p.Status = "running"
			p.PID = proc.Pid
		} else {
			p.Status = "stopped"
			p.PID = 0
		}
		all[name] = p
	}

	list := make([]Profile, 0, len(all))
	for _, p := range all {
		list = append(list, p)
	}
	writeJSON(w, list)
}

func handleProfileByID(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	if name == "" || name == "start" || name == "stop" || name == "logs" {
		return
	}

	profilesMu.Lock()
	defer profilesMu.Unlock()

	all, _ := loadAllProfiles()

	switch r.Method {
	case "PUT":
		var p Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		p.Name = name
		p.Config.URL = normalizePortalURL(p.Config.URL)
		if p.Config.DeviceID2 == "" && p.Config.DeviceID != "" {
			h := sha256.New()
			h.Write([]byte(p.Config.DeviceID + ":device_id:" + p.Config.Token))
			p.Config.DeviceID2 = hex.EncodeToString(h.Sum(nil))
		}
		if p.Config.Signature == "" && p.Config.DeviceID != "" {
			h := sha256.New()
			h.Write([]byte(p.Config.DeviceID + ":signature"))
			p.Config.Signature = hex.EncodeToString(h.Sum(nil))
		}
		all[name] = p
		saveAllProfiles(all)
		writeJSON(w, map[string]string{"ok": "saved"})
	case "DELETE":
		stopProfile(name)
		delete(all, name)
		saveAllProfiles(all)
		os.Remove(filepath.Join(profilesDir, name+".yml"))
		writeJSON(w, map[string]string{"ok": "deleted"})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleProfileStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Name   string `json:"name"`
		Binary string `json:"binary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if req.Binary == "" {
		req.Binary = "./stalkerhek"
	}

	profilesMu.Lock()
	defer profilesMu.Unlock()

	all, _ := loadAllProfiles()
	p, ok := all[req.Name]
	if !ok {
		writeJSON(w, map[string]string{"error": "profile not found"})
		return
	}

	stopProfile(req.Name)

	// Generate temp YAML config for stalkerhek binary
	ymlPath := filepath.Join(profilesDir, req.Name+".yml")
	yml := generateYAML(p.Config)
	if err := ioutil.WriteFile(ymlPath, []byte(yml), 0644); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	cmd := exec.Command(req.Binary, "-config", ymlPath)
	cmd.Stdout, _ = os.Create(filepath.Join(profilesDir, req.Name+".log"))
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	processes[req.Name] = cmd.Process
	log.Printf("Started profile %s (PID %d)", req.Name, cmd.Process.Pid)
	writeJSON(w, map[string]interface{}{"ok": "started", "pid": cmd.Process.Pid})
}

func handleProfileStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	profilesMu.Lock()
	defer profilesMu.Unlock()
	stopProfile(req.Name)
	writeJSON(w, map[string]string{"ok": "stopped"})
}

func handleProfileLogs(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", 400)
		return
	}
	logPath := filepath.Join(profilesDir, name+".log")
	data, _ := ioutil.ReadFile(logPath)
	if len(data) > 50000 {
		data = data[len(data)-50000:]
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(data)
}

func stopProfile(name string) {
	if proc, ok := processes[name]; ok {
		proc.Kill()
		proc.Wait()
		delete(processes, name)
		log.Printf("Stopped profile %s", name)
	}
}

// normalizePortalURL ensures the URL points to the portal API endpoint.
// If the user enters a base URL like "http://host:80/c/", it auto-appends
// "portal.php" to form "http://host:80/c/portal.php".
func normalizePortalURL(raw string) string {
	raw = strings.TrimRight(raw, "/")
	if strings.HasSuffix(raw, ".php") {
		return raw
	}
	return raw + "/portal.php"
}

func generateYAML(c ProfileConfig) string {
	return "portal:\n" +
		"  model: " + c.Model + "\n" +
		"  serial_number: \"" + c.SerialNumber + "\"\n" +
		"  device_id: " + c.DeviceID + "\n" +
		"  device_id2: " + c.DeviceID2 + "\n" +
		"  signature: " + c.Signature + "\n" +
		"  mac: " + c.MAC + "\n" +
		"  url: " + c.URL + "\n" +
		"  time_zone: " + c.TimeZone + "\n" +
		"  token: " + c.Token + "\n" +
		"\n" +
		"hls:\n" +
		"  enabled: true\n" +
		"  bind: " + c.HLSBind + "\n" +
		"\n" +
		"proxy:\n" +
		"  enabled: true\n" +
		"  bind: " + c.ProxyBind + "\n"
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
