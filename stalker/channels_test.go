package stalker

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetrieveChannels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// RetrieveChannels first gets channels, then getGenres
		if r.URL.RawQuery != "" && strings.Contains(r.URL.RawQuery, "action=get_genres") {
			w.Write([]byte(`{"js":[{"id":"1","title":"News"},{"id":"2","title":"Sports"}]}`))
			return
		}
		w.Write([]byte(`{"js":{"data":[
			{"id":"1","name":"Channel One","cmd":"ffmpeg http://stream.example.com/1.m3u8","logo":"logo1.png","tv_genre_id":"1","cmds":[{"id":"cmd1","ch_id":"100"}]},
			{"id":"2","name":"Channel Two","cmd":"ffmpeg http://stream.example.com/2.m3u8","logo":"logo2.png","tv_genre_id":"2","cmds":[{"id":"cmd2","ch_id":"200"}]}
		]}}`))
	}))
	defer server.Close()

	p := &Portal{Location: server.URL, Token: "test"}
	channels, err := p.RetrieveChannels()
	if err != nil {
		t.Fatalf("RetrieveChannels failed: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}

	ch1, ok := channels["Channel One"]
	if !ok {
		t.Fatal("Channel One not found")
	}
	if ch1.CMD != "ffmpeg http://stream.example.com/1.m3u8" {
		t.Errorf("unexpected CMD: %s", ch1.CMD)
	}

	ch2, ok := channels["Channel Two"]
	if !ok {
		t.Fatal("Channel Two not found")
	}
	if ch2.LogoLink != "logo2.png" {
		t.Errorf("unexpected logo: %s", ch2.LogoLink)
	}
}

func TestChannelNewLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"js":{"cmd":"ffmpeg http://stream.example.com/playlist.m3u8","error":""}}`))
	}))
	defer server.Close()

	p := &Portal{Location: server.URL}
	c := &Channel{CMD: "test_channel", Portal: p}
	link, err := c.NewLink(false)
	if err != nil {
		t.Fatalf("NewLink failed: %v", err)
	}
	if link != "http://stream.example.com/playlist.m3u8" {
		t.Errorf("unexpected link: %s", link)
	}
}

func TestChannelNewLinkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"js":{"cmd":"","error":"nothing_to_play"}}`))
	}))
	defer server.Close()

	p := &Portal{Location: server.URL}
	c := &Channel{CMD: "test_channel", Portal: p}
	_, err := c.NewLink(false)
	if err == nil {
		t.Fatal("expected error for nothing_to_play")
	}
}

func TestIsTransientCreateLinkError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"http 500", &httpStatusError{code: 500, status: "500 Internal Server Error"}, true},
		{"http 502", &httpStatusError{code: 502, status: "502 Bad Gateway"}, true},
		{"http 520", &httpStatusError{code: 520, status: "520"}, true},
		{"http 509", &httpStatusError{code: 509, status: "509 Bandwidth Limit"}, true},
		{"http 403 fatal", &httpStatusError{code: 403, status: "403 Forbidden"}, false},
		{"http 404 fatal", &httpStatusError{code: 404, status: "404 Not Found"}, false},
		{"temporary_unavailable", errors.New("create_link failed: temporary_unavailable"), true},
		{"nothing_to_play fatal", errors.New("create_link failed: nothing_to_play"), false},
		{"limit fatal", errors.New("create_link failed: limit"), false},
	}
	for _, tc := range cases {
		if got := isTransientCreateLinkError(tc.err); got != tc.want {
			t.Errorf("%s: isTransientCreateLinkError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestNewLinkRetriesTransient5xx verifies create_link retries a portal 5xx and
// then succeeds, rather than failing the first attempt outright.
func TestNewLinkRetriesTransient5xx(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusInternalServerError) // transient 500 on first try
			return
		}
		w.Write([]byte(`{"js":{"cmd":"ffmpeg http://cdn.example/live/123","error":""}}`))
	}))
	defer server.Close()

	p := &Portal{Location: server.URL}
	c := &Channel{CMD: "test_channel", Portal: p}
	link, err := c.NewLink(true)
	if err != nil {
		t.Fatalf("NewLink should recover from transient 500, got: %v", err)
	}
	if link != "http://cdn.example/live/123" {
		t.Errorf("unexpected link %q", link)
	}
	if hits < 2 {
		t.Errorf("expected a retry (>=2 create_link calls), got %d", hits)
	}
}

func TestGetGenres(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"js":[
			{"id":"1","title":"News"},
			{"id":"2","title":"Sports"},
			{"id":"3","title":"Movies"}
		]}`))
	}))
	defer server.Close()

	p := &Portal{Location: server.URL}
	genres, err := p.getGenres()
	if err != nil {
		t.Fatalf("getGenres failed: %v", err)
	}
	if len(genres) != 3 {
		t.Fatalf("expected 3 genres, got %d", len(genres))
	}
	if genres["1"] != "News" {
		t.Errorf("unexpected genre: %s", genres["1"])
	}
}

func TestRetrieveRadioChannels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"js":{"data":[
			{"id":"1","name":"Radio One","cmd":"ffmpeg http://radio.example.com/1","number":"101"},
			{"id":"2","name":"Radio Two","cmd":"ffmpeg http://radio.example.com/2","number":"102"}
		]}}`))
	}))
	defer server.Close()

	p := &Portal{Location: server.URL}
	channels, err := p.RetrieveRadioChannels()
	if err != nil {
		t.Fatalf("RetrieveRadioChannels failed: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("expected 2 radio channels, got %d", len(channels))
	}
}

func TestGetVODCategories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"js":[
			{"id":"1","title":"Movies","alias":"movies"},
			{"id":"2","title":"Cartoons","alias":"cartoons"}
		]}`))
	}))
	defer server.Close()

	p := &Portal{Location: server.URL}
	cats, err := p.GetVODCategories()
	if err != nil {
		t.Fatalf("GetVODCategories failed: %v", err)
	}
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(cats))
	}
}
