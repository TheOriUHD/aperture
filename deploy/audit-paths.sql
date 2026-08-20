-- Gate G2: must return 0. Any non-zero result after the rewrite means a column was missed,
-- or a code path is still WRITING legacy paths.
SELECT (SELECT count(*) FROM segments        WHERE path          LIKE '/recordings/%' OR path        LIKE '/data/%')
     + (SELECT count(*) FROM segment_copies  WHERE path          LIKE '/recordings/%' OR path        LIKE '/data/%')
     + (SELECT count(*) FROM events          WHERE thumb_path    LIKE '/recordings/%' OR snapshot_path LIKE '/recordings/%')
     + (SELECT count(*) FROM export_jobs     WHERE output_path   LIKE '/data/%')
     + (SELECT count(*) FROM settings        WHERE key='recordings_path' AND value LIKE '/recordings%')
     + (SELECT count(*) FROM storage_locations WHERE type='local' AND json_extract(config_json,'$.path') LIKE '/recordings%')
       AS legacy_paths_remaining;
