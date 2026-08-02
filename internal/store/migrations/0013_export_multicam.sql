-- Multi-camera export: a job can now cover several cameras, produced either as a
-- .zip of one clip each ("zip") or a single composited multi-view ("composite").
-- Legacy single-camera jobs keep camera_id set, cameras empty, mode 'single'.

ALTER TABLE export_jobs ADD COLUMN cameras TEXT NOT NULL DEFAULT '';
ALTER TABLE export_jobs ADD COLUMN mode TEXT NOT NULL DEFAULT 'single';
