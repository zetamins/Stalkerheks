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
	backoffs := []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		if attempt > 0 {
			// Get a fresh CDN URL with new play_token from the portal
			if newLink, linkErr := cr.ChannelRef.StalkerChannel.NewLink(true); linkErr == nil {
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

	if cr.ChannelRef.LinkType == linkTypeHLS {
		cr.ChannelRef.HLSLink = resp.Request.URL.String()
		cr.ChannelRef.HLSLinkRoot = deleteAfterLastSlash(cr.ChannelRef.HLSLink)
		cr.ChannelRef.startKeepAlive()
	}

	inst.handleContent(cr)
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

// retryingFetch fetches a CDN/stream link, retrying transient upstream
// failures — HTTP 458 ("device not prioritized") and 5xx server/CDN errors
// (500, 502, 503, 509, 520, …) — with a short backoff. It reuses the same
// link: the play_token stays valid across a transient server hiccup, and the
// unknown-type resolver (handleContentUnknown) is what re-mints links when a
// fresh token is actually needed. Fatal errors (4xx, connection failures)
// return immediately.
func retryingFetch(inst *Instance, link string) (*http.Response, error) {
	backoffs := []time.Duration{300 * time.Millisecond, 1 * time.Second, 2 * time.Second}
	var resp *http.Response
	var err error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		resp, err = instanceResponse(link, inst)
		if err == nil {
			return resp, nil
		}
		var se *httpStatusError
		if errors.As(err, &se) && (se.code == 458 || se.code >= 500) && attempt < len(backoffs) {
			time.Sleep(backoffs[attempt])
			continue
		}
		return nil, err
	}
	return resp, err
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
