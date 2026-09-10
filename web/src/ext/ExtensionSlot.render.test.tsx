// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";

vi.mock("../lib/api", () => ({
  api: { get: vi.fn(async () => ({})), post: vi.fn(async () => ({})), put: vi.fn(async () => ({})), del: vi.fn(async () => ({})) },
  ApiError: class ApiError extends Error {},
}));

import { ExtensionPanel } from "./ExtensionSlot";
import type { ExtPanel } from "./panels";

const panel: ExtPanel = {
  extId: "com.example.demo",
  extName: "Demo",
  slot: "player-osd",
  entry: "ui/widget.html",
  title: "Demo widget",
} as ExtPanel;

// Deliver a resize the way the SDK does: a postMessage whose source is the panel's
// own iframe (the host ignores anything else).
function askForHeight(frame: HTMLIFrameElement, height: unknown) {
  fireEvent(
    window,
    new MessageEvent("message", {
      source: frame.contentWindow,
      data: { __apReq: true, type: "ui.resize", payload: { height } },
    }),
  );
}

afterEach(cleanup);

describe("ExtensionPanel overlay sizing", () => {
  it("honours an explicit zero instead of coercing it to a default height", () => {
    // The regression: `Number(h) || 180` treated a legitimate 0 as missing, because
    // 0 is falsy. A conditional OSD that reported "nothing to show" was rendered as
    // a 180px frame anyway — an empty box sitting on top of the video.
    const { container } = render(<ExtensionPanel panel={panel} chrome="bare" initialHeight={168} />);
    const frame = container.querySelector("iframe") as HTMLIFrameElement;
    expect(frame).toBeTruthy();
    expect(frame.style.height).toBe("0px"); // overlays start collapsed

    askForHeight(frame, 140);
    expect(frame.style.height).toBe("140px");

    askForHeight(frame, 0);
    expect(frame.style.height).toBe("0px"); // was "180px" before the fix
  });

  it("keeps a collapsed overlay laid out so the panel can still measure itself", () => {
    // display:none suppresses layout inside the frame, so documentElement.scrollHeight
    // there is pinned at 0 and a panel hidden that way can never size itself back
    // into view. Collapsing by height keeps the frame measurable while painting nothing.
    const { container } = render(<ExtensionPanel panel={panel} chrome="bare" />);
    const frame = container.querySelector("iframe") as HTMLIFrameElement;
    expect(frame.style.height).toBe("0px");
    expect(frame.style.display).toBe("block");

    askForHeight(frame, 96); // the panel measures itself and comes back
    expect(frame.style.height).toBe("96px");
    expect(frame.style.display).toBe("block");
  });

  it("still falls back for a non-numeric height, and clamps a card panel's minimum", () => {
    const { container } = render(<ExtensionPanel panel={panel} chrome="card" initialHeight={180} />);
    const frame = container.querySelector("iframe") as HTMLIFrameElement;

    askForHeight(frame, "not-a-number");
    expect(frame.style.height).toBe("180px");

    // A card is never collapsed to nothing — there the empty frame reads as loading.
    askForHeight(frame, 0);
    expect(frame.style.height).toBe("80px");
  });

  it("ignores a resize that did not come from this panel's iframe", () => {
    const { container } = render(<ExtensionPanel panel={panel} chrome="bare" />);
    const frame = container.querySelector("iframe") as HTMLIFrameElement;
    askForHeight(frame, 120);
    expect(frame.style.height).toBe("120px");

    fireEvent(
      window,
      new MessageEvent("message", {
        source: window, // not the iframe
        data: { __apReq: true, type: "ui.resize", payload: { height: 999 } },
      }),
    );
    expect(frame.style.height).toBe("120px");
  });
});

describe("ExtensionPanel overlay width negotiation", () => {
  function askFor(frame: HTMLIFrameElement, payload: Record<string, unknown>) {
    fireEvent(
      window,
      new MessageEvent("message", {
        source: frame.contentWindow,
        data: { __apReq: true, type: "ui.resize", payload },
      }),
    );
  }

  it("lets an overlay shrink its width, so a collapsed widget stops covering the video", () => {
    // A widget that collapses to a small button must give the width back too.
    // Otherwise its reserved column stays over the picture, invisible but still
    // catching clicks meant for the video.
    const { container } = render(<ExtensionPanel panel={panel} chrome="bare" width={168} />);
    const frame = container.querySelector("iframe") as HTMLIFrameElement;
    expect(frame.style.width).toBe("168px");

    askFor(frame, { height: 32, width: 32 });
    expect(frame.style.width).toBe("32px");
    expect(frame.style.height).toBe("32px");

    askFor(frame, { height: 210, width: 176 });
    expect(frame.style.width).toBe("176px");
    expect(frame.style.height).toBe("210px");
  });

  it("leaves a negotiated width alone when a later resize reports height only", () => {
    // The SDK's automatic sizing reports height by itself. That must not reset a
    // width the panel deliberately chose.
    const { container } = render(<ExtensionPanel panel={panel} chrome="bare" width={168} />);
    const frame = container.querySelector("iframe") as HTMLIFrameElement;
    askFor(frame, { height: 32, width: 32 });
    expect(frame.style.width).toBe("32px");

    askFor(frame, { height: 96 }); // height-only, as the ResizeObserver sends
    expect(frame.style.width).toBe("32px");
    expect(frame.style.height).toBe("96px");
  });

  it("ignores a nonsense width", () => {
    const { container } = render(<ExtensionPanel panel={panel} chrome="bare" width={168} />);
    const frame = container.querySelector("iframe") as HTMLIFrameElement;
    askFor(frame, { height: 40, width: 0 });
    expect(frame.style.width).toBe("168px");
    askFor(frame, { height: 40, width: "wide" });
    expect(frame.style.width).toBe("168px");
  });

  it("does not let a card panel negotiate width (the host owns card layout)", () => {
    const { container } = render(<ExtensionPanel panel={panel} chrome="card" />);
    const frame = container.querySelector("iframe") as HTMLIFrameElement;
    askFor(frame, { height: 120, width: 40 });
    expect(frame.style.width).toBe("100%");
  });
});
