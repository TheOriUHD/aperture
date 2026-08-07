package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEmptyNilSlices pins the CLASS, not just one endpoint: every shape a nil slice
// can hide in — a top-level slice, a struct field, a nested struct, a slice of
// structs, a pointer, and a map[string]any value — must serialize as [] not null, so
// the OpenAPI array contract holds for any current or future response. []byte is left
// alone (it marshals as a base64 string, not an array).
func TestEmptyNilSlices(t *testing.T) {
	type inner struct {
		Tags []string `json:"tags"`
	}
	type resp struct {
		A    []string       `json:"a"`    // top-level nil slice
		B    []int          `json:"b"`    // non-nil, must be untouched
		In   inner          `json:"in"`   // nil slice in a nested struct
		List []inner        `json:"list"` // nil slice inside slice-of-structs
		Raw  []byte         `json:"raw"`  // nil []byte → stays null (a string, not an array)
		M    map[string]any `json:"m"`    // nil slice as a map value (the manager's exact case)
		Ptr  *inner         `json:"ptr"`  // nil slice behind a pointer
	}
	v := resp{
		B:    []int{1, 2},
		List: []inner{{}, {Tags: []string{"x"}}},
		M:    map[string]any{"k": []string(nil), "n": 3},
		Ptr:  &inner{},
	}
	b, err := json.Marshal(emptyNilSlices(v))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	for _, want := range []string{
		`"a":[]`,             // top-level
		`"b":[1,2]`,          // non-nil untouched
		`"tags":[]`,          // in.tags + list[0].tags
		`"tags":["x"]`,       // list[1].tags untouched
		`"k":[]`,             // map value emptied (the crash's shape)
		`"raw":null`,         // []byte deliberately left as null
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %s in output; got %s", want, s)
		}
	}
	// The ONLY null must be the []byte; no array field may serialize as null.
	if strings.Count(s, ":null") != 1 {
		t.Errorf("an array field serialized as null (only raw []byte should): %s", s)
	}
}

// TestWriteJSONNilSliceTopLevel covers a handler that returns a bare nil slice.
func TestWriteJSONNilSliceTopLevel(t *testing.T) {
	rec := httptest.NewRecorder()
	var s []string // nil
	writeJSON(rec, 200, s)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("nil-slice response body = %q, want []", got)
	}
}
