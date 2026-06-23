package stalker

import (
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
// flagged or degraded as "not a real box".
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
	params.Set("signature", p.Signature)
	params.Set("auth_second_step", "1")
	params.Set("hw_version", "1.0.00")
	params.Set("not_valid_token", "0")
	params.Set("metrics", p.Metrics())
	params.Set("hw_version_2", p.HWVersion2())
	params.Set("api_signature", "256")
	params.Set("prehash", p.Prehash())
	params.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	params.Set("JsHttpRequest", "1-xml")

	_, err := p.httpRequest(p.Location + "?" + params.Encode())
	return err
}

// getLocalization and getMainInfo mirror two more calls real STBs commonly
// make during boot, right after get_profile and before listing channels.
// Non-fatal: not every portal deployment requires them.
func (p *Portal) getLocalization() error {
	_, err := p.httpRequest(p.Location + "?type=stb&action=get_localization&JsHttpRequest=1-xml")
	return err
}

func (p *Portal) getMainInfo() error {
	_, err := p.httpRequest(p.Location + "?type=account_info&action=get_main_info&JsHttpRequest=1-xml")
	return err
}
