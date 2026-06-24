package hls

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"

	"github.com/erkexzcx/stalkerhek/stalker"
)

var playlist map[string]*Channel
var sortedChannels []string

var (
	serverMu sync.Mutex
	server   *http.Server
)

// Start starts main routine. Blocks until Stop is called or the listener
// fails; callers typically run it in its own goroutine.
func Start(chs map[string]*stalker.Channel, bind string) {
	// Initialize playlist
	playlist = make(map[string]*Channel)
	sortedChannels = make([]string, 0, len(chs))
	for k, v := range chs {
		playlist[k] = &Channel{
			StalkerChannel: v,
			Mux:            &sync.Mutex{},
			Logo: &Logo{
				Mux:  &sync.Mutex{},
				Link: v.Logo(),
			},
			Genre: v.Genre(),
		}
		sortedChannels = append(sortedChannels, k)
	}
	sort.Strings(sortedChannels)

	mux := http.NewServeMux()
	mux.HandleFunc("/iptv", playlistHandler)
	mux.HandleFunc("/iptv/", channelHandler)
	mux.HandleFunc("/logo/", logoHandler)

	srv := &http.Server{Addr: bind, Handler: mux}
	serverMu.Lock()
	server = srv
	serverMu.Unlock()

	log.Println("HLS service should be started!")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println("HLS ListenAndServe error:", err)
	}
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
}
