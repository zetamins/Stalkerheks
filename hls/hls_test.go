package hls

import (
	"testing"
)

func TestGetLinkType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    int
	}{
		{"application/vnd.apple.mpegurl", linkTypeHLS},
		{"application/x-mpegurl", linkTypeHLS},
		{"APPLICATION/VND.APPLE.MPEGURL", linkTypeHLS},
		{"video/mp4", linkTypeMedia},
		{"audio/aac", linkTypeMedia},
		{"application/octet-stream", linkTypeMedia},
		{"video/MP2T", linkTypeMedia},
		{"text/html", linkTypeMedia},
		{"", linkTypeMedia},
	}
	for _, tc := range tests {
		result := getLinkType(tc.contentType)
		if result != tc.expected {
			t.Errorf("getLinkType(%q) = %d, want %d", tc.contentType, result, tc.expected)
		}
	}
}

func TestDeleteAfterLastSlash(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com/path/playlist.m3u8", "http://example.com/path/"},
		{"http://example.com/", "http://example.com/"},
		{"/path/to/file", "/path/to/"},
	}
	for _, tc := range tests {
		result := deleteAfterLastSlash(tc.input)
		if result != tc.expected {
			t.Errorf("deleteAfterLastSlash(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestNewInstance(t *testing.T) {
	inst := NewInstance()
	if inst == nil {
		t.Fatal("NewInstance returned nil")
	}
	if inst.ChannelsReady() {
		t.Error("new instance should not have channels ready")
	}
	if len(inst.Playlist()) != 0 {
		t.Errorf("new instance playlist should be empty, got %d", len(inst.Playlist()))
	}
}

func TestInstanceLifecycle(t *testing.T) {
	inst := NewInstance()
	if inst == nil {
		t.Fatal("NewInstance returned nil")
	}
	// Stop on a fresh instance should not panic
	inst.Stop()
}

func TestChannelValidationZero(t *testing.T) {
	// Channel with zero lastAccess should be invalid
	c := &Channel{}
	if c.isValid() {
		t.Error("channel with zero lastAccess should not be valid")
	}
}

func TestBuildDeviceHash(t *testing.T) {
	hash := buildDeviceHash("MAG544")
	if len(hash) != 40 { // SHA-1 hex
		t.Errorf("buildDeviceHash length = %d, want 40", len(hash))
	}
	hash2 := buildDeviceHash("MAG254")
	if hash == hash2 {
		t.Error("buildDeviceHash should differ for different models")
	}
}
