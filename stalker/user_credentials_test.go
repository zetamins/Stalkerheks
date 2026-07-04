package stalker

import (
	"testing"
)

func TestUserCredentialsValidation(t *testing.T) {
	// User-provided credentials:
	// mac: -:02:3c:b6 (let's try with prefix A0:BB:3E to form A0:BB:3E:02:3C:B6)
	// sn: 41C865D6CA5F8
	// devid1: 0FB2B56128E7A76B9633C050B4B7D0F73D4A91FAD7FEF2FC6190F0AEDC00F5B9
	// devid2: 5E25CD4FD75CEB1C3DBB041B962FC512D359F3D75D377DA75926A279DFDD8419
	// sig: EE2532B623C1103B60AA7DCEE813FE14871C56CDC69818B50ADB43367783F1E0

	p := &Portal{
		Model:        "MAG254",
		SerialNumber: "41C865D6CA5F8",
		DeviceID:     "0FB2B56128E7A76B9633C050B4B7D0F73D4A91FAD7FEF2FC6190F0AEDC00F5B9",
		DeviceID2:    "5E25CD4FD75CEB1C3DBB041B962FC512D359F3D75D377DA75926A279DFDD8419",
		Signature:    "EE2532B623C1103B60AA7DCEE813FE14871C56CDC69818B50ADB43367783F1E0",
		MAC:          "A0:BB:3E:02:3C:B6",
		Location:     "http://example.com/c/portal.php",
		TimeZone:     "Europe/Vilnius",
	}

	c := &Config{
		Portal: p,
		HLS: Service{
			Enabled: true,
			Bind:    "0.0.0.0:9999",
		},
		Proxy: Service{
			Enabled: true,
			Bind:    "0.0.0.0:8888",
		},
	}

	err := c.validate()
	if err != nil {
		t.Errorf("Validation failed for user credentials: %v", err)
	}

	// Double check signature resolution uses the explicit signature we configured
	resolvedSig := p.signature()
	if resolvedSig != "EE2532B623C1103B60AA7DCEE813FE14871C56CDC69818B50ADB43367783F1E0" {
		t.Errorf("Expected signature %s, got %s", p.Signature, resolvedSig)
	}

	// Ensure uppercase normalizations
	if p.MAC != "A0:BB:3E:02:3C:B6" {
		t.Errorf("MAC should be normalized to uppercase, got %s", p.MAC)
	}
}

func TestUserCredentialsWithAlternativeMAC(t *testing.T) {
	// Let's also verify if the user meant 00:1A:79 prefix for the MAC:
	p := &Portal{
		Model:        "MAG254",
		SerialNumber: "41C865D6CA5F8",
		DeviceID:     "0FB2B56128E7A76B9633C050B4B7D0F73D4A91FAD7FEF2FC6190F0AEDC00F5B9",
		DeviceID2:    "5E25CD4FD75CEB1C3DBB041B962FC512D359F3D75D377DA75926A279DFDD8419",
		Signature:    "EE2532B623C1103B60AA7DCEE813FE14871C56CDC69818B50ADB43367783F1E0",
		MAC:          "00:1A:79:02:3C:B6",
		Location:     "http://example.com/c/portal.php",
		TimeZone:     "Europe/Vilnius",
	}

	c := &Config{
		Portal: p,
		HLS: Service{
			Enabled: true,
			Bind:    "0.0.0.0:9999",
		},
		Proxy: Service{
			Enabled: true,
			Bind:    "0.0.0.0:8888",
		},
	}

	err := c.validate()
	if err != nil {
		t.Errorf("Validation failed for user credentials with Infomir MAC: %v", err)
	}

	if p.MAC != "00:1A:79:02:3C:B6" {
		t.Errorf("MAC normalization failed, got %s", p.MAC)
	}
}
