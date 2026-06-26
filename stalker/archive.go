package stalker

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// ArchiveLink retrieves a playable URL for a TV archive (timeshift/catch-up)
// entry. Uses the same create_link pattern as ITV/VOD but with type=tv_archive.
func (p *Portal) ArchiveLink(cmd, series string) (string, error) {
	type tmpStruct struct {
		Js struct {
			Cmd   string `json:"cmd"`
			Error string `json:"error"`
		} `json:"js"`
	}
	var tmp tmpStruct

	link := p.Location + "?action=create_link&type=tv_archive&cmd=" + url.PathEscape(cmd)
	if series != "" {
		link += "&series=" + url.PathEscape(series)
	}
	link += "&forced_storage=&disable_ad=0&download=0&JsHttpRequest=1-xml"

	content, err := p.httpRequest(link)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return "", err
	}
	if tmp.Js.Error != "" {
		return "", errors.New("archive create_link failed: " + tmp.Js.Error)
	}
	if tmp.Js.Cmd == "" {
		return "", errors.New("archive create_link returned empty command")
	}
	strs := strings.Split(tmp.Js.Cmd, " ")
	return strs[len(strs)-1], nil
}

// SetPlayedTVArchive logs TV archive playback start to the portal.
// Returns the history ID that can be used to update playback end time.
func (p *Portal) SetPlayedTVArchive(chID string) (string, error) {
	type tmpStruct struct {
		Js string `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?type=tv_archive&action=set_played&ch_id=" + url.QueryEscape(chID) + "&JsHttpRequest=1-xml")
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return "", err
	}
	return tmp.Js, nil
}

// UpdatePlayedTVArchiveEndTime updates the end time of an archive playback
// session so the portal can track how much of the program was watched.
func (p *Portal) UpdatePlayedTVArchiveEndTime(histID string) error {
	_, err := p.httpRequest(p.Location + "?type=tv_archive&action=update_played_end_time&hist_id=" + url.QueryEscape(histID) + "&JsHttpRequest=1-xml")
	return err
}

// SetPlayedTVArchiveTimeshift logs timeshift playback to the portal.
func (p *Portal) SetPlayedTVArchiveTimeshift(chID string) error {
	_, err := p.httpRequest(p.Location + "?type=tv_archive&action=set_played_timeshift&ch_id=" + url.QueryEscape(chID) + "&JsHttpRequest=1-xml")
	return err
}
