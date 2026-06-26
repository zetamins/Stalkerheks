package stalker

import (
	"encoding/json"
	"net/url"
)

// GetSettings retrieves user settings from the portal. Returns a map of
// setting keys to their values as strings.
func (p *Portal) GetSettings() (map[string]string, error) {
	type tmpStruct struct {
		Js map[string]string `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?type=stb&action=get_settings&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return nil, err
	}
	return tmp.Js, nil
}

// SetSettings sends user settings to the portal. Accepts a map of setting
// keys to values.
func (p *Portal) SetSettings(settings map[string]string) error {
	params := url.Values{}
	params.Set("type", "stb")
	params.Set("action", "set_settings")
	for k, v := range settings {
		params.Set(k, v)
	}
	params.Set("JsHttpRequest", "1-xml")
	_, err := p.httpRequest(p.Location + "?" + params.Encode())
	return err
}
