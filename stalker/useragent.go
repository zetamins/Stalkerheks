package stalker

import "strings"

// isRDKModel reports whether model belongs to the newer RDK/Qt5WebKit-based
// firmware generation (e.g. MAG520/540/544) rather than the legacy
// Qt4/WebKit1 MAG2xx line. Confirmed via firmware disassembly: the literal
// string "QtEmbedded" used in the legacy User-Agent does not exist anywhere
// in a MAG544 (RDK) firmware image — that generation uses Qt5WebKit's
// dynamic platform-token template instead.
func isRDKModel(model string) bool {
	m := strings.ToUpper(model)
	return strings.HasPrefix(m, "MAG5") || strings.HasPrefix(m, "MAG4K")
}

// UserAgent builds the User-Agent header for p.Model. For the legacy MAG2xx
// line this is the well-established, network-verified "QtEmbedded" format.
// For RDK-generation models (MAG5xx) the exact Qt5WebKit platform tokens
// could not be recovered from static analysis of the stripped stbapp binary
// (Qt fills them at runtime, not as a baked-in literal) — the value below is
// a structurally-plausible best effort, not a verified one, so it's gated
// behind model detection to avoid regressing the proven MAG2xx format.
func (p *Portal) UserAgent() string {
	if isRDKModel(p.Model) {
		return "Mozilla/5.0 (X11; Linux armv7l) AppleWebKit/605.1.15 (KHTML, like Gecko) " + p.Model + " stbapp ver: 4 rev: 2116 Mobile Safari/605.1.15"
	}
	return "Mozilla/5.0 (QtEmbedded; U; Linux; C) AppleWebKit/533.3 (KHTML, like Gecko) " + p.Model + " stbapp ver: 4 rev: 2116 Mobile Safari/533.3"
}
