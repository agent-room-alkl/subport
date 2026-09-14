-- Create the subport database (run once, connected to the "postgres" admin DB).
-- Postgres has no "CREATE DATABASE IF NOT EXISTS", so this guards with \gexec.
--
--   psql "host=<host> user=<admin> dbname=postgres sslmode=require" -f 00_create_database.sql
--
-- On Azure Database for PostgreSQL — Flexible Server the admin user can create
-- databases directly. Then apply schema.sql + seed.sql against the new DB.

SELECT 'CREATE DATABASE subport'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'subport')\gexec
