package hls

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/erkexzcx/stalkerhek/stalker"
)

// userAgent defaults to a generic model until SetUserAgent configures the
// real one; built via stalker.BuildUserAgent so HLS segment/media requests
// to the CDN use the same corrected (non-"QtEmbedded") format as the
// portal-facing requests in stalker/useragent.go, instead of an independent,
// stale copy of the format that was already disproven there.
var userAgent = stalker.BuildUserAgent("MAG200")

// Device headers sent with every media download — the streaming server uses
// these for device identification and stream priority assignment.
var deviceMac, deviceModel, deviceSerial, deviceHash string

// SetDeviceHeaders configures the device-identifying headers sent on media
// requests. Real MAG STBs send Mac:, Model:, X-Hash:, and serial/version
// headers on every HLS segment and VOD media download. Without these,
// the streaming server cannot prioritize the device → 458 errors.
func SetDeviceHeaders(mac, model, serial string) {
	deviceMac = mac
	deviceModel = model
	deviceSerial = serial
	// X-Hash: computed like GetHashVersion1(model, version[:56])
	h := sha1.New()
	h.Write([]byte(model + "ImageDescription: " + model + "; ImageDate: 20010101_000000; PORTAL version: 5.6.0; API Version: 0x1811"))
	deviceHash = hex.EncodeToString(h.Sum(nil))
}

// SetUserAgent sets the User-Agent string used for HLS content requests.
func SetUserAgent(model string) {
	userAgent = stalker.BuildUserAgent(model)
}

func download(link string) (content []byte, contentType string, err error) {
	resp, err := response(link)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	content, err = ioutil.ReadAll(resp.Body)
	return content, resp.Header.Get("Content-Type"), err
}

// httpClient fetches HLS segments and media content from the streaming server.
// It disables redirects (the portal redirects to streaming servers; we follow
// manually) and uses aggressive connection pooling with keep-alive to maintain
// stream priority — matching the behavior of the STB's native hls::KeepAliveWatchDog.
var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		MaxIdleConns:        12,
		MaxIdleConnsPerHost: 6,
		MaxConnsPerHost:     6, // Match Qt WebKit default — prevents account connection limit
		IdleConnTimeout:     180 * time.Second,
		// Keep default DisableCompression (false) — HLS needs to read
		// M3U8 content as text for link rewriting.
	},
}

// maxRedirects caps how many consecutive 3xx hops response() will follow.
// Because the client disables auto-follow (CheckRedirect → ErrUseLastResponse)
// so it can reapply the device headers on each hop, response() follows
// redirects by re-issuing the request itself. Without a cap, a redirect loop
// (A→B→A) or an endless chain from a misbehaving CDN would recurse until the
// goroutine's stack is exhausted — and in the handleContentUnknown path it
// would do so while still holding the channel's mutex, deadlocking it. Match
// Go's own http.Client default of 10.
const maxRedirects = 10

func response(link string) (*http.Response, error) {
	return responseFollow(link, 0)
}

func responseFollow(link string, depth int) (*http.Response, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)
	if deviceMac != "" {
		req.Header.Set("Mac", deviceMac)
		req.Header.Set("Model", deviceModel)
		req.Header.Set("X-Hash", deviceHash)
		req.Header.Set("X-SerialNumber", deviceSerial)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if depth >= maxRedirects {
			return nil, errors.New("stopped after " + strconv.Itoa(maxRedirects) + " redirects following " + link)
		}
		linkURL, err := url.Parse(link)
		if err != nil {
			return nil, errors.New("unknown error occurred")
		}
		redirectURL, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			return nil, errors.New("unknown error occurred")
		}
		newLink := linkURL.ResolveReference(redirectURL)
		return responseFollow(newLink.String(), depth+1)
	}

	return nil, errors.New(link + " returned HTTP code " + strconv.Itoa(resp.StatusCode))
}

func addHeaders(from, to http.Header, contentLength bool) {
	for k, v := range from {
		switch k {
		case "Connection":
			to[k] = v
		case "Content-Type":
			to[k] = v
		case "Transfer-Encoding":
			to[k] = v
		case "Cache-Control":
			to[k] = v
		case "Date":
			to[k] = v
		case "Content-Length":
			// This is only useful for unaltered media files. It should not be copied for HLS requests because
			// players will not attempt to receive more bytes from HTTP server than are set here, therefore some HLS
			// contents would not load. E.g. CURL would display error "curl: (18) transfer closed with 83 bytes remaining to read"
			// if set for HLS metadata requests.
			if contentLength {
				to[k] = v
			}
		}
	}
}

func getLinkType(contentType string) int {
	contentType = strings.ToLower(contentType)
	switch {
	case contentType == "application/vnd.apple.mpegurl" || contentType == "application/x-mpegurl":
		return linkTypeHLS
	case strings.HasPrefix(contentType, "video/") || strings.HasPrefix(contentType, "audio/") || contentType == "application/octet-stream":
		return linkTypeMedia
	default:
		return linkTypeMedia
	}
}
