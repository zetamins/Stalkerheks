package hls

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"

	"github.com/erkexzcx/stalkerhek/stalker"
)

// Instance is a per-profile HLS relay server. Each profile gets its own
// Instance so multiple profiles can serve HLS streams concurrently on
// different ports without sharing state.
type Instance struct {
	playlistMu     sync.RWMutex
	playlist       map[string]*Channel
	sortedChannels []string
	channelsReady  bool

	serverMu sync.Mutex
	server   *http.Server

	// Device headers sent with media/CDN requests.
	userAgent    string
	deviceMac    string
	deviceModel  string
	deviceSerial string
	deviceHash   string
}

// NewInstance creates a new HLS relay instance ready for configuration.
func NewInstance() *Instance {
	return &Instance{
		playlist:      make(map[string]*Channel),
		channelsReady: false,
	}
}

// Playlist returns a snapshot of the playlist contents.
func (inst *Instance) Playlist() map[string]*Channel {
	inst.playlistMu.RLock()
	defer inst.playlistMu.RUnlock()
	return inst.playlist
}

// PlaylistSorted returns a snapshot of the sorted channel titles.
func (inst *Instance) PlaylistSorted() []string {
	inst.playlistMu.RLock()
	defer inst.playlistMu.RUnlock()
	return inst.sortedChannels
}

// ChannelsReady returns whether SetChannels has been called.
func (inst *Instance) ChannelsReady() bool {
	inst.playlistMu.RLock()
	defer inst.playlistMu.RUnlock()
	return inst.channelsReady
}

// Serve binds the HLS HTTP server and begins serving immediately — before the
// (potentially slow) portal handshake — so the port is reachable within
// milliseconds. Channel requests return 503 until SetChannels is called.
// Blocks until Stop is called or the listener fails.
func (inst *Instance) Serve(bind string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/iptv", inst.playlistHandler)
	mux.HandleFunc("/iptv/", inst.channelHandler)
	mux.HandleFunc("/logo/", inst.logoHandler)

	srv := &http.Server{Addr: bind, Handler: mux}
	inst.serverMu.Lock()
	inst.server = srv
	inst.serverMu.Unlock()

	log.Println("HLS service listening on", bind)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println("HLS ListenAndServe error:", err)
	}
}

// SetChannels (re)populates the channel playlist, atomically swapping it in
// for the live handlers. Called once the portal's channel list has been
// retrieved, which may be well after Serve has already started accepting
// connections.
func (inst *Instance) SetChannels(chs map[string]*stalker.Channel) {
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
			owner: inst,
		}
		newSorted = append(newSorted, k)
	}
	sort.Strings(newSorted)

	// Stop any keep-alive goroutines from a previous channel set before
	// dropping our reference to them, so a re-fetch doesn't leak them.
	inst.playlistMu.Lock()
	old := inst.playlist
	inst.playlist = newPlaylist
	inst.sortedChannels = newSorted
	inst.channelsReady = true
	inst.playlistMu.Unlock()

	for _, ch := range old {
		ch.stopKeepAlive()
	}
}

// SetUserAgent sets the User-Agent string used for HLS content requests.
func (inst *Instance) SetUserAgent(model string) {
	inst.userAgent = stalker.BuildUserAgent(model)
}

// SetDeviceHeaders configures the device-identifying headers sent on media
// requests. Real MAG STBs send Mac, Model, X-Hash, and serial headers on
// every HLS segment and VOD media download.
func (inst *Instance) SetDeviceHeaders(mac, model, serial string) {
	inst.deviceMac = mac
	inst.deviceModel = model
	inst.deviceSerial = serial
	inst.deviceHash = buildDeviceHash(model)
}

// IsPlaying reports whether any channel has been accessed recently enough to
// still count as "being watched". Used as stalker.Portal's IsPlayingFunc.
func (inst *Instance) IsPlaying() bool {
	playbackActivityMu.Lock()
	defer playbackActivityMu.Unlock()
	if lastPlaybackActivity.IsZero() {
		return false
	}
	return timeSince(lastPlaybackActivity) <= hlsKeepAliveIdleTimeout
}

// Stop gracefully shuts down the HLS HTTP server, if running. Also stops all
// per-channel keep-alive goroutines.
func (inst *Instance) Stop() {
	inst.serverMu.Lock()
	srv := inst.server
	inst.server = nil
	inst.serverMu.Unlock()
	if srv != nil {
		srv.Shutdown(context.Background())
	}
	// Stop the per-channel keep-alive goroutines too.
	inst.playlistMu.RLock()
	chans := make([]*Channel, 0, len(inst.playlist))
	for _, ch := range inst.playlist {
		chans = append(chans, ch)
	}
	inst.playlistMu.RUnlock()
	for _, ch := range chans {
		ch.stopKeepAlive()
	}
}

// userAgent returns the configured User-Agent or a sensible default.
func (inst *Instance) userAgentString() string {
	if inst.userAgent != "" {
		return inst.userAgent
	}
	return stalker.BuildUserAgent("MAG200")
}

// ####################################################
// Backward-compatible package-level API using a default Instance.
// Callers who need multi-profile isolation should create their own Instance
// via NewInstance() and call methods directly.

var defaultInstance = NewInstance()

// Serve binds the default HLS instance on the given address.
func Serve(bind string) { defaultInstance.Serve(bind) }

// SetChannels populates the default HLS instance channel playlist.
func SetChannels(chs map[string]*stalker.Channel) { defaultInstance.SetChannels(chs) }

// SetUserAgent sets the User-Agent on the default HLS instance.
func SetUserAgent(model string) { defaultInstance.SetUserAgent(model) }

// SetDeviceHeaders sets device headers on the default HLS instance.
func SetDeviceHeaders(mac, model, serial string) { defaultInstance.SetDeviceHeaders(mac, model, serial) }

// IsPlaying returns the play state of the default HLS instance.
func IsPlaying() bool { return defaultInstance.IsPlaying() }

// Stop shuts down the default HLS instance.
func Stop() { defaultInstance.Stop() }
