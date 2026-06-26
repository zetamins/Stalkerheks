package stalker

import (
	"errors"
	"regexp"
	"strings"

	"github.com/erkexzcx/stalkerhek/db"
)

// Config contains the runtime configuration for stalkerhek.
type Config struct {
	Portal    *Portal
	HLS       Service
	Proxy     Service
	Dashboard Dashboard
}

// Service holds service enable/bind settings.
type Service struct {
	Enabled bool
	Bind    string
}

// Dashboard holds dashboard settings.
type Dashboard struct {
	Enabled bool
	Bind    string
}

// Portal represents a Stalker portal connection.
type Portal struct {
	Model           string
	SerialNumber    string
	DeviceID        string
	DeviceID2       string
	Signature       string
	MAC             string
	CDNMac          string // mac= embedded in CDN/stream play URLs; bypasses per-MAC 458 anti-sharing. Falls back to MAC when empty. See Portal.cdnMAC.
	Location        string
	Location2       string // fallback portal URL; tried if Location is unreachable at Start(), mirroring real STBs' portal1/portal2 failover
	TimeZone        string
	Token           string
	Random          string // value returned by the portal's handshake response, used as input to GetHashVersion1-derived fields like hw_version_2
	WatchdogTimeout int    // seconds, from get_profile's response; real STBs use this (not a hardcoded value) for the heartbeat interval

	// UIDSecret is the software stand-in for the Hardware Unique Key a real
	// STB's Trustonic TEE would hold — the root secret GetUID() derives
	// device_id2/signature from. See GetUID in stalker/uid.go.
	UIDSecret string

	// IsPlayingFunc reports whether this device is currently relaying a
	// stream to a viewer. Real STBs send the watchdog's cur_play_type as 0
	// while idle and a nonzero place code only while actually playing; this
	// hook lets a caller (e.g. the hls package) supply that signal without
	// stalker importing hls. Left nil, watchdog reports idle (0) always.
	IsPlayingFunc func() bool

	watchdogStop chan struct{} // closed by StopWatchdog to end the periodic goroutine
}

// Content type constants for create_link and retrieval APIs.
const (
	ContentTypeITV       = "itv"
	ContentTypeRadio     = "radio"
	ContentTypeVOD       = "vod"
	ContentTypeKaraoke   = "karaoke"
	ContentTypeTVArchive = "tv_archive"
)

// RadioChannel represents a radio station in the Stalker portal.
type RadioChannel struct {
	Title    string
	CMD      string // channel's identifier for create_link
	Portal   *Portal
}

// VODItem represents a Video-on-Demand catalog entry.
type VODItem struct {
	ID         string
	Name       string
	CMD        string // identifier for create_link
	CategoryID string
	Year       string
	Director   string
	Screenshot string
	GenresStr  string
	Rating     string
	Time       string // duration
	IsMovie    string // "1" if movie, "0" if series
	SeasonID   string
	EpisodeID  string
	Portal     *Portal
}

// VODCategory represents a VOD category from the portal.
type VODCategory struct {
	ID    string
	Title string
	Alias string
}

// EPGEntry represents a single program in the EPG guide.
type EPGEntry struct {
	ID             string
	CHID           string
	Name           string
	Descr          string
	StartTimestamp int64
	StopTimestamp  int64
	StartTime      string // display time string
	StopTime       string // display time string
	MarkArchive    bool
	MarkMemo       bool
	MarkRec        bool
}

// EPGRecord represents EPG data for a channel within a time window.
type EPGRecord struct {
	CHID      string
	CHName    string
	CHType    string
	Programs  []EPGEntry
	FromTS    int64
	ToTS      int64
	TimeMarks []string
}

var regexMAC = regexp.MustCompile(`^[A-F0-9]{2}:[A-F0-9]{2}:[A-F0-9]{2}:[A-F0-9]{2}:[A-F0-9]{2}:[A-F0-9]{2}$`)
var regexTimezone = regexp.MustCompile(`^[a-zA-Z]+/[a-zA-Z]+$`)

// LoadProfile loads a named profile from the database and returns a Config.
func LoadProfile(store *db.Store, name string) (*Config, error) {
	p, ok := store.Get(name)
	if !ok {
		return nil, errors.New("profile not found: " + name)
	}

	c := &Config{
		Portal: &Portal{
			Model:        p.Portal.Model,
			SerialNumber: p.Portal.SerialNumber,
			DeviceID:     p.Portal.DeviceID,
			DeviceID2:    p.Portal.DeviceID2,
			Signature:    p.Portal.Signature,
			MAC:          p.Portal.MAC,
			CDNMac:       p.Portal.CDNMac,
			Location:     p.Portal.URL,
			Location2:    p.Portal.URL2,
			TimeZone:     p.Portal.TimeZone,
			Token:        p.Portal.Token,
			UIDSecret:    p.Portal.UIDSecret,
		},
		HLS: Service{
			Enabled: true,
			Bind:    p.Services.HLSBind,
		},
		Proxy: Service{
			Enabled: true,
			Bind:    p.Services.ProxyBind,
		},
		Dashboard: Dashboard{
			Enabled: p.Dashboard.Bind != "",
			Bind:    p.Dashboard.Bind,
		},
	}

	// device_id2 is keyed only by device_id+token (both already known), so
	// it can be resolved once here rather than persisted — unlike signature
	// (keyed by the handshake's random, which doesn't exist yet), it's safe
	// to fill in eagerly. A manually-configured value (e.g. matching an
	// already-authorized real device) is left untouched. This must run
	// before validate() below, since validate() requires DeviceID2 to be
	// non-empty — every freshly auto-generated profile has it empty until
	// derived here.
	if c.Portal.DeviceID2 == "" && c.Portal.DeviceID != "" && c.Portal.UIDSecret != "" {
		c.Portal.DeviceID2 = c.Portal.GetUID("device_id", c.Portal.Token)
	}

	// Backfill cdn_mac for profiles created before this field existed (the
	// common case on the Android app and any pre-existing install). Save()
	// auto-generates it for new profiles, but profiles already on disk load
	// with an empty value — and an empty cdn_mac falls back to the flagged
	// auth MAC, which 458s on playback. Generate one and persist it so the
	// anti-sharing bypass works automatically, without re-creating profiles.
	if c.Portal.CDNMac == "" {
		c.Portal.CDNMac = db.RandomCDNMac(c.Portal.MAC)
		p.Portal.CDNMac = c.Portal.CDNMac
		_ = store.Save(p) // best-effort persist; the in-memory value works regardless
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Config) validate() error {
	c.Portal.MAC = strings.ToUpper(c.Portal.MAC)
	if c.Portal.CDNMac != "" {
		c.Portal.CDNMac = strings.ToUpper(c.Portal.CDNMac)
		if !regexMAC.MatchString(c.Portal.CDNMac) {
			return errors.New("invalid CDN MAC '" + c.Portal.CDNMac + "'")
		}
	}

	if c.Portal.Model == "" {
		return errors.New("empty model")
	}
	if c.Portal.SerialNumber == "" {
		return errors.New("empty serial number (sn)")
	}
	if c.Portal.DeviceID == "" {
		return errors.New("empty device_id")
	}
	if c.Portal.DeviceID2 == "" {
		return errors.New("empty device_id2")
	}
	if !regexMAC.MatchString(c.Portal.MAC) {
		return errors.New("invalid MAC '" + c.Portal.MAC + "'")
	}
	if c.Portal.Location == "" {
		return errors.New("empty portal url")
	}
	if !regexTimezone.MatchString(c.Portal.TimeZone) {
		return errors.New("invalid timezone '" + c.Portal.TimeZone + "'")
	}
	if !c.HLS.Enabled && !c.Proxy.Enabled {
		return errors.New("no services enabled")
	}
	if c.HLS.Enabled && c.HLS.Bind == "" {
		return errors.New("empty HLS bind")
	}
	if c.Proxy.Enabled && c.Proxy.Bind == "" {
		return errors.New("empty proxy bind")
	}
	if c.Proxy.Enabled && !c.HLS.Enabled {
		return errors.New("HLS service must be enabled when proxy is enabled")
	}
	return nil
}
