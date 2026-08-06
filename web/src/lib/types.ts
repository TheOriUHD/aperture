export type Role = "admin" | "viewer";

export interface Preferences {
  clock24: boolean;
  landing: "dashboard" | "playback" | "alarms" | "cameras";
  timelineZoom: number;
  density: "comfortable" | "compact";
  language: string; // "en" | "de" | "auto"
  timelineRibbon: boolean; // show the temperature/precip ribbon on the timeline
}

export const DEFAULT_PREFS: Preferences = {
  clock24: true,
  landing: "dashboard",
  timelineZoom: 8,
  density: "comfortable",
  language: "auto",
  timelineRibbon: false,
};

export interface User {
  id: string;
  email: string;
  name: string;
  role: Role;
  preferences?: Partial<Preferences>;
  mustChangePassword?: boolean;
  twoFactor?: boolean;
  passkeys?: number;
}

export type RecordMode = "never" | "continuous" | "motion" | "scheduled";

export interface Camera {
  id: string;
  name: string;
  rtspMainUrl: string;
  rtspSubUrl: string;
  enabled: boolean;
  recordMode: RecordMode;
  retentionDays: number;
  storageQuotaBytes: number;
  motionThreshold: number;
  motionDetection: boolean;
  audioDetection: boolean;
  audioThreshold: number; // 0..1 loudness; 0 = built-in default
  locationId: string; // storage location for new footage ("" = default)
  detectMethod: string; // "" = inherit global; off|bridge|onnx
  tempSensor: string; // HA sensor entity id for the overlay ("" = weather)
  fps: number;
  sortOrder: number;
  prerollMs: number;
  postrollMs: number;
  cooldownMs: number;
  minDurationMs: number;
}

export interface StreamToken {
  token: string;
  wsUrl: string;
  hasSub?: boolean; // a low-quality sub stream is available for this camera
}

export interface EventConditions {
  tempC?: number;
  conditionCode?: number;
  precipMm?: number;
  windSpeedMs?: number;
  isNight?: boolean;
}

export interface DetectedObject {
  label: string;
  confidence: number;
  x: number;
  y: number;
  w: number;
  h: number;
  ts?: number;
}

export interface MotionEvent {
  id: string;
  cameraId: string;
  type: string;
  startTs: number;
  endTs: number; // legacy: `now` while ongoing (kept until v2)
  endTsMs?: number | null; // v1: null while ongoing (bug 4.6). Prefer this; fall back to endTs.
  ongoing: boolean;
  score: number;
  thumbUrl?: string;
  reviewed: boolean;
  saved: boolean;
  conditions?: EventConditions;
  topLabel?: string;
  objectSummary?: string;
  objects?: DetectedObject[];
  motionCells?: number[]; // active cells in a 32×18 grid — for Grid Search
}

// The coarse motion-map grid stored per event (must match motion.SearchW/H).
export const MOTION_GRID_W = 32;
export const MOTION_GRID_H = 18;
