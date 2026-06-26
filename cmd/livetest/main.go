package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"time"

	"github.com/erkexzcx/stalkerhek/hls"
	"github.com/erkexzcx/stalkerhek/proxy"
	"github.com/erkexzcx/stalkerhek/stalker"
)

func main() {
	log.SetFlags(log.Ltime)

	// --- Mock Portal ---
	mockPortal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		q := r.URL.Query()
		action := q.Get("action")
		typ := q.Get("type")

		switch {
		case action == "handshake":
			w.Write([]byte(`{"js":{"token":"mock-token-abc123","random":"mock-random-xyz789"}}`))
		case action == "get_profile":
			w.Write([]byte(`{"js":{"status":"0","watchdog_timeout":30}}`))
		case action == "get_localization":
			w.Write([]byte(`{"js":{}}`))
		case action == "get_modules":
			w.Write([]byte(`{"js":{"all_modules":[],"disabled_modules":[]}}`))
		case action == "get_all_channels" && typ == "itv":
			w.Write([]byte(`{"js":{"data":[
				{"id":"1","name":"BBC One","cmd":"ffmpeg http://cdn.example.com/live/bbc_one/index.m3u8?token=secret123","logo":"bbc_one.png","tv_genre_id":"1","cmds":[{"id":"cmd_bbc","ch_id":"1001"}]},
				{"id":"2","name":"CNN HD","cmd":"ffmpeg http://cdn.example.com/live/cnn_hd/index.m3u8?token=secret456","logo":"cnn.png","tv_genre_id":"2","cmds":[{"id":"cmd_cnn","ch_id":"1002"}]},
				{"id":"3","name":"Disмovery","cmd":"ffmpeg http://cdn.example.com/live/discovery/index.m3u8","logo":"disc.png","tv_genre_id":"3","cmds":[{"id":"cmd_disc","ch_id":"1003"}]}
			]}}`))
		case action == "get_genres" && typ == "itv":
			w.Write([]byte(`{"js":[{"id":"1","title":"Entertainment"},{"id":"2","title":"News"},{"id":"3","title":"Documentary"}]}`))
		case action == "get_ordered_list" && typ == "radio":
			w.Write([]byte(`{"js":{"data":[]}}`))
		case action == "create_link" && typ == "itv":
			cmd := q.Get("cmd")
			w.Write([]byte(fmt.Sprintf(`{"js":{"id":"link_1","cmd":"ffmpeg http://cdn.example.com/stream/%s/playlist.m3u8","streamer_id":1,"link_id":100,"load":0,"error":""}}`, cmd)))
		case action == "create_link" && (typ == "vod" || typ == "karaoke" || typ == "tv_archive"):
			w.Write([]byte(`{"js":{"cmd":"ffmpeg http://cdn.example.com/media/test.mpg","error":""}}`))
		case action == "get_events" && typ == "watchdog":
			w.Write([]byte(`{"js":{"data":{"msgs":0,"additional_services_on":"1"}},"text":"ok"}`))
		case action == "get_events" && typ == "log":
			w.Write([]byte(`{"js":1}`))
		default:
			log.Printf("Mock portal: action=%s type=%s", action, typ)
			w.Write([]byte(`{"js":true}`))
		}
	}))
	defer mockPortal.Close()

	log.Printf("Mock portal running at: %s", mockPortal.URL)

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

	// --- Start portal ---
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

	// --- Start HLS + Proxy ---
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

	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("✅ Stalkerheks live test environment ready")
	log.Println("")
	log.Println("   Proxy (STB entry):  http://0.0.0.0:8888/")
	log.Println("   HLS   (streams):    http://0.0.0.0:9999/iptv")
	log.Println("")
	log.Println("   Test commands:")
	log.Println("")
	log.Println("   # 1. Handshake (get token)")
	log.Println(`   curl -s 'http://0.0.0.0:8888/?type=stb&action=handshake&JsHttpRequest=1-xml' | jq .`)
	log.Println("")
	log.Println("   # 2. Get channel list (cmds rewritten)")
	log.Println(`   curl -s 'http://0.0.0.0:8888/?type=itv&action=get_all_channels&JsHttpRequest=1-xml' | jq .`)
	log.Println("")
	log.Println("   # 3. Create link for BBC One")
	log.Println(`   curl -s 'http://0.0.0.0:8888/?action=create_link&type=itv&cmd=ffmpeg+http://cdn.example.com/live/bbc_one/index.m3u8?token=secret123&JsHttpRequest=1-xml' | jq .`)
	log.Println("")
	log.Println("   # 4. HLS playlist")
	log.Println(`   curl -s 'http://0.0.0.0:9999/iptv'`)
	log.Println("")
	log.Println("   # 5. Watchdog")
	log.Println(`   curl -s 'http://0.0.0.0:8888/?type=watchdog&action=get_events&event_active_id=0&init=1&cur_play_type=1&JsHttpRequest=1-xml' | jq .`)
	log.Println("")
	log.Println("   Press Ctrl+C to stop")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Wait for Ctrl+C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	log.Println("Shutting down...")
}
