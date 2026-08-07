import { defineConfig } from "vitest/config";

// Node-environment unit tests only (no DOM). The extension host's identity
// guarantee — "zero extensions ⇒ every first-party surface is unchanged" — is
// pinned at the data layer (panelsForSlot), which needs no browser.
export default defineConfig({
  test: {
    // Node by default (pure logic); component tests opt into jsdom per-file with a
    // `// @vitest-environment jsdom` directive.
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
