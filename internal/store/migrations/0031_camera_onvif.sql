-- Per-camera ONVIF client settings: Aperture talking TO the camera to read and
-- adjust its picture settings (brightness, contrast, day/night …).
--
-- Credentials are separate from the RTSP URL on purpose: a camera's streaming
-- account is often view-only over ONVIF, and changing settings needs an admin
-- account. Empty user/pass means "reuse whatever is embedded in the RTSP URL",
-- which is the zero-config path for cameras that allow it.
ALTER TABLE cameras ADD COLUMN onvif_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cameras ADD COLUMN onvif_port INTEGER NOT NULL DEFAULT 0;  -- 0 = auto-discover
ALTER TABLE cameras ADD COLUMN onvif_user TEXT NOT NULL DEFAULT '';
ALTER TABLE cameras ADD COLUMN onvif_pass_enc TEXT NOT NULL DEFAULT ''; -- encrypted at rest
