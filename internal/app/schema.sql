-- Downstream schema: intentionally separate from upstream Hibernate tables.
CREATE TABLE IF NOT EXISTS ui_users (
 id bigserial PRIMARY KEY, email text UNIQUE NOT NULL, password_hash text NOT NULL,
 role text NOT NULL CHECK (role IN ('ADMIN','USER')), enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ui_credentials (
 id bigserial PRIMARY KEY, name text NOT NULL, endpoint text NOT NULL DEFAULT '',
 region text NOT NULL DEFAULT 'us-east-1', access_key text NOT NULL,
 secret bytea NOT NULL, session_token bytea, path_style boolean NOT NULL DEFAULT false,
 enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ui_grants (
 credential_id bigint NOT NULL REFERENCES ui_credentials(id) ON DELETE CASCADE,
 subject text NOT NULL, PRIMARY KEY(credential_id, subject)
);
CREATE TABLE IF NOT EXISTS ui_sessions (
 token_hash text PRIMARY KEY, user_id bigint NOT NULL REFERENCES ui_users(id) ON DELETE CASCADE,
 credential_id bigint REFERENCES ui_credentials(id) ON DELETE SET NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS ui_sessions_expiry ON ui_sessions(expires_at);
CREATE TABLE IF NOT EXISTS ui_login_attempts (
 subject_hash text PRIMARY KEY, window_start timestamptz NOT NULL, attempts integer NOT NULL
);
CREATE TABLE IF NOT EXISTS ui_audit (
 id bigserial PRIMARY KEY, actor text NOT NULL, action text NOT NULL, bucket text NOT NULL DEFAULT '',
 object_key text NOT NULL DEFAULT '', code text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
