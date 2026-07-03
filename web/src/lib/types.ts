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
}

export interface MotionEvent {
  id: string;
  cameraId: string;
  type: string;
  startTs: number;
  endTs: number;
  ongoing: boolean;
  score: number;
  thumbUrl?: string;
  reviewed: boolean;
  saved: boolean;
  conditions?: EventConditions;
  topLabel?: string;
  objectSummary?: string;
  objects?: DetectedObject[];
}
