-- SQLite: a protected database can be a file inside the SQLITE_ROOT folder.
-- It has no host, port or login, so only network engines need a real port.

ALTER TABLE databases DROP CONSTRAINT IF EXISTS databases_port_check;
ALTER TABLE databases ADD CONSTRAINT databases_port_check
    CHECK (engine = 'sqlite' OR port BETWEEN 1 AND 65535);
