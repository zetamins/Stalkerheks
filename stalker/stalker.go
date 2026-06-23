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
	// but most real boxes always send it.
	if err := p.getProfile(); err != nil {
		log.Println("get_profile failed (continuing anyway):", err)
	}
	if err := p.getLocalization(); err != nil {
		log.Println("get_localization failed (continuing anyway):", err)
	}
	if err := p.getMainInfo(); err != nil {
		log.Println("get_main_info failed (continuing anyway):", err)
	}

	// Run watchdog function once to check for errors:
	if err := p.watchdogUpdate(); err != nil {
		return err
	}

	// Run watchdog function every 2 minutes.
	// Transient errors (502, timeouts) are logged but not fatal.
	go func() {
		for {
			time.Sleep(2 * time.Minute)
			if err := p.watchdogUpdate(); err != nil {
				log.Println("Watchdog update failed (will retry):", err)
			}
		}
	}()

	return nil
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

// WatchdogUpdate performs watchdog update request.
func (p *Portal) watchdogUpdate() error {
	_, err := p.httpRequest(p.Location + "?action=get_events&event_active_id=0&init=0&type=watchdog&cur_play_type=1&JsHttpRequest=1-xml")
	if err != nil {
		return err
	}
	return nil
}
