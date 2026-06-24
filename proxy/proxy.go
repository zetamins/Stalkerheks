package proxy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/erkexzcx/stalkerhek/stalker"
)

var (
	destination string

	config *stalker.Config

	channels map[string]*stalker.Channel

	serverMu sync.Mutex
	server   *http.Server

	// perSTBMu is a map of per-client-IP mutexes. Requests from the same STB are
	// serialized to avoid Cloudflare 520 errors from concurrent bursts. Different
	// STBs use different mutexes so they don't block each other.
	perSTBMu   sync.Mutex
	perSTBLock = make(map[string]*sync.Mutex)

	// randomHex is a persistent 32-char hex random used for handshake responses.
	// Real portal generates a fresh random per handshake; we use a stable one so all
	// STBs behind the proxy share the same random (and thus the same signature base).
	randomHex string
)

// lockSTB returns the mutex for a given client IP and locks it.
// The caller must call unlockSTB to release.
func lockSTB(clientIP string) {
	perSTBMu.Lock()
	mu, ok := perSTBLock[clientIP]
	if !ok {
		mu = &sync.Mutex{}
		perSTBLock[clientIP] = mu
	}
	perSTBMu.Unlock()
	mu.Lock()
}

func unlockSTB(clientIP string) {
	perSTBMu.Lock()
	mu := perSTBLock[clientIP]
	perSTBMu.Unlock()
	if mu != nil {
		mu.Unlock()
	}
}

func init() {
	randomHex = generateRandomHex(32)
}

// generateRandomHex returns a hex-encoded string of n random bytes.
func generateRandomHex(n int) string {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		// fallback in the astronomically unlikely event of crypto/rand failure
		for i := range b {
			b[i] = byte(i ^ 0xA5)
		}
	}
	return hex.EncodeToString(b)
}

// Start starts main routine.
func Start(c *stalker.Config, chs map[string]*stalker.Channel) {
	config = c

	// Channels will be matched by CMD field, not by title
	newChannels := make(map[string]*stalker.Channel)
	for _, v := range chs {
		newChannels[v.CMD] = v
	}
	channels = newChannels

	// extract scheme://hostname:port from given URL, so we don't have to do it later
	link, err := url.Parse(config.Portal.Location)
	if err != nil {
		log.Fatalln(err)
	}
	destination = link.Scheme + "://" + link.Host

	mux := http.NewServeMux()
	mux.HandleFunc("/vod/", vodHandler)
	mux.HandleFunc("/_weather/", weatherProxyHandler)
	mux.HandleFunc("/_geo/", geoProxyHandler)
	mux.HandleFunc("/_speedtest/", speedtestProxyHandler)
	mux.HandleFunc("/", requestHandler)

	srv := &http.Server{Addr: config.Proxy.Bind, Handler: mux}
	serverMu.Lock()
	server = srv
	serverMu.Unlock()

	log.Println("Proxy service should be started!")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println("Proxy ListenAndServe error:", err)
	}
}

// Stop gracefully shuts down the proxy HTTP server, if running. Safe to call
// even if Start was never called or has already returned.
func Stop() {
	serverMu.Lock()
	srv := server
	server = nil
	serverMu.Unlock()
	if srv != nil {
		srv.Shutdown(context.Background())
	}
}

func requestHandler(w http.ResponseWriter, r *http.Request) {
	log.Println(r.RequestURI)

	query := r.URL.Query()

	var tagAction string
	if tmp, found := query["action"]; found {
		tagAction = tmp[0]
	}

	var tagType string
	if tmp, found := query["type"]; found {
		tagType = tmp[0]
	}

	var tagCMD string
	if tmp, found := query["cmd"]; found {
		tagCMD = tmp[0]
	}

	// ################################################
	// Ignore/fake some requests

	// Handshake
	if tagAction == "handshake" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Set-Cookie", "PHPSESSID=null; path=/;")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"js":{"token":"` + config.Portal.Token + `","random":"` + randomHex + `","not_valid":0},"text":"generated in: 0.01s; query counter: 1; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))
		return
	}

	// Watchdog — forwarded to the portal so it knows the device's play state
	// (cur_play_type). This is critical for maintaining stream priority/sessions.
	// The portal's response is stripped of events before reaching the STB.
	// No longer faked — goes through the normal forwarding path with response filtering.

	// Log
	if tagAction == "get_events" && tagType == "log" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"js":1,"text":"generated in: 0.001s; query counter: 7; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))
		return
	}

	// Authentication
	if tagAction == "do_auth" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"js":true,"text":"array(2) {\n  [\"status\"]=>\n  string(2) \"OK\"\n  [\"results\"]=>\n  bool(true)\n}\ngenerated in: 1.033s; query counter: 7; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))
		return
	}

	// Logout
	if tagAction == "logout" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"js":true,"text":"generated in: 0.011s; query counter: 4; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))
		return
	}

	// Rewrite links — only intercept live TV (itv). VOD, karaoke, archive etc.
	// use base64-encoded CMDs that must be forwarded to the real portal.
	if tagAction == "create_link" && (tagType == "itv" || tagType == "") {
		if tagCMD == "" {
			log.Println("STB requested 'create_link', but did not give 'cmd' key in URL query...")
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Find Stalker channel
		channel, found := channels[tagCMD]
		if !found {
			log.Println("STB requested 'create_link', but gave invalid CMD:", tagCMD)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// We must give full path to IPTV stream.
		requestHost, _, _ := net.SplitHostPort(r.Host)
		_, portHLS, _ := net.SplitHostPort(config.HLS.Bind)
		destination = "http://" + requestHost + ":" + portHLS + "/iptv/" + url.PathEscape(channel.Title)

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		responseText := generateNewChannelLink(destination, channel.CMD_ID, channel.CMD_CH_ID)
		w.Write([]byte(responseText))

		fmt.Println(responseText)

		return
	}

	// ################################################
	// Rewrite URL query values — replace all device identifiers
	// with the configured identity so the portal sees only one device.

	// Serial number
	if _, exists := query["sn"]; exists {
		query["sn"] = []string{config.Portal.SerialNumber}
	}

	// Device ID
	if _, exists := query["device_id"]; exists {
		query["device_id"] = []string{config.Portal.DeviceID}
	}

	// Device ID2
	if _, exists := query["device_id2"]; exists {
		query["device_id2"] = []string{config.Portal.DeviceID2}
	}

	// Signature
	if _, exists := query["signature"]; exists {
		query["signature"] = []string{config.Portal.Signature}
	}

	// STB type / model
	if _, exists := query["stb_type"]; exists {
		query["stb_type"] = []string{config.Portal.Model}
	}
	if _, exists := query["model"]; exists {
		query["model"] = []string{config.Portal.Model}
	}

	// Version strings
	if _, exists := query["ver"]; exists {
		query["ver"] = []string{buildVersion()}
	}
	if _, exists := query["image_version"]; exists {
		query["image_version"] = []string{"0x00000015"}
	}
	if _, exists := query["hw_version"]; exists {
		query["hw_version"] = []string{"1.0.00"}
	}

	// Metrics — JSON blob containing mac, sn, model, type, uid
	if _, exists := query["metrics"]; exists {
		query["metrics"] = []string{buildMetrics()}
	}

	// Misc device parameters — set to safe defaults
	if _, exists := query["video_out"]; exists {
		query["video_out"] = []string{"hdmi"}
	}
	if _, exists := query["num_banks"]; exists {
		query["num_banks"] = []string{"1"}
	}
	if _, exists := query["client_type"]; exists {
		query["client_type"] = []string{"STB"}
	}
	if _, exists := query["hd"]; exists {
		query["hd"] = []string{"1"}
	}

	// Hashes — computed with SHA-1 to match GetHashVersion1 behavior on real MAG boxes
	if _, exists := query["hw_version_2"]; exists {
		query["hw_version_2"] = []string{buildHWVersion2()}
	}
	if _, exists := query["api_signature"]; exists {
		query["api_signature"] = []string{"256"}
	}
	if _, exists := query["prehash"]; exists {
		query["prehash"] = []string{buildPrehash()}
	}

	// ################################################
	// Proxy modified request to real Stalker portal and return the response

	// Build (modified) URL
	finalLink := destination + r.URL.Path
	if len(r.URL.RawQuery) != 0 {
		finalLink += "?" + query.Encode()
	}

	// Serialize requests per STB (by client IP) to avoid Cloudflare 520 errors
	// from concurrent bursts, while allowing different STBs to proceed in parallel.
	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	lockSTB(clientIP)
	defer unlockSTB(clientIP)

	resp, err := getRequestWithRetry(finalLink, r)
	if err != nil {
		log.Printf("ERROR forwarding %s: %v", tagAction, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	log.Printf("  -> %s status=%d size=%d", tagAction, resp.StatusCode, resp.ContentLength)

	// Read response body so we can optionally modify it
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	body := string(bodyBytes)

	// Force use_http_tmp_link=1 and use_load_balancing=1 in channel list responses
	// so the STB calls create_link (which we rewrite to HLS) instead of playing
	// direct stream URLs that bypass the proxy.
	if tagAction == "get_all_channels" || tagAction == "get_all_fav_channels" || tagAction == "get_ordered_list" {
		body = strings.ReplaceAll(body, `"use_http_tmp_link":"0"`, `"use_http_tmp_link":"1"`)
		body = strings.ReplaceAll(body, `"use_http_tmp_link":0`, `"use_http_tmp_link":1`)
		body = strings.ReplaceAll(body, `"use_load_balancing":"0"`, `"use_load_balancing":"1"`)
		body = strings.ReplaceAll(body, `"use_load_balancing":0`, `"use_load_balancing":1`)
		log.Printf("  -> %s body modified (forced tmp_link)", tagAction)
	}

	// For VOD/karaoke/archive create_link responses, rewrite the stream URL
	// in the cmd field to route through our proxy so media traffic is tunneled.
	if tagAction == "create_link" && tagType != "itv" && tagType != "" {
		body = rewriteVodResponse(body, r.Host)
	}

	// Watchdog: forward to portal to maintain stream priority/sessions,
	// but strip any portal-to-STB events (reboot, reload, messages, etc.)
	// so the STB behind the proxy doesn't receive commands meant for the
	// proxy's own device identity.
	if tagAction == "get_events" && tagType == "watchdog" {
		body = `{"js":{"data":{"msgs":0,"additional_services_on":"1"}},"text":"generated in: 0.01s; query counter: 4; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`
		log.Printf("  -> watchdog forwarded, response filtered")
	}

	// Rewrite third-party service URLs in portal JS/HTML/CSS responses so
	// weather, geo, and speed test requests go through the proxy instead of
	// leaking the STB's real IP and device info to external services.
	if strings.Contains(r.URL.Path, ".js") || strings.Contains(r.URL.Path, ".html") || r.URL.Path == "/c/" || r.URL.Path == "/" {
		body = rewriteExternalURLs(body, r.Host)
	}

	// Send response — forward the real portal's headers but override Set-Cookie
	// to keep STB browser cookie state clean
	addHeaders(resp.Header, w.Header())
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
	w.Header().Set("Set-Cookie", "PHPSESSID=null; path=/;")
	w.WriteHeader(resp.StatusCode)
	w.Write(bodyBytes)
}

// vodHandler reverse-proxies VOD media content. The STB requests
// /vod/<base64url-encoded-target-url> and we fetch the real URL and stream it back.
func vodHandler(w http.ResponseWriter, r *http.Request) {
	encoded := strings.TrimPrefix(r.URL.Path, "/vod/")
	if encoded == "" {
		http.Error(w, "missing target URL", http.StatusBadRequest)
		return
	}

	targetURL, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		http.Error(w, "invalid target URL encoding", http.StatusBadRequest)
		return
	}

	log.Printf("VOD proxy: %s", targetURL)

	req, err := http.NewRequest("GET", string(targetURL), nil)
	if err != nil {
		http.Error(w, "bad target URL", http.StatusBadRequest)
		return
	}

	// Send device-identifying headers — the streaming server uses these
	// for device recognition and stream priority assignment.
	req.Header.Set("Mac", config.Portal.MAC)
	req.Header.Set("Model", config.Portal.Model)
	req.Header.Set("X-Hash", buildPrehash()) // same hash sent by real MAG STBs

	// Forward range headers so seeking works
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("VOD fetch error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers (Content-Type, Content-Length, Accept-Ranges, etc.)
	for k, v := range resp.Header {
		if k == "Set-Cookie" {
			continue
		}
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// rewriteVodResponse rewrites the stream URL in a VOD create_link JSON response
// to route through the /vod/ proxy endpoint.
func rewriteVodResponse(body string, requestHost string) string {
	// The portal response has "cmd":"ffmpeg http://..." or "cmd":"http://..."
	// Find the URL portion and replace with our proxied version.
	// Look for "cmd":"<stuff>" and replace any http:// URL inside with /vod/<base64>
	prefixes := []string{`"cmd":"ffmpeg `, `"cmd":"`}
	for _, prefix := range prefixes {
		idx := strings.Index(body, prefix)
		if idx < 0 {
			continue
		}
		start := idx + len(prefix)
		// Find the end of the URL (closing quote or space before closing quote)
		end := strings.IndexAny(body[start:], ` "\`)
		if end < 0 {
			end = len(body) - start
		}
		originalURL := body[start : start+end]
		if strings.HasPrefix(originalURL, "http") {
			encoded := base64.URLEncoding.EncodeToString([]byte(originalURL))
			// The STB's command format: "ffmpeg http://<host>/vod/<encoded>"
			newURL := "http://" + requestHost + "/vod/" + encoded
			// Replace in the original prefix format
			if strings.HasPrefix(prefix, `"cmd":"ffmpeg `) {
				body = strings.Replace(body,
					`"cmd":"ffmpeg `+originalURL,
					`"cmd":"ffmpeg `+newURL, 1)
			} else {
				body = strings.Replace(body,
					`"cmd":"`+originalURL,
					`"cmd":"`+newURL, 1)
			}
			log.Printf("  -> VOD URL rewritten: %s -> /vod/...", originalURL[:min(50, len(originalURL))])
		}
		break
	}
	return body
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// rewriteExternalURLs replaces third-party service URLs in portal JS/HTML
// responses with proxy endpoints so device info doesn't leak to external services.
func rewriteExternalURLs(body, host string) string {
	rules := []struct{ from, to string }{
		{"http://weather.infomir.com.ua/", "http://" + host + "/_weather/"},
		{"http://nominatim.openstreetmap.org/", "http://" + host + "/_geo/"},
		{"https://update.infomir.com/speedtest/", "http://" + host + "/_speedtest/"},
	}
	for _, r := range rules {
		body = strings.ReplaceAll(body, r.from, r.to)
	}
	return body
}

// weatherProxyHandler reverse-proxies weather service requests through the proxy.
// Rewrites device MAC/SN in query params to configured values.
func weatherProxyHandler(w http.ResponseWriter, r *http.Request) {
	target := "http://weather.infomir.com.ua" + strings.TrimPrefix(r.URL.Path, "/_weather")
	if r.URL.RawQuery != "" {
		q := r.URL.Query()
		if _, ok := q["mac"]; ok {
			q.Set("mac", config.Portal.MAC)
		}
		if _, ok := q["sn"]; ok {
			q.Set("sn", config.Portal.SerialNumber)
		}
		if _, ok := q["id"]; ok {
			q.Set("id", config.Portal.Model)
		}
		target += "?" + q.Encode()
	}
	log.Printf("Weather proxy: %s", target)
	req, _ := http.NewRequest("GET", target, nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, "weather service error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		if k != "Set-Cookie" {
			w.Header()[k] = v
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// geoProxyHandler proxies OpenStreetMap Nominatim geocoding requests.
func geoProxyHandler(w http.ResponseWriter, r *http.Request) {
	target := "http://nominatim.openstreetmap.org" + strings.TrimPrefix(r.URL.Path, "/_geo")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	log.Printf("Geo proxy: %s", target)
	req, _ := http.NewRequest("GET", target, nil)
	req.Header.Set("User-Agent", config.Portal.UserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, "geo service error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		if k != "Set-Cookie" {
			w.Header()[k] = v
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// speedtestProxyHandler proxies speed test configuration requests.
func speedtestProxyHandler(w http.ResponseWriter, r *http.Request) {
	target := "https://update.infomir.com/speedtest" + strings.TrimPrefix(r.URL.Path, "/_speedtest")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	log.Printf("Speedtest proxy: %s", target)
	req, _ := http.NewRequest("GET", target, nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, "speedtest service error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		if k != "Set-Cookie" {
			w.Header()[k] = v
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
