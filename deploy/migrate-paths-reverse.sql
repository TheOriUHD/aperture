-- Inverse of migrate-paths.sql, for rolling back to the Docker layout.
-- An untested rollback script is not a rollback plan: this is exercised in the rehearsal.
-- '/var/lib/aperture/recordings/' = 29 chars -> substr from 30
-- '/var/lib/aperture/'            = 18 chars -> substr from 19
BEGIN;
UPDATE segments        SET path          = '/recordings/' || substr(path, 30)          WHERE path          LIKE '/var/lib/aperture/recordings/%';
UPDATE segment_copies  SET path          = '/recordings/' || substr(path, 30)          WHERE path          LIKE '/var/lib/aperture/recordings/%';
UPDATE events          SET thumb_path    = '/recordings/' || substr(thumb_path, 30)    WHERE thumb_path    LIKE '/var/lib/aperture/recordings/%';
UPDATE events          SET snapshot_path = '/recordings/' || substr(snapshot_path, 30) WHERE snapshot_path LIKE '/var/lib/aperture/recordings/%';
UPDATE export_jobs     SET output_path   = '/data/'       || substr(output_path, 19)   WHERE output_path   LIKE '/var/lib/aperture/%';
UPDATE storage_locations
   SET config_json = json_set(config_json, '$.path',
         '/recordings' || substr(json_extract(config_json, '$.path'), 30))
 WHERE type = 'local' AND json_extract(config_json, '$.path') LIKE '/var/lib/aperture/recordings%';
COMMIT;
