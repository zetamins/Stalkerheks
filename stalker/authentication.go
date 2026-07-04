package stalker

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"
)

// handshakeClient is the timed client for the very first portal call. The
// original used http.DefaultClient, which has no timeout — a black-holed
// connect (frequent on Huawei/EMUI with IPv6-only DNS answers) stalled the
// entire engine boot here before any port was ever bound. Auto-follow of
// redirects is kept (unlike httpRedirectClient) to preserve the original
// handshake behavior; only the timeouts are added.
var handshakeClient = &http.Client{
	// Shares the same shared-client-under-concurrent-load reasoning as
	// httpRedirectClient (stalker.go) — this process boots one goroutine per
	// profile, and all of them race their handshake through this one client.
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

// handshakeMaxAttempts caps retries for transient handshake errors — same
// contention pattern as RetrieveChannels (channels.go): under concurrent
// multi-profile load a single timeout doesn't mean the portal is down.
const handshakeMaxAttempts = 3

// handshakeRetryDelay is the wait between transient handshake retries.
const handshakeRetryDelay = 2 * time.Second

// Handshake reserves a offered token in Portal. If offered token is not available - new one will be issued by stalker portal and Stalker's config will be updated.
func (p *Portal) handshake() error {
	var contents []byte
	var err error
	for attempt := 1; attempt <= handshakeMaxAttempts; attempt++ {
		contents, err = p.handshakeOnce()
		if err == nil {
			break
		}
		if !isTransientHTTPError(err) || attempt == handshakeMaxAttempts {
			return err
		}
		log.Println("handshake transient failure, retrying:", err)
		time.Sleep(handshakeRetryDelay)
	}

	type tmpStruct struct {
		Js map[string]interface{} `json:"js"`
	}
	var tmp tmpStruct

	if err = json.Unmarshal(contents, &tmp); err != nil {
		log.Println(string(contents))
		return err
	}

	if random, ok := tmp.Js["random"]; ok {
		if s, ok := random.(string); ok {
			p.Random = s
		}
	}

	// Key is lowercase "token", matching the portal's JSON (and the lowercase
	// "random" read above) — checking "Token" never matched, so a
	// server-issued replacement token was silently dropped and we kept using
	// the rejected one.
	tokenVal, ok := tmp.Js["token"]
	if !ok {
		// Token accepted. Using accepted token
		return nil
	}
	// Server provided new token. Using new provided token
	if s, ok := tokenVal.(string); ok && s != "" {
		p.Token = s
	}
	return nil
}

// handshakeOnce performs a single handshake HTTP call and returns the raw
// response body. This HTTP request has different headers from the rest of
// HTTP requests, so it's performed manually rather than via httpRequest.
func (p *Portal) handshakeOnce() ([]byte, error) {
	req, err := http.NewRequest("GET", p.Location+"?type=stb&action=handshake&token="+p.Token+"&JsHttpRequest=1-xml", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", p.UserAgent())
	req.Header.Set("X-User-Agent", "Model: "+p.Model+"; Link: Ethernet")
	// Real MAG STB: no SN header, cookie = mac/stb_lang/timezone only.
	req.Header.Set("Cookie", "mac="+p.MAC+"; stb_lang=en; timezone="+p.TimeZone)

	resp, err := handshakeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}
