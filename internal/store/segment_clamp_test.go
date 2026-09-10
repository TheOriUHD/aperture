package store

import "testing"

// A 33-bit MPEG-TS PTS wrap inside a segment makes ffprobe report a duration of
// hours for a 10-second file. Four such rows in production (Schlafzimmer,
// 2026-08-20 18:55-18:56, ~306 KB each) reported 12.7-13.8h with end_ts a day in
// the future. That poisoned EXT-X-TARGETDURATION (49698 instead of 13) and
// parked every wall-clock offset for the day inside a dead fragment interior,
// which the player renders as a black rectangle.
func TestClampSegmentDuration(t *testing.T) {
	const start = 1787245007000 // 2026-08-20 18:55:07 local

	cases := []struct {
		name        string
		seg         Segment
		wantDurMS   int64
		wantEndTS   int64
		description string
	}{
		{
			name:        "healthy segment is untouched",
			seg:         Segment{StartTS: start, EndTS: start + 10000, DurationMS: 10000},
			wantDurMS:   10000,
			wantEndTS:   start + 10000,
			description: "the overwhelmingly common case must not be rewritten",
		},
		{
			name:        "a long but plausible segment is untouched",
			seg:         Segment{StartTS: start, EndTS: start + 22500, DurationMS: 22500},
			wantDurMS:   22500,
			wantEndTS:   start + 22500,
			description: "segments spanning a recorder restart legitimately reach ~22s",
		},
		{
			name:        "real production poison: 12.71h duration, end_ts a day out",
			seg:         Segment{StartTS: start, EndTS: start + 45765753, DurationMS: 45765753},
			wantDurMS:   nominalSegmentMS,
			wantEndTS:   start + nominalSegmentMS,
			description: "row 279ca0b0: neither duration nor span is believable",
		},
		{
			name:        "absurd duration but a sane span prefers the wall clock",
			seg:         Segment{StartTS: start, EndTS: start + 9800, DurationMS: 49697967},
			wantDurMS:   9800,
			wantEndTS:   start + 9800,
			description: "filenames are epoch seconds from the recorder and are trustworthy",
		},
		{
			name:        "zero duration falls back to the span",
			seg:         Segment{StartTS: start, EndTS: start + 10000, DurationMS: 0},
			wantDurMS:   10000,
			wantEndTS:   start + 10000,
			description: "a row that never got a duration should not become 60s",
		},
		{
			name:        "negative span is repaired from the clamped duration",
			seg:         Segment{StartTS: start, EndTS: start - 5000, DurationMS: 10000},
			wantDurMS:   10000,
			wantEndTS:   start + 10000,
			description: "end_ts before start_ts would invert the timeline",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seg := tc.seg
			clampSegmentDuration(&seg)
			if seg.DurationMS != tc.wantDurMS {
				t.Errorf("DurationMS = %d, want %d (%s)", seg.DurationMS, tc.wantDurMS, tc.description)
			}
			if seg.EndTS != tc.wantEndTS {
				t.Errorf("EndTS = %d, want %d (%s)", seg.EndTS, tc.wantEndTS, tc.description)
			}
			if seg.StartTS != tc.seg.StartTS {
				t.Errorf("StartTS was modified: %d -> %d; start_ts is authoritative",
					tc.seg.StartTS, seg.StartTS)
			}
		})
	}
}
