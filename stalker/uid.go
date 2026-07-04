package stalker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// GetUID emulates the MAG STB's native GetUID() function. On real hardware
// this reads a Hardware Unique Key out of a Trustonic TEE secure element —
// burned in at the factory, never readable even by secure-world code — and
// derives values from it via a KDF. We can't read real silicon's secret, so
// Portal.UIDSecret is software's stand-in for it: a persistent random root
// secret generated once per profile (LoadProfile/db.Save), playing the same
// role a HUK would. This is sound emulation rather than a guess at Infomir's
// real algorithm (which was never recovered from any resource checked, and
// likely never could be — it's hardware-secured): the portal/CDN have no
// cryptographic attestation chain back to real silicon either way, so all
// that actually matters is that values derived from one root secret are
// internally consistent the way a real device's would be.
//
// Uses HMAC-SHA256 rather than a bare hash-of-concatenation: real TEE key
// hierarchies derive descendant keys from a root key via an HMAC-based KDF
// specifically for the domain-separation guarantees bare concatenation
// hashing lacks (e.g. GetUID("a", "bc") must never collide with
// GetUID("ab", "c")).
//
// Known real call patterns (from the JS API docs and Itv.php usage):
//
//	GetUID()                  -> device_id
//	GetUID(random)             -> signature  (keyed by the handshake's random)
//	GetUID("device_id", token) -> device_id2 (keyed by token)
//
// device_id/device_id2/signature are now primarily resolved via the verified
// MAC-based derivation instead (identity.go, LoadProfile, Portal.signature)
// — this HMAC scheme only remains as a fallback for a Portal built without a
// SerialNumber/MAC to derive from.
func (p *Portal) GetUID(args ...string) string {
	mac := hmac.New(sha256.New, []byte(p.UIDSecret))
	for _, a := range args {
		mac.Write([]byte(a))
		mac.Write([]byte{0}) // separator: prevents ("ab","c") colliding with ("a","bc")
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// signature resolves the get_profile "signature" param: a manually
// configured override if set; otherwise the verified real-portal derivation
// (SHA256(sn+mac), see identity.go/fix.md) once sn and MAC are known — which
// LoadProfile fills in eagerly, so this branch mainly matters for a Portal
// built outside LoadProfile; otherwise GetUID(random) as a last-resort
// emulation for the case sn/MAC aren't available at all.
func (p *Portal) signature() string {
	if p.Signature != "" {
		return p.Signature
	}
	if p.SerialNumber != "" && p.MAC != "" {
		return deriveSignature(p.SerialNumber, p.MAC)
	}
	if p.UIDSecret == "" {
		return ""
	}
	return p.GetUID(p.Random)
}
