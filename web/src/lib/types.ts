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
