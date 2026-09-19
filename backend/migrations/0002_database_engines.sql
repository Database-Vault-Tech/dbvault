-- Multiple database engines: every protected database records its engine.
-- Existing rows are PostgreSQL. SSL mode values differ per engine, so they
-- are validated by each engine's driver instead of a PostgreSQL-only CHECK.

ALTER TABLE databases
    ADD COLUMN engine text NOT NULL DEFAULT 'postgres'
        CHECK (engine IN ('postgres', 'mysql', 'mariadb', 'sqlserver', 'sqlite'));

ALTER TABLE databases DROP CONSTRAINT IF EXISTS databases_ssl_mode_check;
