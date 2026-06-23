package stalker

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
)

// Handshake reserves a offered token in Portal. If offered token is not available - new one will be issued by stalker portal and Stalker's config will be updated.
func (p *Portal) handshake() error {
	// This HTTP request has different headers from the rest of HTTP requests, so perform it manually
	type tmpStruct struct {
		Js map[string]interface{} `json:"js"`
	}
	var tmp tmpStruct

	req, err := http.NewRequest("GET", p.Location+"?type=stb&action=handshake&prehash=0&token="+p.Token+"&JsHttpRequest=1-xml", nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", p.UserAgent())
	req.Header.Set("X-User-Agent", "Model: "+p.Model+"; Link: Ethernet")
	req.Header.Set("SN", p.SerialNumber)
	req.Header.Set("Cookie", "PHPSESSID=null; sn="+p.SerialNumber+"; mac="+p.MAC+"; stb_lang=en; timezone="+p.TimeZone)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err = json.Unmarshal(contents, &tmp); err != nil {
		log.Println(string(contents))
		return err
	}

	if random, ok := tmp.Js["random"]; ok {
		if s, ok := random.(string); ok {
			p.Random = s
		}
	}

	token, ok := tmp.Js["Token"]
	if !ok || token == "" {
		// Token accepted. Using accepted token
		return nil
	}
	// Server provided new token. Using new provided token
	p.Token = token.(string)
	return nil
}
