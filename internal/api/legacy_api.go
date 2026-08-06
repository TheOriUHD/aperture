package api

import (
	"regexp"
	"strings"
	"sync"
)

// legacyAPICounter tallies hits on the deprecated /api (non-v1) alias, keyed by
// normalized path, so a stale caller can be IDENTIFIED — not just counted —
// before the alias is retired in 5.4. This is the real precondition the request
// log can't provide: logRequests only records non-GET requests and errors, so it
// misses the GET 200s a stale bundle would make. Incremented only by the
// deprecated instance of apiVersionHeaders, which runs solely on the /api mount.
type legacyAPICounter struct {
	mu     sync.Mutex
	byPath map[string]int64
	total  int64
}

func newLegacyAPICounter() *legacyAPICounter { return &legacyAPICounter{byPath: map[string]int64{}} }

// count records one alias hit against the normalized path. The distinct-key set
// is capped so a caller spraying unique paths can't grow the map without bound;
// overflow lands in "(other)".
func (c *legacyAPICounter) count(path string) {
	key := normalizeAPIPath(path)
	c.mu.Lock()
	if _, ok := c.byPath[key]; ok || len(c.byPath) < 200 {
		c.byPath[key]++
	} else {
		c.byPath["(other)"]++
	}
	c.total++
	c.mu.Unlock()
}

func (c *legacyAPICounter) snapshot() (map[string]int64, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int64, len(c.byPath))
	for k, v := range c.byPath {
		out[k] = v
	}
	return out, c.total
}

// apiIDSeg matches path segments that are resource ids (long hex, uuid, or all
// digits) so per-resource paths collapse to a route shape.
var apiIDSeg = regexp.MustCompile(`^([0-9a-fA-F]{12,}|[0-9a-fA-F-]{36}|[0-9]+)$`)

// normalizeAPIPath collapses id-like segments to ":id" (ignoring a trailing file
// extension, e.g. seg files / thumbnails), so /api/cameras/<hex>/hls and
// /api/events/<hex>/thumb.jpg group by shape rather than exploding the map.
func normalizeAPIPath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if seg == "" {
			continue
		}
		base := seg
		if dot := strings.LastIndexByte(seg, '.'); dot > 0 {
			base = seg[:dot]
		}
		if apiIDSeg.MatchString(base) {
			parts[i] = ":id"
		}
	}
	return strings.Join(parts, "/")
}
