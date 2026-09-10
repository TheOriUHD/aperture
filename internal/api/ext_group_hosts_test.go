package api

import (
	"testing"

	"github.com/TheOriUHD/aperture/internal/ext"
)

// resolveExtHosts must expand a group's url column into one allowed host per
// configured row — the egress an extension that drives N user-named devices needs.
// It must also report `unconfigured` honestly, so a half-filled table surfaces as
// "not configured yet" rather than silently narrowing what the backend may reach.
func TestResolveExtHostsFromGroupColumn(t *testing.T) {
	s := newTestServerNoRouter(t)
	const id = "com.example.multi"
	d := ext.Derived{NetSettings: []string{"cams.address"}}

	t.Run("unset group is unconfigured", func(t *testing.T) {
		hosts, unconfigured := s.resolveExtHosts(id, d)
		if len(hosts) != 0 || !unconfigured {
			t.Fatalf("hosts=%v unconfigured=%v, want none/true", hosts, unconfigured)
		}
	})

	t.Run("every row contributes its host", func(t *testing.T) {
		if err := s.st.ExtKVSet(id, "cams", `[
			{"camera":"a","address":"http://192.168.10.154:2020"},
			{"camera":"b","address":"http://192.168.10.9:2020"}]`); err != nil {
			t.Fatal(err)
		}
		hosts, unconfigured := s.resolveExtHosts(id, d)
		if unconfigured {
			t.Errorf("fully configured group reported unconfigured")
		}
		want := map[string]bool{"192.168.10.154:2020": true, "192.168.10.9:2020": true}
		if len(hosts) != 2 {
			t.Fatalf("hosts = %v, want 2", hosts)
		}
		for _, h := range hosts {
			if !want[h] {
				t.Errorf("unexpected host %q", h)
			}
		}
	})

	t.Run("a row with no address is unconfigured but does not drop the others", func(t *testing.T) {
		if err := s.st.ExtKVSet(id, "cams", `[
			{"camera":"a","address":"http://192.168.10.154:2020"},
			{"camera":"b","address":""}]`); err != nil {
			t.Fatal(err)
		}
		hosts, unconfigured := s.resolveExtHosts(id, d)
		if !unconfigured {
			t.Errorf("a blank row should report unconfigured")
		}
		if len(hosts) != 1 || hosts[0] != "192.168.10.154:2020" {
			t.Fatalf("hosts = %v, want just the configured row", hosts)
		}
	})

	t.Run("a host the user never configured is never allowed", func(t *testing.T) {
		if err := s.st.ExtKVSet(id, "cams", `[{"camera":"a","address":"http://192.168.10.154:2020"}]`); err != nil {
			t.Fatal(err)
		}
		hosts, _ := s.resolveExtHosts(id, d)
		if hostAllowed("192.168.10.99:2020", hosts) {
			t.Fatal("egress escaped the configured rows")
		}
		// Port is part of the identity: the same camera on another port is a different host.
		if hostAllowed("192.168.10.154:80", hosts) {
			t.Fatal("port was ignored when matching an allowed host")
		}
		if !hostAllowed("192.168.10.154:2020", hosts) {
			t.Fatal("the configured row should be allowed")
		}
	})

	t.Run("malformed group json is unconfigured, not a panic", func(t *testing.T) {
		if err := s.st.ExtKVSet(id, "cams", `not json`); err != nil {
			t.Fatal(err)
		}
		hosts, unconfigured := s.resolveExtHosts(id, d)
		if len(hosts) != 0 || !unconfigured {
			t.Fatalf("hosts=%v unconfigured=%v, want none/true", hosts, unconfigured)
		}
	})
}
