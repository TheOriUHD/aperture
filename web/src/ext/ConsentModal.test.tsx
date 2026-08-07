// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConsentModal } from "./ConsentModal";
import type { StageView } from "./types";

function baseStage(over: Partial<StageView> = {}): StageView {
  return {
    stagingId: "s1",
    id: "com.x",
    name: "X",
    version: "2.0.0",
    source: "sideload",
    hasBackend: false,
    hasUI: true,
    consent: ["It cannot read your recordings files, your database, or your passwords."],
    requestsCameras: false,
    requestedCameras: [],
    requestsAll: false,
    cameraConsent: "",
    hasSettings: false,
    ...over,
  };
}

function renderModal(stage: StageView, onConfirm = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <ConsentModal stage={stage} mode="update" onConfirm={onConfirm} onCancel={() => {}} />
    </QueryClientProvider>,
  );
  return onConfirm;
}

describe("ConsentModal — update", () => {
  afterEach(cleanup);

  it("reads as replacing a specific version, not a first install", () => {
    renderModal(baseStage({ update: true, currentVersion: "1.0.0" }));
    expect(screen.getByText("Review update")).toBeTruthy();
    const body = document.body.textContent ?? "";
    expect(body).toContain("Replacing");
    expect(body).toContain("version 1.0.0");
    expect(body).toContain("version 2.0.0");
    // No first-install-only "configure before it can run" hint.
    expect(body).not.toContain("before it can run");
  });

  it("requires an explicit acknowledgement for a downgrade", () => {
    const onConfirm = renderModal(baseStage({ update: true, currentVersion: "3.0.0", downgrade: true }));
    const btn = screen.getByRole("button", { name: /Downgrade to v2\.0\.0/ }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    fireEvent.click(screen.getByRole("checkbox"));
    expect(btn.disabled).toBe(false);
    fireEvent.click(btn);
    expect(onConfirm).toHaveBeenCalledWith(undefined, true);
  });

  it("flags newly requested permissions", () => {
    renderModal(baseStage({ update: true, currentVersion: "1.0.0", permissionsChanged: true }));
    expect(screen.getByText(/requests permissions the installed one didn't/)).toBeTruthy();
  });
});
