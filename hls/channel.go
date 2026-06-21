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
)

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
		newLink, err := c.StalkerChannel.NewLink(false)
		if err != nil {
			return err
		}

		c.Link = newLink
		c.LinkType = 0
	}

	c.lastAccess = time.Now()
	return nil
}

// startKeepAlive starts a background goroutine that periodically refreshes
// the HLS playlist URL to keep the streaming server session alive. Without
// this, the server expires the session and returns 458 (not prioritized).
func (c *Channel) startKeepAlive() {
	c.keepAliveOnce.Do(func() {
		c.keepAliveStop = make(chan struct{})
		go func() {
			ticker := time.NewTicker(hlsKeepAliveInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					// Check if channel is still being watched
					if time.Since(c.lastAccess) > hlsKeepAliveIdleTimeout {
						return
					}
					// Refresh the HLS playlist to keep session alive.
					// We just fetch the playlist URL; the response keeps
					// the streaming server session from expiring.
					resp, err := httpClient.Get(c.HLSLink)
					if err != nil {
						log.Printf("HLS keep-alive refresh failed for %s: %v", c.StalkerChannel.Title, err)
						continue
					}
					resp.Body.Close()
					log.Printf("HLS keep-alive refreshed: %s", c.StalkerChannel.Title)
				case <-c.keepAliveStop:
					return
				}
			}
		}()
	})
}

// stopKeepAlive stops the keep-alive goroutine for this channel.
func (c *Channel) stopKeepAlive() {
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

	// 30 seconds timeout for HLS content
	if c.LinkType == linkTypeHLS {
		return time.Since(c.lastAccess).Seconds() <= 30
	}

	// 5 seconds for everything else
	return time.Since(c.lastAccess).Seconds() <= 5
}
