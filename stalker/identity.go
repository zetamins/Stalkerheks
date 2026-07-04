package stalker

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// The functions below emulate how a real MAG STB derives its device-identity
// fields from nothing but its own MAC address — no hardware secret involved.
// Verified against a live, authenticated Stalker portal session across
// multiple MACs (see fix.md's device-identity audit): computing these from
// only the account's MAC reproduced the exact sn/device_id/device_id2/
// signature values the portal already had on record for that device.
//
// All four operate on the MAC in the same case Stalkerhek uses everywhere
// else in the protocol (uppercase, per Config.validate/regexMAC) — the
// portal treats these as opaque strings, so the derivation must be computed
// from the exact same MAC string that's sent in every other request.

// deriveSN computes the STB serial number: the first 13 hex characters of
// MD5(mac), uppercased.
func deriveSN(mac string) string {
	sum := md5.Sum([]byte(mac))
	return strings.ToUpper(hex.EncodeToString(sum[:])[:13])
}

// deriveDeviceID computes the device_id parameter: SHA256(mac), uppercased.
func deriveDeviceID(mac string) string {
	sum := sha256.Sum256([]byte(mac))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// deriveDeviceID2 computes the device_id2 parameter: SHA256(sn), uppercased
// — keyed by the derived serial number (deriveSN's output), not the raw MAC.
func deriveDeviceID2(sn string) string {
	sum := sha256.Sum256([]byte(sn))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// deriveSignature computes the signature parameter: SHA256(sn+mac),
// uppercased. Unlike HWVersion2/Prehash (hash.go), which are genuinely keyed
// by the handshake's per-session random nonce, this is a static function of
// sn+mac — confirmed identical across repeated handshakes in the audit.
func deriveSignature(sn, mac string) string {
	sum := sha256.Sum256([]byte(sn + mac))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
