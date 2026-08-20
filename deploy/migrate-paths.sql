-- Aperture: rewrite container-era absolute paths to the native layout.
--
-- WHY: segments.path is ABSOLUTE for local storage (internal/store/segments.go:14) and
-- absolute-wins at every read site. Moving the recordings root without this leaves ~78k rows
-- pointing at paths that no longer exist: playback and thumbnails 404, AND the janitor's
-- os.Remove silently no-ops so the disk fills with no error. Only two prefixes exist:
-- '/recordings/' (12 chars -> substr from 13) and '/data/' (6 chars -> substr from 7).
--
-- Prefix-anchored substr, never replace(): a global replace would corrupt any path that
-- contains the prefix mid-string and would mangle backend-relative mirror rows.
-- Every statement is guarded by LIKE, so re-running is a no-op (idempotent).
--
-- RUN WITH THE DAEMON STOPPED. Back up aperture.db first.
BEGIN;

UPDATE segments        SET path          = '/var/lib/aperture/recordings/' || substr(path, 13)          WHERE path          LIKE '/recordings/%';
UPDATE segment_copies  SET path          = '/var/lib/aperture/recordings/' || substr(path, 13)          WHERE path          LIKE '/recordings/%';
UPDATE events          SET thumb_path    = '/var/lib/aperture/recordings/' || substr(thumb_path, 13)    WHERE thumb_path    LIKE '/recordings/%';
UPDATE events          SET snapshot_path = '/var/lib/aperture/recordings/' || substr(snapshot_path, 13) WHERE snapshot_path LIKE '/recordings/%';
UPDATE export_jobs     SET output_path   = '/var/lib/aperture/'            || substr(output_path, 7)    WHERE output_path   LIKE '/data/%';

-- Operator override of the recordings root; if it equals the new default we drop it entirely
-- rather than pin a redundant absolute path.
UPDATE settings SET value = '/var/lib/aperture/recordings' WHERE key = 'recordings_path' AND value = '/recordings';
DELETE FROM settings WHERE key = 'recordings_path' AND value = '/var/lib/aperture/recordings';

-- Local storage locations embed an absolute path in their JSON config. SMB/SFTP rows are
-- left alone: their paths are backend-relative and their credentials are GCM-encrypted.
UPDATE storage_locations
   SET config_json = json_set(config_json, '$.path',
         '/var/lib/aperture/recordings' || substr(json_extract(config_json, '$.path'), 12))
 WHERE type = 'local' AND json_extract(config_json, '$.path') LIKE '/recordings%';

COMMIT;
