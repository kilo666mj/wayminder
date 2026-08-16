#!/bin/sh
set -eu

psql --set=ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=app_password="$WAYMINDER_DB_PASSWORD" <<-'SQL'
	CREATE EXTENSION IF NOT EXISTS vector;
	CREATE ROLE wayminder_app LOGIN PASSWORD :'app_password';
	GRANT CONNECT ON DATABASE wayminder TO wayminder_app;
	GRANT USAGE, CREATE ON SCHEMA public TO wayminder_app;
SQL
