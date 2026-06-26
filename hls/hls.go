package hls

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"

	"github.com/erkexzcx/stalkerhek/stalker"
)

// playlist/sortedChannels are replaced wholesale by SetChannels while handlers
// read them concurrently, so all access goes through playlistMu. channelsReady
// stays false until the first SetChannels, so requests that arrive during the
// portal warm-up get a 503 instead of a spurious "not found".
var (
	playlistMu     sync.RWMutex
	playlist       = map[string]*Channel{}
	sortedChannels []string
	channelsReady  bool
)

var (
	serverMu sync.Mutex
	server   *http.Server
)

// Serve binds the HLS HTTP server and begins serving immediately — before the
// (potentially slow, on some networks minutes-long) portal handshake/channel
// fetch — so the port is reachable within milliseconds. Channel requests
// return 503 until SetChannels is called. Blocks until Stop is called or the
// listener fails; callers typically run it in its own goroutine.
func Serve(bind string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/iptv", playlistHandler)
	mux.HandleFunc("/iptv/", channelHandler)
	mux.HandleFunc("/logo/", logoHandler)

	srv := &http.Server{Addr: bind, Handler: mux}
	serverMu.Lock()
	server = srv
	serverMu.Unlock()

	log.Println("HLS service listening on", bind)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println("HLS ListenAndServe error:", err)
	}
}

// SetChannels (re)populates the channel playlist, atomically swapping it in for
// the live handlers. Called once the portal's channel list has been retrieved,
// which may be well after Serve has already started accepting connections.
func SetChannels(chs map[string]*stalker.Channel) {
	newPlaylist := make(map[string]*Channel, len(chs))
	newSorted := make([]string, 0, len(chs))
	for k, v := range chs {
		newPlaylist[k] = &Channel{
			StalkerChannel: v,
			Mux:            &sync.Mutex{},
			Logo: &Logo{
				Mux:  &sync.Mutex{},
				Link: v.Logo(),
			},
			Genre: v.Genre(),
		}
		newSorted = append(newSorted, k)
	}
	sort.Strings(newSorted)

	// Stop any keep-alive goroutines from a previous channel set before
	// dropping our reference to them, so a re-fetch doesn't leak them.
	playlistMu.Lock()
	old := playlist
	playlist = newPlaylist
	sortedChannels = newSorted
	channelsReady = true
	playlistMu.Unlock()

	for _, ch := range old {
		ch.stopKeepAlive()
	}
}

// channelsAreReady reports whether SetChannels has populated the playlist yet.
func channelsAreReady() bool {
	playlistMu.RLock()
	defer playlistMu.RUnlock()
	return channelsReady
}

// Stop gracefully shuts down the HLS HTTP server, if running. Safe to call
// even if Start was never called or has already returned.
func Stop() {
	serverMu.Lock()
	srv := server
	server = nil
	serverMu.Unlock()
	if srv != nil {
		srv.Shutdown(context.Background())
	}
	// Stop the per-channel keep-alive goroutines too. Shutting down the HTTP
	// server alone left them running (and still hitting the CDN) until their
	// idle timeout — a goroutine leak on every profile stop/restart.
	playlistMu.RLock()
	chans := make([]*Channel, 0, len(playlist))
	for _, ch := range playlist {
		chans = append(chans, ch)
	}
	playlistMu.RUnlock()
	for _, ch := range chans {
		ch.stopKeepAlive()
	}
}
