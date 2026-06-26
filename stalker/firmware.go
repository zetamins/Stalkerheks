package stalker

// Firmware version constants derived from the real MAG544 firmware image
// (Resources/rootfs-2.20.11-pub-544/Img_Ver.txt and imageupdate metadata).
// These replace the previous hardcoded synthetic values with real Infomir
// firmware identifiers.
const (
	// From imageupdate header: "Image Version:220" → hex 0xDC
	firmwareImageVersion = "0x000000DC"

	// From imageupdate header: "VerUpdateAPI:2"
	firmwareUpdateAPI = "2"

	// From rootfs Img_Ver.txt: "ImageVersion: 2.20.11-pub-544 Wed Feb 4 16:38:55 EET 2026"
	firmwareVersion      = "2.20.11-pub-544"
	firmwareVersionShort = "220"
	firmwareBuildDate    = "20260204_163855"

	// The real STB sends api_signature as a constant "256" — this is a
	// protocol-level constant, not a firmware-derived value. Confirmed
	// against the real xpcom.common.js get_profile parameter set.
	apiSignatureConst = "256"
)

// VersionString builds a synthetic firmware version string matching the
// format real MAG STB boxes report in the "ver" get_profile parameter.
// Uses real firmware identifiers extracted from the MAG544 rootfs and
// imageupdate OTA bundle in Resources/.
func (p *Portal) VersionString() string {
	return "ImageDescription: " + p.Model + "; ImageDate: " + firmwareBuildDate + "; PORTAL version: 5.6.0; API Version: 0x1811"
}

// FirmwareImageVersion returns the image_version parameter value derived
// from the real MAG544 firmware's numeric image version.
func FirmwareImageVersion() string { return firmwareImageVersion }

// FirmwareHWVersion returns the hw_version parameter — the real STB
// reports "1.0.00" for MAG544 hardware revision 1.0.
func FirmwareHWVersion() string { return "1.0.00" }

// FirmwareAPISignature returns the api_signature constant.
func FirmwareAPISignature() string { return apiSignatureConst }

// CDNHashInput returns the string used as input to the X-Hash header sent on
// media/CDN requests. Format: model + version_string[:56], matching real MAG
// STB behavior (SHA-1 of this string is the X-Hash header value).
func CDNHashInput(model string) string {
	return model + "ImageDescription: " + model + "; ImageDate: " + firmwareBuildDate + "; PORTAL version: 5.6.0; API Version: 0x1811"
}
