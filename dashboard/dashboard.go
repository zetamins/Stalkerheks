package dashboard

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/erkexzcx/stalkerhek/db"
	"github.com/erkexzcx/stalkerhek/hls"
	"github.com/erkexzcx/stalkerhek/proxy"
	"github.com/erkexzcx/stalkerhek/stalker"
)

var (
	store     *db.Store
	profDir   string
	processes = make(map[string]*os.Process)
	procMu    sync.Mutex

	// channelStore is shared with JNI exports for profile start/status
	channelStore map[string]*stalker.Channel
	chanMu       sync.RWMutex

	// inProcessMode is set to true on Android/JNI where no standalone binary exists.
	inProcessMode bool
)

// SetInProcessMode marks that profiles should be started in the current process (JNI/Android).
func SetInProcessMode() { inProcessMode = true }

// MarkRunning registers a profile as running in-process (called from JNI nativeStartProfile).
func MarkRunning(name string) {
	procMu.Lock()
	processes[name] = &os.Process{Pid: os.Getpid()}
	procMu.Unlock()
}

// SetChannelStore sets the shared channel map (called from JNI init).
func SetChannelStore(ch map[string]*stalker.Channel) { channelStore = ch; }

// startProfileInProcess loads a profile and starts all services in the current process.
// Used when the standalone binary doesn't exist (JNI/Android mode).
func startProfileInProcess(name string) error {
	p, ok := store.Get(name)
	if !ok {
		return os.ErrNotExist
	}
	c, err := stalker.LoadProfile(store, name)
	if err != nil {
		return err
	}

	// Start portal, fetch channels, then start HLS + proxy
	go func() {
		log.Printf("Connecting to portal %s...", name)
		if err := c.Portal.Start(); err != nil {
			log.Printf("Portal %s: %v", name, err)
		}
		chs, err := c.Portal.RetrieveChannels()
		if err != nil {
			log.Printf("Channels %s: %v", name, err)
		}
		chanMu.Lock()
		if channelStore == nil {
			channelStore = make(map[string]*stalker.Channel)
		}
		for _, ch := range chs {
			channelStore[ch.CMD] = ch
		}
		chanMu.Unlock()
		log.Printf("Profile %s: loaded %d channels", name, len(chs))

		// Real STBs dispatch get_all_channels (and other loads) before their
		// first watchdog send, so start the watchdog only after
		// RetrieveChannels above, not as part of Portal.Start().
		if c.HLS.Enabled {
			c.Portal.IsPlayingFunc = hls.IsPlaying
		}
		if err := c.Portal.StartWatchdog(); err != nil {
			log.Printf("Portal %s: failed to start watchdog: %v", name, err)
		}

		if c.HLS.Enabled {
			go func() {
				hls.SetUserAgent(c.Portal.Model)
				hls.SetDeviceHeaders(c.Portal.MAC, c.Portal.Model, c.Portal.SerialNumber)
				hls.Start(channelStore, c.HLS.Bind)
			}()
		}
		if c.Proxy.Enabled {
			go func() {
				proxy.Start(c, channelStore)
			}()
		}
	}()

	// Register as running (caller holds procMu)
	processes[name] = &os.Process{Pid: os.Getpid()}

	log.Printf("Started profile %s in-process (HLS=%s Proxy=%s)", name, c.HLS.Bind, c.Proxy.Bind)
	_ = p
	return nil
}

// Start initializes the dashboard HTTP server.
func Start(dir string, bind string, s *db.Store, profileName string) {
	store = s
	profDir = dir
	os.MkdirAll(profDir, 0755)

	// Register the main process so the dashboard shows it as running
	if profileName != "" {
		processes[profileName] = &os.Process{Pid: os.Getpid()}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveDashboard)
	mux.HandleFunc("/api/profiles", handleProfiles)
	mux.HandleFunc("/api/profiles/", handleProfileByID)
	mux.HandleFunc("/api/profiles/start", handleProfileStart)
	mux.HandleFunc("/api/profiles/stop", handleProfileStop)
	mux.HandleFunc("/api/profiles/logs", handleProfileLogs)

	log.Printf("Dashboard available at http://%s", bind)
	if err := http.ListenAndServe(bind, mux); err != nil {
		log.Printf("Dashboard ListenAndServe error: %v", err)
	}
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

func handleProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var p db.Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if p.Name == "" {
			http.Error(w, "name required", 400)
			return
		}
		if err := store.Save(p); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("Created profile: %s", p.Name)
		writeJSON(w, map[string]string{"ok": "created"})
		return
	}

	list := store.GetAll()
	// Merge running status
	procMu.Lock()
	for i := range list {
		if proc, ok := processes[list[i].Name]; ok {
			list[i].Name = list[i].Name // preserve
			_ = proc
		}
	}
	procMu.Unlock()

	type resp struct {
		db.Profile
		Status string `json:"status"`
		PID    int    `json:"pid"`
	}
	out := make([]resp, 0, len(list))
	procMu.Lock()
	for _, p := range list {
		r := resp{Profile: p, Status: "stopped"}
		if proc, ok := processes[p.Name]; ok {
			r.Status = "running"
			r.PID = proc.Pid
		}
		out = append(out, r)
	}
	procMu.Unlock()
	writeJSON(w, out)
}

func handleProfileByID(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	if name == "" || name == "start" || name == "stop" || name == "logs" {
		return
	}

	switch r.Method {
	case "PUT":
		var p db.Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		p.Name = name
		if err := store.Save(p); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"ok": "saved"})
	case "DELETE":
		procMu.Lock()
		stopProcess(name)
		procMu.Unlock()
		store.Delete(name)
		os.Remove(filepath.Join(profDir, name+".log"))
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
		if exe, err := os.Executable(); err == nil {
			req.Binary = exe
		} else {
			req.Binary = "./stalkerhek"
		}
	}

	if _, ok := store.Get(req.Name); !ok {
		writeJSON(w, map[string]string{"error": "profile not found"})
		return
	}

	procMu.Lock()
	defer procMu.Unlock()

	stopProcess(req.Name)

	// In JNI/Android mode, start profiles in-process instead of spawning a binary.
	if inProcessMode {
		if err := startProfileInProcess(req.Name); err != nil {
			writeJSON(w, map[string]string{"error": "failed to start: " + err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": "started", "pid": os.Getpid()})
		return
	}

	// Standalone binary mode: spawn a new process
	cmd := exec.Command(req.Binary, "-profile", req.Name, "-db", profDir)
	logFile, _ := os.Create(filepath.Join(profDir, req.Name+".log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		writeJSON(w, map[string]string{"error": "failed to start: " + err.Error()})
		return
	}
	processes[req.Name] = cmd.Process
	log.Printf("Started profile %s (PID %d, binary %s)", req.Name, cmd.Process.Pid, req.Binary)
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
	procMu.Lock()
	defer procMu.Unlock()
	stopProcess(req.Name)
	writeJSON(w, map[string]string{"ok": "stopped"})
}

func handleProfileLogs(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", 400)
		return
	}
	logPath := filepath.Join(profDir, name+".log")
	data, _ := ioutil.ReadFile(logPath)
	if len(data) > 50000 {
		data = data[len(data)-50000:]
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(data)
}

func stopProcess(name string) {
	if proc, ok := processes[name]; ok {
		proc.Kill()
		proc.Wait()
		delete(processes, name)
		log.Printf("Stopped profile %s", name)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
