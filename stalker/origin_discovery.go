package stalker

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// cloudflareDirectIPs lists common Cloudflare proxy IPs for direct-origin
// fallback. When DNS fails to resolve the portal hostname, these IPs are
// tried with the original Host header to reach Cloudflare's edge directly.
// This list is non-exhaustive — Cloudflare publishes current ranges at
// https://www.cloudflare.com/ips-v4 .
// 
// Set CloudflareDirectFallback to true to enable this feature.
var CloudflareDirectFallback bool

var cloudflareDirectIPs = []string{
	"172.67.154.216",
	"104.21.0.1",
	"104.21.0.2",
	"104.21.0.3",
	"172.64.0.1",
	"172.64.0.2",
	"172.64.0.3",
}

// originScanner is an HTTP client used for origin IP discovery probes.
// No keep-alives — each probe is a one-shot check against a different IP.
var originScanner = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: -1,
		}).DialContext,
		DisableKeepAlives: true,
	},
}

// DiscoverOriginIPs scans a /24 subnet for live origin servers that respond
// to play/live.php token requests. Returns a list of IP strings (e.g.
// "103.176.90.24"). Only /24 subnets are supported.
//
// The origin reads the MAC from the URL query string, not the Cookie header.
// Cloudflare's rate limit reads from the Cookie. By omitting the Cookie
// entirely when probing and passing mac= in the query string, we avoid
// Cloudflare's rate-limit tracking bucket.
func DiscoverOriginIPs(subnet string, mac string) []string {
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		log.Printf("Origin discovery: invalid subnet %q: %v", subnet, err)
		return nil
	}

	ones, bits := ipnet.Mask.Size()
	if bits != 32 || ones != 24 {
		log.Printf("Origin discovery: only /24 subnets are supported, got /%d", ones)
		return nil
	}

	log.Printf("Origin discovery: scanning %s for live origin servers...", subnet)

	var mu sync.Mutex
	var origins []string
	var wg sync.WaitGroup

	ip := make(net.IP, len(ipnet.IP))
	copy(ip, ipnet.IP)
	ip = nextIP(ip)

	for ; ipnet.Contains(ip); ip = nextIP(ip) {
		ipStr := ip.String()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if probeOrigin(ipStr, mac) {
				mu.Lock()
				origins = append(origins, ipStr)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(origins) > 0 {
		log.Printf("Origin discovery: found %d live origin servers: %v", len(origins), origins)
	} else {
		log.Println("Origin discovery: no live origin servers found")
	}
	return origins
}

// nextIP increments an IPv4 address by 1.
func nextIP(ip net.IP) net.IP {
	n := make(net.IP, len(ip))
	copy(n, ip)
	for i := len(n) - 1; i >= 0; i-- {
		n[i]++
		if n[i] > 0 {
			break
		}
	}
	return n
}

// probeOrigin checks whether the given IP responds as a Stalker origin by
// requesting play/live.php with the MAC in the query string. The origin reads
// the MAC from the URL query string, not the Cookie header — so we omit the
// Cookie entirely to stay in a different rate-limit bucket from Cloudflare.
// Any non-connection-error response (302 with token, 511 for unknown MAC, etc.)
// confirms the IP is a live origin server.
func probeOrigin(ipStr string, mac string) bool {
	u := "http://" + ipStr + ":80/play/live.php?mac=" + mac + "&stream=1&extension=ts"
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "curl/8.0")
	resp, err := originScanner.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// originURL returns the given portal URL with its host replaced by the
// first discovered origin IP. Returns the original URL unchanged when no
// origin IPs have been discovered.
func (p *Portal) originURL(originalURL string) string {
	if len(p.OriginIPs) == 0 {
		return originalURL
	}

	parsed, err := url.Parse(originalURL)
	if err != nil {
		return originalURL
	}

	parsed.Host = p.OriginIPs[0]
	return parsed.String()
}

// tryCloudflareIP attempts to reach the portal through a raw Cloudflare edge
// IP with the correct Host header, bypassing DNS resolution failures.
// Returns the origin base if successful, empty string otherwise.
func (p *Portal) tryCloudflareIP() string {
	if p.Location == "" {
		return ""
	}
	parsed, err := url.Parse(p.Location)
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := parsed.Host
	// Strip port for Host header
	hostOnly, _, err := net.SplitHostPort(host)
	if err != nil {
		hostOnly = host
	}
	if !CloudflareDirectFallback {
		return ""
	}
	for _, cfIP := range cloudflareDirectIPs {
		u := "http://" + cfIP + ":80/play/live.php?mac=" + url.QueryEscape(p.MAC) + "&stream=1&extension=ts"
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			continue
		}
		req.Host = hostOnly
		req.Header.Set("User-Agent", "curl/8.0")
		req.Header.Set("Cookie", "mac="+url.QueryEscape(p.MAC)+"; stb_lang=en; timezone="+url.QueryEscape(p.TimeZone))
		resp, err := originScanner.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		// Any response (including 458/511) confirms this CF IP routes to our portal
		return "http://" + cfIP
	}
	return ""
}
