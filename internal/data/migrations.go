package data

import (
	"time"
)

// Simple forward-only migration runner. Scripts are embedded per dialect and
// tracked in a schema_migrations table.

const sqlite001 = `
CREATE TABLE users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user',
  created_at TEXT NOT NULL
);

CREATE TABLE domains (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  authority TEXT NOT NULL UNIQUE,
  base_url_redirect TEXT NULL,
  regular_404_redirect TEXT NULL,
  invalid_short_url_redirect TEXT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE TABLE short_urls (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  short_code TEXT NOT NULL,
  domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
  long_url TEXT NOT NULL,
  title TEXT NULL,
  title_was_auto_resolved INTEGER NOT NULL DEFAULT 0,
  redirect_status INTEGER NOT NULL DEFAULT 302,
  forward_query INTEGER NOT NULL DEFAULT 1,
  crawlable INTEGER NOT NULL DEFAULT 0,
  max_visits INTEGER NULL,
  valid_since TEXT NULL,
  valid_until TEXT NULL,
  author_user_id INTEGER NULL REFERENCES users(id) ON DELETE SET NULL,
  author_api_key_id INTEGER NULL,
  created_at TEXT NOT NULL,
  UNIQUE (domain_id, short_code)
);
CREATE INDEX idx_short_urls_code ON short_urls(short_code);

CREATE TABLE tags (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE short_url_tags (
  short_url_id INTEGER NOT NULL REFERENCES short_urls(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (short_url_id, tag_id)
);

CREATE TABLE redirect_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  short_url_id INTEGER NOT NULL REFERENCES short_urls(id) ON DELETE CASCADE,
  priority INTEGER NOT NULL,
  long_url TEXT NOT NULL
);

CREATE TABLE redirect_conditions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id INTEGER NOT NULL REFERENCES redirect_rules(id) ON DELETE CASCADE,
  cond_type TEXT NOT NULL,
  match_key TEXT NULL,
  match_value TEXT NOT NULL
);

CREATE TABLE visits (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  short_url_id INTEGER NULL REFERENCES short_urls(id) ON DELETE CASCADE,
  visit_type TEXT NOT NULL DEFAULT 'valid',
  visited_at TEXT NOT NULL,
  referer TEXT NULL,
  user_agent TEXT NULL,
  browser TEXT NULL,
  os TEXT NULL,
  device TEXT NULL,
  is_bot INTEGER NOT NULL DEFAULT 0,
  remote_ip TEXT NULL,
  country_code TEXT NULL,
  country_name TEXT NULL,
  city TEXT NULL,
  latitude REAL NULL,
  longitude REAL NULL,
  visited_url TEXT NULL,
  geo_resolved INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_visits_short_url ON visits(short_url_id, visited_at);
CREATE INDEX idx_visits_type ON visits(visit_type, visited_at);
CREATE INDEX idx_visits_geo_pending ON visits(geo_resolved) WHERE geo_resolved = 0;

CREATE TABLE api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  key_hash TEXT NOT NULL UNIQUE,
  name TEXT NULL,
  role TEXT NOT NULL DEFAULT 'admin',
  domain_id INTEGER NULL REFERENCES domains(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 1,
  expires_at TEXT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE webhooks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  secret TEXT NOT NULL,
  events TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);

CREATE TABLE webhook_deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  webhook_id INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  event TEXT NOT NULL,
  payload TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  last_error TEXT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_webhook_deliveries_due ON webhook_deliveries(status, next_attempt_at);
`

const postgres001 = `
CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE domains (
  id BIGSERIAL PRIMARY KEY,
  authority TEXT NOT NULL UNIQUE,
  base_url_redirect TEXT NULL,
  regular_404_redirect TEXT NULL,
  invalid_short_url_redirect TEXT NULL,
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE short_urls (
  id BIGSERIAL PRIMARY KEY,
  short_code TEXT NOT NULL,
  domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
  long_url TEXT NOT NULL,
  title TEXT NULL,
  title_was_auto_resolved BOOLEAN NOT NULL DEFAULT FALSE,
  redirect_status INT NOT NULL DEFAULT 302,
  forward_query BOOLEAN NOT NULL DEFAULT TRUE,
  crawlable BOOLEAN NOT NULL DEFAULT FALSE,
  max_visits BIGINT NULL,
  valid_since TIMESTAMPTZ NULL,
  valid_until TIMESTAMPTZ NULL,
  author_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
  author_api_key_id BIGINT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE (domain_id, short_code)
);
CREATE INDEX idx_short_urls_code ON short_urls(short_code);

CREATE TABLE tags (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE short_url_tags (
  short_url_id BIGINT NOT NULL REFERENCES short_urls(id) ON DELETE CASCADE,
  tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (short_url_id, tag_id)
);

CREATE TABLE redirect_rules (
  id BIGSERIAL PRIMARY KEY,
  short_url_id BIGINT NOT NULL REFERENCES short_urls(id) ON DELETE CASCADE,
  priority INT NOT NULL,
  long_url TEXT NOT NULL
);

CREATE TABLE redirect_conditions (
  id BIGSERIAL PRIMARY KEY,
  rule_id BIGINT NOT NULL REFERENCES redirect_rules(id) ON DELETE CASCADE,
  cond_type TEXT NOT NULL,
  match_key TEXT NULL,
  match_value TEXT NOT NULL
);

CREATE TABLE visits (
  id BIGSERIAL PRIMARY KEY,
  short_url_id BIGINT NULL REFERENCES short_urls(id) ON DELETE CASCADE,
  visit_type TEXT NOT NULL DEFAULT 'valid',
  visited_at TIMESTAMPTZ NOT NULL,
  referer TEXT NULL,
  user_agent TEXT NULL,
  browser TEXT NULL,
  os TEXT NULL,
  device TEXT NULL,
  is_bot BOOLEAN NOT NULL DEFAULT FALSE,
  remote_ip TEXT NULL,
  country_code TEXT NULL,
  country_name TEXT NULL,
  city TEXT NULL,
  latitude DOUBLE PRECISION NULL,
  longitude DOUBLE PRECISION NULL,
  visited_url TEXT NULL,
  geo_resolved BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_visits_short_url ON visits(short_url_id, visited_at);
CREATE INDEX idx_visits_type ON visits(visit_type, visited_at);
CREATE INDEX idx_visits_geo_pending ON visits(geo_resolved) WHERE geo_resolved = FALSE;

CREATE TABLE api_keys (
  id BIGSERIAL PRIMARY KEY,
  key_hash TEXT NOT NULL UNIQUE,
  name TEXT NULL,
  role TEXT NOT NULL DEFAULT 'admin',
  domain_id BIGINT NULL REFERENCES domains(id) ON DELETE CASCADE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  expires_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE webhooks (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  secret TEXT NOT NULL,
  events TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE webhook_deliveries (
  id BIGSERIAL PRIMARY KEY,
  webhook_id BIGINT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  event TEXT NOT NULL,
  payload TEXT NOT NULL,
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  last_error TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_webhook_deliveries_due ON webhook_deliveries(status, next_attempt_at);
`

const sqlite002 = `
ALTER TABLE users ADD COLUMN auth_source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN oidc_subject TEXT NULL;
CREATE UNIQUE INDEX idx_users_oidc_subject ON users(oidc_subject) WHERE oidc_subject IS NOT NULL;

ALTER TABLE short_urls ADD COLUMN group_name TEXT NULL;
CREATE INDEX idx_short_urls_group ON short_urls(group_name);
`

const postgres002 = `
ALTER TABLE users ADD COLUMN auth_source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN oidc_subject TEXT NULL;
CREATE UNIQUE INDEX idx_users_oidc_subject ON users(oidc_subject) WHERE oidc_subject IS NOT NULL;

ALTER TABLE short_urls ADD COLUMN group_name TEXT NULL;
CREATE INDEX idx_short_urls_group ON short_urls(group_name);
`

func migrationScripts(dialect Dialect) []struct {
	Version int
	Script  string
} {
	switch dialect {
	case Sqlite:
		return []struct {
			Version int
			Script  string
		}{{1, sqlite001}, {2, sqlite002}}
	default:
		return []struct {
			Version int
			Script  string
		}{{1, postgres001}, {2, postgres002}}
	}
}

// Migrate applies all pending migrations. Safe to run on every startup.
func Migrate(db *Db) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
	  version INT PRIMARY KEY,
	  applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}

	applied := map[int]bool{}
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, m := range migrationScripts(db.Dialect) {
		if applied[m.Version] {
			continue
		}
		err := db.WithTx(func(tx *Tx) error {
			if _, err := tx.Exec(m.Script); err != nil {
				return err
			}
			_, err := tx.Exec(
				"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
				m.Version, time.Now().UTC().Format(time.RFC3339Nano))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
