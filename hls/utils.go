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

// buildDeviceHash computes the X-Hash header value sent on media/CDN requests.
// Uses real firmware build date from Resources/rootfs-2.20.11-pub-544.
func buildDeviceHash(model string) string {
	h := sha1.New()
	h.Write([]byte(stalker.CDNHashInput(model)))
	return hex.EncodeToString(h.Sum(nil))
}

func download(link string, userAgent string) (content []byte, contentType string, err error) {
	resp, err := responseWithUA(link, userAgent, "", "", "", "")
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	content, err = ioutil.ReadAll(resp.Body)
	return content, resp.Header.Get("Content-Type"), err
}

// httpClient fetches HLS segments and media content from the streaming server.
var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		MaxIdleConns:        12,
		MaxIdleConnsPerHost: 6,
		MaxConnsPerHost:     6,
		IdleConnTimeout:     180 * time.Second,
	},
}

const maxRedirects = 10

// httpStatusError carries the HTTP status code of a non-success upstream
// response so callers (e.g. the 458 "device not prioritized" retry loop) can
// branch on the specific code rather than treating every failure the same.
type httpStatusError struct {
	link string
	code int
}

func (e *httpStatusError) Error() string {
	return e.link + " returned HTTP code " + strconv.Itoa(e.code)
}

// responseWithUA fetches a URL with the given headers. Used for both HLS content
// and logo downloads.
func responseWithUA(link, userAgent, mac, model, hash, serial string) (*http.Response, error) {
	return responseFollowWithUA(link, userAgent, mac, model, hash, serial, 0)
}

func responseFollowWithUA(link, userAgent, mac, model, hash, serial string, depth int) (*http.Response, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}

	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if mac != "" {
		req.Header.Set("Mac", mac)
		req.Header.Set("Model", model)
		req.Header.Set("X-Hash", hash)
		req.Header.Set("X-SerialNumber", serial)
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
		return responseFollowWithUA(newLink.String(), userAgent, mac, model, hash, serial, depth+1)
	}

	return nil, &httpStatusError{link: link, code: resp.StatusCode}
}

// response fetches a URL using the default instance's device headers (for
// backward compatibility).
func response(link string) (*http.Response, error) {
	inst := defaultInstance
	return responseWithUA(link, inst.userAgentString(), inst.deviceMac, inst.deviceModel, inst.deviceHash, inst.deviceSerial)
}

// instanceResponse fetches a URL with the given instance's device headers.
func instanceResponse(link string, inst *Instance) (*http.Response, error) {
	return responseWithUA(link, inst.userAgentString(), inst.deviceMac, inst.deviceModel, inst.deviceHash, inst.deviceSerial)
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

func timeSince(t time.Time) time.Duration {
	return time.Since(t)
}
