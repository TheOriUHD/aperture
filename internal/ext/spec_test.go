package ext

import (
	"strings"
	"testing"
)

func TestBuildSpecDenyByDefault(t *testing.T) {
	d := Derived{NetHosts: []string{"homeassistant.local:8123"}}
	sp := BuildSpec("com.x", "/data/ext/com.x/backend/main.ts", "/data/ext/com.x",
		"/opt/sdk/mod.ts", "/data/ext/com.x/.import-map.json", "http://localhost:8080", "apt_tok", 65600, d)

	f := strings.Join(sp.DenoFlags, " ")
	for _, must := range []string{
		"--deny-run", "--deny-ffi", "--deny-sys", "--deny-import",
		"--allow-env=APERTURE_TOKEN,APERTURE_API", // exactly the two the SDK reads; no host env
		"--allow-read=/data/ext/com.x", "--allow-write=/data/ext/com.x", "--import-map=",
		"homeassistant.local:8123", // declared host
		"localhost:8080",           // the API host, injected so the SDK can reach it
	} {
		if !strings.Contains(f, must) {
			t.Errorf("flags missing %q\n  in: %s", must, f)
		}
	}
	// Env must NOT be denied wholesale (that would break the SDK's token read), but
	// it must also not be blanket-allowed — only the two SDK vars.
	if strings.Contains(f, "--deny-env") || strings.Contains(f, "--allow-env ") || strings.HasSuffix(f, "--allow-env") {
		t.Errorf("env flag should be the scoped allow-list, got: %s", f)
	}
	if sp.UID != 65600 || sp.GID != 65600 || sp.MemMB != DefaultMemMB || sp.OOMScoreAdj != DefaultOOMScoreAdj {
		t.Errorf("identity/limits: %+v", sp)
	}
	env := strings.Join(sp.Env, " ")
	if !strings.Contains(env, "APERTURE_TOKEN=apt_tok") || !strings.Contains(env, "APERTURE_API=http://localhost:8080") {
		t.Errorf("env missing token/api: %v", sp.Env)
	}
	// No network permission → --deny-net (still reaches the API host though).
	sp2 := BuildSpec("com.y", "/e/m.ts", "/e", "/opt/sdk/mod.ts", "/e/.im.json", "", "t", 1, Derived{})
	if !strings.Contains(strings.Join(sp2.DenoFlags, " "), "--deny-net") {
		t.Errorf("no hosts + no api → --deny-net expected: %v", sp2.DenoFlags)
	}
}
