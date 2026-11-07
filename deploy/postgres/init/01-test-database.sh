#!/bin/sh
# Runs once, when the data volume is first created. Adds the throwaway
# database used by `go test` (DBSRV_TEST_DSN); the tests drop and recreate
# its schema on every run, so never point them at the real database.
set -e
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
	CREATE DATABASE dbsrv_test OWNER "$POSTGRES_USER";
EOSQL
