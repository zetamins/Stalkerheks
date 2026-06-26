package stalker

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
)

// GetEPGInfo retrieves now/next EPG data for all user channels within the given
// period (hours). Default period is 3 hours if <= 0.
func (p *Portal) GetEPGInfo(period int) (map[string][]EPGEntry, error) {
	if period <= 0 {
		period = 3
	}

	type tmpStruct struct {
		Js struct {
			Data map[string][]struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				Descr          string `json:"descr"`
				StartTimestamp string `json:"start_timestamp"`
				StopTimestamp  string `json:"stop_timestamp"`
				StartTime      string `json:"t_time"`
				StopTime       string `json:"t_time_to"`
				MarkArchive    string `json:"mark_archive"`
			} `json:"data"`
		} `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?type=itv&action=get_epg_info&period=" + strconv.Itoa(period) + "&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return nil, err
	}

	result := make(map[string][]EPGEntry, len(tmp.Js.Data))
	for chID, progs := range tmp.Js.Data {
		entries := make([]EPGEntry, 0, len(progs))
		for _, ep := range progs {
			startTS, _ := strconv.ParseInt(ep.StartTimestamp, 10, 64)
			stopTS, _ := strconv.ParseInt(ep.StopTimestamp, 10, 64)
			entries = append(entries, EPGEntry{
				ID:             ep.ID,
				CHID:           chID,
				Name:           ep.Name,
				Descr:          ep.Descr,
				StartTimestamp: startTS,
				StopTimestamp:  stopTS,
				StartTime:      ep.StartTime,
				StopTime:       ep.StopTime,
				MarkArchive:    ep.MarkArchive == "1",
			})
		}
		result[chID] = entries
	}
	return result, nil
}

// GetEPGTable retrieves EPG data for a specific channel within a time window.
// fromTS and toTS are Unix timestamps. Returns per-channel EPG records with
// time marks for program grid display.
func (p *Portal) GetEPGTable(chID string, fromTS, toTS int64) (*EPGRecord, error) {
	params := url.Values{}
	params.Set("type", "epg")
	params.Set("action", "get_data_table")
	params.Set("ch_id", chID)
	params.Set("from", strconv.FormatInt(fromTS, 10))
	params.Set("to", strconv.FormatInt(toTS, 10))
	params.Set("JsHttpRequest", "1-xml")

	type tmpStruct struct {
		Js struct {
			Data []struct {
				CHID   string `json:"ch_id"`
				CHName string `json:"name"`
				CHType string `json:"ch_type"`
				EPG    []struct {
					ID             string `json:"id"`
					Name           string `json:"name"`
					Descr          string `json:"descr"`
					StartTimestamp string `json:"start_timestamp"`
					StopTimestamp  string `json:"stop_timestamp"`
					StartTime      string `json:"t_time"`
					StopTime       string `json:"t_time_to"`
					MarkArchive    string `json:"mark_archive"`
					MarkMemo       string `json:"mark_memo"`
					MarkRec        string `json:"mark_rec"`
				} `json:"epg"`
			} `json:"data"`
			FromTS    string   `json:"from_ts"`
			ToTS      string   `json:"to_ts"`
			TimeMarks []string `json:"time_marks"`
		} `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return nil, err
	}

	if len(tmp.Js.Data) == 0 {
		return nil, errors.New("no EPG data for channel " + chID)
	}

	d := tmp.Js.Data[0]
	rec := &EPGRecord{
		CHID:     d.CHID,
		CHName:   d.CHName,
		CHType:   d.CHType,
		Programs: make([]EPGEntry, 0, len(d.EPG)),
	}
	for _, ep := range d.EPG {
		startTS, _ := strconv.ParseInt(ep.StartTimestamp, 10, 64)
		stopTS, _ := strconv.ParseInt(ep.StopTimestamp, 10, 64)
		rec.Programs = append(rec.Programs, EPGEntry{
			ID:             ep.ID,
			CHID:           d.CHID,
			Name:           ep.Name,
			Descr:          ep.Descr,
			StartTimestamp: startTS,
			StopTimestamp:  stopTS,
			StartTime:      ep.StartTime,
			StopTime:       ep.StopTime,
			MarkArchive:    ep.MarkArchive == "1",
			MarkMemo:       ep.MarkMemo == "1",
			MarkRec:        ep.MarkRec == "1",
		})
	}
	rec.FromTS, _ = strconv.ParseInt(tmp.Js.FromTS, 10, 64)
	rec.ToTS, _ = strconv.ParseInt(tmp.Js.ToTS, 10, 64)
	rec.TimeMarks = tmp.Js.TimeMarks
	return rec, nil
}

// GetSimpleDataTable retrieves paginated EPG data for a single channel on a
// given date (format: Y-m-d).
func (p *Portal) GetSimpleDataTable(chID, date string, page int) (map[string]interface{}, error) {
	params := url.Values{}
	params.Set("type", "epg")
	params.Set("action", "get_simple_data_table")
	params.Set("ch_id", chID)
	params.Set("date", date)
	if page > 0 {
		params.Set("p", strconv.Itoa(page))
	}
	params.Set("JsHttpRequest", "1-xml")

	content, err := p.httpRequest(p.Location + "?" + params.Encode())
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, err
	}
	return result, nil
}
