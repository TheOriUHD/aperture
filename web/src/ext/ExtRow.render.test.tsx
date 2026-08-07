// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// Keep every query/mutation off the network and deterministic.
vi.mock("../lib/api", () => ({
  api: {
    get: vi.fn(async (path: string) => {
      if (path === "/api/cameras") return [{ id: "a", name: "Cam A" }, { id: "b", name: "Cam B" }];
      if (path.endsWith("/logs")) return { lines: [] };
      return [];
    }),
    post: vi.fn(async () => ({})),
    put: vi.fn(async () => ({})),
    del: vi.fn(async () => ({})),
  },
  uploadFile: vi.fn(async () => ({})),
  ApiError: class ApiError extends Error {},
}));

import { ExtRow } from "./ExtRow";
import { ErrorBoundary } from "../components/ui/ErrorBoundary";
import { useExtActions } from "./useExtensions";
import type { ExtItem } from "./types";

function makeExt(over: Partial<ExtItem> = {}): ExtItem {
  return {
    id: "com.x",
    name: "Test Ext",
    version: "1.0.0",
    source: "sideload",
    author: "Author",
    description: "desc",
    enabled: true,
    pinned: false,
    modified: false,
    scopes: [],
    cameraGrants: [],
    consent: ["It cannot read your recordings files, your database, or your passwords."],
    requestsCameras: false,
    requestedCameras: [],
    hasBackend: false,
    hasUI: true,
    canPin: false,
    updatable: false,
    installedAt: 0,
    updatedAt: 0,
    ...over,
  };
}

function renderRow(ext: ExtItem) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Harness() {
    const actions = useExtActions();
    return <ExtRow ext={ext} actions={actions} onUpdate={() => {}} onEdit={() => {}} onUninstall={() => {}} />;
  }
  return render(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
}

const expand = () => fireEvent.click(screen.getByText("Test Ext"));

describe("ExtRow render", () => {
  afterEach(cleanup);

  it("renders collapsed", () => {
    renderRow(makeExt());
    expect(screen.getByText("Test Ext")).toBeTruthy();
  });

  it("expands a normal UI-only extension", () => {
    renderRow(makeExt());
    expand();
    expect(screen.getByText("Permissions")).toBeTruthy();
    expect(screen.queryByText("Logs")).toBeNull(); // no backend → no logs
  });

  // The regression: the API returns null for a nil Go slice, so an all-cameras
  // extension arrives with cameraGrants + requestedCameras === null. Expanding it
  // must not throw.
  it("expands an all-cameras extension whose grant arrays are null", () => {
    const ext = makeExt({
      requestsCameras: true,
      requestedCameras: null as unknown as string[],
      cameraGrants: null as unknown as string[],
    });
    renderRow(ext);
    expand();
    expect(screen.getByText("Cameras")).toBeTruthy();
  });

  it("expands a crash-looping backend with empty logs and no icon", () => {
    renderRow(makeExt({ hasBackend: true, state: "failed", reason: "keeps crashing", icon: undefined }));
    expand();
    expect(screen.getByText("Logs")).toBeTruthy();
    expect(screen.getByText(/Needs attention/)).toBeTruthy();
  });

  it("expands a camera-scoped extension with a specific request", () => {
    renderRow(makeExt({ requestsCameras: true, requestedCameras: ["a"], cameraGrants: ["a"] }));
    expand();
    expect(screen.getByText("Cameras")).toBeTruthy();
  });

  // The safety net: even an unforeseen render throw in one row shows the fallback
  // rather than unmounting the manager.
  it("an error boundary contains a throwing row", () => {
    const Boom = (): JSX.Element => {
      throw new Error("boom");
    };
    render(
      <ErrorBoundary fallback={() => <div>fallback shown</div>}>
        <Boom />
      </ErrorBoundary>,
    );
    expect(screen.getByText("fallback shown")).toBeTruthy();
  });
});
