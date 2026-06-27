package dashboard

import (
	"encoding/json"
	"io"
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
	store   *db.Store
	profDir string

	// processes holds real OS subprocess handles (standalone-binary mode
	// only). Never populate this with a fake/self-referencing Process — it
	// gets Kill()ed by stopProcess, and killing the current process's own
	// PID would take down the whole engine (this previously happened in
	// in-process/Android mode, see inProcessRunning below for the fix).
	processes = make(map[string]*os.Process)
	procMu    sync.Mutex

	// inProcessRunning tracks profiles started in-process (JNI/Android),
	// where there is no separate OS process to kill — stopping means calling
	// the profile's own stop function instead.
	inProcessRunning = make(map[string]func())

	// inProcessInstances holds per-profile HLS and proxy instances for
	// multi-profile isolation. Keyed by profile name.
	inProcessInstances   = make(map[string]*inProcessState)
	inProcessInstancesMu sync.Mutex
)

// inProcessState bundles per-profile service handles for multi-profile isolation.
type inProcessState struct {
	hlsInst   *hls.Instance
	proxyInst *proxy.Instance
	config    *stalker.Config
}

var (

	// channelStore is shared with JNI exports for profile start/status
	channelStore map[string]*stalker.Channel
	chanMu       sync.RWMutex

	// inProcessMode is set to true on Android/JNI where no standalone binary exists.
	inProcessMode bool
)

// SetInProcessMode marks that profiles should be started in the current process (JNI/Android).
func SetInProcessMode() { inProcessMode = true }

// SetupProfileLogging tees the global logger to <dir>/<name>.log (truncated on
// each start) in addition to stderr, so the dashboard's "View Logs" works in
// every run mode — directly-launched binary, dashboard-spawned subprocess, and
// in-process/Android. Previously only the spawned case captured output (via
// redirected stdout), so logs were invisible on Android and for a binary run
// by hand. Best-effort: if the file can't be opened, logging stays stderr-only.
//
// In-process mode shares one global logger, so with multiple concurrent
// profiles the most-recently-started one owns the log file; the common case is
// a single active profile.
func SetupProfileLogging(dir, name string) {
	if dir == "" || name == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, name+".log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}

// MarkRunning registers a profile as running in-process (called from JNI
// nativeStartProfile) along with its stop function.
func MarkRunning(name string, stop func()) {
	procMu.Lock()
	inProcessRunning[name] = stop
	procMu.Unlock()
}

// MarkStopped removes a profile from the in-process running set (called
// from JNI nativeStopProfile/nativeDeleteProfile after actually stopping it).
func MarkStopped(name string) {
	procMu.Lock()
	delete(inProcessRunning, name)
	procMu.Unlock()
}

// SetChannelStore sets the shared channel map (called from JNI init).
func SetChannelStore(ch map[string]*stalker.Channel) { channelStore = ch }

// startProfileInProcess loads a profile and starts all services in the current
// process using per-profile HLS and proxy instances for multi-profile isolation.
func startProfileInProcess(name string) error {
	SetupProfileLogging(profDir, name)
	p, ok := store.Get(name)
	if !ok {
		return os.ErrNotExist
	}
	c, err := stalker.LoadProfile(store, name)
	if err != nil {
		return err
	}

	// Create per-profile instances for isolation.
	hlsInst := hls.NewInstance()
	proxyInst := proxy.NewInstance(c)

	// Store instances so stopProcess can reach them.
	inProcessInstancesMu.Lock()
	inProcessInstances[name] = &inProcessState{
		hlsInst:   hlsInst,
		proxyInst: proxyInst,
		config:    c,
	}
	inProcessInstancesMu.Unlock()

	// Bind the public listeners immediately so the ports are reachable within
	// milliseconds, before the (possibly slow) portal handshake.
	if c.HLS.Enabled {
		hlsInst.SetUserAgent(c.Portal.Model)
		hlsInst.SetDeviceHeaders(c.Portal.MAC, c.Portal.Model, c.Portal.SerialNumber)
		go hlsInst.Serve(c.HLS.Bind)
	}
	if c.Proxy.Enabled {
		go proxyInst.Serve(c.Proxy.Bind)
	}

	// Start portal, fetch channels, then publish them to the listening services.
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
		snapshot := channelStore
		chanMu.Unlock()
		log.Printf("Profile %s: loaded %d channels", name, len(chs))

		if c.HLS.Enabled {
			hlsInst.SetChannels(snapshot)
		}
		if c.Proxy.Enabled {
			proxyInst.SetChannels(snapshot)
			// Also retrieve radio channels (non-fatal)
			if radio, err := c.Portal.RetrieveRadioChannels(); err == nil {
				proxyInst.SetRadioChannels(radio)
			}
		}

		if c.HLS.Enabled {
			c.Portal.IsPlayingFunc = hlsInst.IsPlaying
		}
		if err := c.Portal.StartWatchdog(); err != nil {
			log.Printf("Portal %s: failed to start watchdog: %v", name, err)
		}
	}()

	// Register as running with a real stop function.
	inProcessRunning[name] = func() {
		c.Portal.StopWatchdog()
		if c.HLS.Enabled {
			hlsInst.Stop()
		}
		if c.Proxy.Enabled {
			proxyInst.Stop()
		}
		inProcessInstancesMu.Lock()
		delete(inProcessInstances, name)
		inProcessInstancesMu.Unlock()
	}

	log.Printf("Started profile %s in-process (HLS=%s Proxy=%s)", name, c.HLS.Bind, c.Proxy.Bind)
	_ = p
	return nil
}

// Start initializes the dashboard HTTP server. If this binary is itself
// running a profile directly (the standalone single-profile mode in
// cmd/stalkerhek/main.go), the caller must register it via MarkRunning with
// a real stop function before calling Start — never via a fake
// self-referencing os.Process here, which would get Kill()ed by
// stopProcess and take down the whole engine (the exact bug this dashboard
// package's processes/inProcessRunning split was introduced to fix).
func Start(dir string, bind string, s *db.Store) {
	store = s
	profDir = dir
	os.MkdirAll(profDir, 0755)

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
	html := strings.Replace(dashboardHTML, "/*__QRCODE_JS__*/", qrcodeJS, 1)
	w.Write([]byte(html))
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
		} else if _, ok := inProcessRunning[p.Name]; ok {
			r.Status = "running"
			r.PID = os.Getpid()
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
	if !validProfileName(name) {
		http.Error(w, "invalid name", 400)
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

	// Standalone binary mode: spawn a new process. The child engine self-logs
	// to <profDir>/<name>.log via SetupProfileLogging, so we don't redirect its
	// stdout/stderr here — doing so would write every line twice.
	cmd := exec.Command(req.Binary, "-profile", req.Name, "-db", profDir)
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
	var req struct {
		Name string `json:"name"`
	}
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
	if !validProfileName(name) {
		http.Error(w, "invalid name", 400)
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

// stopProcess stops a running profile, however it was started. Callers must
// hold procMu. Real subprocesses (standalone-binary mode) are killed; an
// in-process profile (JNI/Android mode) is stopped via its own stop
// function — it must never be Kill()ed, since there's no separate OS
// process for it and the registered handle previously pointed at this
// process's own PID, which would take down the whole engine.
func stopProcess(name string) {
	if proc, ok := processes[name]; ok {
		proc.Kill()
		proc.Wait()
		delete(processes, name)
		log.Printf("Stopped profile %s", name)
		return
	}
	if stop, ok := inProcessRunning[name]; ok {
		stop()
		delete(inProcessRunning, name)
		log.Printf("Stopped in-process profile %s", name)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// validProfileName guards the filesystem-touching handlers: a profile name is
// used as a single path element for its <name>.log file, so it must not contain
// path separators or parent references. Without this, /api/profiles/logs?name=
// (a query param, which ServeMux does NOT path-clean) could read arbitrary
// .log files via "../" traversal.
func validProfileName(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
		return false
	}
	return !strings.ContainsAny(name, `/\`+"\x00")
}
