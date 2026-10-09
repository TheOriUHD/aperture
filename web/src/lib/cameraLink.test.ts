import { describe, expect, it } from "vitest";
import { hasLoginMask, linkHost, maskedLoginProblem } from "./cameraLink";

const shown = "rtsp://•••••:•••••@192.168.10.38/Preview_01_main";

describe("linkHost", () => {
  it("splits like the server", () => {
    expect(linkHost("rtsp://•••••:•••••@192.168.10.37:554/Preview_01_main")).toBe("192.168.10.37");
    expect(linkHost("rtsp://admin:p@ss@CAM.local/x#video=copy")).toBe("cam.local");
    expect(linkHost("rtsp://u:p@[fe80::1]:554/x")).toBe("fe80::1");
    expect(linkHost("rtsp://10.0.0.5/a@b")).toBe("10.0.0.5");
    expect(linkHost("not a link")).toBe("");
  });
});

describe("maskedLoginProblem", () => {
  it("accepts the link as shown, or with only the path or port changed", () => {
    expect(maskedLoginProblem(shown, [shown, ""])).toBe("");
    expect(maskedLoginProblem("rtsp://•••••:•••••@192.168.10.38:554/Preview_01_sub", [shown])).toBe("");
  });
  it("accepts a sub link pasted from the main one", () => {
    expect(maskedLoginProblem("rtsp://•••••:•••••@192.168.10.38/Preview_01_sub", ["", shown])).toBe("");
  });
  it("refuses the saved login on a new address", () => {
    expect(maskedLoginProblem("rtsp://•••••:•••••@192.168.10.99/Preview_01_main", [shown])).toMatch(/New address/);
  });
  it("asks for a login when none is saved", () => {
    expect(maskedLoginProblem("rtsp://•••••:•••••@192.168.10.38/x", [])).toMatch(/Replace the dots/);
  });
  it("ignores links without the placeholder", () => {
    expect(hasLoginMask("rtsp://admin:new@192.168.10.99/x")).toBe(false);
    expect(maskedLoginProblem("rtsp://admin:new@192.168.10.99/x", [shown])).toBe("");
  });
});
