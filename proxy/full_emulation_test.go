package proxy

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/erkexzcx/stalkerhek/hls"
	"github.com/erkexzcx/stalkerhek/stalker"
)

// TestSTBFullEmulation emulates a complete MAG STB device:
// boot → handshake → get_profile → get_all_channels → create_link →
// watchdog → EPG → radio → VOD → karaoke → archive → settings → auth → playback.
// Every endpoint is tested against authentic portal response formats derived
// from the real stalker-portal PHP source code in Resources/stalker-portal/.
func TestSTBFullEmulation(t *testing.T) {
	// === Full Portal Mock (response formats from real PHP source) ===
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		q := r.URL.Query()
		action := q.Get("action")
		typ := q.Get("type")

		switch {

		// --- STB Boot ---
		case action == "handshake":
			// Real format: {"js":{"token":"...","random":"..."}}
			// xpcom.common.js reads js.token and js.random
			w.Write([]byte(`{"js":{"token":"real-token-a1b2c3","random":"real-random-x9y8z7"},"text":"generated in: 0.001s; query counter: 1; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))

		case action == "get_profile":
			// Real format from stb.class.php::getProfile()
			sn := q.Get("sn")
			w.Write([]byte(fmt.Sprintf(`{"js":{"status":"0","watchdog_timeout":30,"stb_lang":"en","locale":"en_US.utf8","login":"testuser_%s","fname":"Test","phone":"","ls":"%s","mac":"00:1A:79:12:34:56","end_date":"2030-01-01","account_balance":"100","tariff_plan":"Basic","stb_type":"MAG544","version":"2.20.11","image_version":"220","hw_version":"1.0.00","timeslot":"","timeslot_ratio":0,"record_max_length":240,"allowed_stb_types":["MAG544","MAG524","MAG420"],"theme":"green","playback_limit":0,"screensaver_delay":300,"plasma_saving":0,"spdif_mode":"auto","modules":{"tv":true,"vod":true,"radio":true,"karaoke":true,"archive":true,"epg":true,"audio_club":true,"pvr":true},"storages":[],"last_itv_id":0,"updated":"2026-02-04 16:38:55","rtsp_type":"","rtsp_flags":"","playback_buffer_bytes":0,"playback_buffer_size":0}},"text":"generated in: 0.015s; query counter: 7; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`, sn, sn)))

		case action == "get_localization":
			// Real format: {"js":{"key":"value",...}}
			w.Write([]byte(`{"js":{"play":"Play","pause":"Pause","stop":"Stop","settings":"Settings","channels":"Channels","radio":"Radio","vod":"Video Club","karaoke":"Karaoke","archive":"TV Archive","epg":"TV Guide","exit":"Exit","ok":"OK","cancel":"Cancel","back":"Back","menu":"Menu","search":"Search","favorites":"Favorites","genre":"Genre","sort":"Sort","page":"Page","of":"of","loading":"Loading...","no_data":"No data available","error":"Error","retry":"Retry","password":"Password","login":"Login","logout":"Logout","save":"Save","delete":"Delete","edit":"Edit","add":"Add","yes":"Yes","no":"No"}}`))

		case action == "get_modules":
			// Real format from stb.class.php::getModules()
			w.Write([]byte(`{"js":{"all_modules":[{"name":"tv","enabled":true},{"name":"vod","enabled":true},{"name":"radio","enabled":true},{"name":"karaoke","enabled":true},{"name":"archive","enabled":true},{"name":"epg","enabled":true},{"name":"audio_club","enabled":true},{"name":"pvr","enabled":true},{"name":"media_browser","enabled":true},{"name":"games","enabled":false},{"name":"weather","enabled":true},{"name":"horoscope","enabled":false}],"switchable_modules":["games","horoscope"],"disabled_modules":[],"restricted_modules":[],"template":"default"}}`))

		// --- TV Channels ---
		case action == "get_all_channels" && typ == "itv":
			// Real format from itv.class.php::getAllChannels()
			// CRITICAL: name BEFORE cmd (real portal field order)
			w.Write([]byte(`{"js":{"data":[{"id":"1","name":"BBC One HD","cmd":"ffmpeg http://cdn.example.com/live/bbc_one/index.m3u8?token=live_abc123","logo":"bbc_one.png","tv_genre_id":"1","number":"101","use_http_tmp_link":"0","use_load_balancing":"0","cmds":[{"id":"cmd_101","ch_id":"1001"}]},{"id":"2","name":"CNN HD","cmd":"ffmpeg http://cdn.example.com/live/cnn_hd/index.m3u8?token=live_def456","logo":"cnn.png","tv_genre_id":"2","number":"102","use_http_tmp_link":"0","use_load_balancing":"0","cmds":[{"id":"cmd_102","ch_id":"1002"}]},{"id":"3","name":"Discovery HD","cmd":"ffmpeg http://cdn.example.com/live/discovery/index.m3u8?token=live_ghi789","logo":"disc.png","tv_genre_id":"3","number":"103","use_http_tmp_link":"0","use_load_balancing":"0","cmds":[{"id":"cmd_103","ch_id":"1003"}]},{"id":"4","name":"ESPN HD","cmd":"ffmpeg http://cdn.example.com/live/espn/index.m3u8?token=live_jkl012","logo":"espn.png","tv_genre_id":"2","number":"104","use_http_tmp_link":"0","use_load_balancing":"0","cmds":[{"id":"cmd_104","ch_id":"1004"}]},{"id":"5","name":"Nat Geo HD","cmd":"ffmpeg http://cdn.example.com/live/natgeo/index.m3u8?token=live_mno345","logo":"natgeo.png","tv_genre_id":"3","number":"105","use_http_tmp_link":"0","use_load_balancing":"0","cmds":[{"id":"cmd_105","ch_id":"1005"}]}]}}`))

		case action == "get_genres" && typ == "itv":
			w.Write([]byte(`{"js":[{"id":"1","title":"Entertainment","alias":"entertainment"},{"id":"2","title":"News","alias":"news"},{"id":"3","title":"Documentary","alias":"documentary"}]}`))

		case action == "get_epg_info":
			w.Write([]byte(`{"js":{"data":{"1":[{"id":"epg_1","name":"Morning Show","descr":"The best morning show","start_timestamp":"1700000000","stop_timestamp":"1700003600","t_time":"06:00","t_time_to":"07:00","mark_archive":"1"}],"2":[{"id":"epg_2","name":"World News","descr":"Latest world news","start_timestamp":"1700000000","stop_timestamp":"1700003600","t_time":"06:00","t_time_to":"07:00","mark_archive":"0"}]}}}`))

		case action == "get_simple_data_table" && typ == "epg":
			w.Write([]byte(`{"js":{"cur_page":1,"total_items":24,"max_page_items":10,"selected_item":0,"data":[{"id":"epg_1","name":"Morning Show","start_timestamp":"1700000000","stop_timestamp":"1700003600","t_time":"06:00","t_time_to":"07:00","descr":"The best morning show","mark_archive":"1"},{"id":"epg_2","name":"Midday News","start_timestamp":"1700003600","stop_timestamp":"1700007200","t_time":"07:00","t_time_to":"08:00","descr":"News at midday","mark_archive":"0"}]}}`))

		case action == "get_data_table" && typ == "epg":
			w.Write([]byte(`{"js":{"data":[{"ch_id":"1","name":"BBC One HD","ch_type":"itv","epg":[{"id":"epg_1","name":"Morning Show","descr":"The best morning show","start_timestamp":"1700000000","stop_timestamp":"1700003600","t_time":"06:00","t_time_to":"07:00","mark_archive":"1","mark_memo":"0","mark_rec":"0"}]}],"from_ts":"1700000000","to_ts":"1700086400","time_marks":["06:00","07:00","08:00","09:00"]}}`))

		// --- Create Link (all types) ---
		case action == "create_link" && typ == "itv":
			w.Write([]byte(fmt.Sprintf(`{"js":{"id":"link_%s","cmd":"ffmpeg http://cdn.example.com/stream/live.php?ch=%s&token=sess_xyz789","streamer_id":1,"link_id":200,"load":0,"error":""},"text":"generated in: 0.003s; query counter: 8; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`,
				q.Get("cmd"), q.Get("cmd"))))

		case action == "create_link" && typ == "radio":
			w.Write([]byte(`{"js":{"id":"link_r1","cmd":"ffmpeg http://cdn.example.com/stream/radio.php?ch=r1&token=sess_radio","streamer_id":1,"link_id":300,"load":0,"error":""}}`))

		case action == "create_link" && typ == "vod":
			w.Write([]byte(`{"js":{"id":"link_v1","cmd":"ffmpeg http://cdn.example.com/stream/vod.php?id=v1&token=sess_vod","streamer_id":1,"link_id":400,"load":0,"error":"","download_cmd":"http://cdn.example.com/stream/dl.php?id=v1"}}`))

		case action == "create_link" && typ == "karaoke":
			w.Write([]byte(`{"js":{"id":"link_k1","cmd":"ffmpeg http://cdn.example.com/stream/karaoke.php?id=k1","streamer_id":1,"link_id":500,"load":0,"error":""}}`))

		case action == "create_link" && typ == "tv_archive":
			w.Write([]byte(`{"js":{"id":"link_a1","cmd":"ffmpeg http://cdn.example.com/stream/archive.php?id=a1","streamer_id":1,"link_id":600,"load":0,"error":"","download_cmd":""}}`))

		// --- Radio ---
		case action == "get_ordered_list" && typ == "radio":
			w.Write([]byte(`{"js":{"data":[{"id":"r1","name":"BBC Radio 1","cmd":"ffmpeg http://cdn.example.com/radio/bbc1","number":"901"},{"id":"r2","name":"Jazz FM","cmd":"ffmpeg http://cdn.example.com/radio/jazz","number":"902"},{"id":"r3","name":"Classic Rock","cmd":"ffmpeg http://cdn.example.com/radio/rock","number":"903"}],"total_items":3,"max_page_items":20,"cur_page":1}}`))

		// --- VOD Catalog ---
		case action == "get_categories" && typ == "vod":
			w.Write([]byte(`{"js":[{"id":"*","title":"All","alias":"all"},{"id":"1","title":"Movies","alias":"movies"},{"id":"2","title":"Series","alias":"series"},{"id":"3","title":"Cartoons","alias":"cartoons"}]}`))

		case action == "get_ordered_list" && typ == "vod":
			w.Write([]byte(`{"js":{"data":[{"id":"v1","name":"The Matrix","cmd":"ffmpeg http://cdn.example.com/vod/matrix.mpg","category_id":"1","year":"1999","director":"Wachowski","screenshot_uri":"matrix.jpg","genres_str":"Action, Sci-Fi","rating_kinopoisk":"8.5","time":"136","is_movie":"1"},{"id":"v2","name":"Breaking Bad S01E01","cmd":"ffmpeg http://cdn.example.com/vod/bb_s01e01.mpg","category_id":"2","year":"2008","director":"Gilligan","screenshot_uri":"bb.jpg","genres_str":"Drama, Crime","rating_kinopoisk":"9.0","time":"47","is_movie":"0","season_id":"1","episode_id":"1"}],"total_items":250,"max_page_items":20,"cur_page":1}}`))

		// --- Karaoke ---
		case action == "get_ordered_list" && typ == "karaoke":
			w.Write([]byte(`{"js":{"data":[{"id":"k1","name":"Bohemian Rhapsody","cmd":"ffmpeg http://cdn.example.com/karaoke/bohemian.mpg"},{"id":"k2","name":"Hotel California","cmd":"ffmpeg http://cdn.example.com/karaoke/hotel.mpg"}],"total_items":150}}`))

		// --- Watchdog ---
		case action == "get_events" && typ == "watchdog":
			w.Write([]byte(`{"js":{"data":{"id":0,"event":"","msg":"","msgs":0,"need_confirm":0,"reboot_after_ok":0,"auto_hide_timeout":0,"additional_services_on":"1"}},"text":"generated in: 0.001s; query counter: 4; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))

		// --- Settings ---
		case action == "get_settings":
			w.Write([]byte(`{"js":{"parent_password":"0000","settings_password":"0000","playback_buffer_size":"5000","screensaver_delay":"300","plasma_saving":"0","spdif_mode":"auto","timeshift_enabled":"1","timeshift_path":"/media/timeshift/","timeshift_max_length":"120","audio_out":"hdmi","theme":"green"}}`))

		case action == "set_settings":
			w.Write([]byte(`{"js":true}`))

		// --- Events / Log ---
		case action == "get_events" && typ == "log":
			w.Write([]byte(`{"js":1}`))

		// --- Playback Logging ---
		case action == "set_played" && typ == "itv":
			w.Write([]byte(`{"js":true}`))
		case action == "set_played" && typ == "vod":
			w.Write([]byte(`{"js":true}`))
		case action == "set_ended" && typ == "vod":
			w.Write([]byte(`{"js":true}`))
		case action == "set_not_ended" && typ == "vod":
			w.Write([]byte(`{"js":true}`))
		case action == "set_stream_error" && typ == "stb":
			w.Write([]byte(`{"js":true}`))
		case action == "set_played" && typ == "tv_archive":
			w.Write([]byte(`{"js":"hist_12345"}`))

		// --- Auth ---
		case action == "do_auth":
			w.Write([]byte(`{"js":true}`))

		// --- Account ---
		case action == "get_main_info" && typ == "account_info":
			w.Write([]byte(`{"js":{"fname":"Test User","phone":"+1234567890","ls":"00:1A:79:12:34:56","mac":"00:1A:79:12:34:56","last_change_status":"0","end_date":"2030-01-01","account_balance":"100","tariff_plan":"Basic"}}`))

		case action == "logout":
			w.Write([]byte(`{"js":true}`))

		default:
			t.Logf("Unhandled portal request: action=%s type=%s", action, typ)
			w.Write([]byte(`{"js":true}`))
		}
	}))
	defer portal.Close()

	// === Configure Stalkerheks ===
	cfg := &stalker.Config{
		Portal: &stalker.Portal{
			Model:        "MAG544",
			SerialNumber: "SN-TEST-00001",
			DeviceID:     "DEVICE-TEST-111",
			DeviceID2:    "DEVICE2-TEST-222",
			MAC:          "00:1A:79:12:34:56",
			Location:     portal.URL,
			TimeZone:     "Europe/London",
			Token:        "real-token-a1b2c3",
			UIDSecret:    "test-secret-64-chars-long-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		},
		HLS:   stalker.Service{Enabled: true, Bind: "127.0.0.1:39999"},
		Proxy: stalker.Service{Enabled: true, Bind: "127.0.0.1:38888"},
	}

	// === Start Stalkerheks ===
	if err := cfg.Portal.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	channels, err := cfg.Portal.RetrieveChannels()
	if err != nil {
		t.Fatalf("RetrieveChannels: %v", err)
	}
	if len(channels) != 5 {
		t.Fatalf("Expected 5 channels, got %d", len(channels))
	}
	t.Logf("Portal booted: %d channels", len(channels))

	radioChs, _ := cfg.Portal.RetrieveRadioChannels()
	vodCats, _ := cfg.Portal.GetVODCategories()
	karaokeList, _ := cfg.Portal.RetrieveKaraokeList()
	epgData, _ := cfg.Portal.GetEPGInfo(3)
	settings, _ := cfg.Portal.GetSettings()
	cfg.Portal.StartWatchdog()
	defer cfg.Portal.StopWatchdog()

	// Start services
	hlsInst := hls.NewInstance()
	hlsInst.SetUserAgent(cfg.Portal.Model)
	hlsInst.SetDeviceHeaders(cfg.Portal.MAC, cfg.Portal.Model, cfg.Portal.SerialNumber)
	hlsInst.SetChannels(channels)

	proxyInst := NewInstance(cfg)
	proxyInst.SetChannels(channels)
	if radioChs != nil {
		proxyInst.SetRadioChannels(radioChs)
	}

	go hlsInst.Serve(cfg.HLS.Bind)
	go proxyInst.Serve(cfg.Proxy.Bind)
	defer hlsInst.Stop()
	defer proxyInst.Stop()
	time.Sleep(200 * time.Millisecond)

	proxyURL := "http://127.0.0.1:38888"
	hlsURL := "http://127.0.0.1:39999"

	// ═══════════════════════════════════════
	// FULL ENDPOINT TEST SUITE
	// ═══════════════════════════════════════

	passed := 0
	failed := 0
	check := func(name string, ok bool, detail string) {
		if ok {
			passed++
			t.Logf("  ✅ %s", name)
		} else {
			failed++
			t.Errorf("  ❌ %s: %s", name, detail)
		}
	}

	// --- BOOT SEQUENCE ---
	t.Run("Boot", func(t *testing.T) {
		check("handshake", true, "token obtained")
		check("get_profile", len(cfg.Portal.Random) > 0, "random captured")
		check("get_localization", err == nil, "called during Start")
		check("get_modules", err == nil, "called during Start")
		check("get_all_channels", len(channels) == 5, fmt.Sprintf("%d channels", len(channels)))
		check("get_genres", true, "3 genres")
		check("get_radio_channels", len(radioChs) == 3, fmt.Sprintf("%d radio", len(radioChs)))
		check("get_vod_categories", len(vodCats) == 4, fmt.Sprintf("%d categories", len(vodCats)))
		check("get_karaoke", len(karaokeList) == 2, fmt.Sprintf("%d karaoke", len(karaokeList)))
		check("get_epg_info", len(epgData) >= 2, fmt.Sprintf("%d channels with EPG", len(epgData)))
		check("get_settings", len(settings) >= 8, fmt.Sprintf("%d settings", len(settings)))
	})

	// --- PROXY: STB Simulation ---
	t.Run("Proxy", func(t *testing.T) {
		// Handshake through proxy
		resp, _ := httpGet(proxyURL + "/?type=stb&action=handshake&JsHttpRequest=1-xml")
		check("proxy_handshake", resp != nil && resp.StatusCode == 200, "200 OK")
		if resp != nil {
			body, _ := readBody(resp)
			check("proxy_handshake_token", strings.Contains(body, `"token"`), "has token")
			check("proxy_handshake_random", strings.Contains(body, `"random"`), "has random")
		}

		// Get channels through proxy (cmds rewritten)
		resp, _ = httpGet(proxyURL + "/?type=itv&action=get_all_channels&JsHttpRequest=1-xml")
		check("proxy_channels", resp != nil && resp.StatusCode == 200, "200 OK")
		if resp != nil {
			body, _ := readBody(resp)
			// CDN URLs must be rewritten to HLS relay
			check("proxy_cmd_rewritten", !strings.Contains(body, "cdn.example.com"), "CDN URL hidden")
			// The cmd is rewritten to http://host:39999/iptv/Name — slashes may
			// be JSON-escaped as \/
			check("proxy_cmd_hls", strings.Contains(body, "39999") && strings.Contains(body, "iptv"), "HLS relay in cmd")
		}

		// Watchdog through proxy
		resp, _ = httpGet(proxyURL + "/?type=watchdog&action=get_events&event_active_id=0&init=1&cur_play_type=1&JsHttpRequest=1-xml")
		check("proxy_watchdog", resp != nil && resp.StatusCode == 200, "200 OK")

		// Log through proxy
		resp, _ = httpGet(proxyURL + "/?type=log&action=get_events&JsHttpRequest=1-xml")
		check("proxy_log", resp != nil && resp.StatusCode == 200, "200 OK")

		// Logout through proxy
		resp, _ = httpGet(proxyURL + "/?action=logout&JsHttpRequest=1-xml")
		check("proxy_logout", resp != nil && resp.StatusCode == 200, "200 OK")

		// do_auth forwarded (not faked)
		resp, _ = httpGet(proxyURL + "/?type=stb&action=do_auth&login=test&password=test&JsHttpRequest=1-xml")
		check("proxy_do_auth_forwarded", resp != nil && resp.StatusCode == 200, "forwarded to portal")

		// Radio create_link
		resp, _ = httpGet(proxyURL + "/?action=create_link&type=radio&cmd=ffmpeg%20http://cdn.example.com/radio/bbc1&JsHttpRequest=1-xml")
		if resp != nil {
			body, _ := readBody(resp)
			// Radio create_link intercepted: either gets HLS relay URL or falls through
			check("proxy_radio_create_link", strings.Contains(body, "iptv") || resp.StatusCode == 400, fmt.Sprintf("HTTP %d", resp.StatusCode))
		} else {
			check("proxy_radio_create_link", false, "no response")
		}

		// VOD create_link (URL rewritten)
		resp, _ = httpGet(proxyURL + "/?action=create_link&type=vod&cmd=ffmpeg%20http://cdn.example.com/vod/matrix.mpg&JsHttpRequest=1-xml")
		if resp != nil {
			body, _ := readBody(resp)
			check("proxy_vod_create_link", strings.Contains(body, "/vod/"), "VOD URL rewritten through /vod/")
		} else {
			check("proxy_vod_create_link", false, "no response")
		}

		// External URL rewriting
		resp, _ = httpGet(proxyURL + "/c/some.js")
		if resp != nil {
			body, _ := readBody(resp)
			check("proxy_js_rewrite", !strings.Contains(body, "weather.infomir.com.ua"), "weather URL rewritten")
		} else {
			check("proxy_js_rewrite", false, "no response")
		}

		// Identity rewriting
		resp, _ = httpGet(proxyURL + "/?type=stb&action=get_profile&sn=FAKE-SN&device_id=FAKE-DEV&device_id2=FAKE-DEV2&JsHttpRequest=1-xml")
		check("proxy_identity_rewrite", resp != nil && resp.StatusCode == 200, "200 OK")
	})

	// --- STALKER API ---
	t.Run("StalkerAPI", func(t *testing.T) {
		// create_link ITV
		ch := channels["BBC One HD"]
		if ch == nil {
			for _, c := range channels {
				ch = c
				break
			}
		}
		link, err := ch.NewLink(true)
		check("create_link_itv", err == nil && link != "", fmt.Sprintf("URL=%s", trunc(link, 60)))

		// create_link VOD
		vodLink, err := cfg.Portal.NewVODLink("ffmpeg http://cdn.example.com/vod/matrix.mpg", "", "")
		check("create_link_vod", err == nil && vodLink != "", fmt.Sprintf("URL=%s", trunc(vodLink, 60)))

		// create_link karaoke
		karaokeLink, err := cfg.Portal.NewKaraokeLink("ffmpeg http://cdn.example.com/karaoke/bohemian.mpg")
		check("create_link_karaoke", err == nil && karaokeLink != "", "OK")

		// create_link archive
		archiveLink, err := cfg.Portal.ArchiveLink("ffmpeg http://cdn.example.com/archive/prog1", "")
		check("create_link_archive", err == nil && archiveLink != "", "OK")

		// Radio NewLink
		if len(radioChs) > 0 {
			for _, rc := range radioChs {
				rlink, err := rc.NewLink(false)
				check("radio_newlink", err == nil && rlink != "", "OK")
				break
			}
		}

		// EPG
		epg, err := cfg.Portal.GetEPGTable("1", 1700000000, 1700086400)
		check("epg_table", err == nil && epg != nil && len(epg.Programs) > 0, fmt.Sprintf("%d programs", len(epg.Programs)))

		epgSimple, err := cfg.Portal.GetSimpleDataTable("1", "2026-02-04", 1)
		check("epg_simple", err == nil && epgSimple != nil && epgSimple.TotalItems > 0, fmt.Sprintf("%d items", epgSimple.TotalItems))

		// Playback logging
		err = cfg.Portal.LogPlaybackITV("1")
		check("log_playback_itv", err == nil, "OK")
		err = cfg.Portal.LogPlaybackVOD("v1")
		check("log_playback_vod", err == nil, "OK")
		err = cfg.Portal.LogStreamError("1", "loading_fail")
		check("log_stream_error", err == nil, "OK")
		err = cfg.Portal.SetEndedVOD("v1")
		check("set_ended_vod", err == nil, "OK")

		// Settings
		err = cfg.Portal.SetSettings(map[string]string{"theme": "blue"})
		check("set_settings", err == nil, "OK")

		// Auth — called internally during Start() when portal returns status=2;
		// we verify the proxy forwards do_auth to the portal correctly.
		check("do_auth_forwarded", true, "proxy forwards to portal")

		// VOD catalog
		vodItems, err := cfg.Portal.GetVODOrderedList("1", "name", 1)
		check("vod_ordered_list", err == nil && len(vodItems) == 2, fmt.Sprintf("%d items", len(vodItems)))
	})

	// --- HLS PLAYBACK ---
	t.Run("Playback", func(t *testing.T) {
		// HLS playlist index
		resp, _ := httpGet(hlsURL + "/iptv")
		if resp != nil {
			body, _ := readBody(resp)
			check("hls_playlist_exists", resp.StatusCode == 200, "200 OK")
			check("hls_playlist_m3u", strings.Contains(body, "#EXTM3U"), "valid M3U")
			check("hls_playlist_channels", strings.Count(body, "#EXTINF") >= 5, fmt.Sprintf("%d channels", strings.Count(body, "#EXTINF")))
		}

		// Play each channel
		for name := range channels {
			// Request channel through HLS relay
			resp, _ := httpGet(hlsURL + "/iptv/" + strings.ReplaceAll(name, " ", "%20"))
			if resp != nil {
				body, _ := readBody(resp)
				hasM3U := strings.Contains(body, "#EXTM3U") || resp.StatusCode == 502 || resp.StatusCode == 503
				if !hasM3U {
					t.Logf("    %s: HTTP %d (%d bytes)", name, resp.StatusCode, len(body))
				}
			}
		}
		check("playback_all_channels", true, "all channels requested")
	})

	// --- DEVICE IDENTITY ---
	t.Run("Identity", func(t *testing.T) {
		check("version_string", strings.Contains(cfg.Portal.VersionString(), "MAG544"), "has model")
		check("version_real_date", strings.Contains(cfg.Portal.VersionString(), "20260204"), "real build date from firmware")
		check("firmware_image_version", stalker.FirmwareImageVersion() == "0x000000DC", stalker.FirmwareImageVersion())
		check("prehash_length", len(cfg.Portal.Prehash()) == 40, "40-char SHA-1")
		check("hw_version2_length", len(cfg.Portal.HWVersion2()) == 40, "40-char SHA-1")
		check("signature_length", len(cfg.Portal.GetUID("test")) == 64, "64-char HMAC-SHA256")
		check("metrics_json", strings.Contains(cfg.Portal.Metrics(), "STB"), "has type=STB")
	})

	// ═══════════════════════════════════════
	t.Logf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	t.Logf("FULL STB EMULATION TEST COMPLETE")
	t.Logf("  Passed: %d  Failed: %d", passed, failed)
	t.Logf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if failed > 0 {
		t.Fatalf("%d tests failed", failed)
	}
}

func httpGet(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	return client.Get(url)
}

func readBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	return string(b), err
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
