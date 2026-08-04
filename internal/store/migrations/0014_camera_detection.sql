-- Per-camera detection controls: an on/off toggle for the motion trigger, and a
-- per-camera override of the object-detection method (empty = inherit the global
-- Settings backend; off|bridge|onnx = force that method for this camera).
ALTER TABLE cameras ADD COLUMN motion_detection INTEGER NOT NULL DEFAULT 1;
ALTER TABLE cameras ADD COLUMN detect_method TEXT NOT NULL DEFAULT '';
