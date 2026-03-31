CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    phone TEXT NULL,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    status INTEGER NOT NULL,
    roles INTEGER[] NOT NULL DEFAULT '{}',
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    registration_idempotency_key TEXT NULL UNIQUE,
    last_login_at TIMESTAMPTZ NULL,
    deactivated_at TIMESTAMPTZ NULL,
    deactivation_reason TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_status_created_at
    ON users (status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_users_created_at
    ON users (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_users_name_search
    ON users (first_name, last_name);

CREATE TABLE IF NOT EXISTS user_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_agent TEXT NULL,
    ip_address TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id
    ON user_sessions (user_id);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id_revoked_at
    ON user_sessions (user_id, revoked_at);

CREATE INDEX IF NOT EXISTS idx_user_sessions_expires_at
    ON user_sessions (expires_at);
