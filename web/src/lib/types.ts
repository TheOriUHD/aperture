export type Role = "admin" | "viewer";

export interface User {
  id: string;
  email: string;
  name: string;
  role: Role;
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
}

export interface StreamToken {
  token: string;
  wsUrl: string;
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
}
