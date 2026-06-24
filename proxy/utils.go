package proxy

import (
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
			req.Header.Set("User-Agent", config.Portal.UserAgent())
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
			// Never forward this. requestHandler rewrites response bodies as
			// plain text (forcing use_http_tmp_link, rewriting VOD/external
			// URLs, stripping watchdog events) — if the portal honors this
			// and compresses, DisableCompression on this client only skips
			// auto-decompression for responses *it* asked to compress, not
			// ones compressed because we explicitly forwarded this header.
			// A gzip body would make every one of those rewrites silently
			// no-op instead of erroring, so just never ask for compression.
			continue
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

// buildMetrics, buildVersion, buildPrehash and buildHWVersion2 delegate to
// stalker.Portal's shared implementations (stalker/hash.go) so the proxy's
// fabricated identity fields use the same verified GetHashVersion1 emulation
// (a 3-stage SHA-1 chain with a fixed salt, confirmed via dynamic ARM
// emulation — an earlier static-disassembly pass had wrongly concluded MD5)
// as the outbound get_profile call.
func buildMetrics() string {
	return config.Portal.Metrics()
}

func buildVersion() string {
	return config.Portal.VersionString()
}

func buildPrehash() string {
	return config.Portal.Prehash()
}

func buildHWVersion2() string {
	return config.Portal.HWVersion2()
}

// buildSignature resolves the signature query param the same way
// stalker.Portal does for its own outbound get_profile call: a manually
// configured override if set, otherwise GetUID(randomHex) — randomHex is
// this proxy's own fabricated handshake random, returned to every
// downstream STB it impersonates the portal for, so it plays the same role
// here that a real handshake's random plays in the outbound client.
func buildSignature() string {
	if config.Portal.Signature != "" {
		return config.Portal.Signature
	}
	return config.Portal.GetUID(randomHex)
}

func generateNewChannelLink(link, id, ch_id string) string {
	return `{"js":{"id":"` + id + `","cmd":"` + specialLinkEscape(link) + `","streamer_id":0,"link_id":` + ch_id + `,"load":0,"error":""},"text":"array(6) {\n  [\"id\"]=>\n  string(4) \"` + id + `\"\n  [\"cmd\"]=>\n  string(99) \"` + specialLinkEscape(link) + `\"\n  [\"streamer_id\"]=>\n  int(0)\n  [\"link_id\"]=>\n  int(` + ch_id + `)\n  [\"load\"]=>\n  int(0)\n  [\"error\"]=>\n  string(0) \"\"\n}\ngenerated in: 0.01s; query counter: 8; cache hits: 0; cache miss: 0; php errors: 0; sql errors: 0;"}`
}

func specialLinkEscape(i string) string {
	return strings.ReplaceAll(i, "/", "\\/")
}
