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
			l3Recovered := false
			if sharingLimited {
				// Fast path: try L3 reconnect using the cached L3 path and
				// CDN node. The CDN caches L3→channel independently of
				// Cloudflare, so the same L3 may still work even after a
				// 458 from Cloudflare's rate-limiter. Tries the cached node
				// first, then discovered origin IPs (multi-CDN fallback).
				if l3URL, l3Err := cr.ChannelRef.reconnectWithCachedL3(inst); l3Err == nil {
					cr.ChannelRef.Link = l3URL
					l3Recovered = true
				} else {
					// L3 reconnect failed; do a fresh handshake. The new
					// handshake gets a fresh token and may route to a
					// different Cloudflare edge node with available capacity.
					if err := cr.ChannelRef.StalkerChannel.Portal.RefreshToken(); err != nil {
						log.Printf("token refresh failed on 458 retry: %v", err)
					}
				}
			}
			if !l3Recovered {
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
		}
		resp, err = instanceResponse(cr.ChannelRef.Link, inst)
		if err != nil {
			// instanceResponse closes the body and reports a non-2xx
			// status as an httpStatusError. A 458 (either the auth MAC over
			// its sharing limit, or play/live.php's per-MAC token-issuance
			// rate limit — fix1.md §4.2/§6.1, both cleared the same way from
			// here: a fresh play_token, optionally on a different MAC) or a
			// transient upstream/CDN 5xx (500, 509, 520, …) clears on retry —
			// back off and re-resolve the link with a fresh play_token on the
			// next attempt. 511 is excluded even though it's numerically 5xx:
			// Cloudflare returns it for a MAC that was never handshaked/isn't
			// registered at all, which no amount of retrying fixes.
			var se *httpStatusError
			if errors.As(err, &se) && se.code != 511 && (se.code == 458 || se.code >= 500) && attempt < len(backoffs) {
				// A 458 specifically means the auth MAC is over its sharing
				// limit (or rate limit); switch subsequent re-resolves to the
				// cdn_mac — fix1.md §6.1 confirms the limit is per-MAC, so an
				// alternate MAC has its own independent budget.
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

	// Cache the effective CDN URL (after all redirects) for fast reconnection.
	// The cached URL lets validate() skip NewLink/live.php on the next request
	// if the CDN node still serves it.
	cacheURL := resp.Request.URL.String()
	cr.ChannelRef.SetCachedCDNURL(cacheURL)

	// Start keep-alive based on content type. HLS gets playlist refreshes;
	// media streams get periodic HEAD requests to keep the TCP path alive.
	if cr.ChannelRef.LinkType == linkTypeHLS {
		cr.ChannelRef.startKeepAlive()
	} else {
		cr.ChannelRef.startMediaKeepAlive()
	}

	// Stream the response we just fetched, instead of closing it and
	// re-fetching the same link. The old path resolved the play URL twice per
	// request (once to detect the type, once to stream), which doubled
	// time-to-first-byte and added a second failure point. cr.Channel is the
	// snapshot the streaming helpers read after the lock is released.
	cr.Channel = cr.ChannelRef
	if cr.ChannelRef.LinkType == linkTypeHLS {
		cr.ChannelRef.HLSLink = resp.Request.URL.String()
		cr.ChannelRef.HLSLinkRoot = deleteAfterLastSlash(cr.ChannelRef.HLSLink)
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
