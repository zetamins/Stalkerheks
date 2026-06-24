package stalker

import (
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"
)

// Start connects to stalker portal, reserves token, starts watchdog etc.
func (p *Portal) Start() error {
	// Reserve token in Stalker portal. Real STBs are provisioned with two
	// portal URLs (portal1/portal2) and fail over to the second if the
	// first is unreachable; mirror that here.
	if err := p.handshake(); err != nil {
		if p.Location2 == "" {
			return err
		}
		log.Println("Handshake failed against primary portal URL, trying fallback:", err)
		p.Location, p.Location2 = p.Location2, p.Location
		if err := p.handshake(); err != nil {
			return err
		}
	}

	// Submit device profile, as real STB hardware does right after handshake
	// and before listing channels. Non-fatal: some portals don't require it,
	// but most real boxes always send it. (get_main_info is deliberately NOT
	// called here — confirmed against the real Ministra client JS that it's
	// only fired when a user opens the Account screen, never during boot.)
	if err := p.getProfile(); err != nil {
		log.Println("get_profile failed (continuing anyway):", err)
	}
	if err := p.getLocalization(); err != nil {
		log.Println("get_localization failed (continuing anyway):", err)
	}
	if err := p.getModules(); err != nil {
		log.Println("get_modules failed (continuing anyway):", err)
	}

	return nil
}

// StartWatchdog sends the first post-boot watchdog ping and starts the
// periodic watchdog goroutine. Real STBs dispatch get_all_channels (and the
// other channel/EPG/recording loads) before their first watchdog send
// (confirmed in the real client's xpcom.common.js boot sequence) — callers
// should call this only after retrieving the channel list, not from Start()
// itself, to match that ordering.
func (p *Portal) StartWatchdog() error {
	// Run watchdog function once to check for errors. Real STBs send
	// init=1 only on this first post-boot watchdog call (confirmed in the
	// real client's watchdog.js: send_request(true) on startup, false on
	// every subsequent tick) — never sending it at all is itself an
	// unrealistic pattern, since genuine hardware reports a fresh boot
	// every time it reconnects (power cycle, network drop, portal reload).
	if err := p.watchdogUpdate(true); err != nil {
		return err
	}

	// Real STBs default to a 30s watchdog interval (confirmed in the real
	// client's watchdog.js) and use get_profile's "watchdog_timeout" field
	// when the portal specifies one — a fixed 2-minute interval is both
	// slower than genuine hardware and ignores the server's preference.
	watchdogInterval := 30 * time.Second
	if p.WatchdogTimeout > 0 {
		watchdogInterval = time.Duration(p.WatchdogTimeout) * time.Second
	}

	p.watchdogStop = make(chan struct{})

	// Transient errors (502, timeouts) are logged but not fatal.
	go func(stop chan struct{}) {
		for {
			select {
			case <-time.After(watchdogInterval):
				if err := p.watchdogUpdate(false); err != nil {
					log.Println("Watchdog update failed (will retry):", err)
				}
			case <-stop:
				return
			}
		}
	}(p.watchdogStop)

	return nil
}

// StopWatchdog stops the periodic watchdog goroutine started by
// StartWatchdog. Safe to call even if the watchdog was never started.
func (p *Portal) StopWatchdog() {
	if p.watchdogStop != nil {
		close(p.watchdogStop)
		p.watchdogStop = nil
	}
}

func (p *Portal) httpRequest(link string) ([]byte, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", p.UserAgent())
	req.Header.Set("X-User-Agent", "Model: "+p.Model+"; Link: Ethernet")
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("SN", p.SerialNumber)

	cookieText := "PHPSESSID=null; sn=" + url.QueryEscape(p.SerialNumber) + "; mac=" + url.QueryEscape(p.MAC) + "; stb_lang=en; timezone=" + url.QueryEscape(p.TimeZone) + ";"

	req.Header.Set("Cookie", cookieText)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("Site '" + link + "' returned " + resp.Status)
	}

	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return contents, nil
}

// watchdogUpdate performs a watchdog update request. init should be true only
// for the first call after a fresh connection (matches real STB behavior).
func (p *Portal) watchdogUpdate(init bool) error {
	initVal := "0"
	if init {
		initVal = "1"
	}
	_, err := p.httpRequest(p.Location + "?action=get_events&event_active_id=0&init=" + initVal + "&type=watchdog&cur_play_type=" + p.curPlayType() + "&JsHttpRequest=1-xml")
	if err != nil {
		return err
	}
	return nil
}

// curPlayType reports the watchdog's cur_play_type value. Real STBs send 0
// while idle and 1 (live TV) only while actively playing (confirmed in the
// real client's watchdog.js get_current_place()); a hardcoded 1 would claim
// this device is always watching TV even when nothing is being relayed.
func (p *Portal) curPlayType() string {
	if p.IsPlayingFunc != nil && p.IsPlayingFunc() {
		return "1"
	}
	return "0"
}
