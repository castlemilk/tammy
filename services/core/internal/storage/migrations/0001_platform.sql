CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY CHECK (version > 0),
  name TEXT NOT NULL UNIQUE,
  sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
  applied_at TEXT NOT NULL
);

CREATE TABLE workspace_metadata (
  key TEXT PRIMARY KEY,
  value BLOB NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at TEXT NOT NULL
);

CREATE TABLE header_operation_ids (
  operation_id TEXT PRIMARY KEY,
  operation_kind TEXT NOT NULL CHECK (operation_kind IN ('CREATE','PASSPHRASE_CHANGE','RECOVERY','ADMIN_RECOVERY')),
  header_version INTEGER NOT NULL CHECK (header_version > 0),
  committed_at TEXT NOT NULL
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL COLLATE NOCASE UNIQUE,
  display_name TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('PENDING','ACTIVE','LOCKED','DISABLED')),
  password_verifier BLOB,
  password_changed_at TEXT,
  failed_attempts INTEGER NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
  locked_until TEXT,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE roles (
  code TEXT PRIMARY KEY CHECK (code IN ('workspace_admin','business_preparer','business_lodger','auditor')),
  owner_module TEXT NOT NULL DEFAULT 'identity' CHECK (owner_module = 'identity')
);

CREATE TABLE user_roles (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_code TEXT NOT NULL REFERENCES roles(code) ON DELETE RESTRICT,
  assigned_at TEXT NOT NULL,
  assigned_by_user_id TEXT REFERENCES users(id) ON DELETE RESTRICT,
  PRIMARY KEY (user_id, role_code)
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  asserted_factor_at TEXT,
  last_active_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  closed_at TEXT,
  state TEXT NOT NULL CHECK (state IN ('ACTIVE','CLOSED','EXPIRED')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE UNIQUE INDEX one_active_session ON sessions((state)) WHERE state = 'ACTIVE';

CREATE TABLE totp_credentials (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  encrypted_secret BLOB NOT NULL,
  last_counter INTEGER NOT NULL DEFAULT -1 CHECK (last_counter >= -1),
  state TEXT NOT NULL CHECK (state IN ('PENDING','ACTIVE','DISABLED')),
  enrolled_at TEXT NOT NULL,
  disabled_at TEXT
);

CREATE TABLE recovery_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  recovery_version INTEGER NOT NULL CHECK (recovery_version > 0),
  wrapped_recovery_material BLOB NOT NULL,
  operation_id TEXT NOT NULL UNIQUE,
  updated_at TEXT NOT NULL
);

CREATE TABLE attempt_journal_anchors (
  scope TEXT NOT NULL,
  subject_hash TEXT NOT NULL,
  sequence INTEGER NOT NULL CHECK (sequence >= 0),
  chain_hmac BLOB NOT NULL,
  attempt_count INTEGER NOT NULL CHECK (attempt_count >= 0),
  cooldown_until TEXT,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (scope, subject_hash)
);

CREATE TABLE idempotency_records (
  operation_key TEXT PRIMARY KEY,
  command_type TEXT NOT NULL,
  semantic_sha256 TEXT NOT NULL CHECK (length(semantic_sha256) = 64),
  result_type TEXT NOT NULL,
  result_proto BLOB NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('ELECTED','COMMITTED','FAILED')),
  created_at TEXT NOT NULL,
  committed_at TEXT
);

CREATE TABLE audit_envelopes (
  sequence INTEGER PRIMARY KEY CHECK (sequence > 0),
  event_id TEXT NOT NULL UNIQUE,
  event_type TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  actor_user_id TEXT REFERENCES users(id) ON DELETE RESTRICT,
  payload_proto BLOB NOT NULL,
  previous_hmac BLOB NOT NULL,
  envelope_hmac BLOB NOT NULL UNIQUE
);

CREATE TABLE audit_mirror_metadata (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  mirrored_sequence INTEGER NOT NULL DEFAULT 0 CHECK (mirrored_sequence >= 0),
  mirrored_hmac BLOB,
  mirror_status TEXT NOT NULL CHECK (mirror_status IN ('CURRENT','PENDING','DEGRADED')),
  updated_at TEXT NOT NULL
);

CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('PENDING','RUNNING','RETRY_WAIT','CANCEL_REQUESTED','CANCELLED','COMPLETED','FAILED')),
  operation_key TEXT NOT NULL UNIQUE,
  semantic_sha256 TEXT NOT NULL CHECK (length(semantic_sha256) = 64),
  payload_proto BLOB NOT NULL,
  result_proto BLOB,
  progress_proto BLOB,
  lease_owner TEXT,
  lease_expires_at TEXT,
  lease_generation INTEGER NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  next_attempt_at TEXT,
  commit_point_reached INTEGER NOT NULL DEFAULT 0 CHECK (commit_point_reached IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX jobs_election ON jobs(state, next_attempt_at, lease_expires_at, created_at, id);

CREATE TRIGGER jobs_immutable_input_guard
BEFORE UPDATE ON jobs
WHEN OLD.id IS NOT NEW.id
  OR OLD.kind IS NOT NEW.kind
  OR OLD.operation_key IS NOT NEW.operation_key
  OR OLD.semantic_sha256 IS NOT NEW.semantic_sha256
  OR OLD.payload_proto IS NOT NEW.payload_proto
  OR OLD.created_at IS NOT NEW.created_at
BEGIN
  SELECT RAISE(ABORT, 'job input is immutable');
END;

CREATE TABLE job_checkpoints (
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  checkpoint_proto BLOB NOT NULL,
  checkpoint_sha256 TEXT NOT NULL CHECK (length(checkpoint_sha256) = 64),
  committed_at TEXT NOT NULL,
  PRIMARY KEY (job_id, sequence)
);

CREATE TRIGGER job_checkpoints_no_update
BEFORE UPDATE ON job_checkpoints
BEGIN
  SELECT RAISE(ABORT, 'job checkpoints are immutable');
END;

CREATE TRIGGER job_checkpoints_no_delete
BEFORE DELETE ON job_checkpoints
BEGIN
  SELECT RAISE(ABORT, 'job checkpoints are immutable');
END;

CREATE TABLE backup_evidence (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE RESTRICT,
  encrypted_sha256 TEXT NOT NULL CHECK (length(encrypted_sha256) = 64),
  byte_length INTEGER NOT NULL CHECK (byte_length > 0),
  state TEXT NOT NULL CHECK (state IN ('STAGED','VERIFIED','RETAINED','DELETED')),
  created_at TEXT NOT NULL
);

CREATE TABLE restore_evidence (
  id TEXT PRIMARY KEY,
  operation_key TEXT NOT NULL UNIQUE,
  source_sha256 TEXT NOT NULL CHECK (length(source_sha256) = 64),
  predecessor_sha256 TEXT,
  state TEXT NOT NULL CHECK (state IN ('STAGED','VERIFIED','ACTIVATED','ROLLED_BACK')),
  created_at TEXT NOT NULL,
  activated_at TEXT
);

CREATE TABLE organisation_evidence_objects (
  id TEXT PRIMARY KEY,
  mime_type TEXT NOT NULL CHECK (mime_type IN ('application/pdf','image/jpeg','image/png')),
  byte_length INTEGER NOT NULL CHECK (byte_length BETWEEN 1 AND 1048576),
  sha256 BLOB NOT NULL CHECK (length(sha256) = 32),
  encrypted_bytes BLOB NOT NULL CHECK (length(encrypted_bytes) = byte_length),
  created_by_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  supersedes_evidence_id TEXT UNIQUE REFERENCES organisation_evidence_objects(id) ON DELETE RESTRICT
);

CREATE TABLE organisation_verifications (
  id TEXT PRIMARY KEY,
  operation_key TEXT NOT NULL UNIQUE REFERENCES idempotency_records(operation_key) ON DELETE RESTRICT,
  semantic_sha256 BLOB NOT NULL CHECK (length(semantic_sha256) = 32),
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  evidence_object_id TEXT NOT NULL UNIQUE REFERENCES organisation_evidence_objects(id) ON DELETE RESTRICT,
  source_type TEXT NOT NULL,
  source_id TEXT NOT NULL,
  source_revision INTEGER NOT NULL CHECK (source_revision > 0),
  source_content_hash BLOB NOT NULL CHECK (length(source_content_hash) = 32),
  source_method INTEGER NOT NULL CHECK (source_method IN (1,2)),
  state INTEGER NOT NULL CHECK (state BETWEEN 1 AND 5),
  verified_legal_name TEXT NOT NULL CHECK (length(verified_legal_name) <= 256),
  verified_entity_type TEXT NOT NULL CHECK (length(verified_entity_type) <= 96),
  recorded_at TEXT NOT NULL,
  expires_at TEXT NOT NULL CHECK (expires_at >= recorded_at),
  supersedes_verification_id TEXT UNIQUE REFERENCES organisation_verifications(id) ON DELETE RESTRICT,
  created_by_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE (organisation_id, source_type, source_id, source_revision)
);

CREATE TRIGGER organisation_evidence_no_update
BEFORE UPDATE ON organisation_evidence_objects
BEGIN
  SELECT RAISE(ABORT, 'organisation evidence is immutable');
END;

CREATE TRIGGER organisation_evidence_no_delete
BEFORE DELETE ON organisation_evidence_objects
BEGIN
  SELECT RAISE(ABORT, 'organisation evidence is immutable');
END;

CREATE TRIGGER organisation_verification_no_update
BEFORE UPDATE ON organisation_verifications
BEGIN
  SELECT RAISE(ABORT, 'organisation verification is immutable');
END;

CREATE TRIGGER organisation_verification_no_delete
BEFORE DELETE ON organisation_verifications
BEGIN
  SELECT RAISE(ABORT, 'organisation verification is immutable');
END;

INSERT INTO roles(code) VALUES
  ('workspace_admin'),
  ('business_preparer'),
  ('business_lodger'),
  ('auditor');
