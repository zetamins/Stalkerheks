package hls

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/erkexzcx/stalkerhek/stalker"
)

const (
	linkTypeUnknown = 0
	linkTypeHLS     = 1
	linkTypeMedia   = 2

	hlsKeepAliveInterval    = 25 * time.Second
	hlsKeepAliveIdleTimeout = 120 * time.Second
	hlsLinkValidityTimeout  = 90 * time.Second
	cdnURLMaxAge            = 30 * time.Minute
	cdnKeepAliveInterval    = 10 * time.Second
)

// Per-instance playback activity tracking — used by IsPlaying() to report
// watch state for watchdog cur_play_type.
var (
	playbackActivityMu   sync.Mutex
	lastPlaybackActivity time.Time
)

// markPlaybackActivity records that some channel was just accessed by a viewer.
func markPlaybackActivity() {
	playbackActivityMu.Lock()
	lastPlaybackActivity = time.Now()
	playbackActivityMu.Unlock()
}

// Logo stores TV channel logo details.
type Logo struct {
	Mux              *sync.Mutex
	Link             string
	Cache            []byte
	CacheContentType string
}

// Channel stores TV channel details for HLS relay.
type Channel struct {
	StalkerChannel *stalker.Channel

	Mux *sync.Mutex

	Link     string
	LinkType int

	HLSLink     string
	HLSLinkRoot string

	// CachedCDNURL is the effective CDN stream URL (after redirects) from a
	// previous successful live.php request. Cached for up to cdnURLMaxAge so
	// reconnects can skip live.php entirely if the CDN node still streams.
	CachedCDNURL       string
	CachedCDNURLExpiry time.Time
	// CachedL3Path is the triple-base64 path segment from the CDN URL.
	// On reconnect to the same CDN node, this path may still be valid even
	// after the live.php token that generated it has been invalidated —
	// the CDN node caches the L3→channel mapping independently of Cloudflare.
	CachedL3Path string
	// CachedCDNNode is the IP:port of the CDN node from the CDN URL.
	CachedCDNNode string
	// CachedChannelID is the numeric channel/stream ID extracted from the
	// CDN URL's last path segment. Used to reconstruct CDN URLs for L3
	// reconnection without calling live.php again.
	CachedChannelID string

	lastAccess time.Time

	keepAliveStop    chan struct{}
	keepAliveOnce    sync.Once
	mediaKeepAliveStop chan struct{}
	mediaKeepAliveOnce sync.Once

	Logo  *Logo
	Genre string

	// owner is the HLS Instance this channel belongs to. Used to access
	// device headers and user agent for media/CDN requests.
	owner *Instance
}

func (c *Channel) validate() error {
	if !c.isValid() {
		// Try cached CDN URL first — if the CDN node still streams the
		// cached URL, we skip the NewLink / live.php call entirely, saving
		// the round-trip and avoiding token-rate-limit risk.
		if cachedURL, ok := c.GetCachedCDNURL(); ok && c.owner != nil {
			resp, err := instanceResponse(cachedURL, c.owner)
			if err == nil {
				resp.Body.Close()
				c.Link = cachedURL
				c.LinkType = 0 // unknown — handleContentUnknown will detect it
				c.lastAccess = time.Now()
				markPlaybackActivity()
				return nil
			}
		}
		c.ClearCachedCDNURL()

		newLink, err := c.StalkerChannel.NewLink(true)
		if err != nil {
			return err
		}

		c.Link = newLink
		c.LinkType = 0
	}

	c.lastAccess = time.Now()
	markPlaybackActivity()
	return nil
}

// SetCachedCDNURL stores the effective CDN URL and extracts the L3 path,
// node, and channel ID for reconnection. The cache is valid for cdnURLMaxAge
// (30 minutes). Must be called with c.Mux held.
func (c *Channel) SetCachedCDNURL(rawURL string) {
	c.CachedCDNURL = rawURL
	c.CachedCDNURLExpiry = time.Now().Add(cdnURLMaxAge)
	c.CachedL3Path = extractL3Path(rawURL)
	c.CachedCDNNode = extractCDNNode(rawURL)
	c.CachedChannelID = extractLastPathSegment(rawURL)
}

// GetCachedCDNURL returns the cached CDN URL if it is still within the
// validity window. Must be called with c.Mux held.
func (c *Channel) GetCachedCDNURL() (string, bool) {
	if c.CachedCDNURL == "" {
		return "", false
	}
	if time.Now().After(c.CachedCDNURLExpiry) {
		return "", false
	}
	return c.CachedCDNURL, true
}

// ClearCachedCDNURL invalidates the cached CDN URL. Must be called with
// c.Mux held.
func (c *Channel) ClearCachedCDNURL() {
	c.CachedCDNURL = ""
	c.CachedCDNURLExpiry = time.Time{}
	c.CachedL3Path = ""
	c.CachedCDNNode = ""
}

// extractL3Path extracts the triple-base64 path segment from a CDN stream
// URL. CDN URLs have the format:
//
//	http://host:port/live/play/{triple_base64}/{channel_id}
//
// Returns empty string on parse failure or unexpected path structure.
func extractL3Path(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if p == "play" && i+2 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// extractCDNNode extracts the host (IP:port) from a CDN stream URL.
func extractCDNNode(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// extractLastPathSegment returns the last non-empty path segment from a URL.
func extractLastPathSegment(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// reconnectWithCachedL3 tries to reconnect to the CDN node using the cached
// L3 path. The CDN node caches the L3→channel mapping independently of
// Cloudflare, so even after a 458 (Cloudflare rate-limit), the node may still
// serve content for the cached L3 path. Returns the working URL or an error.
// Constructs URLs against multiple candidate hosts (cached node first, then
// any discovered origin IPs) for multi-CDN fallback.
func (c *Channel) reconnectWithCachedL3(owner *Instance) (string, error) {
	if c.CachedL3Path == "" || c.CachedChannelID == "" {
		return "", fmt.Errorf("no cached L3 path or channel ID")
	}

	// Collect candidate hosts: cached CDN node first, then origin IPs.
	candidates := []string{}
	if c.CachedCDNNode != "" {
		candidates = append(candidates, c.CachedCDNNode)
	}
	if c.StalkerChannel != nil && c.StalkerChannel.Portal != nil {
		for _, ip := range c.StalkerChannel.Portal.OriginIPs {
			host := ip
			if !strings.Contains(host, ":") {
				host += ":80"
			}
			if host != c.CachedCDNNode {
				candidates = append(candidates, host)
			}
		}
	}

	for _, host := range candidates {
		u := fmt.Sprintf("http://%s/live/play/%s/%s", host, c.CachedL3Path, c.CachedChannelID)
		resp, err := instanceResponse(u, owner)
		if err != nil {
			continue
		}
		resp.Body.Close()
		// Any 2xx with streamable content means the L3 path is still valid.
		return u, nil
	}
	return "", fmt.Errorf("L3 reconnect failed for all candidates")
}

// startKeepAlive starts a background goroutine that periodically refreshes
// the HLS playlist URL to keep the streaming server session alive.
func (c *Channel) startKeepAlive() {
	c.keepAliveOnce.Do(func() {
		stop := make(chan struct{})
		c.keepAliveStop = stop
		go func() {
			ticker := time.NewTicker(hlsKeepAliveInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					c.Mux.Lock()
					lastAccess := c.lastAccess
					hlsLink := c.HLSLink
					owner := c.owner
					c.Mux.Unlock()

					if time.Since(lastAccess) > hlsKeepAliveIdleTimeout {
						return
					}

					// Use owner's device headers for CDN requests
					resp, err := instanceResponse(hlsLink, owner)
					if err != nil {
						log.Printf("HLS keep-alive refresh failed for %s: %v", c.StalkerChannel.Title, err)
						continue
					}
					resp.Body.Close()
				case <-stop:
					return
				}
			}
		}()
	})
}

// startMediaKeepAlive starts a background goroutine that periodically sends
// a lightweight HEAD request to the cached CDN URL to keep the TCP connection
// to the CDN node alive. This prevents the CDN or intermediate firewalls from
// closing the connection during idle periods, so reconnect can reuse the same
// TCP session without a new handshake.
func (c *Channel) startMediaKeepAlive() {
	c.mediaKeepAliveOnce.Do(func() {
		stop := make(chan struct{})
		c.mediaKeepAliveStop = stop
		go func() {
			ticker := time.NewTicker(cdnKeepAliveInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					c.Mux.Lock()
					lastAccess := c.lastAccess
					link := c.Link
					owner := c.owner
					c.Mux.Unlock()

					if time.Since(lastAccess) > hlsKeepAliveIdleTimeout {
						return
					}
					if link == "" || owner == nil {
						continue
					}

					req, err := http.NewRequest("HEAD", link, nil)
					if err != nil {
						continue
					}
					// Use the instance's device headers so the CDN accepts
					// the request.
					req.Header.Set("User-Agent", owner.userAgentString())
					req.Header.Set("Mac", owner.deviceMac)
					req.Header.Set("Model", owner.deviceModel)
					req.Header.Set("X-Hash", owner.deviceHash)
					req.Header.Set("X-SerialNumber", owner.deviceSerial)
					resp, err := httpClient.Do(req)
					if err != nil {
						continue
					}
					resp.Body.Close()
				case <-stop:
					return
				}
			}
		}()
	})
}

// stopKeepAlive stops both the HLS and media keep-alive goroutines for this
// channel.
func (c *Channel) stopKeepAlive() {
	c.Mux.Lock()
	defer c.Mux.Unlock()
	if c.keepAliveStop != nil {
		close(c.keepAliveStop)
		c.keepAliveStop = nil
	}
	if c.mediaKeepAliveStop != nil {
		close(c.mediaKeepAliveStop)
		c.mediaKeepAliveStop = nil
	}
}

func (c *Channel) isValid() bool {
	if c.lastAccess.IsZero() {
		return false
	}
	return time.Since(c.lastAccess) <= hlsLinkValidityTimeout
}
