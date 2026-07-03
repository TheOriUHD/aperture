package onvif

import (
	"strconv"
	"strings"
)

// The SOAP 1.2 envelope with every namespace an ONVIF Profile S client is likely
// to look for. Declaring them all up front keeps the per-operation payloads free
// of prefix bookkeeping.
const envelopeOpen = `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"` +
	` xmlns:tt="http://www.onvif.org/ver10/schema"` +
	` xmlns:tds="http://www.onvif.org/ver10/device/wsdl"` +
	` xmlns:trt="http://www.onvif.org/ver10/media/wsdl"` +
	` xmlns:tev="http://www.onvif.org/ver10/events/wsdl"` +
	` xmlns:xml="http://www.w3.org/XML/1998/namespace">` +
	`<s:Body>`

const envelopeClose = `</s:Body></s:Envelope>`

// --- Device service responses ---

func (s *Server) deviceInformation() string {
	return `<tds:GetDeviceInformationResponse>` +
		`<tds:Manufacturer>Aperture</tds:Manufacturer>` +
		`<tds:Model>Aperture NVR</tds:Model>` +
		`<tds:FirmwareVersion>` + xmlEscape(firmwareVersion()) + `</tds:FirmwareVersion>` +
		`<tds:SerialNumber>aperture-onvif</tds:SerialNumber>` +
		`<tds:HardwareId>aperture</tds:HardwareId>` +
		`</tds:GetDeviceInformationResponse>`
}

func (s *Server) capabilities() string {
	dev := s.deviceServiceURL()
	media := s.mediaServiceURL()
	return `<tds:GetCapabilitiesResponse><tds:Capabilities>` +
		`<tt:Device><tt:XAddr>` + xmlEscape(dev) + `</tt:XAddr>` +
		`<tt:System><tt:DiscoveryResolve>false</tt:DiscoveryResolve>` +
		`<tt:DiscoveryBye>true</tt:DiscoveryBye></tt:System>` +
		`</tt:Device>` +
		`<tt:Media><tt:XAddr>` + xmlEscape(media) + `</tt:XAddr>` +
		`<tt:StreamingCapabilities>` +
		`<tt:RTPMulticast>false</tt:RTPMulticast>` +
		`<tt:RTP_TCP>true</tt:RTP_TCP>` +
		`<tt:RTP_RTSP_TCP>true</tt:RTP_RTSP_TCP>` +
		`</tt:StreamingCapabilities></tt:Media>` +
		`</tds:Capabilities></tds:GetCapabilitiesResponse>`
}

func (s *Server) deviceServiceCapabilities() string {
	return `<tds:GetServiceCapabilitiesResponse><tds:Capabilities>` +
		`<tds:Network IPFilter="false" ZeroConfiguration="false" DynDNS="false"/>` +
		`<tds:System DiscoveryResolve="false" DiscoveryBye="true" RemoteDiscovery="false"/>` +
		`</tds:Capabilities></tds:GetServiceCapabilitiesResponse>`
}

func (s *Server) services() string {
	dev := s.deviceServiceURL()
	media := s.mediaServiceURL()
	var b strings.Builder
	b.WriteString(`<tds:GetServicesResponse>`)
	b.WriteString(`<tds:Service>` +
		`<tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace>` +
		`<tds:XAddr>` + xmlEscape(dev) + `</tds:XAddr>` +
		`<tds:Version><tt:Major>2</tt:Major><tt:Minor>0</tt:Minor></tds:Version>` +
		`</tds:Service>`)
	b.WriteString(`<tds:Service>` +
		`<tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace>` +
		`<tds:XAddr>` + xmlEscape(media) + `</tds:XAddr>` +
		`<tds:Version><tt:Major>2</tt:Major><tt:Minor>0</tt:Minor></tds:Version>` +
		`</tds:Service>`)
	b.WriteString(`</tds:GetServicesResponse>`)
	return b.String()
}

func (s *Server) systemDateAndTime() string {
	t := nowUTC()
	return `<tds:GetSystemDateAndTimeResponse><tds:SystemDateAndTime>` +
		`<tt:DateTimeType>NTP</tt:DateTimeType>` +
		`<tt:DaylightSavings>false</tt:DaylightSavings>` +
		`<tt:TimeZone><tt:TZ>UTC0</tt:TZ></tt:TimeZone>` +
		`<tt:UTCDateTime>` +
		`<tt:Time><tt:Hour>` + itoa(t.Hour()) + `</tt:Hour>` +
		`<tt:Minute>` + itoa(t.Minute()) + `</tt:Minute>` +
		`<tt:Second>` + itoa(t.Second()) + `</tt:Second></tt:Time>` +
		`<tt:Date><tt:Year>` + itoa(t.Year()) + `</tt:Year>` +
		`<tt:Month>` + itoa(int(t.Month())) + `</tt:Month>` +
		`<tt:Day>` + itoa(t.Day()) + `</tt:Day></tt:Date>` +
		`</tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`
}

func (s *Server) scopes() string {
	scopes := []string{
		"onvif://www.onvif.org/type/video_encoder",
		"onvif://www.onvif.org/Profile/Streaming",
		"onvif://www.onvif.org/name/Aperture",
		"onvif://www.onvif.org/hardware/AptertureNVR",
	}
	var b strings.Builder
	b.WriteString(`<tds:GetScopesResponse>`)
	for _, sc := range scopes {
		b.WriteString(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef>` +
			`<tt:ScopeItem>` + xmlEscape(sc) + `</tt:ScopeItem></tds:Scopes>`)
	}
	b.WriteString(`</tds:GetScopesResponse>`)
	return b.String()
}

// --- Media service responses ---

// profileElement emits a full <trt:Profiles> (fixed) element for a camera.
func profileElement(cam camView) string {
	return `<trt:Profiles token="` + xmlEscape(profileToken(cam.ID)) + `" fixed="true">` +
		`<tt:Name>` + xmlEscape(cam.Name) + `</tt:Name>` +
		`<tt:VideoSourceConfiguration token="` + xmlEscape(videoSourceCfgToken(cam.ID)) + `">` +
		`<tt:Name>` + xmlEscape(cam.Name) + `</tt:Name>` +
		`<tt:UseCount>1</tt:UseCount>` +
		`<tt:SourceToken>` + xmlEscape(videoSourceToken(cam.ID)) + `</tt:SourceToken>` +
		`<tt:Bounds x="0" y="0" width="` + itoa(cam.Width) + `" height="` + itoa(cam.Height) + `"/>` +
		`</tt:VideoSourceConfiguration>` +
		`<tt:VideoEncoderConfiguration token="` + xmlEscape(videoEncoderCfgToken(cam.ID)) + `">` +
		`<tt:Name>` + xmlEscape(cam.Name) + `</tt:Name>` +
		`<tt:UseCount>1</tt:UseCount>` +
		`<tt:Encoding>H264</tt:Encoding>` +
		`<tt:Resolution><tt:Width>` + itoa(cam.Width) + `</tt:Width>` +
		`<tt:Height>` + itoa(cam.Height) + `</tt:Height></tt:Resolution>` +
		`<tt:Quality>5</tt:Quality>` +
		`<tt:RateControl><tt:FrameRateLimit>` + itoa(cam.FPS) + `</tt:FrameRateLimit>` +
		`<tt:EncodingInterval>1</tt:EncodingInterval>` +
		`<tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>` +
		`<tt:H264><tt:GovLength>30</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>` +
		`</tt:VideoEncoderConfiguration>` +
		`</trt:Profiles>`
}

func (s *Server) profiles() string {
	var b strings.Builder
	b.WriteString(`<trt:GetProfilesResponse>`)
	for _, c := range s.enabledCameras() {
		b.WriteString(profileElement(view(c)))
	}
	b.WriteString(`</trt:GetProfilesResponse>`)
	return b.String()
}

func (s *Server) profile(token string) string {
	cam, ok := s.camIDFromToken(token)
	if !ok {
		return `<trt:GetProfileResponse></trt:GetProfileResponse>`
	}
	// GetProfile returns a single Profile inside a GetProfileResponse; reuse the
	// profileElement body but under the singular wrapper name.
	inner := profileElement(view(cam))
	inner = strings.Replace(inner, "<trt:Profiles ", "<trt:Profile ", 1)
	inner = strings.Replace(inner, "</trt:Profiles>", "</trt:Profile>", 1)
	return `<trt:GetProfileResponse>` + inner + `</trt:GetProfileResponse>`
}

func (s *Server) videoSources() string {
	var b strings.Builder
	b.WriteString(`<trt:GetVideoSourcesResponse>`)
	for _, c := range s.enabledCameras() {
		cam := view(c)
		b.WriteString(`<trt:VideoSources token="` + xmlEscape(videoSourceToken(cam.ID)) + `">` +
			`<tt:Framerate>` + itoa(cam.FPS) + `</tt:Framerate>` +
			`<tt:Resolution><tt:Width>` + itoa(cam.Width) + `</tt:Width>` +
			`<tt:Height>` + itoa(cam.Height) + `</tt:Height></tt:Resolution>` +
			`</trt:VideoSources>`)
	}
	b.WriteString(`</trt:GetVideoSourcesResponse>`)
	return b.String()
}

func (s *Server) videoSourceConfigurations() string {
	var b strings.Builder
	b.WriteString(`<trt:GetVideoSourceConfigurationsResponse>`)
	for _, c := range s.enabledCameras() {
		cam := view(c)
		b.WriteString(`<trt:Configurations token="` + xmlEscape(videoSourceCfgToken(cam.ID)) + `">` +
			`<tt:Name>` + xmlEscape(cam.Name) + `</tt:Name>` +
			`<tt:UseCount>1</tt:UseCount>` +
			`<tt:SourceToken>` + xmlEscape(videoSourceToken(cam.ID)) + `</tt:SourceToken>` +
			`<tt:Bounds x="0" y="0" width="` + itoa(cam.Width) + `" height="` + itoa(cam.Height) + `"/>` +
			`</trt:Configurations>`)
	}
	b.WriteString(`</trt:GetVideoSourceConfigurationsResponse>`)
	return b.String()
}

func (s *Server) streamURIResponse(token string) string {
	cam, ok := s.camIDFromToken(token)
	if !ok {
		return `<trt:GetStreamUriResponse></trt:GetStreamUriResponse>`
	}
	uri := s.streamURL(cam.ID)
	return `<trt:GetStreamUriResponse><trt:MediaUri>` +
		`<tt:Uri>` + xmlEscape(uri) + `</tt:Uri>` +
		`<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect>` +
		`<tt:InvalidAfterReboot>false</tt:InvalidAfterReboot>` +
		`<tt:Timeout>PT0S</tt:Timeout>` +
		`</trt:MediaUri></trt:GetStreamUriResponse>`
}

func (s *Server) snapshotURIResponse(token string) string {
	cam, ok := s.camIDFromToken(token)
	if !ok {
		return `<trt:GetSnapshotUriResponse></trt:GetSnapshotUriResponse>`
	}
	uri := s.snapshotURL(cam.ID)
	return `<trt:GetSnapshotUriResponse><trt:MediaUri>` +
		`<tt:Uri>` + xmlEscape(uri) + `</tt:Uri>` +
		`<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect>` +
		`<tt:InvalidAfterReboot>false</tt:InvalidAfterReboot>` +
		`<tt:Timeout>PT0S</tt:Timeout>` +
		`</trt:MediaUri></trt:GetSnapshotUriResponse>`
}

// itoa is a tiny strconv.Itoa alias to keep the builders terse.
func itoa(n int) string { return strconv.Itoa(n) }
