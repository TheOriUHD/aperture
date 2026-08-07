-- Pass 2 manager fields on the extensions row (additive, forward-only).
--   pinned   -- user granted this extension a nav-rail entry + full page (T2.1).
--            Never manifest-asserted: the manifest may DECLARE a nav-panel view,
--            but only the user decides it earns rail space. Off by default.
--   modified -- the user edited this extension's files in the in-app editor (T2.3),
--            so it no longer matches its installed bundle. Shown as a badge; blocks
--            "up to date" claims from the update checker.
ALTER TABLE extensions ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE extensions ADD COLUMN modified INTEGER NOT NULL DEFAULT 0;
