package stalker

// UserAgent builds the User-Agent header for p.Model.
//
// The legacy "QtEmbedded; U; Linux; C" format previously used here was
// checked against three independent real MAG firmware images (MAG544/RDK,
// MAG322, MAG351) — the literal string "QtEmbedded" does not exist in any of
// them. All three instead resolve through Qt5WebKit's own
// QWebPageAdapter::defaultUserAgentStringEv, following the standard
// cross-platform WebKit template:
//
//	Mozilla/5.0 (%1%2%3) AppleWebKit/%4 (KHTML, like Gecko) %99 Safari/%5
//
// where %99 is Infomir's app-name token, confirmed in the MAG544 binary as
// "<Model> stbapp ver: <X> rev: <Y> Mobile". The %1%2%3 platform tokens are
// filled by Qt at runtime rather than baked into the binary as a literal, so
// the exact value couldn't be recovered from static analysis — "X11; Linux
// armv7l" below follows WebKit's long-standing cross-port convention of
// reporting "X11; Linux <arch>" for Linux builds regardless of the actual
// windowing backend, but is not a verified literal.
func (p *Portal) UserAgent() string {
	return BuildUserAgent(p.Model)
}

// BuildUserAgent builds the same User-Agent string as Portal.UserAgent for a
// given model, without requiring a full Portal. Exported so other packages
// (e.g. hls, which talks to the CDN/streaming server directly rather than
// the portal) can use the same corrected format instead of drifting onto
// their own copy.
func BuildUserAgent(model string) string {
	return "Mozilla/5.0 (X11; Linux armv7l) AppleWebKit/605.1.15 (KHTML, like Gecko) " + model + " stbapp ver: 4 rev: 2116 Mobile Safari/605.1.15"
}
