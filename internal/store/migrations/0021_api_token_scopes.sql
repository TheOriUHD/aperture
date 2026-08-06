-- 0021: scoped API tokens. A token's authority is limited to these scopes and
-- (optionally) to these camera grants; empty scopes = no scopes, empty grants =
-- the owner's full ACL. Additive: both columns have a constant default, so this
-- is a metadata-only change (no table rewrite).
ALTER TABLE api_tokens ADD COLUMN scopes        TEXT NOT NULL DEFAULT '';
ALTER TABLE api_tokens ADD COLUMN camera_grants TEXT NOT NULL DEFAULT '';
