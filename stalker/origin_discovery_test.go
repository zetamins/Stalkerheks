package stalker

import (
	"net"
	"testing"
)

func TestNextIP(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"192.168.0.1", "192.168.0.2"},
		{"192.168.0.255", "192.168.1.0"},
		{"192.168.255.255", "192.169.0.0"},
		{"10.0.0.1", "10.0.0.2"},
		{"10.0.0.255", "10.0.1.0"},
	}

	for _, tc := range tests {
		ip := net.ParseIP(tc.in)
		if ip == nil {
			t.Fatalf("bad test IP: %s", tc.in)
		}
		ip4 := ip.To4()
		if ip4 == nil {
			t.Fatalf("not IPv4: %s", tc.in)
		}
		got := nextIP(ip4).String()
		if got != tc.out {
			t.Errorf("nextIP(%s) = %s, want %s", tc.in, got, tc.out)
		}
	}
}

func TestDiscoverOriginIPs_invalidSubnet(t *testing.T) {
	origins := DiscoverOriginIPs("not-a-subnet", "00:00:00:00:00:00")
	if origins != nil {
		t.Errorf("expected nil for invalid subnet, got %v", origins)
	}
}

func TestDiscoverOriginIPs_non24Subnet(t *testing.T) {
	origins := DiscoverOriginIPs("10.0.0.0/16", "00:00:00:00:00:00")
	if origins != nil {
		t.Errorf("expected nil for non-/24 subnet, got %v", origins)
	}
}

func TestProbeOrigin_miss(t *testing.T) {
	// 127.0.0.1 should not have anything listening on port 80 (requires root)
	if probeOrigin("127.0.0.1", "00:00:00:00:00:00") {
		t.Error("probeOrigin(127.0.0.1) = true, want false (nothing listens on :80)")
	}
}

func TestDiscoverOriginIPs_emptyOnNoSubnet(t *testing.T) {
	origins := DiscoverOriginIPs("", "00:00:00:00:00:00")
	if origins != nil {
		t.Errorf("expected nil for empty subnet, got %v", origins)
	}
}

func TestOriginURL_noOrigins(t *testing.T) {
	p := &Portal{}
	original := "http://tres.4vps.info/stalker_portal/server/load.php?action=create_link&type=itv&cmd=123"
	got := p.originURL(original)
	if got != original {
		t.Errorf("originURL with no OriginIPs changed URL: %s -> %s", original, got)
	}
}

func TestOriginURL_withOrigins(t *testing.T) {
	p := &Portal{
		OriginIPs: []string{"103.176.90.24"},
	}
	original := "http://tres.4vps.info/stalker_portal/server/load.php?action=create_link&type=itv&cmd=123&JsHttpRequest=1-xml"
	want := "http://103.176.90.24/stalker_portal/server/load.php?action=create_link&type=itv&cmd=123&JsHttpRequest=1-xml"
	got := p.originURL(original)
	if got != want {
		t.Errorf("originURL = %s, want %s", got, want)
	}
}

func TestOriginURL_withHTTPS(t *testing.T) {
	p := &Portal{
		OriginIPs: []string{"103.176.90.24"},
	}
	original := "https://tres.4vps.info:443/stalker_portal/server/load.php?action=create_link"
	want := "https://103.176.90.24/stalker_portal/server/load.php?action=create_link"
	got := p.originURL(original)
	if got != want {
		t.Errorf("originURL = %s, want %s", got, want)
	}
}

func TestOriginURL_picksFirstOrigin(t *testing.T) {
	p := &Portal{
		OriginIPs: []string{"103.176.90.24", "103.176.90.25", "103.176.90.26"},
	}
	original := "http://portal.example.com/c/portal.php?action=create_link"
	want := "http://103.176.90.24/c/portal.php?action=create_link"
	got := p.originURL(original)
	if got != want {
		t.Errorf("originURL = %s, want %s", got, want)
	}
}

func TestOriginURL_malformedURL(t *testing.T) {
	p := &Portal{
		OriginIPs: []string{"103.176.90.24"},
	}
	original := ":://bad-url"
	got := p.originURL(original)
	if got != original {
		t.Errorf("originURL should return original on parse error, got %s", got)
	}
}
