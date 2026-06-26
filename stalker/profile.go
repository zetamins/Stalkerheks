package stalker

import (
	"encoding/json"
	"net/url"
	"strconv"
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
	params.Set("image_version", FirmwareImageVersion())
	params.Set("video_out", "hdmi")
	params.Set("device_id", p.DeviceID)
	params.Set("device_id2", p.DeviceID2)
	params.Set("signature", p.signature())
	params.Set("auth_second_step", "0") // Real STB sends 0 on initial boot, 1 only after portal auth dialog (xpcom.common.js:914-929)
	params.Set("hw_version", FirmwareHWVersion())
	params.Set("not_valid_token", "0")
	// The real MAG STB sends exactly 14 params (plus JsHttpRequest).
	// client_type, metrics, hw_version_2, api_signature, prehash, timestamp
	// are NOT sent by real firmware — removed to match xpcom.common.js exactly.
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

// getLocalization retrieves the full localization map from the portal.
// Real STBs use this to populate UI strings for the current locale.
func (p *Portal) getLocalization() (map[string]string, error) {
	type tmpStruct struct {
		Js map[string]string `json:"js"`
	}
	var tmp tmpStruct
	content, err := p.httpRequest(p.Location + "?type=stb&action=get_localization&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return nil, err
	}
	return tmp.Js, nil
}

// ModuleInfo describes a single portal module as returned by get_modules.
type ModuleInfo struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Switched bool   `json:"switched"`
}

// getModules retrieves the module configuration from the portal.
// Real STBs use this to determine which UI features to show.
func (p *Portal) getModules() ([]ModuleInfo, error) {
	type tmpStruct struct {
		Js struct {
			AllModules        []ModuleInfo `json:"all_modules"`
			SwitchableModules []string     `json:"switchable_modules"`
			DisabledModules   []string     `json:"disabled_modules"`
			RestrictedModules []string     `json:"restricted_modules"`
		} `json:"js"`
	}
	var tmp tmpStruct
	content, err := p.httpRequest(p.Location + "?type=stb&action=get_modules&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return nil, err
	}
	return tmp.Js.AllModules, nil
}
