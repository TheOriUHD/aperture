// What /system/status says about a camera's recording. Only "failed" is a problem:
// a camera outside its schedule is idle by design and must never look broken.
export type RecordingState = "recording" | "notScheduled" | "off" | "failed";

export type Level = "ok" | "idle" | "warn" | "down";

export function recordingDisplay(state: RecordingState): { level: Level; label: string; title: string } {
  switch (state) {
    case "recording":
      return { level: "ok", label: "rec", title: "Recording" };
    case "notScheduled":
      return { level: "idle", label: "idle", title: "Not scheduled to record right now" };
    case "off":
      return { level: "warn", label: "off", title: "Recording is off for this camera" };
    case "failed":
      return { level: "down", label: "rec", title: "Should be recording, but no footage is arriving" };
  }
}
