// Shapes returned by the extension-manager API (Pass 2). Kept in one place so the
// manager, the consent modal, and the permission renderer agree.

export type ExtSource = "store" | "sideload";
export type ExtState = "starting" | "running" | "stopped" | "failed";

/** One installed extension, as GET /api/ext returns it (the enriched extView). */
export interface ExtItem {
  id: string;
  name: string;
  version: string;
  source: ExtSource;
  author?: string;
  description?: string;
  enabled: boolean;
  pinned: boolean;
  modified: boolean;
  scopes: string[];
  cameraGrants: string[];
  consent: string[];
  requestsCameras: boolean;
  requestedCameras: string[]; // the manifest ceiling; [] = all
  requestsAll: boolean; // requested "all" (future-inclusive) vs a specific list
  cameraConsent: string; // honest one-liner for the camera section ("" when no request)
  hasSettings: boolean; // has a settings schema → the row shows a settings form
  hasBackend: boolean;
  hasUI: boolean;
  canPin: boolean;
  updatable: boolean;
  revertable?: boolean;
  icon?: string;
  state?: ExtState; // backend supervisor state (backend extensions only)
  reason?: string; // failure reason when state === "failed"
  installedAt: number;
  updatedAt: number;
}

// One field of an extension's settings schema (mirrors ext.SettingField).
export type SettingType = "text" | "secret" | "number" | "boolean" | "select";
export interface SettingField {
  key: string;
  label: string;
  type: SettingType;
  help?: string;
  placeholder?: string;
  required?: boolean;
  default?: unknown;
  options?: { value: string; label: string }[];
}

// GET /ext/{id}/settings — schema + current NON-secret values + which secrets are set.
export interface SettingsView {
  schema: SettingField[];
  values: Record<string, string>;
  secretSet: Record<string, boolean>;
}

/** Consent metadata returned by a stage step (install/url/store), pre-commit. */
export interface StageView {
  stagingId: string;
  id: string;
  name: string;
  version: string;
  author?: string;
  description?: string;
  source: ExtSource;
  hasBackend: boolean;
  hasUI: boolean;
  consent: string[];
  requestsCameras: boolean;
  requestedCameras: string[]; // the manifest ceiling; [] = all
  requestsAll: boolean;
  cameraConsent: string; // honest one-liner shown in the consent camera section
  hasSettings: boolean; // configured after install, from the extension's row
}

export interface StoreView {
  configured: boolean;
  extensions: StoreItem[];
}
export interface StoreItem {
  id: string;
  name: string;
  version: string;
  author?: string;
  description?: string;
  installed: boolean;
}

export interface UpdateInfo {
  available: boolean;
  currentVersion: string;
  latestVersion?: string;
  bundleUrl?: string;
  consent?: string[];
  permissionsChanged?: boolean;
}

export interface ExtNavItem {
  id: string;
  name: string;
  icon?: string;
}
