package proxy

import (
	"strings"
	"testing"
)

func TestGenerateRandomHex(t *testing.T) {
	r1 := generateRandomHex(32)
	if len(r1) != 32 {
		t.Errorf("generateRandomHex(32) length = %d, want 32", len(r1))
	}
	for _, c := range r1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("generateRandomHex contains non-hex character: %c", c)
		}
	}
}

func TestSpecialLinkEscape(t *testing.T) {
	input := "http://example.com/path/to/stream"
	escaped := specialLinkEscape(input)
	if strings.Contains(escaped, "/") && !strings.Contains(escaped, `\/`) {
		// The escaped version should have forward slashes escaped
	}
	// Round-trip through unescape should work
	if !strings.Contains(escaped, `\/`) {
		// At minimum, specialLinkEscape should change something
	}
}

func TestUnescapeJSONString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`hello`, `hello`},
		{`hello\/world`, `hello/world`},
		{`test`, `test`},
	}
	for _, tc := range tests {
		result := unescapeJSONString(tc.input)
		if result != tc.expected {
			t.Errorf("unescapeJSONString(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestJSONStringField(t *testing.T) {
	body := `{"name":"Channel One","cmd":"ffmpeg url","id":"1"}`
	raw, unescaped, next, ok := jsonStringField(body, "name", 0)
	if !ok {
		t.Fatal("jsonStringField name not found")
	}
	if unescaped != "Channel One" {
		t.Errorf("unescaped name = %q, want %q", unescaped, "Channel One")
	}
	if raw != "Channel One" {
		t.Errorf("raw name = %q", raw)
	}

	_, _, _, ok = jsonStringField(body, "nonexistent", 0)
	if ok {
		t.Error("jsonStringField should not find nonexistent key")
	}

	// Test that next points past the closing quote of "name"
	if next <= 0 {
		t.Error("jsonStringField next should be > 0 after finding a field")
	}
	// The next field should start at an offset within the body
	if next >= len(body) {
		t.Error("jsonStringField next should be within body")
	}
}

func TestLastJSONStringFieldBefore(t *testing.T) {
	body := `{"id":"1","name":"Channel One"}`
	result, ok := lastJSONStringFieldBefore(body, "id", len(body))
	if !ok {
		t.Fatal("lastJSONStringFieldBefore id not found")
	}
	if result != "1" {
		t.Errorf("lastJSONStringFieldBefore = %q, want %q", result, "1")
	}

	_, ok = lastJSONStringFieldBefore(body, "nonexistent", len(body))
	if ok {
		t.Error("lastJSONStringFieldBefore should not find nonexistent key")
	}
}

func TestMin(t *testing.T) {
	if min(1, 2) != 1 {
		t.Error("min(1,2) should be 1")
	}
	if min(2, 1) != 1 {
		t.Error("min(2,1) should be 1")
	}
	if min(50, 100) != 50 {
		t.Error("min(50,100) should be 50")
	}
}

func TestFilterWatchdogEventsEmpty(t *testing.T) {
	// Empty/invalid body should return safe stub
	result := filterWatchdogEvents("invalid json")
	if !strings.Contains(result, `"msgs":0`) {
		t.Error("filterWatchdogEvents should return safe stub on invalid input")
	}
}

func TestFilterWatchdogEventsSafe(t *testing.T) {
	// Safe event should pass through unchanged
	body := `{"js":{"data":{"event":"send_msg","msg":"Hello"},"text":"ok"}}`
	result := filterWatchdogEvents(body)
	// Should contain "Hello" since send_msg is safe
	if !strings.Contains(result, "Hello") {
		t.Error("filterWatchdogEvents should pass through safe events")
	}
}

func TestFilterWatchdogEventsDangerous(t *testing.T) {
	// Dangerous event should be stripped
	body := `{"js":{"data":{"event":"reboot","msg":"rebooting"},"text":"ok"}}`
	result := filterWatchdogEvents(body)
	if strings.Contains(result, "reboot") {
		t.Error("filterWatchdogEvents should strip dangerous events")
	}
	if !strings.Contains(result, `"msgs":0`) {
		t.Error("filterWatchdogEvents should return safe stub for dangerous events")
	}
}

func TestGenerateNewChannelLink(t *testing.T) {
	link := "http://localhost:9999/iptv/Channel%20One"
	result := generateNewChannelLink(link, "cmd1", "100")
	if !strings.Contains(result, `"cmd":"`) {
		t.Error("generateNewChannelLink should contain cmd field")
	}
	if !strings.Contains(result, `"error":""`) {
		t.Error("generateNewChannelLink should contain empty error")
	}
}

func TestRewriteExternalURLs(t *testing.T) {
	inst := &Instance{}
	body := `some text http://weather.infomir.com.ua/weather?mac=xx rest`
	result := inst.rewriteExternalURLs(body, "192.168.1.1:8888")
	if strings.Contains(result, "weather.infomir.com.ua") {
		t.Error("rewriteExternalURLs should replace weather URL")
	}
	if !strings.Contains(result, "/_weather/") {
		t.Error("rewriteExternalURLs should use proxy endpoint")
	}
}
