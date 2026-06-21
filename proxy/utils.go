package proxy

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpClient proxies requests to the real portal. It disables automatic gzip
// decompression so compressed responses pass through transparently. Connection
// pooling and keep-alive are tuned to mimic a real STB browser which reuses a
// persistent connection — this is critical for getting priority service from the
// portal (new connections per request are treated as low-priority/fresh clients).
var httpClient = &http.Client{
	Transport: &http.Transport{
		DisableCompression:    true,
		MaxIdleConns:          12,
		MaxIdleConnsPerHost:   6,
		MaxConnsPerHost:       6, // Match Qt WebKit default — prevents account connection limit errors
		IdleConnTimeout:       120 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

func getRequest(link string, originalRequest *http.Request) (*http.Response, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}

	// Parse the destination so we can set the correct Host header
	destURL, _ := url.Parse(link)

	for k, v := range originalRequest.Header {
		switch k {
		case "Authorization":
			req.Header.Set("Authorization", "Bearer "+config.Portal.Token)
		case "Cookie":
			cookieText := "PHPSESSID=null; sn=" + url.QueryEscape(config.Portal.SerialNumber) + "; mac=" + url.QueryEscape(config.Portal.MAC) + "; stb_lang=en; timezone=" + url.QueryEscape(config.Portal.TimeZone) + ";"
			req.Header.Set("Cookie", cookieText)
		case "User-Agent":
			req.Header.Set("User-Agent", "Mozilla/5.0 (QtEmbedded; U; Linux; C) AppleWebKit/533.3 (KHTML, like Gecko) "+config.Portal.Model+" stbapp ver: 4 rev: 2116 Mobile Safari/533.3")
		case "X-User-Agent":
			req.Header.Set("X-User-Agent", "Model: "+config.Portal.Model+"; Link: Ethernet")
		case "Sn":
			// STB sends serial number in "SN" header — rewrite it
			req.Header.Set("Sn", config.Portal.SerialNumber)
		case "Host":
			// Replace with real portal's host so the portal sees the correct Host header
			if destURL != nil {
				req.Header.Set("Host", destURL.Host)
			}
		case "Referer":
			// Drop — Cloudflare WAF blocks requests where Referer matches the request URL.
			// Not sending Referer is fine; real STBs don't always send it either.
			continue
		case "Origin":
			// Drop — not needed for GET requests and can trigger WAF rules.
			continue
		case "Accept-Encoding":
			// Pass through the STB's accepted encodings so the portal can compress
			req.Header.Set(k, v[0])
		default:
			// Skip internal Go headers and hop-by-hop headers
			if strings.HasPrefix(k, "X-") && k != "X-User-Agent" {
				// Drop unknown X-* headers from the STB (could leak device info)
				continue
			}
			req.Header.Set(k, v[0])
		}
	}

	return httpClient.Do(req)
}

// getRequestWithRetry wraps getRequest with exponential backoff for transient
// errors (458 rate-limit, 502/503 server errors, connection resets). This
// matches the STB's EConnResetContext::multipleRequestsOk() behavior.
func getRequestWithRetry(link string, r *http.Request) (*http.Response, error) {
	backoffs := []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second}
	var lastErr error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		resp, err := getRequest(link, r)
		if err != nil {
			lastErr = err
			if attempt < len(backoffs) {
				time.Sleep(backoffs[attempt])
			}
			continue
		}
		// Retry on rate-limit and transient server errors
		if resp.StatusCode == 458 || resp.StatusCode == 502 || resp.StatusCode == 503 {
			resp.Body.Close()
			if attempt < len(backoffs) {
				time.Sleep(backoffs[attempt])
			}
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

func addHeaders(from, to http.Header) {
	for k, v := range from {
		// Never propagate Set-Cookie from the real portal to the STB;
		// we set our own PHPSESSID=null cookie instead.
		if k == "Set-Cookie" {
			continue
		}
		// Preserve multi-value headers correctly (e.g. Cache-Control, Set-Cookie)
		to[k] = v
	}
}

func generateNewChannelLink(link, id, ch_id string) string {
	return `{"js":{"id":"` + id + `","cmd":"` + specialLinkEscape(link) + `","streamer_id":0,"link_id":` + ch_id + `,"load":0,"error":""},"text":"array(6) {\n  [\"id\"]=>\n  string(4) \"` + id + `\"\n  [\"cmd\"]=>\n  string(99) \"` + specialLinkEscape(link) + `\"\n  [\"streamer_id\"]=>\n  int(0)\n  [\"link_id\"]=>\n  int(` + ch_id + `)\n  [\"load\"]=>\n  int(0)\n  [\"error\"]=>\n  string(0) \"\"\n}\ngenerated in: 0.01s; query counter: 8; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`
}

func specialLinkEscape(i string) string {
	return strings.ReplaceAll(i, "/", "\\/")
}

// buildMetrics builds a fake metrics JSON string with the configured device identity.
// The portal JS sends metrics as: {"mac":"...","sn":"...","model":"...","type":"STB","uid":"...","random":"..."}
func buildMetrics() string {
	m := map[string]string{
		"mac":   config.Portal.MAC,
		"sn":    config.Portal.SerialNumber,
		"model": config.Portal.Model,
		"type":  "STB",
		"uid":   config.Portal.DeviceID2,
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// buildVersion builds a synthetic version string matching what MAG STB boxes send.
// Format: "ImageDescription: ...; ImageDate: ...; PORTAL version: ...; API Version: ..."
func buildVersion() string {
	return "ImageDescription: " + config.Portal.Model + "; ImageDate: 20010101_000000; PORTAL version: 5.6.0; API Version: 0x1811"
}

// sha1Hex returns the SHA-1 hash of the given string as a 40-char hex string.
// GetHashVersion1 on real MAG boxes appears to return SHA-1 hashes (40 hex chars).
func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// buildPrehash returns a proper hex hash for the prehash parameter.
// Real STB computes: GetHashVersion1(model, version[:56])
func buildPrehash() string {
	ver := buildVersion()
	if len(ver) > 56 {
		ver = ver[:56]
	}
	return sha1Hex(config.Portal.Model + ver)
}

// buildHWVersion2 returns a proper hex hash for the hw_version_2 parameter.
// Real STB computes: GetHashVersion1(JSON.stringify(metrics), random)
func buildHWVersion2() string {
	metrics := buildMetrics()
	return sha1Hex(metrics + randomHex)
}

// emulateGetUID emulates the MAG STB's GetUID() native function.
// On real hardware, GetUID uses a hardware-bound secret key stored in a secure
// element (Trustonic TEE). We can't access that key, so we use the configured
// DeviceID as the base secret and derive values with SHA-256 — producing the
// same 64-char hex format as real MAG hardware.
//
// Usage patterns from portal JS:
//   GetUID()               → device_id  (persistent hardware ID)
//   GetUID(random)          → signature  (keyed by handshake random)
//   GetUID('device_id', tok)→ device_id2 (keyed by token)
//   GetUID(tok) == GetUID(tok,tok) → always true on real HW (second arg ignored)
func emulateGetUID(args ...string) string {
	h := sha256.New()
	h.Write([]byte(config.Portal.DeviceID))
	for _, a := range args {
		h.Write([]byte(":"))
		h.Write([]byte(a))
	}
	return hex.EncodeToString(h.Sum(nil))
}
