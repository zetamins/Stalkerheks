package stalker

import (
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Channel stores information about channel in Stalker portal. This is not a real TV channel representation, but details on how to retrieve a working channel's URL.
type Channel struct {
	Title    string             // Used for Proxy service to generate fake response to new URL request
	CMD      string             // channel's identifier in Stalker portal
	LogoLink string             // Link to logo
	Portal   *Portal            // Reference to portal from where this channel is taken from
	GenreID  string             // Stores genre ID (category ID)
	Genres   *map[string]string // Stores mappings for genre ID -> genre title

	CMD_ID    string // Used for Proxy service to generate fake response to new URL request
	CMD_CH_ID string // Used for Proxy service to generate fake response to new URL request
}

// createLinkMaxAttempts caps retries for transient create_link errors.
const createLinkMaxAttempts = 3

// createLinkRetryDelay is the wait between transient create_link retries.
const createLinkRetryDelay = 1 * time.Second

// NewLink retrieves a link to the working channel. Retrieved link can be
// played in VLC or Kodi, but expires very soon if not being constantly
// opened (used). When retry is true, the real server's two transient error
// codes ("limit", "temporary_unavailable" — confirmed against Itv.php's
// createLink) are retried a few times before giving up; the two fatal codes
// ("nothing_to_play", "link_fault") are never retried.
func (c *Channel) NewLink(retry bool) (string, error) {
	attempts := 1
	if retry {
		attempts = createLinkMaxAttempts
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		link, err := c.newLinkOnce()
		if err == nil {
			return link, nil
		}
		lastErr = err
		if !retry || !isTransientCreateLinkError(err) || attempt == attempts {
			break
		}
		log.Println("create_link transient failure, retrying:", err)
		time.Sleep(createLinkRetryDelay)
	}
	return "", lastErr
}

func isTransientCreateLinkError(err error) bool {
	// Transient upstream/CDN hiccups — the portal returning HTTP 5xx (500,
	// 502, 503, 520, 522, …) clears on retry, like a real STB re-issuing
	// create_link a moment later. 4xx (auth, not-found) are fatal.
	var se *httpStatusError
	if errors.As(err, &se) {
		return se.code >= 500
	}
	// Real STB treats "limit" as FATAL (shows notice, does not retry).
	// Only "temporary_unavailable" is retried on create_link level.
	return strings.Contains(err.Error(), "temporary_unavailable")
}

func (c *Channel) newLinkOnce() (string, error) {
	type tmpStruct struct {
		Js struct {
			Cmd   string `json:"cmd"`
			Error string `json:"error"`
		} `json:"js"`
	}
	var tmp tmpStruct

	link := c.Portal.Location + "?action=create_link&type=itv&cmd=" + url.PathEscape(c.CMD) + "&series=&forced_storage=&disable_ad=0&download=0&JsHttpRequest=1-xml"
	content, err := c.Portal.httpRequest(link)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(content, &tmp); err != nil {
		log.Println("Failed to retrieve new link...")
		return "", err
	}

	// The real server's create_link can fail in two distinct shapes: a
	// normal response with a non-empty "error" field (e.g. "limit",
	// "nothing_to_play"), or — when ad campaigns are configured for the
	// itv placement — an entirely different ad-playlist array shape that
	// silently decodes to an empty Cmd here. Either way, an empty Cmd is
	// never a valid playable link, so surface it as an error rather than
	// returning "" as if it were a successful, working link.
	if tmp.Js.Error != "" {
		return "", errors.New("create_link failed: " + tmp.Js.Error)
	}
	if tmp.Js.Cmd == "" {
		return "", errors.New("create_link returned an empty command (channel unavailable or ad-playlist response)")
	}

	strs := strings.Split(tmp.Js.Cmd, " ")
	link = strs[len(strs)-1]
	link = rewriteMACParam(link, c.Portal.cdnMAC())
	return link, nil
}

// Logo returns full link to channel's logo
func (c *Channel) Logo() string {
	if c.LogoLink == "" {
		return ""
	}
	return strings.TrimRight(c.Portal.Location, "/") + "/misc/logos/320/" + c.LogoLink
}

// Genre returns a genre title
func (c *Channel) Genre() string {
	g, ok := (*c.Genres)[c.GenreID]
	if !ok {
		g = "Other"
	}
	return strings.Title(g)
}

// RetrieveChannels retrieves all TV channels from stalker portal.
func (p *Portal) RetrieveChannels() (map[string]*Channel, error) {
	type tmpStruct struct {
		Js struct {
			Data []struct {
				ID      string `json:"id"`          // Channel ID — stable and unique, unlike Name (some portals list multiple channels under the same name)
				Name    string `json:"name"`        // Title of channel
				Cmd     string `json:"cmd"`         // Some sort of URL used to request channel real URL
				Logo    string `json:"logo"`        // Link to logo
				GenreID string `json:"tv_genre_id"` // Genre ID
				CMDs    []struct {
					ID    string `json:"id"`    // Used for Proxy service to generate fake response to new URL request
					CH_ID string `json:"ch_id"` // Used for Proxy service to generate fake response to new URL request
				} `json:"cmds"`
			} `json:"data"`
		} `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?type=itv&action=get_all_channels&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}

	// Dump json output to file
	//ioutil.WriteFile("/tmp/dumpedchannels.json", content, 0644)

	if err := json.Unmarshal(content, &tmp); err != nil {
		log.Fatalln(string(content))
	}

	genres, err := p.getGenres()
	if err != nil {
		return nil, err
	}

	// Build channels list and return. Keyed by title since that's what
	// every URL (/iptv/<title>) and the dashboard/JNI APIs use as the
	// channel identity — but title alone isn't guaranteed unique (some
	// portals list multiple distinct channels under the same name; ~7% of
	// channels collided on one real operator's list), so a colliding name
	// is disambiguated with the channel's actual ID rather than silently
	// losing every same-named channel but the last one to a map-key
	// collision, with the others becoming permanently unreachable.
	channels := make(map[string]*Channel, len(tmp.Js.Data))
	for _, v := range tmp.Js.Data {
		title := v.Name
		if _, exists := channels[title]; exists {
			title = v.Name + " (" + v.ID + ")"
		}
		// Not every channel carries a "cmds" entry (disabled/placeholder
		// channels on some portals list an empty array) — indexing [0]
		// unconditionally panicked and took down the whole channel load.
		var cmdID, cmdCHID string
		if len(v.CMDs) > 0 {
			cmdID = v.CMDs[0].ID
			cmdCHID = v.CMDs[0].CH_ID
		}
		channels[title] = &Channel{
			Title:     title,
			CMD:       v.Cmd,
			LogoLink:  v.Logo,
			Portal:    p,
			GenreID:   v.GenreID,
			Genres:    &genres,
			CMD_ID:    cmdID,
			CMD_CH_ID: cmdCHID,
		}
	}

	return channels, nil
}

func (p *Portal) getGenres() (map[string]string, error) {
	type tmpStruct struct {
		Js []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?action=get_genres&type=itv&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(content, &tmp); err != nil {
		log.Fatalln(string(content))
	}

	genres := make(map[string]string, len(tmp.Js))
	for _, el := range tmp.Js {
		genres[el.ID] = el.Title
	}

	return genres, nil
}

// ####################################################
// Radio Channels

// RetrieveRadioChannels retrieves all radio channels from the portal.
func (p *Portal) RetrieveRadioChannels() (map[string]*RadioChannel, error) {
	type tmpStruct struct {
		Js struct {
			Data []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Cmd    string `json:"cmd"`
				Number string `json:"number"`
			} `json:"data"`
		} `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?type=radio&action=get_ordered_list&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(content, &tmp); err != nil {
		log.Println("Failed to parse radio channel list:", err)
		return nil, err
	}

	channels := make(map[string]*RadioChannel, len(tmp.Js.Data))
	for _, v := range tmp.Js.Data {
		title := v.Name
		if _, exists := channels[title]; exists {
			title = v.Name + " (" + v.ID + ")"
		}
		channels[title] = &RadioChannel{
			Title:  title,
			CMD:    v.Cmd,
			Portal: p,
		}
	}
	return channels, nil
}

// NewLink retrieves a playable URL for a radio channel.
func (c *RadioChannel) NewLink(retry bool) (string, error) {
	attempts := 1
	if retry {
		attempts = createLinkMaxAttempts
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		link, err := c.newLinkOnce()
		if err == nil {
			return link, nil
		}
		lastErr = err
		if !retry || !isTransientCreateLinkError(err) || attempt == attempts {
			break
		}
		log.Println("radio create_link transient failure, retrying:", err)
		time.Sleep(createLinkRetryDelay)
	}
	return "", lastErr
}

func (c *RadioChannel) newLinkOnce() (string, error) {
	type tmpStruct struct {
		Js struct {
			Cmd   string `json:"cmd"`
			Error string `json:"error"`
		} `json:"js"`
	}
	var tmp tmpStruct

	link := c.Portal.Location + "?action=create_link&type=radio&cmd=" + url.PathEscape(c.CMD) + "&series=&forced_storage=&disable_ad=0&download=0&JsHttpRequest=1-xml"
	content, err := c.Portal.httpRequest(link)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(content, &tmp); err != nil {
		return "", err
	}

	if tmp.Js.Error != "" {
		return "", errors.New("radio create_link failed: " + tmp.Js.Error)
	}
	if tmp.Js.Cmd == "" {
		return "", errors.New("radio create_link returned empty command")
	}

	strs := strings.Split(tmp.Js.Cmd, " ")
	link = strs[len(strs)-1]
	link = rewriteMACParam(link, c.Portal.cdnMAC())
	return link, nil
}

// ####################################################
// Video on Demand (VOD)

// GetVODCategories retrieves the VOD category list from the portal.
func (p *Portal) GetVODCategories() ([]VODCategory, error) {
	type tmpStruct struct {
		Js []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Alias string `json:"alias"`
		} `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?type=vod&action=get_categories&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return nil, err
	}

	cats := make([]VODCategory, 0, len(tmp.Js))
	for _, v := range tmp.Js {
		cats = append(cats, VODCategory{ID: v.ID, Title: v.Title, Alias: v.Alias})
	}
	return cats, nil
}

// GetVODOrderedList retrieves paginated VOD items optionally filtered by category.
func (p *Portal) GetVODOrderedList(categoryID, sortBy string, page int) ([]VODItem, error) {
	params := url.Values{}
	params.Set("type", "vod")
	params.Set("action", "get_ordered_list")
	if categoryID != "" {
		params.Set("category", categoryID)
	}
	if sortBy != "" {
		params.Set("sortby", sortBy)
	}
	if page > 0 {
		params.Set("p", strconv.Itoa(page))
	}
	params.Set("JsHttpRequest", "1-xml")

	type tmpStruct struct {
		Js struct {
			Data []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				Cmd        string `json:"cmd"`
				CategoryID string `json:"category_id"`
				Year       string `json:"year"`
				Director   string `json:"director"`
				Screenshot string `json:"screenshot_uri"`
				GenresStr  string `json:"genres_str"`
				Rating     string `json:"rating_kinopoisk"`
				Time       string `json:"time"`
				IsMovie    string `json:"is_movie"`
				SeasonID   string `json:"season_id"`
				EpisodeID  string `json:"episode_id"`
			} `json:"data"`
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

	items := make([]VODItem, 0, len(tmp.Js.Data))
	for _, v := range tmp.Js.Data {
		items = append(items, VODItem{
			ID: v.ID, Name: v.Name, CMD: v.Cmd,
			CategoryID: v.CategoryID, Year: v.Year, Director: v.Director,
			Screenshot: v.Screenshot, GenresStr: v.GenresStr,
			Rating: v.Rating, Time: v.Time, IsMovie: v.IsMovie,
			SeasonID: v.SeasonID, EpisodeID: v.EpisodeID, Portal: p,
		})
	}
	return items, nil
}

// NewVODLink retrieves a playable URL for a VOD item.
func (p *Portal) NewVODLink(cmd, series, forcedStorage string) (string, error) {
	type tmpStruct struct {
		Js struct {
			Cmd   string `json:"cmd"`
			Error string `json:"error"`
		} `json:"js"`
	}
	var tmp tmpStruct

	link := p.Location + "?action=create_link&type=vod&cmd=" + url.PathEscape(cmd)
	if series != "" {
		link += "&series=" + url.PathEscape(series)
	} else {
		link += "&series="
	}
	if forcedStorage != "" {
		link += "&forced_storage=" + url.PathEscape(forcedStorage)
	} else {
		link += "&forced_storage="
	}
	link += "&disable_ad=0&download=0&JsHttpRequest=1-xml"

	content, err := p.httpRequest(link)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return "", err
	}
	if tmp.Js.Error != "" {
		return "", errors.New("vod create_link failed: " + tmp.Js.Error)
	}
	if tmp.Js.Cmd == "" {
		return "", errors.New("vod create_link returned empty command")
	}
	strs := strings.Split(tmp.Js.Cmd, " ")
	link = strs[len(strs)-1]
	link = rewriteMACParam(link, p.cdnMAC())
	return link, nil
}

// ####################################################
// Karaoke

// RetrieveKaraokeList retrieves karaoke items from the portal.
func (p *Portal) RetrieveKaraokeList() (map[string]*Channel, error) {
	type tmpStruct struct {
		Js struct {
			Data []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Cmd  string `json:"cmd"`
			} `json:"data"`
		} `json:"js"`
	}
	var tmp tmpStruct

	content, err := p.httpRequest(p.Location + "?type=karaoke&action=get_ordered_list&JsHttpRequest=1-xml")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return nil, err
	}

	items := make(map[string]*Channel, len(tmp.Js.Data))
	for _, v := range tmp.Js.Data {
		title := v.Name
		if _, exists := items[title]; exists {
			title = v.Name + " (" + v.ID + ")"
		}
		items[title] = &Channel{
			Title:  title,
			CMD:    v.Cmd,
			Portal: p,
		}
	}
	return items, nil
}

// NewKaraokeLink retrieves a playable URL for a karaoke item.
func (p *Portal) NewKaraokeLink(cmd string) (string, error) {
	type tmpStruct struct {
		Js struct {
			Cmd   string `json:"cmd"`
			Error string `json:"error"`
		} `json:"js"`
	}
	var tmp tmpStruct

	link := p.Location + "?action=create_link&type=karaoke&cmd=" + url.PathEscape(cmd) + "&series=&forced_storage=&disable_ad=0&download=0&JsHttpRequest=1-xml"
	content, err := p.httpRequest(link)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(content, &tmp); err != nil {
		return "", err
	}
	if tmp.Js.Error != "" {
		return "", errors.New("karaoke create_link failed: " + tmp.Js.Error)
	}
	if tmp.Js.Cmd == "" {
		return "", errors.New("karaoke create_link returned empty command")
	}
	strs := strings.Split(tmp.Js.Cmd, " ")
	link = strs[len(strs)-1]
	link = rewriteMACParam(link, p.cdnMAC())
	return link, nil
}

// cdnMAC returns the MAC to embed in CDN/stream play URLs (the mac= query
// param). The portal flags the account's real MAC for anti-sharing and returns
// HTTP 458 on stream requests carrying it, but the play_token isn't bound to
// the MAC — so a configured alternate (CDNMac) gets the same stream through.
// Falls back to the auth MAC when unset, preserving the original behavior.
func (p *Portal) cdnMAC() string {
	if p.CDNMac != "" {
		return p.CDNMac
	}
	return p.MAC
}

// rewriteMACParam replaces the mac= query parameter in a URL with the
// configured device MAC. The real portal embeds its own registered MAC
// in stream URLs; the CDN checks this against the streaming session.
func rewriteMACParam(link, newMAC string) string {
	if newMAC == "" || !strings.Contains(link, "mac=") {
		return link
	}
	// Find mac= parameter and replace its value
	idx := strings.Index(link, "mac=")
	if idx < 0 {
		return link
	}
	start := idx + 4
	end := strings.IndexAny(link[start:], "& ")
	if end < 0 {
		// mac= is the last parameter
		return link[:start] + newMAC
	}
	return link[:start] + newMAC + link[start+end:]
}
