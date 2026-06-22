//go:build cgo

package main

/*
#include <jni.h>

static const char* jniReadString(JNIEnv* env, jstring str) {
    if (str == NULL) return "";
    return (*env)->GetStringUTFChars(env, str, NULL);
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

	"github.com/erkexzcx/stalkerhek/dashboard"
	"github.com/erkexzcx/stalkerhek/db"
	"github.com/erkexzcx/stalkerhek/hls"
	"github.com/erkexzcx/stalkerhek/proxy"
	"github.com/erkexzcx/stalkerhek/stalker"
)

var (
	store    *db.Store
	channels map[string]*stalker.Channel
	configs  = make(map[int]*stalker.Config)
	nextID   = 1
)

func readStr(env *C.JNIEnv, str C.jstring) string {
	return C.GoString(C.jniReadString(env, str))
}

func makeStr(env *C.JNIEnv, s string) C.jstring {
	return C.jniMakeString(env, C.CString(s))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeInit
func Java_com_stalkerhek_app_engine_EngineBridge_nativeInit(env *C.JNIEnv, cls C.jclass, jdataDir C.jstring) C.jstring {
	dataDir := readStr(env, jdataDir)
	s, err := db.Open(dataDir + "/stalkerhek.db")
	if err != nil {
		return makeStr(env, `{"ok":false,"error":"`+err.Error()+`"}`)
	}
	store = s
	profiles := store.GetAll()

	// Mark in-process mode so the dashboard starts profiles without spawning a binary
	dashboard.SetInProcessMode()

	// Start dashboard web UI on 0.0.0.0:8080 (runs ListenAndServe in this goroutine)
	go dashboard.Start(dataDir, "0.0.0.0:8080", store, "")

	return makeStr(env, fmt.Sprintf(`{"ok":true,"profiles_loaded":%d}`, len(profiles)))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeStartProfile
func Java_com_stalkerhek_app_engine_EngineBridge_nativeStartProfile(env *C.JNIEnv, cls C.jclass, jprofileJson C.jstring) C.jstring {
	jsonStr := readStr(env, jprofileJson)

	// Extract just the profile name from Kotlin JSON — the full profile is already in the store.
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil || req.Name == "" {
		return makeStr(env, `{"phase":"error","message":"Invalid JSON or missing name","running":false}`)
	}

	c, err := stalker.LoadProfile(store, req.Name)
	if err != nil {
		return makeStr(env, fmt.Sprintf(`{"phase":"error","message":"%s","running":false}`, err.Error()))
	}

	// Save config for status lookups
	id := nextID
	configs[id] = c
	nextID++

	// Register in dashboard's process tracker so /api/profiles shows "running"
	dashboard.MarkRunning(req.Name)

	// Start portal + fetch channels, then start HLS + proxy in parallel goroutines
	go func() {
		log.Printf("Connecting to portal %s...", req.Name)
		if err := c.Portal.Start(); err != nil {
			log.Printf("Portal %s: %v", req.Name, err)
		}
		chs, err := c.Portal.RetrieveChannels()
		if err != nil {
			log.Printf("Channels %s: %v", req.Name, err)
		}
		channels = chs
		log.Printf("Profile %s: loaded %d channels", req.Name, len(chs))

		if c.HLS.Enabled {
			go func() {
				hls.SetUserAgent(c.Portal.Model)
				hls.SetDeviceHeaders(c.Portal.MAC, c.Portal.Model, c.Portal.SerialNumber)
				hls.Start(channels, c.HLS.Bind)
			}()
		}
		if c.Proxy.Enabled {
			go func() {
				proxy.Start(c, channels)
			}()
		}
	}()

	hlsPort := c.HLS.Bind[strings.LastIndexByte(c.HLS.Bind, ':')+1:]
	proxyPort := c.Proxy.Bind[strings.LastIndexByte(c.Proxy.Bind, ':')+1:]
	return makeStr(env, fmt.Sprintf(
		`{"phase":"starting","message":"OK","hls_addr":"0.0.0.0:%s","proxy_addr":"0.0.0.0:%s","running":true,"channels_count":%d}`,
		hlsPort, proxyPort, len(channels)))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeStopProfile
func Java_com_stalkerhek_app_engine_EngineBridge_nativeStopProfile(env *C.JNIEnv, cls C.jclass, jid C.jint) C.jstring {
	return makeStr(env, `{"ok":"true"}`)
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeGetChannels
func Java_com_stalkerhek_app_engine_EngineBridge_nativeGetChannels(env *C.JNIEnv, cls C.jclass, jid C.jint, jtype C.jstring) C.jstring {
	if channels == nil {
		return makeStr(env, `[]`)
	}
	type chInfo struct {
		Cmd     string `json:"cmd"`
		Title   string `json:"title"`
		Genre   string `json:"genre"`
		GenreID string `json:"genreId"`
	}
	list := make([]chInfo, 0, len(channels))
	for _, ch := range channels {
		list = append(list, chInfo{Cmd: ch.CMD, Title: ch.Title, GenreID: ch.GenreID})
	}
	b, _ := json.Marshal(list)
	return makeStr(env, string(b))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeGetProfiles
func Java_com_stalkerhek_app_engine_EngineBridge_nativeGetProfiles(env *C.JNIEnv, cls C.jclass) C.jstring {
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
func Java_com_stalkerhek_app_engine_EngineBridge_nativeGetProfileStatus(env *C.JNIEnv, cls C.jclass, jid C.jint) C.jstring {
	id := int(jid)
	c, ok := configs[id]
	if !ok || c == nil {
		return makeStr(env, `{"phase":"idle","message":"Not started","running":false,"channels_count":0,"hls_addr":"","proxy_addr":""}`)
	}
	hlsPort := c.HLS.Bind[strings.LastIndexByte(c.HLS.Bind, ':')+1:]
	proxyPort := c.Proxy.Bind[strings.LastIndexByte(c.Proxy.Bind, ':')+1:]
	return makeStr(env, fmt.Sprintf(
		`{"phase":"running","message":"OK","running":true,"channels_count":%d,"hls_addr":"0.0.0.0:%s","proxy_addr":"0.0.0.0:%s"}`,
		len(channels), hlsPort, proxyPort))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeCreateProfile
func Java_com_stalkerhek_app_engine_EngineBridge_nativeCreateProfile(env *C.JNIEnv, cls C.jclass, jprofileJson C.jstring) C.jstring {
	jsonStr := readStr(env, jprofileJson)
	var p db.Profile
	json.Unmarshal([]byte(jsonStr), &p)
	store.Save(p)
	b, _ := json.Marshal(p)
	return makeStr(env, string(b))
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeDeleteProfile
func Java_com_stalkerhek_app_engine_EngineBridge_nativeDeleteProfile(env *C.JNIEnv, cls C.jclass, jid C.jint) C.jstring {
	return makeStr(env, `{"ok":"true"}`)
}

//export Java_com_stalkerhek_app_engine_EngineBridge_nativeShutdown
func Java_com_stalkerhek_app_engine_EngineBridge_nativeShutdown(env *C.JNIEnv, cls C.jclass) C.jstring {
	return makeStr(env, `{"ok":"true"}`)
}
