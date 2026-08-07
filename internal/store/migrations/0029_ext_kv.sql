-- Per-extension private key/value storage (the `storage: true` manifest permission).
-- Namespaced by ext_id with no cross-extension access enforced in the API layer;
-- a per-extension row + byte quota is enforced in Go; rows are destroyed on
-- uninstall (the lifecycle table's "ext_kv: destroyed"). Kept OUT of the flat
-- settings namespace so an extension can never read or clobber another subsystem's
-- config, and vice versa.
CREATE TABLE IF NOT EXISTS ext_kv (
    ext_id     TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (ext_id, key)
);
