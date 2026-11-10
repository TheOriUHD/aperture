package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHardwareProfileRoundTrip(t *testing.T) {
	s := newTestServer(t)
	admin := cred{"cli", bearerCred(testCLIToken)}

	// Before anyone sets it there is no profile, but detection is always there.
	rec := s.reqAs(http.MethodGet, "/api/v1/system/hardware", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Profile  *hardwareProfile `json:"profile"`
		Detected struct {
			LogicalCPUs int `json:"logicalCpus"`
		} `json:"detected"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &got))
	if got.Profile != nil {
		t.Fatalf("fresh install has a profile: %+v", got.Profile)
	}
	if got.Detected.LogicalCPUs < 1 {
		t.Fatalf("detection missing: %s", rec.Body.String())
	}

	body := `{"cpuId":"amd-ryzen-7-5800x","cpuName":"AMD Ryzen 7 5800X","threads":8,"ramGb":6,"nicGbps":1,
		"accelerator":{"id":"coral-usb","name":"Google Coral USB Accelerator"}}`
	rec = s.reqBody(http.MethodPut, "/api/v1/system/hardware", admin, strings.NewReader(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	rec = s.reqAs(http.MethodGet, "/api/v1/system/hardware", admin)
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &got))
	p := got.Profile
	if p == nil || p.CPUID != "amd-ryzen-7-5800x" || p.Threads != 8 || p.RAMGB != 6 || p.NICGbps != 1 ||
		p.Accelerator == nil || p.Accelerator.ID != "coral-usb" || p.UpdatedAt == 0 {
		t.Fatalf("stored profile: %+v", p)
	}
}

func TestHardwareProfileRejectsNonsense(t *testing.T) {
	s := newTestServer(t)
	admin := cred{"cli", bearerCred(testCLIToken)}
	for name, body := range map[string]string{
		"no cpu":        `{"cpuId":"","cpuName":"x","threads":4,"ramGb":8,"nicGbps":1}`,
		"bad cpu id":    `{"cpuId":"Robert'); DROP","cpuName":"x","threads":4,"ramGb":8,"nicGbps":1}`,
		"zero threads":  `{"cpuId":"intel-n100","cpuName":"Intel N100","threads":0,"ramGb":8,"nicGbps":1}`,
		"tiny ram":      `{"cpuId":"intel-n100","cpuName":"Intel N100","threads":4,"ramGb":0.1,"nicGbps":1}`,
		"no nic":        `{"cpuId":"intel-n100","cpuName":"Intel N100","threads":4,"ramGb":8,"nicGbps":0}`,
		"bad accel":     `{"cpuId":"intel-n100","cpuName":"Intel N100","threads":4,"ramGb":8,"nicGbps":1,"accelerator":{"id":"","name":""}}`,
		"unknown field": `{"cpuId":"intel-n100","cpuName":"Intel N100","threads":4,"ramGb":8,"nicGbps":1,"gpu":"x"}`,
	} {
		rec := s.reqBody(http.MethodPut, "/api/v1/system/hardware", admin, strings.NewReader(body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	if s.hardwareProfile() != nil {
		t.Fatal("a rejected profile was stored")
	}
}
