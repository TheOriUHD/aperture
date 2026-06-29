-- Notification rules gain optional environmental conditions (time-of-day,
-- precipitation) evaluated against the context layer at event time. Stored as a
-- small JSON blob so the condition set can grow without further migrations.
ALTER TABLE notify_rules ADD COLUMN conditions TEXT NOT NULL DEFAULT '';
