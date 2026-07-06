package stalker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Helper tests ---------------------------------------------------------

func TestExtractStreamID(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"691399", "691399"},
		{"ffmpeg http://cdn.example/live/play/12345.m3u8", "12345"},
		{"ffmpeg http://cdn.example/stream/999.ts", "999"},
		{"http://cdn.example/ch/456.m3u8", "456"},
		{"", ""},
		{"not-a-number", ""},
	}
	for _, tc := range tests {
		got := extractStreamID(tc.cmd)
		if got != tc.want {
			t.Errorf("extractStreamID(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestIsAllDigits(t *testing.T) {
	if !isAllDigits("12345") {
		t.Error("isAllDigits(12345) = false")
	}
	if isAllDigits("12a45") {
		t.Error("isAllDigits(12a45) = true")
	}
	if isAllDigits("") {
		t.Error("isAllDigits(\"\") = true")
	}
}

func TestOriginBase_withOrigins(t *testing.T) {
	p := &Portal{
		OriginIPs: []string{"103.176.90.24"},
		Location:  "http://tres.4vps.info/stalker_portal/server/load.php",
	}
	got := p.originBase()
	want := "http://103.176.90.24"
	if got != want {
		t.Errorf("originBase = %q, want %q", got, want)
	}
}

func TestOriginBase_noOrigins(t *testing.T) {
	p := &Portal{
		Location: "http://tres.4vps.info/stalker_portal/server/load.php",
	}
	got := p.originBase()
	want := "http://tres.4vps.info"
	if got != want {
		t.Errorf("originBase = %q, want %q", got, want)
	}
}

func TestOriginBase_empty(t *testing.T) {
	p := &Portal{}
	got := p.originBase()
	if got != "" {
		t.Errorf("originBase with empty Location = %q, want \"\"", got)
	}
}

func TestResolveURL_absolute(t *testing.T) {
	got := resolveURL("http://origin/a/b.php", "http://cdn.example/live/play/token")
	want := "http://cdn.example/live/play/token"
	if got != want {
		t.Errorf("resolveURL = %q, want %q", got, want)
	}
}

func TestResolveURL_rootRelative(t *testing.T) {
	got := resolveURL("http://origin/stalker_portal/server/load.php", "/live/play/token")
	want := "http://origin/live/play/token"
	if got != want {
		t.Errorf("resolveURL = %q, want %q", got, want)
	}
}

func TestResolveURL_relative(t *testing.T) {
	got := resolveURL("http://origin/stalker_portal/server/load.php", "live/play/token")
	want := "http://origin/stalker_portal/server/live/play/token"
	if got != want {
		t.Errorf("resolveURL = %q, want %q", got, want)
	}
}

// --- Bypass method tests --------------------------------------------------

func TestTryDirectHLS_found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".m3u8") {
			w.Write([]byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://cdn/stream.ts\n"))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	c := &Channel{
		CMD:    "12345",
		CMD_ID: "12345",
	}
	// Manually create a portal with the test server as origin
	c.Portal = &Portal{
		OriginIPs: []string{server.Listener.Addr().String()},
		MAC:       "00:1A:79:00:00:01",
	}

	link, err := c.tryDirectHLS(c.Portal.originBase())
	if err != nil {
		t.Fatalf("tryDirectHLS failed: %v", err)
	}
	if !strings.Contains(link, "/ch/12345.m3u8") {
		t.Errorf("unexpected HLS link: %s", link)
	}
}

func TestTryDirectHLS_notFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer server.Close()

	c := &Channel{CMD: "99999", CMD_ID: "99999"}
	c.Portal = &Portal{
		OriginIPs: []string{server.Listener.Addr().String()},
	}

	_, err := c.tryDirectHLS(c.Portal.originBase())
	if err == nil {
		t.Fatal("expected error for non-existent HLS path")
	}
}

func TestTryPlayLiveOrigin_302(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "play/live.php") {
			w.Header().Set("Location", "/live/play/abcdef12345")
			w.WriteHeader(302)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	c := &Channel{
		CMD: "691399",
		Portal: &Portal{
			MAC:       "00:1A:79:00:00:01",
			OriginIPs: []string{server.Listener.Addr().String()},
		},
	}

	link, err := c.tryPlayLiveOrigin(c.Portal.originBase(), "691399")
	if err != nil {
		t.Fatalf("tryPlayLiveOrigin failed: %v", err)
	}
	if !strings.Contains(link, "/live/play/abcdef12345") {
		t.Errorf("unexpected play URL: %s", link)
	}
}

func TestTryPlayLiveOrigin_456(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(456)
	}))
	defer server.Close()

	c := &Channel{
		CMD: "691399",
		Portal: &Portal{
			MAC:       "00:1A:79:00:00:01",
			OriginIPs: []string{server.Listener.Addr().String()},
		},
	}

	_, err := c.tryPlayLiveOrigin(c.Portal.originBase(), "691399")
	if err == nil {
		t.Fatal("expected error for 456 response")
	}
}

// --- Integration test: full bypass flow from 458 --------------------------

func TestNewLinkBypassViaDirectHLS(t *testing.T) {
	// Cloudflare returns 458 for everything
	cloudflare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(458)
	}))
	defer cloudflare.Close()

	// Origin returns 458 for create_link (also rate-limited) but serves
	// HLS direct paths with no auth — the bypass should kick in.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "action=create_link") {
			w.WriteHeader(458)
			return
		}
		if strings.Contains(r.URL.Path, ".m3u8") {
			w.Write([]byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://cdn/stream.ts\n"))
			return
		}
		w.WriteHeader(404)
	}))
	defer origin.Close()

	p := &Portal{
		Location:  cloudflare.URL,
		MAC:       "00:1A:79:00:00:01",
		Model:     "MAG254",
		TimeZone:  "Europe/Vilnius",
		Token:     "test-token",
		OriginIPs: []string{origin.Listener.Addr().String()},
	}

	c := &Channel{
		Title:  "Test Channel",
		CMD:    "691399",
		CMD_ID: "691399",
		Portal: p,
	}

	link, err := c.NewLink(false)
	if err != nil {
		t.Fatalf("NewLink should fall back to HLS direct bypass, got: %v", err)
	}
	if !strings.Contains(link, "/ch/691399.m3u8") {
		t.Errorf("unexpected bypass link: %s", link)
	}
}

func TestNewLinkBypassViaPlayLiveOrigin(t *testing.T) {
	// Cloudflare returns 458 for everything
	cloudflare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(458)
	}))
	defer cloudflare.Close()

	// Origin returns 458 for create_link, 404 for HLS direct, but has
	// play/live.php working — the second bypass method should succeed.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "action=create_link") {
			w.WriteHeader(458)
			return
		}
		if strings.Contains(r.URL.Path, "play/live.php") {
			w.Header().Set("Location", "/live/play/token123")
			w.WriteHeader(302)
			return
		}
		w.WriteHeader(404)
	}))
	defer origin.Close()

	p := &Portal{
		Location:  cloudflare.URL,
		MAC:       "00:1A:79:00:00:01",
		Model:     "MAG254",
		TimeZone:  "Europe/Vilnius",
		Token:     "test-token",
		OriginIPs: []string{origin.Listener.Addr().String()},
	}

	c := &Channel{
		Title:  "Test",
		CMD:    "691399",
		CMD_ID: "691399",
		Portal: p,
	}

	link, err := c.NewLink(false)
	if err != nil {
		t.Fatalf("NewLink should fall back to play/live.php bypass, got: %v", err)
	}
	if !strings.Contains(link, "/live/play/token123") {
		t.Errorf("unexpected bypass link: %s", link)
	}
}

// --- isBypassableError tests ----------------------------------------------

func TestIsBypassableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"456 no sub", &httpStatusError{code: 456, status: "456 No Subscription"}, true},
		{"458 rate limit", &httpStatusError{code: 458, status: "458 Rate Limited"}, true},
		{"403 forbidden", &httpStatusError{code: 403, status: "403 Forbidden"}, true},
		{"500 transient", &httpStatusError{code: 500, status: "500 Internal Error"}, false},
		{"502 transient", &httpStatusError{code: 502, status: "502 Bad Gateway"}, false},
		{"404 not found", &httpStatusError{code: 404, status: "404 Not Found"}, false},
		{"limit fatal", &httpStatusError{code: 200, status: "200 OK"}, false},
		{"limit app error", errApplication("limit"), true},
		{"nothing_to_play", errApplication("nothing_to_play"), false},
		{"temporary_unavailable", errApplication("temporary_unavailable"), false},
	}
	for _, tc := range tests {
		got := isBypassableError(tc.err)
		if got != tc.want {
			t.Errorf("%s: isBypassableError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// errApplication creates an error that looks like a JSON-level create_link failure.
func errApplication(msg string) error {
	return &testError{"create_link failed: " + msg}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
