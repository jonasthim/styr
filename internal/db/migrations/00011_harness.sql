-- +goose Up
-- v1.0 "second harness": a session already carried a `harness` column from
-- 00001, but nothing ever chose anything other than 'claude'. Now a profile
-- names the harness sessions started under it default to, and the OpenAI
-- Codex CLI needs a credential of its own.
--
-- Codex authenticates the child process with an OpenAI API key in
-- OPENAI_API_KEY (v1.0 scope: the headless `codex login --device-auth` spike
-- is deliberately not attempted — see docs/HARNESSES.md). The key is sealed
-- with the same secretbox as claude_tokens and stored per user, with the
-- sentinel user id '__service__' holding the single service-wide key
-- unattended runs use, exactly as claude_tokens does.
ALTER TABLE profiles ADD COLUMN harness TEXT NOT NULL DEFAULT 'claude';

CREATE TABLE codex_credentials (
  user_id TEXT PRIMARY KEY,
  api_key_ciphertext BLOB NOT NULL,
  nonce BLOB NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  added_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE codex_credentials;
ALTER TABLE profiles DROP COLUMN harness;
