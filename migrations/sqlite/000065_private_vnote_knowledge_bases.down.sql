DROP TABLE IF EXISTS vnote_import_gates;
DROP TABLE IF EXISTS vnote_import_states;
DROP INDEX IF EXISTS idx_private_kb_owner;
ALTER TABLE knowledge_bases DROP COLUMN v_note_root_id;
ALTER TABLE knowledge_bases DROP COLUMN private_owner_id;
