import { afterEach, describe, expect, it } from "vitest";
import { escapeStack } from "./escapeStack";

// The stacking contract, without a DOM: Escape is handled by the most recently
// registered overlay only, and unwinds correctly as overlays close.
describe("escapeStack — topmost-only dismissal", () => {
  afterEach(() => {
    escapeStack.handlers.length = 0;
  });

  it("dispatches to the most recently pushed handler only", () => {
    const calls: string[] = [];
    escapeStack.push(() => calls.push("marketplace"));
    escapeStack.push(() => calls.push("consent"));
    expect(escapeStack.dispatch()).toBe(true);
    expect(calls).toEqual(["consent"]); // only the top, not both
  });

  it("falls back to the layer beneath once the top unregisters", () => {
    const calls: string[] = [];
    const closeMarketplace = escapeStack.push(() => calls.push("marketplace"));
    const closeConsent = escapeStack.push(() => calls.push("consent"));

    escapeStack.dispatch(); // closes consent
    closeConsent();
    escapeStack.dispatch(); // now the marketplace is topmost
    expect(calls).toEqual(["consent", "marketplace"]);

    closeMarketplace();
    expect(escapeStack.dispatch()).toBe(false); // nothing left to handle
  });

  it("removes the right instance even with identical-looking handlers", () => {
    const calls: string[] = [];
    const a = () => calls.push("a");
    const b = () => calls.push("b");
    const removeA = escapeStack.push(a);
    escapeStack.push(b);
    removeA(); // remove the lower one; b stays on top
    escapeStack.dispatch();
    expect(calls).toEqual(["b"]);
    expect(escapeStack.handlers).toHaveLength(1);
  });
});
