package hls

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (inst *Instance) handleContent(cr *ContentRequest) {
	linkType := cr.ChannelRef.LinkType

	if linkType == linkTypeUnknown {
		inst.handleContentUnknown(cr)
		return
	}

	// Snapshot channel pointer so we can release the lock without copying
	// sync.Once (which is illegal — go vet warns about noCopy).
	cr.Channel = cr.ChannelRef
	cr.ChannelRef.Mux.Unlock()

	switch linkType {
	case linkTypeHLS:
		inst.handleContentHLS(cr)
	case linkTypeMedia:
		inst.handleContentMedia(cr)
	default:
		http.Error(cr.ResponseWriter, "invalid media type", http.StatusInternalServerError)
	}
}

func (inst *Instance) handleContentUnknown(cr *ContentRequest) {
	// Retry on 458 (device not prioritized) — the real STB player gets a
	// fresh create_link (new play_token) on each retry. The CDN may
	// invalidate tokens after a failed attempt, so we re-resolve the link
	// instead of retrying the same URL.
	var resp *http.Response
	var err error
	// Keep total retry time bounded so first-byte stays under typical player
	// connect timeouts. Each retry re-mints a fresh play_token (and a fresh
	// CDN edge), which is what actually clears a 458 or a per-edge 509.
	backoffs := []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second}
	// sharingLimited becomes true once the registered (auth) MAC hits the
	// portal's per-MAC sharing limit (458). From then on we re-resolve the link
	// with the configured cdn_mac, which carries its own separate limit and so
	// bypasses the cap at the CDN level. The cdn_mac stays unused until this
	// point, keeping it unflagged for when it is genuinely needed.
	sharingLimited := false
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		if attempt > 0 {
			// Get a fresh CDN URL with new play_token from the portal. On a
			// sharing-limit fallback, mint it on the cdn_mac instead of the
			// auth MAC.
			var newLink string
			var linkErr error
			if sharingLimited && cr.ChannelRef.StalkerChannel.HasCDNMAC() {
				newLink, linkErr = cr.ChannelRef.StalkerChannel.NewLinkCDNMAC(true)
			} else {
				newLink, linkErr = cr.ChannelRef.StalkerChannel.NewLink(true)
			}
			if linkErr == nil {
				cr.ChannelRef.Link = newLink
			}
		}
		resp, err = instanceResponse(cr.ChannelRef.Link, inst)
		if err != nil {
			// instanceResponse closes the body and reports a non-2xx
			// status as an httpStatusError. A 458 ("device not
			// prioritized") or a transient upstream/CDN 5xx (500, 509,
			// 520, …) clears on retry — back off and re-resolve the link
			// with a fresh play_token on the next attempt.
			var se *httpStatusError
			if errors.As(err, &se) && (se.code == 458 || se.code >= 500) && attempt < len(backoffs) {
				// A 458 specifically means the auth MAC is over its sharing
				// limit; switch subsequent re-resolves to the cdn_mac.
				if se.code == 458 {
					sharingLimited = true
				}
				time.Sleep(backoffs[attempt])
				continue
			}
			break
		}
		break
	}
	if err != nil {
		cr.ChannelRef.Mux.Unlock()
		http.Error(cr.ResponseWriter, "internal server error", http.StatusInternalServerError)
		log.Println(err)
		return
	}
	defer resp.Body.Close()

	cr.ChannelRef.LinkType = getLinkType(resp.Header.Get("Content-Type"))

	// Stream the response we just fetched, instead of closing it and
	// re-fetching the same link. The old path resolved the play URL twice per
	// request (once to detect the type, once to stream), which doubled
	// time-to-first-byte and added a second failure point. cr.Channel is the
	// snapshot the streaming helpers read after the lock is released.
	cr.Channel = cr.ChannelRef
	if cr.ChannelRef.LinkType == linkTypeHLS {
		cr.ChannelRef.HLSLink = resp.Request.URL.String()
		cr.ChannelRef.HLSLinkRoot = deleteAfterLastSlash(cr.ChannelRef.HLSLink)
		cr.ChannelRef.startKeepAlive()
		link := cr.ChannelRef.HLSLink
		cr.ChannelRef.Mux.Unlock()
		inst.handleEstablishedContentHLS(cr, resp, link)
		return
	}
	cr.ChannelRef.Mux.Unlock()
	inst.handleEstablishedContentMedia(cr, resp)
}

func (inst *Instance) handleContentHLS(cr *ContentRequest) {
	var link string
	if cr.Suffix == "" {
		link = cr.Channel.HLSLink
	} else {
		link = cr.Channel.HLSLinkRoot + cr.Suffix
	}

	resp, err := retryingFetch(inst, link)
	if err != nil {
		http.Error(cr.ResponseWriter, "internal server error", http.StatusInternalServerError)
		log.Println(err)
		return
	}
	defer resp.Body.Close()

	inst.handleEstablishedContentHLS(cr, resp, link)
}

// retryingFetch fetches a cached/known CDN link, retrying only a transient
// gateway hiccup (500/502/503/504) with a short backoff. It reuses the same
// fixed link, so the failures a same-link retry can't fix — 458 (needs a fresh
// play_token) and 509 (a per-edge bandwidth cap) — are surfaced immediately
// rather than burning the player's timeout; those are handled by re-resolution
// in handleContentUnknown. Other errors (4xx, connection failures) also return
// immediately.
func retryingFetch(inst *Instance, link string) (*http.Response, error) {
	backoffs := []time.Duration{300 * time.Millisecond, 800 * time.Millisecond}
	var resp *http.Response
	var err error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		resp, err = instanceResponse(link, inst)
		if err == nil {
			return resp, nil
		}
		var se *httpStatusError
		if errors.As(err, &se) && isTransientGateway(se.code) && attempt < len(backoffs) {
			time.Sleep(backoffs[attempt])
			continue
		}
		return nil, err
	}
	return resp, err
}

// isTransientGateway reports whether a status is a transient gateway/server
// error that a retry against the same URL can plausibly clear.
func isTransientGateway(code int) bool {
	switch code {
	case 500, 502, 503, 504:
		return true
	}
	return false
}

func (inst *Instance) handleEstablishedContentHLS(cr *ContentRequest, resp *http.Response, link string) {
	prefix := "http://" + cr.Request.Host + "/iptv/" + url.PathEscape(cr.Title) + "/"

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	switch {
	case contentType == "application/vnd.apple.mpegurl" || contentType == "application/x-mpegurl":
		content := rewriteLinks(&resp.Body, prefix, cr.Channel.HLSLinkRoot)
		addHeaders(resp.Header, cr.ResponseWriter.Header(), false)
		cr.ResponseWriter.WriteHeader(http.StatusOK)
		fmt.Fprint(cr.ResponseWriter, content)
	default:
		inst.handleEstablishedContentMedia(cr, resp)
	}
}

func (inst *Instance) handleContentMedia(cr *ContentRequest) {
	resp, err := retryingFetch(inst, cr.Channel.Link)
	if err != nil {
		http.Error(cr.ResponseWriter, "internal server error", http.StatusInternalServerError)
		log.Println(err)
		return
	}
	defer resp.Body.Close()

	inst.handleEstablishedContentMedia(cr, resp)
}

func (inst *Instance) handleEstablishedContentMedia(cr *ContentRequest, resp *http.Response) {
	addHeaders(resp.Header, cr.ResponseWriter.Header(), true)
	cr.ResponseWriter.WriteHeader(resp.StatusCode)
	io.Copy(cr.ResponseWriter, resp.Body)
}
