package stalker

import (
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// getProfile sends an STB identity profile to the Stalker portal, mirroring
// the call real hardware makes immediately after handshake and before
// listing channels. Many portal deployments gate channel visibility,
// playback features or anti-clone checks on a profile having been submitted
// with a plausible device identity (device_id/device_id2/signature, hashes,
// hardware descriptors) — skipping it is a common way STB emulators get
// flagged or degraded as "not a real box". This exact parameter set/order was
// confirmed against the real Ministra client JS (xpcom.common.js).
//
// The response also carries the server's preferred watchdog_timeout — real
// STBs read it here (not a separate call) and use it for the heartbeat
// interval, so we capture it onto Portal for Start() to use.
func (p *Portal) getProfile() error {
	params := url.Values{}
	params.Set("type", "stb")
	params.Set("action", "get_profile")
	params.Set("hd", "1")
	params.Set("ver", p.VersionString())
	params.Set("num_banks", "1")
	params.Set("sn", p.SerialNumber)
	params.Set("stb_type", p.Model)
	params.Set("client_type", "STB")
	params.Set("image_version", "0x00000015")
	params.Set("video_out", "hdmi")
	params.Set("device_id", p.DeviceID)
	params.Set("device_id2", p.DeviceID2)
	params.Set("signature", p.signature())
	params.Set("auth_second_step", "1")
	params.Set("hw_version", "1.0.00")
	params.Set("not_valid_token", "0")
	params.Set("metrics", p.Metrics())
	params.Set("hw_version_2", p.HWVersion2())
	params.Set("api_signature", "256")
	params.Set("prehash", p.Prehash())
	params.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	params.Set("JsHttpRequest", "1-xml")

	body, err := p.httpRequest(p.Location + "?" + params.Encode())
	if err != nil {
		return err
	}

	type tmpStruct struct {
		Js struct {
			WatchdogTimeout interface{} `json:"watchdog_timeout"`
		} `json:"js"`
	}
	var tmp tmpStruct
	if err := json.Unmarshal(body, &tmp); err == nil {
		switch v := tmp.Js.WatchdogTimeout.(type) {
		case float64:
			if v > 0 {
				p.WatchdogTimeout = int(v)
			}
		case string:
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				p.WatchdogTimeout = n
			}
		}
	}
	return nil
}

// getLocalization and getModules mirror two more calls real STBs make during
// boot, right after get_profile and before listing channels. Non-fatal: not
// every portal deployment requires them.
func (p *Portal) getLocalization() error {
	_, err := p.httpRequest(p.Location + "?type=stb&action=get_localization&JsHttpRequest=1-xml")
	return err
}

func (p *Portal) getModules() error {
	_, err := p.httpRequest(p.Location + "?type=stb&action=get_modules&JsHttpRequest=1-xml")
	return err
}
