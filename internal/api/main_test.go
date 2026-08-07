package api

import (
	"os"
	"testing"

	"github.com/TheOriUHD/aperture/internal/ext"
)

// TestMain lets the supervisor's spawn trampoline work inside the test binary. When
// the supervisor launches a backend extension it re-execs /proc/self/exe with the
// __ext-spawn subcommand to apply rlimits/oom before exec'ing Deno; in a test that
// self-exe IS this binary, so it must run the trampoline first. RunSpawnTrampoline
// is a no-op for a normal test invocation (arg[1] != the subcommand), so every other
// test is unaffected.
func TestMain(m *testing.M) {
	ext.RunSpawnTrampoline()
	os.Exit(m.Run())
}
