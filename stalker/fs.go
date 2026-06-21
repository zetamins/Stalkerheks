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
	Model        string
	SerialNumber string
	DeviceID     string
	DeviceID2    string
	Signature    string
	MAC          string
	Location     string
	TimeZone     string
	Token        string
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
			Location:     p.Portal.URL,
			TimeZone:     p.Portal.TimeZone,
			Token:        p.Portal.Token,
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

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	c.Portal.MAC = strings.ToUpper(c.Portal.MAC)

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
