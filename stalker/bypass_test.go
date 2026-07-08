package stalker

import (
	"testing"
)

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

func errApplication(msg string) error {
	return &testError{"create_link failed: " + msg}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
