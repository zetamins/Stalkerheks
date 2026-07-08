package proxy

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/erkexzcx/stalkerhek/stalker"
)

// httpClient proxies requests to the real portal.
var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DisableCompression: false,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          12,
		MaxIdleConnsPerHost:   6,
		MaxConnsPerHost:       6,
		IdleConnTimeout:       120 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

func getRequest(link string, originalRequest *http.Request, config *stalker.Config) (*http.Response, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}

	destURL, _ := url.Parse(link)

	// Always set minimum STB identity headers regardless of what the STB
	// sends. Cloudflare requires these to recognise the request as a
	// legitimate STB; omitting them triggers a challenge/interstitial page.
	// Note: Sn is deliberately NOT set — real MAG STBs don't send it,
	// and adding it makes the request look different from a real STB,
	// which can trigger Cloudflare WAF rules.
	req.Header.Set("Authorization", "Bearer "+config.Portal.Token)
	req.Header.Set("Cookie", "mac="+url.QueryEscape(config.Portal.MAC)+"; stb_lang=en; timezone="+url.QueryEscape(config.Portal.TimeZone))
	req.Header.Set("User-Agent", config.Portal.UserAgent())
	req.Header.Set("X-User-Agent", "Model: "+config.Portal.Model+"; Link: Ethernet")

	for k, v := range originalRequest.Header {
		switch k {
		case "Authorization", "Cookie", "User-Agent", "X-User-Agent", "Sn":
			// Sn is never sent by real MAG STBs and is not set above;
			// explicitly skip it so an STB can't inject it either.
		case "Host":
			if destURL != nil {
				// Strip default port from Host header. Some Cloudflare
				// WAF rules flag requests with ":80" in Host as unusual.
				host := destURL.Host
				if strings.HasSuffix(host, ":80") && destURL.Scheme == "http" {
					host = destURL.Hostname()
				} else if strings.HasSuffix(host, ":443") && destURL.Scheme == "https" {
					host = destURL.Hostname()
				}
				req.Header.Set("Host", host)
			}
		case "Referer":
			continue
		case "Origin":
			continue
		case "Accept-Encoding":
			continue
		default:
			if strings.HasPrefix(k, "X-") {
				continue
			}
			req.Header[k] = v
		}
	}

	return httpClient.Do(req)
}

func getRequestWithRetry(link string, r *http.Request, config *stalker.Config) (*http.Response, error) {
	backoffs := []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second}
	var lastErr error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		resp, err := getRequest(link, r, config)
		if err != nil {
			lastErr = err
			if attempt < len(backoffs) {
				time.Sleep(backoffs[attempt])
			}
			continue
		}
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
		if k == "Set-Cookie" {
			continue
		}
		to[k] = v
	}
}
