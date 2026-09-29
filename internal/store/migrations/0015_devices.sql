-- Device auth for TCP listeners (docs/superpowers/specs/2026-09-29-remote-access-auth-design.md).
-- Only SHA-256 hashes of tokens and pairing codes are stored.

CREATE TABLE devices (
  id           INTEGER PRIMARY KEY,
  name         TEXT    NOT NULL,
  kind         TEXT    NOT NULL CHECK (kind IN ('mobile','web')),
  token_hash   TEXT    NOT NULL UNIQUE,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER,
  revoked_at   INTEGER
);

CREATE TABLE pairing_codes (
  code_hash  TEXT    PRIMARY KEY,
  expires_at INTEGER NOT NULL,
  used_at    INTEGER
);
