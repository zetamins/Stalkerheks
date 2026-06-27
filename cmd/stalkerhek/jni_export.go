//go:build cgo

package main

/*
#include <jni.h>
#include <stdlib.h>

static const char* jniReadString(JNIEnv* env, jstring str) {
    if (str == NULL) return NULL;
    return (*env)->GetStringUTFChars(env, str, NULL);
}

static void jniReleaseString(JNIEnv* env, jstring str, const char* cs) {
    if (str != NULL && cs != NULL) (*env)->ReleaseStringUTFChars(env, str, cs);
}

static jstring jniMakeString(JNIEnv* env, const char* s) {
    return (*env)->NewStringUTF(env, s);
}
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"unsafe"

	"github.com/erkexzcx/stalkerhek/dashboard"
	"github.com/erkexzcx/stalkerhek/db"
	"github.com/erkexzcx/stalkerhek/hls"
	"github.com/erkexzcx/stalkerhek/proxy"
	"github.com/erkexzcx/stalkerhek/stalker"
)

var (
	store *db.Store

	// jniDataDir is the Android app data dir (holds stalkerhek.db and the
	// per-profile .log files the dashboard reads). Captured in nativeInit.
	jniDataDir string

	// dashboardStarted guards the single dashboard HTTP listener. nativeInit is
	// re-called on engine restart; starting dashboard.Start twice makes the
	// second ListenAndServe(:8080) fail with "address already in use".
	dashboardStarted bool

	// configsByName/channelsByName are keyed by profile name — the only
	// stable identity profiles actually have (db.Store has no integer ID).
	configsByName  = make(map[string]*stalker.Config)
	channelsByName = make(map[string]map[string]*stalker.Channel)
	stateMu        sync.Mutex

	// jniHLSInstances/jniProxyInstances hold per-profile service instances
	// for multi-profile isolation in JNI/Android mode.
	jniHLSInstances   = make(map[string]*hls.Instance)
	jniProxyInstances = make(map[string]*proxy.Instance)
)

// profileNameByID resolves a positional JNI/Kotlin-facing id (1-based, same
// order as nativeGetProfiles) back to a profile name.
func profileNameByID(id int) (string, bool) {
	profiles := store.GetAll()
	if id < 1 || id > len(profiles) {
		return "", false
	}
	return profiles[id-1].Name, true
}

func readStr(env *C.JNIEnv, str C.jstring) string {
	if str == 0 {
		return ""
	}
	// GetStringUTFChars pins a JNI char buffer that must be released, or it
	// leaks on every call; C.GoString copies it first so the Go string stays
	// valid after release.
	cs := C.jniReadString(env, str)
	if cs == nil {
		return ""
	}
	defer C.jniReleaseString(env, str, cs)
	return C.GoString(cs)
}

func makeStr(env *C.JNIEnv, s string) C.jstring {
	// C.CString mallocs a copy that NewStringUTF then duplicates into a Java
	// String — the malloc'd buffer must be freed or it leaks on every call.
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return C.jniMakeString(env, cs)
}

// jniRecover turns a panic inside a JNI-exported function into a logged error
// response instead of crashing the app. A Go panic that unwinds across the cgo
// boundary aborts the whole process (SIGABRT) — no Kotlin try/catch can save it
// — so every exported call defers this with its named return. Used as:
//
//	func Java_..._x(env *C.JNIEnv, ...) (result C.jstring) {
//	    defer jniRecover(env, "x", &result)
//	    ...
//	}
func jniRecover(env *C.JNIEnv, name string, out *C.jstring) {
	if r := recover(); r != nil {
		log.Printf("JNI %s panic recovered: %v", name, r)
		*out = makeStr(env, fmt.Sprintf(`{"ok":false,"running":false,"error":"engine panic in %s"}`, name))
	}
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeInit
func Java_com_stalkerhek_app_engine_EngineBridge_nativeInit(env *C.JNIEnv, cls C.jclass, jdataDir C.jstring) (result C.jstring) {
	defer jniRecover(env, "nativeInit", &result)
	dataDir := readStr(env, jdataDir)
	jniDataDir = dataDir
	s, err := db.Open(dataDir + "/stalkerhek.db")
	if err != nil {
		return makeStr(env, `{"ok":false,"error":"`+err.Error()+`"}`)
	}
	store = s
	profiles := store.GetAll()

	// Mark in-process mode so the dashboard starts profiles without spawning a binary
	dashboard.SetInProcessMode()

	// Route the dashboard "Start" button through the JNI start path so both share
	// one set of per-profile HLS/proxy instances (no duplicate listeners / port
	// collision when a profile is started from both the app and the dashboard).
	dashboard.SetInProcessStarter(func(name string) error {
		_, err := jniStartProfile(name)
		return err
	})

	// Start dashboard web UI on 0.0.0.0:8080 once (it outlives engine restarts).
	if !dashboardStarted {
		dashboardStarted = true
		go dashboard.Start(dataDir, "0.0.0.0:8080", store)
	}

	return makeStr(env, fmt.Sprintf(`{"ok":true,"profiles_loaded":%d}`, len(profiles)))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeStartProfile
func Java_com_stalkerhek_app_engine_EngineBridge_nativeStartProfile(env *C.JNIEnv, cls C.jclass, jprofileJson C.jstring) (result C.jstring) {
	defer jniRecover(env, "nativeStartProfile", &result)
	jsonStr := readStr(env, jprofileJson)

	// Extract just the profile name from Kotlin JSON — the full profile is already in the store.
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil || req.Name == "" {
		return makeStr(env, `{"phase":"error","message":"Invalid JSON or missing name","running":false}`)
	}

	c, err := jniStartProfile(req.Name)
	if err != nil {
		return makeStr(env, fmt.Sprintf(`{"phase":"error","message":"%s","running":false}`, err.Error()))
	}

	hlsPort := c.HLS.Bind[strings.LastIndexByte(c.HLS.Bind, ':')+1:]
	proxyPort := c.Proxy.Bind[strings.LastIndexByte(c.Proxy.Bind, ':')+1:]
	return makeStr(env, fmt.Sprintf(
		`{"phase":"starting","message":"OK","hls_addr":"0.0.0.0:%s","proxy_addr":"0.0.0.0:%s","running":true,"channels_count":0}`,
		hlsPort, proxyPort))
}

// jniStartProfile loads a profile and starts all its services in-process: it
// tees logs, registers the profile in the dashboard's process tracker, creates
// per-profile HLS/proxy instances, binds their listeners immediately, and
// connects to the portal + fetches channels in the background.
//
// It is the single in-process start path on Android: the JNI nativeStartProfile
// calls it directly, and the dashboard "Start" button calls it via the callback
// registered with dashboard.SetInProcessStarter. Without this sharing the two
// paths kept separate instance state and could spin up duplicate HLS/proxy
// servers on the same ports (bind collision) for one profile.
func jniStartProfile(name string) (*stalker.Config, error) {
	// Tee engine logs to <dataDir>/<name>.log so the dashboard "View Logs" shows
	// them on Android (in-process mode writes no per-profile log otherwise).
	dashboard.SetupProfileLogging(jniDataDir, name)

	c, err := stalker.LoadProfile(store, name)
	if err != nil {
		return nil, err
	}

	// Stop any previous run of this profile first (idempotent restart).
	stopProfileByName(name)

	stateMu.Lock()
	configsByName[name] = c
	stateMu.Unlock()

	// Register in dashboard's process tracker so /api/profiles shows
	// "running", with the actual stop function (never a fake PID).
	dashboard.MarkRunning(name, func() { stopProfileByName(name) })

	// Create per-profile instances for multi-profile isolation.
	hlsInst := hls.NewInstance()
	proxyInst := proxy.NewInstance(c)

	// Store instances so stopProfileByName can clean them up.
	stateMu.Lock()
	jniHLSInstances[name] = hlsInst
	jniProxyInstances[name] = proxyInst
	stateMu.Unlock()

	// Bind the public listeners immediately so :HLS/:proxy are reachable within
	// milliseconds — before the (possibly minutes-long, on flaky networks)
	// portal handshake. Channel data is injected via SetChannels once the
	// portal list is retrieved; until then channel requests return 503.
	if c.HLS.Enabled {
		hlsInst.SetUserAgent(c.Portal.Model)
		hlsInst.SetDeviceHeaders(c.Portal.MAC, c.Portal.Model, c.Portal.SerialNumber)
		go hlsInst.Serve(c.HLS.Bind)
	}
	if c.Proxy.Enabled {
		go proxyInst.Serve(c.Proxy.Bind)
	}

	// Connect to the portal + fetch channels in the background, then publish
	// the channel list to the already-listening services.
	go func() {
		// A panic in this background goroutine (portal I/O, JSON parsing,
		// channel publish) would otherwise abort the whole app — recover and log.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Profile %s background goroutine panic recovered: %v", name, r)
			}
		}()
		log.Printf("Connecting to portal %s...", name)
		if err := c.Portal.Start(); err != nil {
			log.Printf("Portal %s: %v", name, err)
		}
		chs, err := c.Portal.RetrieveChannels()
		if err != nil {
			log.Printf("Channels %s: %v", name, err)
		}
		stateMu.Lock()
		channelsByName[name] = chs
		stateMu.Unlock()
		log.Printf("Profile %s: loaded %d channels", name, len(chs))

		if c.HLS.Enabled {
			hlsInst.SetChannels(chs)
		}
		if c.Proxy.Enabled {
			proxyInst.SetChannels(chs)
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

	return c, nil
}

// stopProfileByName stops a running profile's watchdog/HLS/proxy and clears
// its in-memory state. Safe to call on a profile that isn't running.
func stopProfileByName(name string) {
	stateMu.Lock()
	c, ok := configsByName[name]
	delete(configsByName, name)
	delete(channelsByName, name)
	hlsInst := jniHLSInstances[name]
	delete(jniHLSInstances, name)
	proxyInst := jniProxyInstances[name]
	delete(jniProxyInstances, name)
	stateMu.Unlock()

	if ok && c != nil {
		c.Portal.StopWatchdog()
	}
	// Stop per-profile instances directly (multi-profile safe).
	if hlsInst != nil {
		hlsInst.Stop()
	}
	if proxyInst != nil {
		proxyInst.Stop()
	}
	dashboard.MarkStopped(name)
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeStopProfile
func Java_com_stalkerhek_app_engine_EngineBridge_nativeStopProfile(env *C.JNIEnv, cls C.jclass, jid C.jint) (result C.jstring) {
	defer jniRecover(env, "nativeStopProfile", &result)
	name, ok := profileNameByID(int(jid))
	if !ok {
		return makeStr(env, `{"ok":false,"error":"unknown profile id"}`)
	}
	stopProfileByName(name)
	return makeStr(env, `{"ok":true}`)
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeGetChannels
func Java_com_stalkerhek_app_engine_EngineBridge_nativeGetChannels(env *C.JNIEnv, cls C.jclass, jid C.jint, jtype C.jstring) (result C.jstring) {
	defer jniRecover(env, "nativeGetChannels", &result)
	name, ok := profileNameByID(int(jid))
	if !ok {
		return makeStr(env, `[]`)
	}
	stateMu.Lock()
	chs := channelsByName[name]
	stateMu.Unlock()
	if chs == nil {
		return makeStr(env, `[]`)
	}
	type chInfo struct {
		Cmd     string `json:"cmd"`
		Title   string `json:"title"`
		Genre   string `json:"genre"`
		GenreID string `json:"genreId"`
	}
	list := make([]chInfo, 0, len(chs))
	for _, ch := range chs {
		list = append(list, chInfo{Cmd: ch.CMD, Title: ch.Title, Genre: ch.Genre(), GenreID: ch.GenreID})
	}
	b, _ := json.Marshal(list)
	return makeStr(env, string(b))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeGetProfiles
func Java_com_stalkerhek_app_engine_EngineBridge_nativeGetProfiles(env *C.JNIEnv, cls C.jclass) (result C.jstring) {
	defer jniRecover(env, "nativeGetProfiles", &result)
	profiles := store.GetAll()
	type po struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		PortalURL string `json:"portalUrl"`
		MAC       string `json:"mac"`
	}
	list := make([]po, 0)
	for i, p := range profiles {
		list = append(list, po{ID: i + 1, Name: p.Name, PortalURL: p.Portal.URL, MAC: p.Portal.MAC})
	}
	b, _ := json.Marshal(list)
	return makeStr(env, string(b))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeGetProfileStatus
func Java_com_stalkerhek_app_engine_EngineBridge_nativeGetProfileStatus(env *C.JNIEnv, cls C.jclass, jid C.jint) (result C.jstring) {
	defer jniRecover(env, "nativeGetProfileStatus", &result)
	name, ok := profileNameByID(int(jid))
	if !ok {
		return makeStr(env, `{"phase":"idle","message":"Not started","running":false,"channels_count":0,"hls_addr":"","proxy_addr":""}`)
	}
	stateMu.Lock()
	c, running := configsByName[name]
	chs := channelsByName[name]
	stateMu.Unlock()
	if !running || c == nil {
		return makeStr(env, `{"phase":"idle","message":"Not started","running":false,"channels_count":0,"hls_addr":"","proxy_addr":""}`)
	}
	hlsPort := c.HLS.Bind[strings.LastIndexByte(c.HLS.Bind, ':')+1:]
	proxyPort := c.Proxy.Bind[strings.LastIndexByte(c.Proxy.Bind, ':')+1:]
	return makeStr(env, fmt.Sprintf(
		`{"phase":"running","message":"OK","running":true,"channels_count":%d,"hls_addr":"0.0.0.0:%s","proxy_addr":"0.0.0.0:%s"}`,
		len(chs), hlsPort, proxyPort))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeCreateProfile
func Java_com_stalkerhek_app_engine_EngineBridge_nativeCreateProfile(env *C.JNIEnv, cls C.jclass, jprofileJson C.jstring) (result C.jstring) {
	defer jniRecover(env, "nativeCreateProfile", &result)
	jsonStr := readStr(env, jprofileJson)

	// The Kotlin ProfileConfig is a FLAT shape (portalUrl/mac/hls_port at the
	// top level), but db.Profile is nested (portal{...}, services{...}). A plain
	// Unmarshal into db.Profile would leave portal/services empty, producing an
	// unusable profile — so map the flat fields explicitly.
	var f struct {
		Name         string `json:"name"`
		PortalURL    string `json:"portalUrl"`
		MAC          string `json:"mac"`
		Model        string `json:"model"`
		SerialNumber string `json:"serial_number"`
		DeviceID     string `json:"device_id"`
		DeviceID2    string `json:"device_id2"`
		Signature    string `json:"signature"`
		TimeZone     string `json:"timezone"`
		HLSPort      int    `json:"hls_port"`
		ProxyPort    int    `json:"proxy_port"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &f); err != nil || f.Name == "" {
		return makeStr(env, `{"error":"invalid profile JSON or missing name"}`)
	}

	hlsPort := f.HLSPort
	if hlsPort == 0 {
		hlsPort = 9999
	}
	proxyPort := f.ProxyPort
	if proxyPort == 0 {
		proxyPort = 8888
	}
	p := db.Profile{
		Name: f.Name,
		Portal: db.PortalConfig{
			Model:        f.Model,
			SerialNumber: f.SerialNumber,
			DeviceID:     f.DeviceID,
			DeviceID2:    f.DeviceID2,
			Signature:    f.Signature,
			MAC:          f.MAC,
			URL:          f.PortalURL,
			TimeZone:     f.TimeZone,
		},
		Services: db.ServiceConfig{
			ProxyBind: fmt.Sprintf("0.0.0.0:%d", proxyPort),
			HLSBind:   fmt.Sprintf("0.0.0.0:%d", hlsPort),
		},
	}
	// Save fills in token/uid_secret (and device_id2/signature are derived on
	// load), like a profile created through the dashboard.
	if err := store.Save(p); err != nil {
		return makeStr(env, fmt.Sprintf(`{"error":%q}`, err.Error()))
	}

	// Respond in the flat shape Kotlin's ProfileConfig decodes.
	out, _ := json.Marshal(struct {
		Name      string `json:"name"`
		PortalURL string `json:"portalUrl"`
		MAC       string `json:"mac"`
	}{p.Name, p.Portal.URL, p.Portal.MAC})
	return makeStr(env, string(out))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeDeleteProfile
func Java_com_stalkerhek_app_engine_EngineBridge_nativeDeleteProfile(env *C.JNIEnv, cls C.jclass, jid C.jint) (result C.jstring) {
	defer jniRecover(env, "nativeDeleteProfile", &result)
	name, ok := profileNameByID(int(jid))
	if !ok {
		return makeStr(env, `{"ok":false,"error":"unknown profile id"}`)
	}
	stopProfileByName(name)
	if err := store.Delete(name); err != nil {
		return makeStr(env, fmt.Sprintf(`{"ok":false,"error":"%s"}`, err.Error()))
	}
	return makeStr(env, `{"ok":true}`)
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeShutdown
func Java_com_stalkerhek_app_engine_EngineBridge_nativeShutdown(env *C.JNIEnv, cls C.jclass) (result C.jstring) {
	defer jniRecover(env, "nativeShutdown", &result)
	stateMu.Lock()
	names := make([]string, 0, len(configsByName))
	for name := range configsByName {
		names = append(names, name)
	}
	stateMu.Unlock()
	for _, name := range names {
		stopProfileByName(name)
	}
	return makeStr(env, `{"ok":true}`)
}
