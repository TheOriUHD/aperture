-- Timelapse export: a job can be rendered sped-up N× ("speed"). 0 or 1 = realtime
-- (stream-copy as before); N > 1 forces a libx264 transcode with setpts=PTS/N and
-- drops audio. Realtime jobs are unaffected.

ALTER TABLE export_jobs ADD COLUMN speed INTEGER NOT NULL DEFAULT 0;
