package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRecordQualityIsPerCameraAndOptional pins the API side of recording quality:
// it round-trips, defaults to "as the camera sends it", and rejects nonsense without
// saving anything.
func TestRecordQualityIsPerCameraAndOptional(t *testing.T) {
	s, admin, _ := newCredTest(t)
	put := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return s.reqBody("PUT", "/api/v1/cameras/cam1", admin, strings.NewReader(body))
	}
	var got struct {
		RecordHeight int `json:"recordHeight"`
		RecordFps    int `json:"recordFps"`
		RecordKbps   int `json:"recordKbps"`
	}
	read := func() {
		t.Helper()
		mustDo(t, json.Unmarshal(s.reqAs("GET", "/api/v1/cameras/cam1", admin).Body.Bytes(), &got))
	}

	read()
	if got.RecordHeight != 0 || got.RecordFps != 0 || got.RecordKbps != 0 {
		t.Fatalf("a camera must start on the untouched stream: %+v", got)
	}
	if rec := put(`{"recordHeight":1080,"recordFps":15,"recordKbps":2000}`); rec.Code != http.StatusOK {
		t.Fatalf("set quality: %d %s", rec.Code, rec.Body.String())
	}
	read()
	if got.RecordHeight != 1080 || got.RecordFps != 15 || got.RecordKbps != 2000 {
		t.Fatalf("quality did not round-trip: %+v", got)
	}
	// An unrelated save leaves it alone; 0 goes back to the original stream.
	if rec := put(`{"name":"Renamed"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename: %d", rec.Code)
	}
	read()
	if got.RecordHeight != 1080 {
		t.Fatalf("an unrelated save reset the quality: %+v", got)
	}
	for _, body := range []string{`{"recordHeight":5}`, `{"recordFps":500}`, `{"recordKbps":-1}`} {
		if rec := put(body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, rec.Code)
		}
	}
	read()
	if got.RecordHeight != 1080 || got.RecordFps != 15 || got.RecordKbps != 2000 {
		t.Fatalf("a refused save changed the quality: %+v", got)
	}
	if rec := put(`{"recordHeight":0,"recordFps":0,"recordKbps":0}`); rec.Code != http.StatusOK {
		t.Fatalf("back to original: %d", rec.Code)
	}
	read()
	if got.RecordHeight != 0 || got.RecordFps != 0 || got.RecordKbps != 0 {
		t.Fatalf("0 should clear the quality: %+v", got)
	}
}
