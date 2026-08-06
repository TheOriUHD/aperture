package api

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
)

// Keyset pagination convention for v1 collections.
//
// A cursor is an OPAQUE base64url token whose payload is the sort key of the
// last row a client received. Listing endpoints order by a unique, stable key
// and select `... WHERE key < cursor ...` (newest-first), so a page never
// duplicates or skips a row when rows are inserted or deleted between fetches —
// the flaw of offset paging. The next cursor is returned in the `X-Next-Cursor`
// response header (additive: the JSON body shape is unchanged, so first-party
// clients that ignore the header keep working). Extensions follow the header
// until it is absent, which marks the end of the collection.
//
// Keys are per-collection: events use `seq`; audit uses row `id`; segments use
// a `(start_ts,id)` composite. The payload format is the caller's business —
// clients must treat the whole token as opaque.

const (
	defaultPageLimit = 100
	maxPageLimit     = 1000
)

// pageLimit reads ?limit, clamping to [1,maxPageLimit]. An absent or invalid
// value yields def (or defaultPageLimit if def<=0).
func pageLimit(r *http.Request, def int) int {
	if def <= 0 {
		def = defaultPageLimit
	}
	v, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || v <= 0 {
		return def
	}
	if v > maxPageLimit {
		return maxPageLimit
	}
	return v
}

// encodeCursor wraps a sort-key payload into an opaque token.
func encodeCursor(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// decodeCursor unwraps a token to its payload. ok is false when the token is
// malformed base64.
func decodeCursor(token string) (payload string, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// cursorInt reads ?cursor as a single int64 key (events, audit). present is
// false when no cursor was supplied. ok is false when a cursor was supplied but
// is malformed — the handler should answer 400 rather than silently ignore it,
// so a client never pages past a typo believing it reached the end.
func cursorInt(r *http.Request) (key int64, present, ok bool) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return 0, false, true
	}
	payload, good := decodeCursor(raw)
	if !good {
		return 0, true, false
	}
	n, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return 0, true, false
	}
	return n, true, true
}

// setNextCursor sets X-Next-Cursor to the encoded int64 key when the page was
// full (len == limit, so more rows may follow). A short page ends the
// collection and leaves the header unset.
func setNextCursor(w http.ResponseWriter, returned, limit int, lastKey int64) {
	if returned >= limit && limit > 0 {
		w.Header().Set("X-Next-Cursor", encodeCursor(strconv.FormatInt(lastKey, 10)))
	}
}

// encodeSegmentCursor encodes a segments (start_ts,id) composite keyset key.
// segment ids are random strings, so start_ts alone can tie; the id breaks it.
func encodeSegmentCursor(startTS int64, id string) string {
	return encodeCursor(strconv.FormatInt(startTS, 10) + "|" + id)
}

// cursorSegment reads ?cursor as a (start_ts,id) segment key. Semantics match
// cursorInt: present=false when absent, ok=false when malformed.
func cursorSegment(r *http.Request) (startTS int64, id string, present, ok bool) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return 0, "", false, true
	}
	payload, good := decodeCursor(raw)
	if !good {
		return 0, "", true, false
	}
	sep := strings.IndexByte(payload, '|')
	if sep < 0 {
		return 0, "", true, false
	}
	n, err := strconv.ParseInt(payload[:sep], 10, 64)
	if err != nil {
		return 0, "", true, false
	}
	return n, payload[sep+1:], true, true
}
