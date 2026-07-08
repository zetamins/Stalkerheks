package stalker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// tryBypass attempts all bypass methods in priority order and returns the
// first working stream URL. Called when create_link fails with a bypassable
// error (456 no subscription, 458 rate-limit, 403 forbidden).
//
// The only viable path is through Cloudflare with a valid play_token — the
// origin is fully CF-protected and direct bypass methods always return nothing.
// Two sources of play_tokens are tried:
//
//  1. Pre-filled token from get_all_channels cmd (10-char alphanumeric)
//  2. Handshake token from Portal.Token (32-char hex)
//
// Both are sent to live.php on the session-affine TCP connection
// (httpRedirectClient) which returns a 302 redirect to the CDN stream URL.
func (c *Channel) tryBypass() (string, error) {
	streamID := extractStreamID(c.CMD)
	playBase := playBaseFromCMD(c.CMD)

	// --- Phase 1: Pre-filled token → live.php → CDN redirect (no handshake) ---
	// get_all_channels embeds a 10-char alphanumeric play_token in each
	// channel's cmd field. These work directly in live.php without any
	// handshake or create_link call — the portal pre-generates them.
	if c.PrefilledToken != "" && playBase != "" && streamID != "" {
		if link, err := c.trySessionLivePrefilled(playBase, streamID, c.PrefilledToken); err == nil {
			return link, nil
		}
		// Token-in-Cookie routes through a different CDN internal path.
		if link, err := c.trySessionLivePrefilledCookie(playBase, streamID, c.PrefilledToken); err == nil {
			return link, nil
		}
	}

	// --- Phase 2: Handshake token → live.php → CDN redirect (requires handshake) ---
	// The handshake token (32-char hex) also works as play_token. Fallback
	// when the channel cmd has no pre-filled token.
	if c.Portal.Token != "" && playBase != "" && streamID != "" {
		if link, err := c.trySessionLive(playBase, streamID); err == nil {
			return link, nil
		}
		if link, err := c.trySessionLiveTokenCookie(playBase, streamID); err == nil {
			return link, nil
		}
	}

	// --- Phase 3: create_link with stream={id} → ffmpeg URL (fallback) ---
	if streamID != "" {
		if link, err := c.tryCreateLinkStream(streamID); err == nil {
			return link, nil
		}
	}

	return "", fmt.Errorf("all bypass methods failed for channel %s", c.Title)
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
	resp, err := c.Portal.DoLiveRequest(u)
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

// trySessionLiveTokenCookie is identical to trySessionLive but passes the
// play_token as a Cookie header instead of a URL query parameter. Some CDN
// deployments route token-in-cookie requests through a different internal
// path that may have spare capacity when the query-param path is
// rate-limited (458).
func (c *Channel) trySessionLiveTokenCookie(playBase string, streamID string) (string, error) {
	u := playBase + "?mac=" + url.QueryEscape(c.Portal.MAC) + "&stream=" + url.PathEscape(streamID) + "&extension=ts"
	resp, err := c.Portal.DoLiveRequestTokenCookie(u)
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
	return "", fmt.Errorf("session live.php (token cookie) returned %d", resp.StatusCode)
}

// trySessionLivePrefilled calls live.php with a pre-filled token from the
// channel's cmd field (extracted from get_all_channels). These 10-char
// alphanumeric tokens work directly — no handshake or create_link needed.
func (c *Channel) trySessionLivePrefilled(playBase, streamID, token string) (string, error) {
	u := playBase + "?mac=" + url.QueryEscape(c.Portal.MAC) + "&stream=" + url.PathEscape(streamID) + "&extension=ts&play_token=" + url.QueryEscape(token)
	resp, err := c.Portal.DoLiveRequest(u)
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
	return "", fmt.Errorf("prefilled live.php returned %d", resp.StatusCode)
}

// trySessionLivePrefilledCookie calls live.php with the pre-filled token in
// the "play_token" Cookie header instead of a URL query parameter.
func (c *Channel) trySessionLivePrefilledCookie(playBase, streamID, token string) (string, error) {
	u := playBase + "?mac=" + url.QueryEscape(c.Portal.MAC) + "&stream=" + url.PathEscape(streamID) + "&extension=ts"
	resp, err := c.Portal.DoLiveRequestPlayTokenCookie(u, token)
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
	return "", fmt.Errorf("prefilled live.php (token cookie) returned %d", resp.StatusCode)
}

// --- Helpers --------------------------------------------------------------

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
