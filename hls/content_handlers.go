package hls

import (
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
	// Retry on 458 (device not prioritized) — the real STB player retries
	// while the streaming server registers the device. Up to 5 attempts
	// with 500ms/1s/2s/3s backoff, matching real MAG firmware behavior.
	var resp *http.Response
	var err error
	backoffs := []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		resp, err = instanceResponse(cr.ChannelRef.Link, inst)
		if err != nil {
			break
		}
		if resp.StatusCode == 458 && attempt < len(backoffs) {
			resp.Body.Close()
			time.Sleep(backoffs[attempt])
			continue
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

	resp, err := instanceResponse(link, inst)
	if err != nil {
		http.Error(cr.ResponseWriter, "internal server error", http.StatusInternalServerError)
		log.Println(err)
		return
	}
	defer resp.Body.Close()

	inst.handleEstablishedContentHLS(cr, resp, link)
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
	resp, err := instanceResponse(cr.Channel.Link, inst)
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
