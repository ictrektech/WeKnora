CREATE INDEX vnote_sync_notes_deletions ON vnote_sync_notes(sync_root_id,status,next_attempt_at,id) WHERE operation='delete';
