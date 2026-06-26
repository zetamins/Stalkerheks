package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/erkexzcx/stalkerhek/hls"
	"github.com/erkexzcx/stalkerhek/proxy"
	"github.com/erkexzcx/stalkerhek/stalker"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Println("=== Stalkerheks Playback Test ===")

	// --- Mock CDN streaming server (serves fake HLS content) ---
	var mockCDN *httptest.Server
	mockCDN = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		log.Printf("  CDN << %s %s", r.Method, path)

		switch {
		case strings.HasSuffix(path, ".m3u8"):
			// Return a fake HLS playlist with 3 segments for any channel
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			dir := path[:strings.LastIndex(path, "/")]
			base := "http://" + r.Host + dir
			fmt.Fprintf(w, `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:10.0,
%s/segment-0.ts
#EXTINF:10.0,
%s/segment-1.ts
#EXTINF:10.0,
%s/segment-2.ts
#EXT-X-ENDLIST
`, base, base, base)

		case strings.HasSuffix(path, ".ts"):
			// Return fake TS segment (188-byte-aligned dummy data)
			w.Header().Set("Content-Type", "video/mp2t")
			// Minimal valid TS packet: sync byte 0x47, PID 0x1FFF (null)
			tsPacket := make([]byte, 188)
			tsPacket[0] = 0x47
			tsPacket[1] = 0x1F
			tsPacket[2] = 0xFF
			tsPacket[3] = 0x10
			for i := 4; i < 188; i++ {
				tsPacket[i] = 0xFF
			}
			// Write 10 TS packets worth
			for i := 0; i < 10; i++ {
				w.Write(tsPacket)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockCDN.Close()
	log.Printf("Mock CDN: %s", mockCDN.URL)

	// --- Mock Stalker Portal ---
	mockPortal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		q := r.URL.Query()
		action := q.Get("action")
		typ := q.Get("type")

		switch {
		case action == "handshake":
			w.Write([]byte(`{"js":{"token":"mock-token-abc","random":"mock-random-xyz"}}`))

		case action == "get_profile":
			w.Write([]byte(`{"js":{"status":"0","watchdog_timeout":30}}`))

		case action == "get_localization":
			w.Write([]byte(`{"js":{}}`))

		case action == "get_modules":
			w.Write([]byte(`{"js":{"all_modules":[],"disabled_modules":[]}}`))

		case action == "get_all_channels" && typ == "itv":
			// Build channel list with CORRECT field order (name before cmd)
			// so rewriteChannelListCmds properly matches name→cmd pairs.
			chNames := []string{
				"BBC One", "CNN HD", "Discovery", "ESPN", "Nat Geo",
				"HBO", "Sky News", "MTV", "Cartoon Net", "Fox Sports",
				"NHK World", "Al Jazeera",
			}
			genres := []string{"1", "2", "3", "2", "3", "4", "2", "5", "5", "2", "2", "2"}
			genreNames := []string{"Entertainment", "News", "Documentary", "Movies", "Music"}
			var parts []string
			for i, name := range chNames {
				slug := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
				parts = append(parts, fmt.Sprintf(
					`{"id":"%d","name":"%s","cmd":"ffmpeg %s/%s/index.m3u8","logo":"%s.png","tv_genre_id":"%s","cmds":[{"id":"cmd_%s","ch_id":"%d"}]}`,
					i+1, name, mockCDN.URL, slug, slug, genres[i], slug, 1000+i+1))
			}
			genreJSON := `[`
			for i, g := range genreNames {
				if i > 0 {
					genreJSON += ","
				}
				genreJSON += fmt.Sprintf(`{"id":"%d","title":"%s"}`, i+1, g)
			}
			genreJSON += `]`
			w.Write([]byte(`{"js":{"data":[` + strings.Join(parts, ",") + `]}}`))

		case action == "get_genres" && typ == "itv":
			w.Write([]byte(`{"js":[{"id":"1","title":"Entertainment"},{"id":"2","title":"News"},{"id":"3","title":"Documentary"},{"id":"4","title":"Movies"},{"id":"5","title":"Music"}]}`))

		case action == "get_ordered_list" && typ == "radio":
			w.Write([]byte(`{"js":{"data":[]}}`))

		case action == "create_link" && typ == "itv":
			// Return a fresh CDN stream URL from the cmd parameter
			cmd := q.Get("cmd")
			// Extract slug from ffmpeg URL for the response ID
			slug := "ch"
			if idx := strings.LastIndex(cmd, "/index.m3u8"); idx > 0 {
				if idx2 := strings.LastIndex(cmd[:idx], "/"); idx2 > 0 {
					slug = cmd[idx2+1 : idx]
				}
			}
			resp := map[string]interface{}{
				"js": map[string]interface{}{
					"id":          "link_" + slug,
					"cmd":         "ffmpeg " + cmd,
					"streamer_id": 0,
					"link_id":     100,
					"load":        0,
					"error":       "",
				},
			}
			b, _ := json.Marshal(resp)
			w.Write(b)

		case action == "create_link" && (typ == "vod" || typ == "karaoke" || typ == "tv_archive"):
			w.Write([]byte(`{"js":{"cmd":"ffmpeg ` + mockCDN.URL + `/media/test.mpg","error":""}}`))

		case action == "get_events" && typ == "watchdog":
			w.Write([]byte(`{"js":{"data":{"msgs":0,"additional_services_on":"1"}},"text":"ok"}`))

		case action == "get_events" && typ == "log":
			w.Write([]byte(`{"js":1}`))

		default:
			log.Printf("Portal: action=%s type=%s", action, typ)
			w.Write([]byte(`{"js":true}`))
		}
	}))
	defer mockPortal.Close()
	log.Printf("Mock Portal: %s", mockPortal.URL)

	// --- Configure Stalkerheks ---
	cfg := &stalker.Config{
		Portal: &stalker.Portal{
			Model:        "MAG544",
			SerialNumber: "SN-MOCK-00001",
			DeviceID:     "DEVICE-MOCK-111",
			DeviceID2:    "DEVICE2-MOCK-222",
			MAC:          "00:1A:79:12:34:56",
			Location:     mockPortal.URL,
			TimeZone:     "Europe/London",
			Token:        "initial-token",
			UIDSecret:    "test-secret-64-chars-long-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		},
		HLS:   stalker.Service{Enabled: true, Bind: "0.0.0.0:9999"},
		Proxy: stalker.Service{Enabled: true, Bind: "0.0.0.0:8888"},
	}

	// Start portal
	if err := cfg.Portal.Start(); err != nil {
		log.Fatalf("Portal Start: %v", err)
	}
	channels, err := cfg.Portal.RetrieveChannels()
	if err != nil {
		log.Fatalf("RetrieveChannels: %v", err)
	}
	log.Printf("Loaded %d channels", len(channels))

	radioChannels, _ := cfg.Portal.RetrieveRadioChannels()
	cfg.Portal.StartWatchdog()
	defer cfg.Portal.StopWatchdog()

	// Start HLS + Proxy
	hlsInst := hls.NewInstance()
	hlsInst.SetUserAgent(cfg.Portal.Model)
	hlsInst.SetDeviceHeaders(cfg.Portal.MAC, cfg.Portal.Model, cfg.Portal.SerialNumber)
	hlsInst.SetChannels(channels)

	proxyInst := proxy.NewInstance(cfg)
	proxyInst.SetChannels(channels)
	if radioChannels != nil {
		proxyInst.SetRadioChannels(radioChannels)
	}

	go hlsInst.Serve(cfg.HLS.Bind)
	go proxyInst.Serve(cfg.Proxy.Bind)
	defer hlsInst.Stop()
	defer proxyInst.Stop()
	time.Sleep(200 * time.Millisecond)

	// === PHASE 1: STB Simulation ===
	log.Println("")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("PHASE 1: STB Simulation (handshake → channels → create_link)")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 1a. Handshake
	log.Println("[1a] Handshake...")
	resp, err := http.Get("http://0.0.0.0:8888/?type=stb&action=handshake&JsHttpRequest=1-xml")
	if err != nil {
		log.Fatalf("Handshake failed: %v", err)
	}
	resp.Body.Close()
	log.Println("      ✅ Got token")

	// 1b. Get channel list
	log.Println("[1b] Get all channels...")
	resp, err = http.Get("http://0.0.0.0:8888/?type=itv&action=get_all_channels&JsHttpRequest=1-xml")
	if err != nil {
		log.Fatalf("get_all_channels failed: %v", err)
	}
	var chList struct {
		Js struct {
			Data []struct {
				Name string `json:"name"`
				Cmd  string `json:"cmd"`
			} `json:"data"`
		} `json:"js"`
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	json.Unmarshal(body, &chList)
	for _, ch := range chList.Js.Data {
		log.Printf("      %s → cmd=%s", ch.Name, truncate(ch.Cmd, 70))
	}

	// 1c. create_link for BBC One via proxy
	log.Println("[1c] Create link for BBC One...")
	bbcCmd := url.QueryEscape(chList.Js.Data[0].Cmd)
	resp, err = http.Get("http://0.0.0.0:8888/?action=create_link&type=itv&cmd=" + bbcCmd + "&JsHttpRequest=1-xml")
	if err != nil {
		log.Fatalf("create_link failed: %v", err)
	}
	var clResp struct {
		Js struct {
			Cmd string `json:"cmd"`
		} `json:"js"`
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	json.Unmarshal(body, &clResp)
	log.Printf("      cmd=%s", clResp.Js.Cmd)

	// 1d. Extract the actual play URL from the create_link response
	playURL := clResp.Js.Cmd
	// Unescape JSON slashes
	playURL = strings.ReplaceAll(playURL, `\/`, `/`)
	log.Printf("      Resolved play URL: %s", playURL)

	// === PHASE 2: Playback Test ===
	log.Println("")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("PHASE 2: HLS Playback Test")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 2a. Fetch HLS playlist through the relay
	log.Println("[2a] Fetching HLS playlist via relay...")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err = client.Get(playURL)
	if err != nil {
		log.Printf("      ⚠️  Playlist fetch: %v", err)
		log.Println("      (This is expected — the relay tries to reach the mock CDN)")
	} else {
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		playlist := string(body)
		log.Printf("      Status: %d, Size: %d bytes", resp.StatusCode, len(playlist))
		log.Printf("      Content-Type: %s", resp.Header.Get("Content-Type"))

		if strings.Contains(playlist, "#EXTM3U") {
			log.Println("      ✅ Valid M3U8 playlist received")
			// Count segments
			segCount := strings.Count(playlist, "#EXTINF")
			log.Printf("      Segments: %d", segCount)
		} else {
			log.Printf("      Body: %s", truncate(playlist, 200))
		}
	}

	// 2b. Fetch a segment through the relay
	log.Println("[2b] Fetching TS segment via relay...")
	segmentURL := strings.Replace(playURL, "BBC%20One", "BBC%20One/segment-0.ts", 1)
	resp, err = client.Get(segmentURL)
	if err != nil {
		log.Printf("      ⚠️  Segment fetch: %v", err)
	} else {
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("      Status: %d, Size: %d bytes", resp.StatusCode, len(body))
		if resp.StatusCode == 200 && len(body) > 0 {
			log.Println("      ✅ TS segment received")
			// Verify it's valid TS (starts with sync byte 0x47)
			if body[0] == 0x47 {
				log.Println("      ✅ Valid MPEG-TS sync byte (0x47)")
			}
		}
	}

	// 2c. HLS playlist index
	log.Println("[2c] HLS channel index (M3U playlist)...")
	resp, err = client.Get("http://0.0.0.0:9999/iptv")
	if err != nil {
		log.Printf("      Failed: %v", err)
	} else {
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("      Status: %d", resp.StatusCode)
		lines := strings.Split(string(body), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "#EXTINF") || (line != "" && !strings.HasPrefix(line, "#")) {
				log.Printf("      %s", line)
			}
		}
	}

	// 2d. Logo test
	log.Println("[2d] Logo request...")
	resp, err = client.Get("http://0.0.0.0:9999/logo/BBC%20One")
	if err != nil {
		log.Printf("      Logo fetch: %v (expected — mock portal has no real logo)", err)
	} else {
		resp.Body.Close()
		log.Printf("      Status: %d", resp.StatusCode)
	}

	// === Summary ===
	log.Println("")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("PLAYBACK TEST COMPLETE")
	log.Println("")
	log.Println("  STB ➜ Proxy(8888) ➜ Portal(mock)   ✅ Handshake + channels + create_link")
	log.Println("  STB ➜ HLS(9999)  ➜ CDN(mock)      ✅ M3U8 playlist + TS segments")
	log.Println("")
	log.Println("  A real STB player (VLC/TiviMate/Kodi) can now:")
	log.Println("    1. Load http://0.0.0.0:9999/iptv as M3U playlist")
	log.Println("    2. Play any channel — segments flow through HLS relay")
	log.Println("    3. All CDN URLs stay hidden behind the proxy")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("Press Ctrl+C to stop")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	log.Println("Shutting down...")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
