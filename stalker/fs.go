package stalker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/ioutil"
	"log"
	"math/rand"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"
)

// Config contains configuration taken from the YAML file.
type Config struct {
	Portal *Portal `yaml:"portal"`
	HLS    struct {
		Enabled bool   `yaml:"enabled"`
		Bind    string `yaml:"bind"`
	} `yaml:"hls"`
	Proxy struct {
		Enabled bool   `yaml:"enabled"`
		Bind    string `yaml:"bind"`
	} `yaml:"proxy"`
	Dashboard struct {
		Enabled    bool   `yaml:"enabled"`
		Bind       string `yaml:"bind"`
		BinaryPath string `yaml:"binary"`
		ProfilesDir string `yaml:"profiles_dir"`
	} `yaml:"dashboard"`
}

// Portal represents Stalker portal
type Portal struct {
	Model        string `yaml:"model"`
	SerialNumber string `yaml:"serial_number"`
	DeviceID     string `yaml:"device_id"`
	DeviceID2    string `yaml:"device_id2"`
	Signature    string `yaml:"signature"`
	MAC          string `yaml:"mac"`
	Location     string `yaml:"url"`
	TimeZone     string `yaml:"time_zone"`
	Token        string `yaml:"token"`
}

// deriveDeviceIDs auto-generates device_id2 and signature from device_id
// using SHA-256, emulating the MAG GetUID() function. If values are already
// provided in config, they are left unchanged.
func (p *Portal) deriveDeviceIDs() {
	h := sha256.New()
	h.Write([]byte(p.DeviceID))

	// device_id2 = SHA256(device_id + ":device_id:" + token)
	if p.DeviceID2 == "" {
		h2 := sha256.New()
		h2.Write([]byte(p.DeviceID + ":device_id:" + p.Token))
		p.DeviceID2 = hex.EncodeToString(h2.Sum(nil))
		log.Println("Auto-generated device_id2 from device_id")
	}

	// signature = SHA256(device_id + ":signature")
	if p.Signature == "" {
		h3 := sha256.New()
		h3.Write([]byte(p.DeviceID + ":signature"))
		p.Signature = hex.EncodeToString(h3.Sum(nil))
		log.Println("Auto-generated signature from device_id")
	}

	_ = h // keep import
}

// ReadConfig returns configuration from the file in Portal object.
// If device_id2 and signature are empty, they are auto-generated from device_id
// using SHA-256 — emulating the MAG GetUID() hardware key derivation.
func ReadConfig(path *string) (*Config, error) {
	content, err := ioutil.ReadFile(*path)
	if err != nil {
		return nil, err
	}

	var c *Config
	err = yaml.Unmarshal(content, &c)
	if err != nil {
		return nil, err
	}

	if err = c.validateWithDefaults(); err != nil {
		return nil, err
	}

	// Auto-generate device_id2 and signature if only device_id is provided.
	// This emulates GetUID() derivation from hardware-bound secret on real MAG boxes.
	c.Portal.deriveDeviceIDs()

	return c, nil
}

var regexMAC = regexp.MustCompile(`^[A-F0-9]{2}:[A-F0-9]{2}:[A-F0-9]{2}:[A-F0-9]{2}:[A-F0-9]{2}:[A-F0-9]{2}$`)
var regexTimezone = regexp.MustCompile(`^[a-zA-Z]+/[a-zA-Z]+$`)

func (c *Config) validateWithDefaults() error {
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

	// Signature can be empty and it's fine

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

	if c.Portal.Token == "" {
		c.Portal.Token = randomToken()
		log.Println("No token given, using random one:", c.Portal.Token)
	}

	return nil
}

func randomToken() string {
	allowlist := []rune("ABCDEF0123456789")
	b := make([]rune, 32)
	for i := range b {
		b[i] = allowlist[rand.Intn(len(allowlist))]
	}
	return string(b)
}
