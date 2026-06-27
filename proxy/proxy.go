package proxy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/erkexzcx/stalkerhek/stalker"
)

// Instance is a per-profile proxy server. Each profile gets its own Instance
// so multiple profiles can serve proxy connections concurrently on different
// ports without sharing device identity or channel state.
type Instance struct {
	config      *stalker.Config
	destination string
	randomHex   string

	channelsMu      sync.RWMutex
	channels        map[string]*stalker.Channel
	channelsByTitle map[string]*stalker.Channel

	radioChannelsMu      sync.RWMutex
	radioChannels        map[string]*stalker.RadioChannel
	radioChannelsByTitle map[string]*stalker.RadioChannel

	serverMu sync.Mutex
	server   *http.Server

	perSTBMu   sync.Mutex
	perSTBLock map[string]*sync.Mutex
}

// NewInstance creates a new proxy Instance configured for the given portal
// config. The HLS bind is extracted from config to rewrite create_link URLs.
func NewInstance(c *stalker.Config) *Instance {
	link, err := url.Parse(c.Portal.Location)
	if err != nil {
		log.Fatalln(err)
	}
	return &Instance{
		config:               c,
		destination:          link.Scheme + "://" + link.Host,
		randomHex:            generateRandomHex(32),
		channels:             make(map[string]*stalker.Channel),
		channelsByTitle:      make(map[string]*stalker.Channel),
		radioChannels:        make(map[string]*stalker.RadioChannel),
		radioChannelsByTitle: make(map[string]*stalker.RadioChannel),
		perSTBLock:           make(map[string]*sync.Mutex),
	}
}

// lockSTB returns the mutex for a given client IP and locks it.
func (inst *Instance) lockSTB(clientIP string) {
	inst.perSTBMu.Lock()
	mu, ok := inst.perSTBLock[clientIP]
	if !ok {
		mu = &sync.Mutex{}
		inst.perSTBLock[clientIP] = mu
	}
	inst.perSTBMu.Unlock()
	mu.Lock()
}

func (inst *Instance) unlockSTB(clientIP string) {
	inst.perSTBMu.Lock()
	mu := inst.perSTBLock[clientIP]
	inst.perSTBMu.Unlock()
	if mu != nil {
		mu.Unlock()
	}
}

// generateRandomHex returns a hex-encoded string of n random bytes.
func generateRandomHex(n int) string {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(i ^ 0xA5)
		}
	}
	return hex.EncodeToString(b)
}

// Serve binds the proxy listener and begins serving immediately — before
// channels are loaded — so the port is reachable within milliseconds.
func (inst *Instance) Serve(bind string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/vod/", inst.vodHandler)
	mux.HandleFunc("/_weather/", inst.weatherProxyHandler)
	mux.HandleFunc("/_geo/", inst.geoProxyHandler)
	mux.HandleFunc("/_speedtest/", inst.speedtestProxyHandler)
	mux.HandleFunc("/", inst.requestHandler)

	srv := &http.Server{Addr: bind, Handler: mux}
	inst.serverMu.Lock()
	inst.server = srv
	inst.serverMu.Unlock()

	log.Println("Proxy service listening on", bind)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println("Proxy ListenAndServe error:", err)
	}
}

// SetChannels populates the channel lookup maps for create_link and response
// rewriting.
func (inst *Instance) SetChannels(chs map[string]*stalker.Channel) {
	newChannels := make(map[string]*stalker.Channel, len(chs))
	for _, v := range chs {
		newChannels[v.CMD] = v
	}
	inst.channelsMu.Lock()
	inst.channels = newChannels
	inst.channelsByTitle = chs
	inst.channelsMu.Unlock()
}

// SetRadioChannels populates the radio channel lookup maps.
func (inst *Instance) SetRadioChannels(chs map[string]*stalker.RadioChannel) {
	newByCMD := make(map[string]*stalker.RadioChannel, len(chs))
	newByTitle := make(map[string]*stalker.RadioChannel, len(chs))
	for _, v := range chs {
		newByCMD[v.CMD] = v
		newByTitle[v.Title] = v
	}
	inst.radioChannelsMu.Lock()
	inst.radioChannels = newByCMD
	inst.radioChannelsByTitle = newByTitle
	inst.radioChannelsMu.Unlock()
}

// Stop gracefully shuts down the proxy HTTP server.
func (inst *Instance) Stop() {
	inst.serverMu.Lock()
	srv := inst.server
	inst.server = nil
	inst.serverMu.Unlock()
	if srv != nil {
		// A bounded shutdown so an in-flight long-lived request can't block the
		// restart indefinitely; force-close anything still open after the grace
		// period. (See hls.Instance.Stop — same live-stream hang.)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			srv.Close()
		}
	}
}

func (inst *Instance) requestHandler(w http.ResponseWriter, r *http.Request) {
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

	// Handshake — fake it for downstream STBs
	if tagAction == "handshake" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Set-Cookie", "PHPSESSID=null; path=/;")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"js":{"token":"` + inst.config.Portal.Token + `","random":"` + inst.randomHex + `","not_valid":0},"text":"generated in: 0.01s; query counter: 1; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))
		return
	}

	// Log — fake it
	if tagAction == "get_events" && tagType == "log" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"js":1,"text":"generated in: 0.001s; query counter: 7; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))
		return
	}

	// Logout — fake it
	if tagAction == "logout" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"js":true,"text":"generated in: 0.011s; query counter: 4; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`))
		return
	}

	// ITV create_link — intercept and rewrite to HLS URL
	if tagAction == "create_link" && (tagType == "itv" || tagType == "") {
		inst.handleITVCreateLink(w, r, tagCMD)
		return
	}

	// Radio create_link — intercept and rewrite to HLS URL
	if tagAction == "create_link" && tagType == "radio" {
		inst.handleRadioCreateLink(w, r, tagCMD)
		return
	}

	// ################################################
	// Rewrite URL query values with configured device identity

	if _, exists := query["sn"]; exists {
		query["sn"] = []string{inst.config.Portal.SerialNumber}
	}
	if _, exists := query["device_id"]; exists {
		query["device_id"] = []string{inst.config.Portal.DeviceID}
	}
	if _, exists := query["device_id2"]; exists {
		query["device_id2"] = []string{inst.config.Portal.DeviceID2}
	}
	if _, exists := query["signature"]; exists {
		query["signature"] = []string{inst.buildSignature()}
	}
	if _, exists := query["stb_type"]; exists {
		query["stb_type"] = []string{inst.config.Portal.Model}
	}
	if _, exists := query["model"]; exists {
		query["model"] = []string{inst.config.Portal.Model}
	}
	if _, exists := query["ver"]; exists {
		query["ver"] = []string{inst.buildVersion()}
	}
	if _, exists := query["image_version"]; exists {
		query["image_version"] = []string{stalker.FirmwareImageVersion()}
	}
	if _, exists := query["hw_version"]; exists {
		query["hw_version"] = []string{stalker.FirmwareHWVersion()}
	}
	if _, exists := query["metrics"]; exists {
		query["metrics"] = []string{inst.buildMetrics()}
	}
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
	if _, exists := query["hw_version_2"]; exists {
		query["hw_version_2"] = []string{inst.buildHWVersion2()}
	}
	if _, exists := query["api_signature"]; exists {
		query["api_signature"] = []string{stalker.FirmwareAPISignature()}
	}
	if _, exists := query["prehash"]; exists {
		query["prehash"] = []string{inst.buildPrehash()}
	}

	// ################################################
	// Proxy modified request to real portal

	finalLink := inst.destination + r.URL.Path
	if len(r.URL.RawQuery) != 0 {
		finalLink += "?" + query.Encode()
	}

	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	queuedAt := time.Now()
	inst.lockSTB(clientIP)
	waited := time.Since(queuedAt)
	defer inst.unlockSTB(clientIP)

	sentAt := time.Now()
	resp, err := getRequestWithRetry(finalLink, r, inst.config)
	if err != nil {
		log.Printf("ERROR forwarding %s after %v: %v", tagAction, time.Since(sentAt).Round(time.Millisecond), err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	log.Printf("  -> %s status=%d size=%d queue=%v upstream=%v", tagAction, resp.StatusCode, resp.ContentLength, waited.Round(time.Millisecond), time.Since(sentAt).Round(time.Millisecond))

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	body := string(bodyBytes)

	// Rewrite channel list responses
	if tagAction == "get_all_channels" || tagAction == "get_all_fav_channels" || tagAction == "get_ordered_list" {
		body = strings.ReplaceAll(body, `"use_http_tmp_link":"0"`, `"use_http_tmp_link":"1"`)
		body = strings.ReplaceAll(body, `"use_http_tmp_link":0`, `"use_http_tmp_link":1`)
		body = strings.ReplaceAll(body, `"use_load_balancing":"0"`, `"use_load_balancing":"1"`)
		body = strings.ReplaceAll(body, `"use_load_balancing":0`, `"use_load_balancing":1`)
		requestHost, _, _ := net.SplitHostPort(r.Host)
		body = inst.rewriteChannelListCmds(body, requestHost)
		log.Printf("  -> %s body modified (forced tmp_link, cmd rewritten)", tagAction)
	}

	// Rewrite VOD/karaoke/archive create_link responses
	if tagAction == "create_link" && tagType != "itv" && tagType != "" {
		body = inst.rewriteVodResponse(body, r.Host)
	}

	// Watchdog: forward, filter dangerous events
	if tagAction == "get_events" && tagType == "watchdog" {
		body = filterWatchdogEvents(body)
		log.Printf("  -> watchdog forwarded, events filtered")
	}

	// Rewrite third-party service URLs
	if strings.Contains(r.URL.Path, ".js") || strings.Contains(r.URL.Path, ".html") || r.URL.Path == "/c/" || r.URL.Path == "/" {
		body = inst.rewriteExternalURLs(body, r.Host)
	}

	addHeaders(resp.Header, w.Header())
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.Header().Set("Set-Cookie", "PHPSESSID=null; path=/;")
	w.WriteHeader(resp.StatusCode)
	w.Write([]byte(body))
}

// handleITVCreateLink handles the ITV create_link interception.
func (inst *Instance) handleITVCreateLink(w http.ResponseWriter, r *http.Request, tagCMD string) {
	if tagCMD == "" {
		log.Println("STB requested 'create_link', but did not give 'cmd' key in URL query...")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	inst.channelsMu.RLock()
	channel, found := inst.channels[tagCMD]
	if !found {
		if idx := strings.Index(tagCMD, "/iptv/"); idx >= 0 {
			if title, err := url.PathUnescape(tagCMD[idx+len("/iptv/"):]); err == nil {
				channel, found = inst.channelsByTitle[title]
			}
		}
	}
	inst.channelsMu.RUnlock()
	if !found {
		log.Println("STB requested 'create_link', but gave invalid CMD:", tagCMD)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	requestHost, _, _ := net.SplitHostPort(r.Host)
	_, portHLS, _ := net.SplitHostPort(inst.config.HLS.Bind)
	hlsURL := "http://" + requestHost + ":" + portHLS + "/iptv/" + url.PathEscape(channel.Title)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	responseText := generateNewChannelLink(hlsURL, channel.CMD_ID, channel.CMD_CH_ID)
	w.Write([]byte(responseText))
	fmt.Println(responseText)
}

// handleRadioCreateLink handles the radio create_link interception.
func (inst *Instance) handleRadioCreateLink(w http.ResponseWriter, r *http.Request, tagCMD string) {
	if tagCMD == "" {
		log.Println("STB requested radio 'create_link', but did not give 'cmd' key in URL query...")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	inst.radioChannelsMu.RLock()
	channel, found := inst.radioChannels[tagCMD]
	if !found {
		if idx := strings.Index(tagCMD, "/radio/"); idx >= 0 {
			if title, err := url.PathUnescape(tagCMD[idx+len("/radio/"):]); err == nil {
				channel, found = inst.radioChannelsByTitle[title]
			}
		}
	}
	inst.radioChannelsMu.RUnlock()
	if !found {
		log.Println("STB requested radio 'create_link', but gave invalid CMD:", tagCMD)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	requestHost, _, _ := net.SplitHostPort(r.Host)
	_, portHLS, _ := net.SplitHostPort(inst.config.HLS.Bind)
	hlsURL := "http://" + requestHost + ":" + portHLS + "/iptv/" + url.PathEscape(channel.Title)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	responseText := generateNewChannelLink(hlsURL, "", "")
	w.Write([]byte(responseText))
}

// vodHandler reverse-proxies VOD media content.
func (inst *Instance) vodHandler(w http.ResponseWriter, r *http.Request) {
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

	req.Header.Set("Mac", inst.config.Portal.MAC)
	req.Header.Set("Model", inst.config.Portal.Model)
	req.Header.Set("X-Hash", inst.buildPrehash())

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

	for k, v := range resp.Header {
		if k == "Set-Cookie" {
			continue
		}
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// rewriteVodResponse rewrites the stream URL in a create_link response to
// route through the /vod/ proxy endpoint.
func (inst *Instance) rewriteVodResponse(body, requestHost string) string {
	prefixes := []string{`"cmd":"ffmpeg `, `"cmd":"`}
	for _, prefix := range prefixes {
		idx := strings.Index(body, prefix)
		if idx < 0 {
			continue
		}
		start := idx + len(prefix)
		end := strings.IndexAny(body[start:], ` "`)
		if end < 0 {
			end = len(body) - start
		}
		originalURL := body[start : start+end]
		decodedURL := unescapeJSONString(originalURL)
		if strings.HasPrefix(decodedURL, "http") {
			encoded := base64.URLEncoding.EncodeToString([]byte(decodedURL))
			newURL := "http://" + requestHost + "/vod/" + encoded
			if strings.HasPrefix(prefix, `"cmd":"ffmpeg `) {
				body = strings.Replace(body,
					`"cmd":"ffmpeg `+originalURL,
					`"cmd":"ffmpeg `+newURL, 1)
			} else {
				body = strings.Replace(body,
					`"cmd":"`+originalURL,
					`"cmd":"`+newURL, 1)
			}
			log.Printf("  -> VOD URL rewritten: %s -> /vod/...", decodedURL[:min(50, len(decodedURL))])
		}
		break
	}
	return body
}

// rewriteChannelListCmds rewrites channel "cmd" fields to HLS relay URLs.
func (inst *Instance) rewriteChannelListCmds(body, requestHost string) string {
	_, portHLS, _ := net.SplitHostPort(inst.config.HLS.Bind)

	inst.channelsMu.RLock()
	defer inst.channelsMu.RUnlock()

	var sb strings.Builder
	sb.Grow(len(body))
	i := 0
	for {
		nameRaw, name, afterName, ok := jsonStringField(body, "name", i)
		if !ok {
			sb.WriteString(body[i:])
			break
		}
		cmdRaw, _, afterCmd, ok := jsonStringField(body, "cmd", afterName)
		if !ok {
			sb.WriteString(body[i:])
			break
		}

		channel, found := inst.channelsByTitle[name]
		if !found {
			if id, idOK := lastJSONStringFieldBefore(body, "id", afterName-len(nameRaw)); idOK {
				channel, found = inst.channelsByTitle[name+" ("+id+")"]
			}
		}
		if !found {
			sb.WriteString(body[i:afterCmd])
			i = afterCmd
			continue
		}

		newURL := "http://" + requestHost + ":" + portHLS + "/iptv/" + url.PathEscape(channel.Title)
		replacement := newURL
		if strings.HasPrefix(strings.ReplaceAll(cmdRaw, `\/`, `/`), "ffmpeg ") {
			replacement = "ffmpeg " + newURL
		}

		valueStart := afterCmd - len(cmdRaw)
		sb.WriteString(body[i:valueStart])
		sb.WriteString(specialLinkEscape(replacement))
		i = afterCmd
	}
	return sb.String()
}

// rewriteExternalURLs replaces third-party service URLs with proxy endpoints.
func (inst *Instance) rewriteExternalURLs(body, host string) string {
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

// weatherProxyHandler reverse-proxies weather service requests.
func (inst *Instance) weatherProxyHandler(w http.ResponseWriter, r *http.Request) {
	target := "http://weather.infomir.com.ua" + strings.TrimPrefix(r.URL.Path, "/_weather")
	if r.URL.RawQuery != "" {
		q := r.URL.Query()
		if _, ok := q["mac"]; ok {
			q.Set("mac", inst.config.Portal.MAC)
		}
		if _, ok := q["sn"]; ok {
			q.Set("sn", inst.config.Portal.SerialNumber)
		}
		if _, ok := q["id"]; ok {
			q.Set("id", inst.config.Portal.Model)
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

// geoProxyHandler proxies OpenStreetMap geocoding requests.
func (inst *Instance) geoProxyHandler(w http.ResponseWriter, r *http.Request) {
	target := "http://nominatim.openstreetmap.org" + strings.TrimPrefix(r.URL.Path, "/_geo")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	log.Printf("Geo proxy: %s", target)
	req, _ := http.NewRequest("GET", target, nil)
	req.Header.Set("User-Agent", inst.config.Portal.UserAgent())
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
func (inst *Instance) speedtestProxyHandler(w http.ResponseWriter, r *http.Request) {
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

// buildMetrics, buildVersion, buildPrehash, buildHWVersion2, buildSignature
// delegate to the configured portal's shared implementations.
func (inst *Instance) buildMetrics() string    { return inst.config.Portal.Metrics() }
func (inst *Instance) buildVersion() string    { return inst.config.Portal.VersionString() }
func (inst *Instance) buildPrehash() string    { return inst.config.Portal.Prehash() }
func (inst *Instance) buildHWVersion2() string { return inst.config.Portal.HWVersion2() }
func (inst *Instance) buildSignature() string {
	if inst.config.Portal.Signature != "" {
		return inst.config.Portal.Signature
	}
	return inst.config.Portal.GetUID(inst.randomHex)
}

// ####################################################
// Package-level utility functions (stateless)

func jsonStringField(body, key string, i int) (raw, unescaped string, next int, ok bool) {
	marker := `"` + key + `":"`
	idx := strings.Index(body[i:], marker)
	if idx < 0 {
		return "", "", 0, false
	}
	start := i + idx + len(marker)
	end := start
	for end < len(body) {
		if body[end] == '\\' {
			end += 2
			continue
		}
		if body[end] == '"' {
			break
		}
		end++
	}
	if end > len(body) {
		end = len(body)
	}
	raw = body[start:end]
	return raw, unescapeJSONString(raw), end, true
}

func unescapeJSONString(raw string) string {
	var s string
	if err := json.Unmarshal([]byte(`"`+raw+`"`), &s); err != nil {
		return raw
	}
	return s
}

func lastJSONStringFieldBefore(body, key string, before int) (unescaped string, ok bool) {
	marker := `"` + key + `":"`
	idx := strings.LastIndex(body[:before], marker)
	if idx < 0 {
		return "", false
	}
	start := idx + len(marker)
	end := start
	for end < before && end < len(body) {
		if body[end] == '\\' {
			end += 2
			continue
		}
		if body[end] == '"' {
			break
		}
		end++
	}
	return unescapeJSONString(body[start:end]), true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func generateNewChannelLink(link, id, chID string) string {
	return `{"js":{"id":"` + id + `","cmd":"` + specialLinkEscape(link) + `","streamer_id":0,"link_id":` + chID + `,"load":0,"error":""},"text":"array(6) {\n  [\"id\"]=>\n  string(4) \"` + id + `\"\n  [\"cmd\"]=>\n  string(99) \"` + specialLinkEscape(link) + `\"\n  [\"streamer_id\"]=>\n  int(0)\n  [\"link_id\"]=>\n  int(` + chID + `)\n  [\"load\"]=>\n  int(0)\n  [\"error\"]=>\n  string(0) \"\"\n}\ngenerated in: 0.01s; query counter: 8; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`
}

func specialLinkEscape(i string) string {
	return strings.ReplaceAll(i, "/", "\\/")
}

// filterWatchdogEvents parses a watchdog response and strips dangerous portal
// commands while allowing safe message events through.
func filterWatchdogEvents(body string) string {
	type eventData struct {
		ID              int    `json:"id"`
		Event           string `json:"event"`
		Msg             string `json:"msg,omitempty"`
		NeedConfirm     int    `json:"need_confirm"`
		RebootAfterOk   int    `json:"reboot_after_ok"`
		AutoHideTimeout int    `json:"auto_hide_timeout"`
		Param1          string `json:"param1,omitempty"`
		AdditionalOn    string `json:"additional_services_on"`
	}

	type tmpStruct struct {
		Js struct {
			Data eventData `json:"data"`
		} `json:"js"`
		Text string `json:"text"`
	}

	var tmp tmpStruct
	if err := json.Unmarshal([]byte(body), &tmp); err != nil {
		return `{"js":{"data":{"msgs":0,"additional_services_on":"1"}},"text":"generated in: 0.01s; query counter: 4; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`
	}

	safeEvents := map[string]bool{
		"send_msg": true, "send_msg_with_video": true, "show_menu": true,
		"update_epg": true, "update_subscription": true, "update_channels": true,
		"mount_all_storages": true, "play_channel": true, "play_radio_channel": true,
	}

	event := tmp.Js.Data.Event
	if event == "" || safeEvents[event] {
		return body
	}

	log.Printf("  -> filtered dangerous watchdog event: %s", event)
	return `{"js":{"data":{"msgs":0,"additional_services_on":"1"}},"text":"generated in: 0.01s; query counter: 4; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`
}

// ####################################################
// Backward-compatible package-level API

var defaultInstance *Instance

// Serve binds the default proxy instance on the given address.
func Serve(c *stalker.Config, bind string) {
	defaultInstance = NewInstance(c)
	defaultInstance.Serve(bind)
}

// SetChannels populates the default proxy instance channel maps.
func SetChannels(chs map[string]*stalker.Channel) {
	if defaultInstance != nil {
		defaultInstance.SetChannels(chs)
	}
}

// SetRadioChannels populates the default proxy instance radio channel maps.
func SetRadioChannels(chs map[string]*stalker.RadioChannel) {
	if defaultInstance != nil {
		defaultInstance.SetRadioChannels(chs)
	}
}

// Stop shuts down the default proxy instance.
func Stop() {
	if defaultInstance != nil {
		defaultInstance.Stop()
	}
}
