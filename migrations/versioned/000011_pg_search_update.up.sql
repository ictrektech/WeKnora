-- Migration 000011: Update pg_search extension to latest version
-- Equivalent to: psql -c 'ALTER EXTENSION pg_search UPDATE;'

DO $$
BEGIN
    IF current_setting('app.skip_embedding', true) = 'true' THEN
        RAISE NOTICE 'Skipping pg_search update (app.skip_embedding=true)';
        RETURN;
    END IF;

    BEGIN
        ALTER EXTENSION pg_search UPDATE;
    EXCEPTION
        WHEN insufficient_privilege THEN
            RAISE NOTICE 'Skipping pg_search extension update: current database user is not the extension owner';
    END;
END $$;
