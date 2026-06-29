package api

import (
	"time"

	"github.com/TheOriUHD/aperture/internal/astro"
	"github.com/TheOriUHD/aperture/internal/site"
)

// condEnricher attaches the ambient conditions that were current at an event to
// its JSON. It joins against the weather time-series and the astro layer at
// query time, so it works even for events recorded before the context layer
// existed. SunTimes are memoized per day across a single list request.
type condEnricher struct {
	srv  *Server
	site site.Site
	loc  *time.Location
	suns map[string]*astro.SunTimes
}

func (s *Server) newCondEnricher() *condEnricher {
	loc := site.Load(s.st)
	tz, err := time.LoadLocation(loc.Timezone)
	if err != nil {
		tz = time.UTC
	}
	return &condEnricher{srv: s, site: loc, loc: tz, suns: map[string]*astro.SunTimes{}}
}

// conditions returns the conditions map for an event start (unix ms), or nil
// when no site is configured.
func (c *condEnricher) conditions(startMs int64) map[string]any {
	if !c.site.Configured {
		return nil
	}
	out := map[string]any{}
	tsSec := startMs / 1000
	if ws, err := c.srv.st.WeatherAt(tsSec); err == nil && ws != nil {
		if ws.TempC != nil {
			out["tempC"] = *ws.TempC
		}
		if ws.ConditionCode != nil {
			out["conditionCode"] = *ws.ConditionCode
		}
		if ws.PrecipMm != nil {
			out["precipMm"] = *ws.PrecipMm
		}
		if ws.WindSpeedMs != nil {
			out["windSpeedMs"] = *ws.WindSpeedMs
		}
	}
	// Day or night, from the astro layer.
	t := time.Unix(tsSec, 0).In(c.loc)
	key := t.Format("2006-01-02")
	sun := c.suns[key]
	if sun == nil {
		noon := time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, c.loc)
		s := astro.Sun(noon, c.site.Lat, c.site.Lon)
		sun = &s
		c.suns[key] = sun
	}
	out["isNight"] = sun.Sunrise > 0 && sun.Sunset > 0 && (tsSec < sun.Sunrise || tsSec > sun.Sunset)
	return out
}
