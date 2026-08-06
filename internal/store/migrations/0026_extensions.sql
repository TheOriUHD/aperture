-- 0026: extensions (Extension System, Pass 1 T1.1) — the registry row for an
-- installed extension. Additive: a brand-new table, no rewrite of anything large.
-- The extension's FILES live on disk under DataDir/ext/<id>/ (never in the DB);
-- this row is the metadata the config backup captures and the supervisor/manager
-- reconcile against on-disk installs after a restore. scopes/camera_grants are CSV
-- (matching api_tokens); ext_uid is the per-extension non-root uid the backend
-- process is dropped to; source distinguishes a reviewed store item from a
-- sideload for the consent warning.
CREATE TABLE IF NOT EXISTS extensions (
    id            TEXT PRIMARY KEY,           -- reverse-DNS manifest id (immutable identity)
    name          TEXT NOT NULL,
    version       TEXT NOT NULL,              -- semver
    source        TEXT NOT NULL,              -- 'store' | 'sideload'
    update_url    TEXT NOT NULL DEFAULT '',   -- optional self-hosted update source
    manifest_json TEXT NOT NULL,              -- the full validated manifest
    scopes        TEXT NOT NULL DEFAULT '',   -- CSV of granted API scopes
    camera_grants TEXT NOT NULL DEFAULT '',   -- CSV of camera ids (composed as owner ∩ grants)
    enabled       INTEGER NOT NULL DEFAULT 0,
    ext_uid       INTEGER NOT NULL DEFAULT 0, -- per-extension non-root uid for the Deno backend
    installed_by  TEXT NOT NULL DEFAULT '',   -- user id that installed it (the token owner)
    installed_at  INTEGER NOT NULL,           -- unix ms
    updated_at    INTEGER NOT NULL            -- unix ms
);
