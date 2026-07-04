package stalker

import (
	"path/filepath"
	"testing"

	"github.com/erkexzcx/stalkerhek/db"
)

// Known-good vectors from fix.md's device-identity audit (§1.3 and §1.6),
// verified against a live, authenticated Stalker portal session. These pin
// the derive* functions to the real algorithm rather than just re-deriving
// whatever the code happens to compute.
func TestDeriveIdentityFixMDVectors(t *testing.T) {
	tests := []struct {
		name      string
		mac       string
		sn        string
		deviceID  string
		deviceID2 string
		signature string
	}{
		{
			name:      "fix.md §1.3",
			mac:       "A0:BB:3E:02:3c:b6",
			sn:        "41C865D6CA5F8",
			deviceID:  "0FB2B56128E7A76B9633C050B4B7D0F73D4A91FAD7FEF2FC6190F0AEDC00F5B9",
			deviceID2: "5E25CD4FD75CEB1C3DBB041B962FC512D359F3D75D377DA75926A279DFDD8419",
			signature: "EE2532B623C1103B60AA7DCEE813FE14871C56CDC69818B50ADB43367783F1E0",
		},
		{
			name:      "fix.md §1.6",
			mac:       "00:1A:79:00:29:B4",
			sn:        "A955D3B2115A1",
			deviceID:  "239CB1BB82415493A524845BEEA8A0A9CD410D52AEC47FE9F5BC01A056573267",
			deviceID2: "9AB0C1EE1DD15FFF0250C5A278B6A0F1D17FA62F481B02151C1532E9867F31D2",
			signature: "2DCA33515E35A991A151406F4900A6616B76F881D1E02DBF2F2859A4E5E9DE37",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveSN(tt.mac); got != tt.sn {
				t.Errorf("deriveSN(%q) = %q, want %q", tt.mac, got, tt.sn)
			}
			if got := deriveDeviceID(tt.mac); got != tt.deviceID {
				t.Errorf("deriveDeviceID(%q) = %q, want %q", tt.mac, got, tt.deviceID)
			}
			if got := deriveDeviceID2(tt.sn); got != tt.deviceID2 {
				t.Errorf("deriveDeviceID2(%q) = %q, want %q", tt.sn, got, tt.deviceID2)
			}
			if got := deriveSignature(tt.sn, tt.mac); got != tt.signature {
				t.Errorf("deriveSignature(%q, %q) = %q, want %q", tt.sn, tt.mac, got, tt.signature)
			}
		})
	}
}

// A profile configured with only a MAC (no SerialNumber/DeviceID/DeviceID2/
// Signature) must load successfully via LoadProfile, with all four fields
// auto-derived from the MAC — this is the actual usability win fix.md's
// audit unlocks: no more requiring the operator to first extract identity
// fields from an already-authorized real device.
func TestLoadProfileDerivesIdentityFromMACAlone(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "stalkerhek.db"))
	if err != nil {
		t.Fatal(err)
	}

	const mac = "a0:bb:3e:02:3c:b6" // lowercase on input; LoadProfile normalizes
	if err := store.Save(db.Profile{
		Name: "mac-only",
		Portal: db.PortalConfig{
			MAC:      mac,
			URL:      "http://example.com/c/portal.php",
			TimeZone: "Europe/Vilnius",
		},
	}); err != nil {
		t.Fatal(err)
	}

	c, err := LoadProfile(store, "mac-only")
	if err != nil {
		t.Fatalf("LoadProfile failed for MAC-only profile: %v", err)
	}

	wantSN := deriveSN("A0:BB:3E:02:3C:B6")
	wantDeviceID := deriveDeviceID("A0:BB:3E:02:3C:B6")
	wantDeviceID2 := deriveDeviceID2(wantSN)
	wantSignature := deriveSignature(wantSN, "A0:BB:3E:02:3C:B6")

	if c.Portal.SerialNumber != wantSN {
		t.Errorf("SerialNumber = %q, want %q", c.Portal.SerialNumber, wantSN)
	}
	if c.Portal.DeviceID != wantDeviceID {
		t.Errorf("DeviceID = %q, want %q", c.Portal.DeviceID, wantDeviceID)
	}
	if c.Portal.DeviceID2 != wantDeviceID2 {
		t.Errorf("DeviceID2 = %q, want %q", c.Portal.DeviceID2, wantDeviceID2)
	}
	if c.Portal.Signature != wantSignature {
		t.Errorf("Signature = %q, want %q", c.Portal.Signature, wantSignature)
	}
	if got := c.Portal.signature(); got != wantSignature {
		t.Errorf("signature() = %q, want %q", got, wantSignature)
	}
}
