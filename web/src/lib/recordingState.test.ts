import { describe, expect, it } from "vitest";
import { recordingDisplay } from "./recordingState";

describe("recordingDisplay", () => {
  it("only shows red for a real failure", () => {
    expect(recordingDisplay("failed").level).toBe("down");
    for (const state of ["recording", "notScheduled", "off"] as const) {
      expect(recordingDisplay(state).level).not.toBe("down");
    }
  });

  it("shows a camera outside its schedule as idle, not off or broken", () => {
    expect(recordingDisplay("notScheduled")).toMatchObject({ level: "idle", label: "idle" });
  });
});
