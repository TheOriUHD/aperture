package stream

import (
	"regexp"
	"strings"
)

// protectLink matches a link UniFi Protect hands out: its console serves every
// camera on port 7441 (RTSPS) or 7447 (RTSP), at a path that is the stream key.
var protectLink = regexp.MustCompile(`(?i)^rtsp[sx]?://[^/?#]+:(7441|7447)/[A-Za-z0-9]{16,}([?#].*)?$`)

// IsProtectSource reports whether a camera link comes from UniFi Protect.
func IsProtectSource(link string) bool { return protectLink.MatchString(strings.TrimSpace(link)) }

// NormalizeSource rewrites a UniFi Protect RTSPS link into the form go2rtc plays.
// Protect hands out rtsps://console:7441/<key>?enableSrtp, but the console's
// certificate is self-signed, so the link becomes rtspx:// (go2rtc's RTSPS without
// a certificate check), and enableSrtp only means something to Protect's own apps.
// Every other link is returned as it is.
func NormalizeSource(link string) string {
	link = strings.TrimSpace(link)
	if !IsProtectSource(link) || !strings.HasPrefix(strings.ToLower(link), "rtsps://") {
		return link
	}
	link = "rtspx://" + link[len("rtsps://"):]
	link = strings.Replace(link, "?enableSrtp&", "?", 1)
	link = strings.Replace(link, "&enableSrtp", "", 1)
	return strings.TrimSuffix(link, "?enableSrtp")
}

// playable reports whether go2rtc can pull a link as a camera source.
func playable(link string) bool {
	l := strings.ToLower(link)
	return strings.HasPrefix(l, "rtsp://") || strings.HasPrefix(l, "rtsps://") || strings.HasPrefix(l, "rtspx://")
}
