package hls

import (
	"log"
	"sync"
	"time"

	"github.com/erkexzcx/stalkerhek/stalker"
)

const (
	linkTypeUnknown = 0 // default
	linkTypeHLS     = 1
	linkTypeMedia   = 2

	// hlsKeepAliveInterval is how often to refresh the HLS playlist URL
	// to keep the stream session alive on the streaming server. This mimics
	// the STB's native hls::KeepAliveWatchDog behavior.
	hlsKeepAliveInterval = 25 * time.Second

	// hlsKeepAliveIdleTimeout is how long a channel can go without any STB
	// requests before the keep-alive goroutine stops.
	hlsKeepAliveIdleTimeout = 120 * time.Second

	// hlsLinkValidityTimeout is how long a resolved channel link is reused
	// before we force a fresh create_link. Kept generous so slow-starting
	// channels (heavy initial buffering / slow upstream — e.g. some FHD
	// feeds) have time to actually begin playback before the link is treated
	// as stale and re-resolved. Safe to extend because the keep-alive
	// goroutine refreshes the streaming-server session every
	// hlsKeepAliveInterval, so the upstream session won't expire underneath
	// it; and forcing create_link too eagerly on a busy portal risks
	// transient "limit" errors that break playback outright.
	hlsLinkValidityTimeout = 90 * time.Second
)

var (
	playbackActivityMu   sync.Mutex
	lastPlaybackActivity time.Time
)

// markPlaybackActivity records that some channel was just accessed by a
// viewer. Called from validate() on every content request.
func markPlaybackActivity() {
	playbackActivityMu.Lock()
	lastPlaybackActivity = time.Now()
	playbackActivityMu.Unlock()
}

// IsPlaying reports whether any channel has been accessed recently enough to
// still count as "being watched". Used as stalker.Portal's IsPlayingFunc, so
// the outbound watchdog's cur_play_type reflects real playback state instead
// of always claiming live TV is playing. Reuses the same idle threshold as
// the HLS keep-alive goroutine, since that's this codebase's existing
// definition of "still being watched".
func IsPlaying() bool {
	playbackActivityMu.Lock()
	defer playbackActivityMu.Unlock()
	if lastPlaybackActivity.IsZero() {
		return false
	}
	return time.Since(lastPlaybackActivity) <= hlsKeepAliveIdleTimeout
}

// Logo stores TV channel logo details.
type Logo struct {
	Mux              *sync.Mutex
	Link             string // Link to channel's URL
	Cache            []byte // Actual logo
	CacheContentType string // Logo type
}

// Channel stores TV channel details.
type Channel struct {
	StalkerChannel *stalker.Channel // Reference to Stalker channel

	Mux *sync.Mutex // Mux for channel.

	Link     string // Original link, retrieved from Stalkerhek middleware
	LinkType int    // Original link's type

	HLSLink     string // Updated HLS TV channel's link
	HLSLinkRoot string // Used for HLS relative paths

	lastAccess time.Time // Last access time of this channel, so we know when to request new channel from Stalker middleware

	keepAliveStop chan struct{} // Signal to stop the keep-alive goroutine
	keepAliveOnce sync.Once     // Ensure only one keep-alive goroutine per channel

	Logo *Logo // Reference to channel's logo

	Genre string // TV channel genre. This field does not require synchronization
}

func (c *Channel) validate() error {
	if !c.isValid() {
		newLink, err := c.StalkerChannel.NewLink(true)
		if err != nil {
			return err
		}

		c.Link = newLink
		c.LinkType = 0
	}

	c.lastAccess = time.Now()
	markPlaybackActivity()
	return nil
}

// startKeepAlive starts a background goroutine that periodically refreshes
// the HLS playlist URL to keep the streaming server session alive. Without
// this, the server expires the session and returns 458 (not prioritized).
func (c *Channel) startKeepAlive() {
	c.keepAliveOnce.Do(func() {
		// Capture the stop channel in a local so the goroutine never re-reads
		// c.keepAliveStop (which stopKeepAlive nils out under c.Mux) — reading
		// it unsynchronized was both a data race and a way to miss the signal.
		stop := make(chan struct{})
		c.keepAliveStop = stop
		go func() {
			ticker := time.NewTicker(hlsKeepAliveInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					// lastAccess and HLSLink are mutated by request handlers
					// under c.Mux, so read them under the same lock rather than
					// racing on the bare fields.
					c.Mux.Lock()
					lastAccess := c.lastAccess
					hlsLink := c.HLSLink
					c.Mux.Unlock()
					// Check if channel is still being watched
					if time.Since(lastAccess) > hlsKeepAliveIdleTimeout {
						return
					}
					// Refresh the HLS playlist to keep session alive.
					// We just fetch the playlist URL; the response keeps
					// the streaming server session from expiring.
					resp, err := httpClient.Get(hlsLink)
					if err != nil {
						log.Printf("HLS keep-alive refresh failed for %s: %v", c.StalkerChannel.Title, err)
						continue
					}
					resp.Body.Close()
					log.Printf("HLS keep-alive refreshed: %s", c.StalkerChannel.Title)
				case <-stop:
					return
				}
			}
		}()
	})
}

// stopKeepAlive stops the keep-alive goroutine for this channel. Guarded by
// c.Mux (the same lock startKeepAlive's caller holds when assigning
// keepAliveStop) so concurrent calls can't double-close the channel, and
// idempotent so calling it on a channel that never started one is a no-op.
func (c *Channel) stopKeepAlive() {
	c.Mux.Lock()
	defer c.Mux.Unlock()
	if c.keepAliveStop != nil {
		close(c.keepAliveStop)
		c.keepAliveStop = nil
	}
}

func (c *Channel) isValid() bool {
	// If channel has never been accessed
	if c.lastAccess.IsZero() {
		return false
	}

	// Same generous validity window for HLS and direct media: gives the
	// player enough headroom to start playback (initial buffering, slow
	// upstream) before the link is treated as stale and a fresh create_link
	// is forced.
	return time.Since(c.lastAccess) <= hlsLinkValidityTimeout
}
