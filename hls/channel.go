package hls

import (
	"log"
	"sync"
	"time"

	"github.com/erkexzcx/stalkerhek/stalker"
)

const (
	linkTypeUnknown = 0
	linkTypeHLS     = 1
	linkTypeMedia   = 2

	hlsKeepAliveInterval    = 25 * time.Second
	hlsKeepAliveIdleTimeout = 120 * time.Second
	hlsLinkValidityTimeout  = 90 * time.Second
)

// Per-instance playback activity tracking — used by IsPlaying() to report
// watch state for watchdog cur_play_type.
var (
	playbackActivityMu   sync.Mutex
	lastPlaybackActivity time.Time
)

// markPlaybackActivity records that some channel was just accessed by a viewer.
func markPlaybackActivity() {
	playbackActivityMu.Lock()
	lastPlaybackActivity = time.Now()
	playbackActivityMu.Unlock()
}

// Logo stores TV channel logo details.
type Logo struct {
	Mux              *sync.Mutex
	Link             string
	Cache            []byte
	CacheContentType string
}

// Channel stores TV channel details for HLS relay.
type Channel struct {
	StalkerChannel *stalker.Channel

	Mux *sync.Mutex

	Link     string
	LinkType int

	HLSLink     string
	HLSLinkRoot string

	lastAccess time.Time

	keepAliveStop chan struct{}
	keepAliveOnce sync.Once

	Logo  *Logo
	Genre string

	// owner is the HLS Instance this channel belongs to. Used to access
	// device headers and user agent for media/CDN requests.
	owner *Instance
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
// the HLS playlist URL to keep the streaming server session alive.
func (c *Channel) startKeepAlive() {
	c.keepAliveOnce.Do(func() {
		stop := make(chan struct{})
		c.keepAliveStop = stop
		go func() {
			ticker := time.NewTicker(hlsKeepAliveInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					c.Mux.Lock()
					lastAccess := c.lastAccess
					hlsLink := c.HLSLink
					owner := c.owner
					c.Mux.Unlock()

					if time.Since(lastAccess) > hlsKeepAliveIdleTimeout {
						return
					}

					// Use owner's device headers for CDN requests
					resp, err := instanceResponse(hlsLink, owner)
					if err != nil {
						log.Printf("HLS keep-alive refresh failed for %s: %v", c.StalkerChannel.Title, err)
						continue
					}
					resp.Body.Close()
				case <-stop:
					return
				}
			}
		}()
	})
}

// stopKeepAlive stops the keep-alive goroutine for this channel.
func (c *Channel) stopKeepAlive() {
	c.Mux.Lock()
	defer c.Mux.Unlock()
	if c.keepAliveStop != nil {
		close(c.keepAliveStop)
		c.keepAliveStop = nil
	}
}

func (c *Channel) isValid() bool {
	if c.lastAccess.IsZero() {
		return false
	}
	return time.Since(c.lastAccess) <= hlsLinkValidityTimeout
}
