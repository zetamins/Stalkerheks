package stalker

import (
	"errors"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// maxPortalRedirects caps redirect hops in doHTTPRequest. Like hls.response,
// this client follows redirects manually (to reapply the full header set on
// every hop), so without a cap a redirect loop (A→B→A) or an endless chain
// would recurse until the goroutine's stack is exhausted and the process
// crashes. Match Go's own http.Client default of 10.
const maxPortalRedirects = 10

// Start connects to stalker portal, reserves token, starts watchdog etc.
func (p *Portal) Start() error {
	// Best-effort NTP time sync before portal connection. Real STBs sync
	// their clock on boot; a drifted clock is a detectable anomaly.
	syncNTP()

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
	if _, err := p.getLocalization(); err != nil {
		log.Println("get_localization failed (continuing anyway):", err)
	}
	if _, err := p.getModules(); err != nil {
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

// httpRedirectClient never auto-follows redirects: Go's default client
// strips Authorization and Cookie headers whenever a redirect crosses to a
// different host (e.g. portal -> CDN), which silently breaks multi-hop
// redirect chains. httpRequest follows redirects itself instead, reapplying
// the full header set on every hop.
var httpRedirectClient = &http.Client{
	// Overall per-request cap. The portal API calls are small JSON, so a
	// bounded timeout is safe here (this client never streams media — that's
	// the proxy/hls clients). Without it, a single un-timed call could hang
	// for the OS-level TCP timeout: on devices that get a black-holed IPv6
	// address (common on Huawei/EMUI when DNS returns only AAAA, with no IPv4
	// to race against), the ~7 sequential boot calls stacked up into a
	// many-minute startup during which the proxy/HLS ports never even bound.
	Timeout: 30 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		// Fail a dead/black-holed connection fast instead of waiting on the
		// OS default, so a slow portal can't stall the whole engine startup.
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          8,
		IdleConnTimeout:       90 * time.Second,
	},
}

func (p *Portal) httpRequest(link string) ([]byte, error) {
	resp, err := p.doHTTPRequest(link)
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

func (p *Portal) doHTTPRequest(link string) (*http.Response, error) {
	return p.doHTTPRequestFollow(link, 0)
}

func (p *Portal) doHTTPRequestFollow(link string, depth int) (*http.Response, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", p.UserAgent())
	req.Header.Set("X-User-Agent", "Model: "+p.Model+"; Link: Ethernet")
	req.Header.Set("Authorization", "Bearer "+p.Token)
	// Real MAG STB does NOT send an SN header — removed to match firmware.
	// Cookie exactly matches real MAG STB (xpcom.common.js set_cookie):
	// mac, stb_lang, timezone — no sn, no fabricated PHPSESSID.
	cookieText := "mac=" + url.QueryEscape(p.MAC) + "; stb_lang=en; timezone=" + url.QueryEscape(p.TimeZone)

	req.Header.Set("Cookie", cookieText)

	resp, err := httpRedirectClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return resp, nil
	}

	defer resp.Body.Close()
	if depth >= maxPortalRedirects {
		return nil, errors.New("stopped after " + strconv.Itoa(maxPortalRedirects) + " redirects following " + link)
	}
	linkURL, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	redirectURL, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		return nil, err
	}
	return p.doHTTPRequestFollow(linkURL.ResolveReference(redirectURL).String(), depth+1)
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

// LogPlaybackITV logs that a TV channel was watched. Real STBs send this
// after a percentage of the content has been played.
func (p *Portal) LogPlaybackITV(channelID string) error {
	_, err := p.httpRequest(p.Location + "?type=itv&action=set_played&itv_id=" + url.PathEscape(channelID) + "&JsHttpRequest=1-xml")
	return err
}

// LogPlaybackVOD logs that a VOD item was watched.
func (p *Portal) LogPlaybackVOD(videoID string) error {
	_, err := p.httpRequest(p.Location + "?type=vod&action=set_played&video_id=" + url.PathEscape(videoID) + "&JsHttpRequest=1-xml")
	return err
}

// LogStreamError reports a stream error (e.g. loading fail) to the portal.
func (p *Portal) LogStreamError(chID, event string) error {
	_, err := p.httpRequest(p.Location + "?type=stb&action=set_stream_error&ch_id=" + url.PathEscape(chID) + "&event=" + url.PathEscape(event) + "&JsHttpRequest=1-xml")
	return err
}

// SetEndedVOD marks a VOD item as fully watched on the portal.
func (p *Portal) SetEndedVOD(videoID string) error {
	_, err := p.httpRequest(p.Location + "?type=vod&action=set_ended&video_id=" + url.PathEscape(videoID) + "&JsHttpRequest=1-xml")
	return err
}

// SetNotEndedVOD marks a VOD item as partially watched (for resume).
func (p *Portal) SetNotEndedVOD(videoID, series, endTime string) error {
	params := url.Values{}
	params.Set("type", "vod")
	params.Set("action", "set_not_ended")
	params.Set("video_id", videoID)
	if series != "" {
		params.Set("series", series)
	}
	params.Set("end_time", endTime)
	params.Set("JsHttpRequest", "1-xml")
	_, err := p.httpRequest(p.Location + "?" + params.Encode())
	return err
}

// ntpServer is the NTP server used for time synchronization before portal
// connection. Real STBs sync their clock via NTP on boot; a drifted clock
// is a detectable anomaly (watchdog timestamps, handshake timing).
const ntpServer = "pool.ntp.org:123"

// ntpEpochDelta is the NTP epoch (1900-01-01) minus Unix epoch (1970-01-01)
// in seconds, for converting between NTP and Unix timestamps.
const ntpEpochDelta = 2208988800

// syncNTP performs a best-effort NTP time query. On success, logs the offset
// between the local clock and the NTP reference. Does not set the system clock
// (requires root); the offset is informational and non-fatal.
func syncNTP() {
	conn, err := net.DialTimeout("udp", ntpServer, 5*time.Second)
	if err != nil {
		log.Println("NTP sync skipped (network not available):", err)
		return
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}

	// NTP v4 client request packet (48 bytes):
	// - byte 0: LI=0, VN=4, Mode=3 (client)
	// - bytes 1-47: zeros (no optional fields needed for a basic query)
	req := make([]byte, 48)
	req[0] = 0x23 // 00 100 011 = Leap=0, Version=4, Mode=3

	if _, err := conn.Write(req); err != nil {
		log.Println("NTP query failed:", err)
		return
	}

	resp := make([]byte, 48)
	if _, err := conn.Read(resp); err != nil {
		log.Println("NTP response failed:", err)
		return
	}

	// Extract transmit timestamp (bytes 40-47): 32-bit integer seconds +
	// 32-bit fractional seconds. Convert integer seconds to Unix time.
	secs := int64(resp[40])<<24 | int64(resp[41])<<16 | int64(resp[42])<<8 | int64(resp[43])
	if secs < ntpEpochDelta {
		return // invalid timestamp
	}
	ntpTime := secs - ntpEpochDelta
	offset := time.Duration(ntpTime-time.Now().Unix()) * time.Second
	log.Printf("NTP sync: offset %v from %s", offset, ntpServer)
}
