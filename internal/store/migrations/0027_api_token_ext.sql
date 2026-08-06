-- 0027: link an API token to an owning extension (Extension System, T1.4).
-- Additive column with a constant default (metadata-only, no table rewrite). A
-- backend extension's scoped token carries its extension id so the whole set can
-- be revoked in one statement when the extension is disabled or uninstalled.
-- Empty = an ordinary (non-extension) PAT.
ALTER TABLE api_tokens ADD COLUMN ext_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_api_tokens_ext ON api_tokens (ext_id);
