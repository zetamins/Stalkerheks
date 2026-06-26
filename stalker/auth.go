package stalker

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"
)

// OAuthConfig carries optional OAuth provider credentials for doAuth.
// When nil, doAuth sends only the device identity.
type OAuthConfig struct {
	OAuthProvider string // e.g. "google", "facebook"
	OAuthToken    string
	OAuthUID      string
}

// doAuth sends the authentication request to the portal. Real STBs call this
// after handshake to complete the second authentication step. Some portals
// require OAuth tokens; others accept the device identity alone.
func (p *Portal) doAuth(oauth *OAuthConfig) error {
	params := url.Values{}
	params.Set("type", "stb")
	params.Set("action", "do_auth")
	params.Set("sn", p.SerialNumber)
	params.Set("mac", p.MAC)
	params.Set("device_id", p.DeviceID)
	params.Set("device_id2", p.DeviceID2)
	params.Set("signature", p.signature())
	params.Set("hd", "1")
	params.Set("ver", p.VersionString())
	params.Set("stb_type", p.Model)
	params.Set("client_type", "STB")
	params.Set("image_version", FirmwareImageVersion())
	params.Set("video_out", "hdmi")
	params.Set("num_banks", "1")
	params.Set("hw_version", FirmwareHWVersion())
	params.Set("hw_version_2", p.HWVersion2())
	params.Set("api_signature", FirmwareAPISignature())
	params.Set("prehash", p.Prehash())
	params.Set("metrics", p.Metrics())
	params.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	params.Set("JsHttpRequest", "1-xml")

	if oauth != nil {
		if oauth.OAuthProvider != "" {
			params.Set("oauth_provider", oauth.OAuthProvider)
		}
		if oauth.OAuthToken != "" {
			params.Set("oauth_token", oauth.OAuthToken)
		}
		if oauth.OAuthUID != "" {
			params.Set("oauth_uid", oauth.OAuthUID)
		}
	}

	content, err := p.httpRequest(p.Location + "?" + params.Encode())
	if err != nil {
		return err
	}

	// Some portals return {"js":true} on success, others return structured errors
	type tmpStruct struct {
		Js *struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"js"`
	}
	var tmp tmpStruct
	if err := json.Unmarshal(content, &tmp); err != nil {
		// If we can't parse the response, assume success (non-JSON or {"js":true})
		return nil
	}
	if tmp.Js != nil && tmp.Js.Status == "FAIL" {
		return errors.New("auth failed: " + tmp.Js.Message)
	}
	return nil
}
