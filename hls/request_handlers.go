package hls

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
)

// Handles '/iptv' requests
func (inst *Instance) playlistHandler(w http.ResponseWriter, r *http.Request) {
	if !inst.ChannelsReady() {
		http.Error(w, "channels still loading, try again shortly", http.StatusServiceUnavailable)
		return
	}

	inst.playlistMu.RLock()
	titles := make([]string, len(inst.sortedChannels))
	copy(titles, inst.sortedChannels)
	genres := make(map[string]string, len(titles))
	for _, title := range titles {
		if ch := inst.playlist[title]; ch != nil {
			genres[title] = ch.Genre
		}
	}
	inst.playlistMu.RUnlock()

	w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintln(w, "#EXTM3U")
	for _, title := range titles {
		link := "/iptv/" + url.PathEscape(title)
		logo := "/logo/" + url.PathEscape(title)

		fmt.Fprintf(w, "#EXTINF:-1 tvg-logo=\"%s\" group-title=\"%s\", %s\n%s\n", logo, genres[title], title, link)
	}
}

// Handles '/iptv/' requests
func (inst *Instance) channelHandler(w http.ResponseWriter, r *http.Request) {
	if !inst.ChannelsReady() {
		http.Error(w, "channels still loading, try again shortly", http.StatusServiceUnavailable)
		return
	}
	cr, err := inst.getContentRequest(w, r, "/iptv/")
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	cr.ChannelRef.Mux.Lock()

	if err = cr.ChannelRef.validate(); err != nil {
		cr.ChannelRef.Mux.Unlock()
		http.Error(w, "internal server error", http.StatusInternalServerError)
		log.Println(err)
		return
	}

	inst.handleContent(cr)
}

// Handles '/logo/' requests
func (inst *Instance) logoHandler(w http.ResponseWriter, r *http.Request) {
	if !inst.ChannelsReady() {
		http.Error(w, "channels still loading, try again shortly", http.StatusServiceUnavailable)
		return
	}
	cr, err := inst.getContentRequest(w, r, "/logo/")
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	cr.ChannelRef.Logo.Mux.Lock()

	if len(cr.ChannelRef.Logo.Cache) == 0 {
		img, contentType, err := download(cr.ChannelRef.Logo.Link, inst.userAgentString())
		if err != nil {
			cr.ChannelRef.Logo.Mux.Unlock()
			http.Error(w, "internal server error", http.StatusInternalServerError)
			log.Println(err)
			return
		}
		cr.ChannelRef.Logo.Cache = img
		cr.ChannelRef.Logo.CacheContentType = contentType
	}

	logo := *cr.ChannelRef.Logo
	cr.ChannelRef.Logo.Mux.Unlock()

	w.Header().Set("Content-Type", logo.CacheContentType)
	w.Write(logo.Cache)
}
