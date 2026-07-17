-- Migration: 000072_refresh_vivibit_builtin_agent_prompt (rollback)
-- Description: No-op rollback. The forward migration only refreshes legacy
-- builtin quick-answer prompt rows that still used the old upstream identity.
DO $$ BEGIN RAISE NOTICE '[Migration 000072 rollback] No-op'; END $$;
