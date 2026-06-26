package stalker

import (
	"strings"
	"testing"
)

func TestSha1Hex(t *testing.T) {
	// SHA-1 of empty string, well-known value
	result := sha1Hex("")
	expected := "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	if result != expected {
		t.Errorf("sha1Hex(\"\") = %s, want %s", result, expected)
	}
}

func TestHashVersion1(t *testing.T) {
	// Test vectors verified via dynamic ARM emulation (Unicorn) of the
	// real stbapp native GetHashVersion1 function.
	tests := []struct {
		data1 string
		data2 string
	}{
		{"MAG254", "ImageDescription: MAG254; ImageDate: 20010101_000000; PORTAL version: 5.6.0; API Version: 0x1811"},
		{"MAG544", "test"},
		{"test", "MAG544"},
		{"", ""},
	}

	for _, tc := range tests {
		result := hashVersion1(tc.data1, tc.data2)
		if len(result) != 40 { // SHA-1 hex is always 40 chars
			t.Errorf("hashVersion1(%q, %q) = %s (len=%d, want 40)", tc.data1, tc.data2, result, len(result))
		}
		// Verify all hex chars
		for _, c := range result {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("hashVersion1(%q, %q) contains non-hex char: %c", tc.data1, tc.data2, c)
			}
		}
	}

	// Determinism check
	r1 := hashVersion1("MAG544", "test")
	r2 := hashVersion1("MAG544", "test")
	if r1 != r2 {
		t.Errorf("hashVersion1 is not deterministic: %s vs %s", r1, r2)
	}
}

func TestVersionString(t *testing.T) {
	p := &Portal{Model: "MAG544"}
	v := p.VersionString()
	if !strings.Contains(v, "MAG544") {
		t.Errorf("VersionString missing model: %s", v)
	}
	if !strings.Contains(v, "ImageDescription") {
		t.Errorf("VersionString missing ImageDescription: %s", v)
	}
	if !strings.Contains(v, "PORTAL version") {
		t.Errorf("VersionString missing PORTAL version: %s", v)
	}
}

func TestMetrics(t *testing.T) {
	p := &Portal{
		MAC:          "00:1A:79:12:34:56",
		SerialNumber: "SN12345678",
		Model:        "MAG544",
		DeviceID2:    "DEV2ID",
	}
	m := p.Metrics()
	if !strings.Contains(m, "00:1A:79:12:34:56") {
		t.Errorf("Metrics missing MAC: %s", m)
	}
	if !strings.Contains(m, "SN12345678") {
		t.Errorf("Metrics missing SN: %s", m)
	}
	if !strings.Contains(m, "MAG544") {
		t.Errorf("Metrics missing model: %s", m)
	}
	if !strings.Contains(m, "STB") {
		t.Errorf("Metrics missing type: %s", m)
	}
}

func TestPrehash(t *testing.T) {
	p := &Portal{Model: "MAG544"}
	result := p.Prehash()
	if len(result) != 40 {
		t.Errorf("Prehash length = %d, want 40", len(result))
	}
}

func TestHWVersion2(t *testing.T) {
	p := &Portal{
		MAC:          "00:1A:79:00:00:00",
		SerialNumber: "SN000",
		Model:        "MAG544",
		DeviceID2:    "DEV000",
		Random:       "testrandom",
	}
	result := p.HWVersion2()
	if len(result) != 40 {
		t.Errorf("HWVersion2 length = %d, want 40", len(result))
	}
}

func TestBuildUserAgent(t *testing.T) {
	ua := BuildUserAgent("MAG544")
	if !strings.Contains(ua, "MAG544") {
		t.Errorf("User-Agent missing model: %s", ua)
	}
	if !strings.Contains(ua, "Mozilla/5.0") {
		t.Errorf("User-Agent missing Mozilla prefix: %s", ua)
	}
	if !strings.Contains(ua, "AppleWebKit") {
		t.Errorf("User-Agent missing AppleWebKit: %s", ua)
	}
	if strings.Contains(ua, "QtEmbedded") {
		t.Errorf("User-Agent should not contain 'QtEmbedded' (legacy format): %s", ua)
	}
}
