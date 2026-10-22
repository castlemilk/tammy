CREATE TABLE audit_chain_headers_v1 (
  workspace_id TEXT NOT NULL,
  generation INTEGER NOT NULL CHECK (generation > 0),
  chain_salt BLOB NOT NULL CHECK (length(chain_salt) = 32),
  genesis_hash BLOB NOT NULL CHECK (length(genesis_hash) = 32),
  current_sequence INTEGER NOT NULL DEFAULT 0 CHECK (current_sequence >= 0),
  current_head BLOB NOT NULL CHECK (length(current_head) = 32),
  created_at TEXT NOT NULL CHECK (
    length(created_at) = 30
    AND created_at GLOB
      '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'
    AND substr(created_at, 12, 2) BETWEEN '00' AND '23'
    AND substr(created_at, 15, 2) BETWEEN '00' AND '59'
    AND substr(created_at, 18, 2) BETWEEN '00' AND '59'
    AND strftime('%Y-%m-%dT%H:%M:%S', created_at) IS substr(created_at, 1, 19)
  ),
  PRIMARY KEY (workspace_id, generation),
  CHECK ((current_sequence = 0 AND current_head = genesis_hash) OR current_sequence > 0)
);

CREATE TRIGGER audit_chain_headers_v1_no_conflicting_insert
BEFORE INSERT ON audit_chain_headers_v1
WHEN EXISTS (
  SELECT 1 FROM audit_chain_headers_v1
  WHERE workspace_id = NEW.workspace_id AND generation = NEW.generation
)
BEGIN
  SELECT RAISE(ABORT, 'audit chain headers cannot be replaced');
END;

CREATE TABLE audit_events_v1 (
  workspace_id TEXT NOT NULL,
  generation INTEGER NOT NULL CHECK (generation > 0),
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  event_id TEXT NOT NULL UNIQUE,
  event_type INTEGER NOT NULL CHECK (event_type > 0),
  occurred_at TEXT NOT NULL,
  actor_user_id TEXT,
  session_id TEXT,
  organisation_id TEXT,
  command_id TEXT,
  command_type TEXT NOT NULL CHECK (length(command_type) BETWEEN 1 AND 256),
  idempotency_key TEXT,
  source_proto BLOB,
  affected_resources_proto BLOB,
  before_semantic_hash BLOB CHECK (before_semantic_hash IS NULL OR length(before_semantic_hash) = 32),
  after_semantic_hash BLOB CHECK (after_semantic_hash IS NULL OR length(after_semantic_hash) = 32),
  result_type TEXT,
  result_sha256 BLOB CHECK (result_sha256 IS NULL OR length(result_sha256) = 32),
  outcome_code TEXT,
  payload_type TEXT NOT NULL CHECK (length(payload_type) BETWEEN 1 AND 256),
  payload_schema_fingerprint BLOB NOT NULL CHECK (length(payload_schema_fingerprint) = 32),
  payload_proto BLOB NOT NULL,
  payload_json BLOB NOT NULL,
  canonical_event BLOB NOT NULL,
  event_proto BLOB NOT NULL,
  previous_hash BLOB NOT NULL CHECK (length(previous_hash) = 32),
  event_hash BLOB NOT NULL UNIQUE CHECK (length(event_hash) = 32),
  PRIMARY KEY (workspace_id, generation, sequence),
  FOREIGN KEY (workspace_id, generation)
    REFERENCES audit_chain_headers_v1(workspace_id, generation) ON DELETE RESTRICT
);

CREATE TRIGGER audit_events_v1_no_conflicting_insert
BEFORE INSERT ON audit_events_v1
WHEN EXISTS (
  SELECT 1 FROM audit_events_v1
  WHERE (workspace_id = NEW.workspace_id AND generation = NEW.generation AND sequence = NEW.sequence)
    OR event_id = NEW.event_id
    OR event_hash = NEW.event_hash
)
BEGIN
  SELECT RAISE(ABORT, 'audit events cannot be replaced');
END;

CREATE TRIGGER audit_events_v1_no_update
BEFORE UPDATE ON audit_events_v1
BEGIN
  SELECT RAISE(ABORT, 'audit events are immutable');
END;

CREATE TRIGGER audit_events_v1_no_delete
BEFORE DELETE ON audit_events_v1
BEGIN
  SELECT RAISE(ABORT, 'audit events are immutable');
END;

CREATE TRIGGER audit_chain_headers_v1_no_delete
BEFORE DELETE ON audit_chain_headers_v1
BEGIN
  SELECT RAISE(ABORT, 'audit chain headers cannot be deleted');
END;

CREATE TRIGGER audit_chain_headers_v1_linked_advance_only
BEFORE UPDATE ON audit_chain_headers_v1
WHEN OLD.workspace_id IS NOT NEW.workspace_id
  OR OLD.generation IS NOT NEW.generation
  OR OLD.chain_salt IS NOT NEW.chain_salt
  OR OLD.genesis_hash IS NOT NEW.genesis_hash
  OR OLD.created_at IS NOT NEW.created_at
  OR NEW.current_sequence IS NOT OLD.current_sequence + 1
  OR NEW.current_head IS OLD.current_head
  OR NOT EXISTS (
    SELECT 1
    FROM audit_events_v1
    WHERE workspace_id = OLD.workspace_id
      AND generation = OLD.generation
      AND sequence = NEW.current_sequence
      AND previous_hash = OLD.current_head
      AND event_hash = NEW.current_head
  )
BEGIN
  SELECT RAISE(ABORT, 'audit chain header advance must match its immutable event');
END;

CREATE TABLE audit_descriptor_sets_v1 (
  fingerprint BLOB NOT NULL PRIMARY KEY CHECK (length(fingerprint) = 32),
  descriptor_set BLOB NOT NULL CHECK (length(descriptor_set) BETWEEN 1 AND 67108864),
  created_at TEXT NOT NULL CHECK (
    length(created_at) = 30
    AND created_at GLOB
      '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'
    AND substr(created_at, 12, 2) BETWEEN '00' AND '23'
    AND substr(created_at, 15, 2) BETWEEN '00' AND '59'
    AND substr(created_at, 18, 2) BETWEEN '00' AND '59'
    AND strftime('%Y-%m-%dT%H:%M:%S', created_at) IS substr(created_at, 1, 19)
  )
);

CREATE TRIGGER audit_descriptor_sets_v1_no_conflicting_insert
BEFORE INSERT ON audit_descriptor_sets_v1
WHEN EXISTS (
  SELECT 1 FROM audit_descriptor_sets_v1 WHERE fingerprint = NEW.fingerprint
)
BEGIN
  SELECT RAISE(ABORT, 'audit descriptor sets cannot be replaced');
END;

CREATE TRIGGER audit_descriptor_sets_v1_no_update
BEFORE UPDATE ON audit_descriptor_sets_v1
BEGIN
  SELECT RAISE(ABORT, 'audit descriptor sets are immutable');
END;

CREATE TRIGGER audit_descriptor_sets_v1_no_delete
BEFORE DELETE ON audit_descriptor_sets_v1
BEGIN
  SELECT RAISE(ABORT, 'audit descriptor sets are immutable');
END;

CREATE TABLE audit_signing_keys_v1 (
  key_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  generation INTEGER NOT NULL CHECK (typeof(generation) = 'integer' AND generation > 0),
  epoch INTEGER NOT NULL CHECK (typeof(epoch) = 'integer' AND epoch > 0),
  public_key BLOB NOT NULL CHECK (length(public_key) = 32),
  encrypted_private_key BLOB NOT NULL CHECK (length(encrypted_private_key) = 80),
  nonce BLOB NOT NULL CHECK (length(nonce) = 12),
  encryption_algorithm TEXT NOT NULL CHECK (encryption_algorithm = 'AES-256-GCM/HKDF-SHA256-v1'),
  signing_algorithm TEXT NOT NULL CHECK (signing_algorithm = 'Ed25519'),
  predecessor_key_id TEXT,
  predecessor_signature BLOB CHECK (predecessor_signature IS NULL OR length(predecessor_signature) = 64),
  successor_possession_signature BLOB CHECK (
    successor_possession_signature IS NULL OR length(successor_possession_signature) = 64
  ),
  rotation_prior_sequence INTEGER CHECK (
    rotation_prior_sequence IS NULL OR
    (typeof(rotation_prior_sequence) = 'integer' AND rotation_prior_sequence >= 0)
  ),
  rotation_prior_head BLOB CHECK (rotation_prior_head IS NULL OR length(rotation_prior_head) = 32),
  created_at TEXT NOT NULL CHECK (
    length(created_at) = 30
    AND created_at GLOB
      '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'
    AND substr(created_at, 12, 2) BETWEEN '00' AND '23'
    AND substr(created_at, 15, 2) BETWEEN '00' AND '59'
    AND substr(created_at, 18, 2) BETWEEN '00' AND '59'
    AND strftime('%Y-%m-%dT%H:%M:%S', created_at) IS substr(created_at, 1, 19)
  ),
  retired_at TEXT CHECK (
    retired_at IS NULL OR (
      length(retired_at) = 30
      AND retired_at GLOB
        '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'
      AND substr(retired_at, 12, 2) BETWEEN '00' AND '23'
      AND substr(retired_at, 15, 2) BETWEEN '00' AND '59'
      AND substr(retired_at, 18, 2) BETWEEN '00' AND '59'
      AND strftime('%Y-%m-%dT%H:%M:%S', retired_at) IS substr(retired_at, 1, 19)
      AND retired_at > created_at
    )
  ),
  UNIQUE (workspace_id, key_id),
  UNIQUE (workspace_id, epoch),
  UNIQUE (workspace_id, key_id, epoch),
  FOREIGN KEY (workspace_id, predecessor_key_id)
    REFERENCES audit_signing_keys_v1(workspace_id, key_id) ON DELETE RESTRICT,
  CHECK (
    (epoch = 1
      AND predecessor_key_id IS NULL
      AND predecessor_signature IS NULL
      AND successor_possession_signature IS NULL
      AND rotation_prior_sequence IS NULL
      AND rotation_prior_head IS NULL)
    OR
    (epoch > 1
      AND predecessor_key_id IS NOT NULL
      AND predecessor_signature IS NOT NULL
      AND successor_possession_signature IS NOT NULL
      AND rotation_prior_sequence IS NOT NULL
      AND rotation_prior_head IS NOT NULL)
  )
);

CREATE UNIQUE INDEX audit_signing_keys_v1_one_active_per_workspace
  ON audit_signing_keys_v1(workspace_id)
  WHERE retired_at IS NULL;

CREATE TRIGGER audit_signing_keys_v1_no_conflicting_insert
BEFORE INSERT ON audit_signing_keys_v1
WHEN EXISTS (
    SELECT 1 FROM audit_signing_keys_v1 WHERE key_id = NEW.key_id
  )
  OR (
    NEW.retired_at IS NULL
    AND EXISTS (
      SELECT 1 FROM audit_signing_keys_v1
      WHERE workspace_id = NEW.workspace_id AND retired_at IS NULL
    )
  )
  OR (
    NEW.epoch = 1
    AND EXISTS (
      SELECT 1 FROM audit_signing_keys_v1 WHERE workspace_id = NEW.workspace_id
    )
  )
  OR (
    NEW.epoch > 1
    AND NOT EXISTS (
      SELECT 1 FROM audit_signing_keys_v1 predecessor
      WHERE predecessor.workspace_id = NEW.workspace_id
        AND predecessor.key_id = NEW.predecessor_key_id
        AND predecessor.epoch + 1 = NEW.epoch
        AND predecessor.generation <= NEW.generation
        AND predecessor.retired_at = NEW.created_at
    )
  )
BEGIN
  SELECT RAISE(ABORT, 'audit signing keys cannot be replaced');
END;

CREATE TRIGGER audit_signing_keys_v1_no_delete
BEFORE DELETE ON audit_signing_keys_v1
BEGIN
  SELECT RAISE(ABORT, 'audit signing keys cannot be deleted');
END;

CREATE TRIGGER audit_signing_keys_v1_retire_only
BEFORE UPDATE ON audit_signing_keys_v1
WHEN OLD.key_id IS NOT NEW.key_id
  OR OLD.workspace_id IS NOT NEW.workspace_id
  OR OLD.generation IS NOT NEW.generation
  OR OLD.epoch IS NOT NEW.epoch
  OR OLD.public_key IS NOT NEW.public_key
  OR OLD.encrypted_private_key IS NOT NEW.encrypted_private_key
  OR OLD.nonce IS NOT NEW.nonce
  OR OLD.encryption_algorithm IS NOT NEW.encryption_algorithm
  OR OLD.signing_algorithm IS NOT NEW.signing_algorithm
  OR OLD.predecessor_key_id IS NOT NEW.predecessor_key_id
  OR OLD.predecessor_signature IS NOT NEW.predecessor_signature
  OR OLD.successor_possession_signature IS NOT NEW.successor_possession_signature
  OR OLD.rotation_prior_sequence IS NOT NEW.rotation_prior_sequence
  OR OLD.rotation_prior_head IS NOT NEW.rotation_prior_head
  OR OLD.created_at IS NOT NEW.created_at
  OR OLD.retired_at IS NOT NULL
  OR NEW.retired_at IS NULL
  OR length(NEW.retired_at) != 30
  OR NEW.retired_at NOT GLOB
    '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'
  OR substr(NEW.retired_at, 12, 2) NOT BETWEEN '00' AND '23'
  OR substr(NEW.retired_at, 15, 2) NOT BETWEEN '00' AND '59'
  OR substr(NEW.retired_at, 18, 2) NOT BETWEEN '00' AND '59'
  OR strftime('%Y-%m-%dT%H:%M:%S', NEW.retired_at) IS NOT substr(NEW.retired_at, 1, 19)
  OR julianday(NEW.retired_at) IS NULL
  OR NEW.retired_at <= OLD.created_at
BEGIN
  SELECT RAISE(ABORT, 'audit signing keys permit one immutable retirement');
END;

CREATE TABLE audit_signing_key_state_v1 (
  workspace_id TEXT PRIMARY KEY,
  root_key_id TEXT NOT NULL,
  active_key_id TEXT NOT NULL,
  active_epoch INTEGER NOT NULL CHECK (typeof(active_epoch) = 'integer' AND active_epoch > 0),
  FOREIGN KEY (workspace_id, root_key_id)
    REFERENCES audit_signing_keys_v1(workspace_id, key_id) ON DELETE RESTRICT,
  FOREIGN KEY (workspace_id, active_key_id, active_epoch)
    REFERENCES audit_signing_keys_v1(workspace_id, key_id, epoch) ON DELETE RESTRICT
);

CREATE TRIGGER audit_signing_key_state_v1_no_conflicting_insert
BEFORE INSERT ON audit_signing_key_state_v1
WHEN EXISTS (
    SELECT 1 FROM audit_signing_key_state_v1 WHERE workspace_id = NEW.workspace_id
  )
  OR NEW.active_epoch != 1
  OR NEW.root_key_id IS NOT NEW.active_key_id
  OR NOT EXISTS (
    SELECT 1 FROM audit_signing_keys_v1 root
    WHERE root.workspace_id = NEW.workspace_id
      AND root.key_id = NEW.root_key_id
      AND root.epoch = 1
      AND root.predecessor_key_id IS NULL
      AND root.retired_at IS NULL
  )
BEGIN
  SELECT RAISE(ABORT, 'audit signing key state cannot be replaced');
END;

CREATE TRIGGER audit_signing_key_state_v1_no_delete
BEFORE DELETE ON audit_signing_key_state_v1
BEGIN
  SELECT RAISE(ABORT, 'audit signing key state cannot be deleted');
END;

CREATE TRIGGER audit_signing_key_state_v1_linked_advance_only
BEFORE UPDATE ON audit_signing_key_state_v1
WHEN OLD.workspace_id IS NOT NEW.workspace_id
  OR OLD.root_key_id IS NOT NEW.root_key_id
  OR NEW.active_epoch IS NOT OLD.active_epoch + 1
  OR OLD.active_key_id IS NEW.active_key_id
  OR NOT EXISTS (
    SELECT 1
    FROM audit_signing_keys_v1 predecessor
    JOIN audit_signing_keys_v1 successor
      ON successor.workspace_id = predecessor.workspace_id
      AND successor.predecessor_key_id = predecessor.key_id
      AND successor.epoch = predecessor.epoch + 1
    JOIN audit_events_v1 rotation_event
      ON rotation_event.workspace_id = successor.workspace_id
      AND rotation_event.generation = successor.generation
      AND rotation_event.sequence = successor.rotation_prior_sequence + 1
      AND rotation_event.previous_hash = successor.rotation_prior_head
      AND rotation_event.event_type = 15
      AND rotation_event.occurred_at = successor.created_at
    JOIN audit_chain_headers_v1 rotation_header
      ON rotation_header.workspace_id = rotation_event.workspace_id
      AND rotation_header.generation = rotation_event.generation
      AND rotation_header.current_sequence = rotation_event.sequence
      AND rotation_header.current_head = rotation_event.event_hash
    WHERE predecessor.workspace_id = OLD.workspace_id
      AND predecessor.key_id = OLD.active_key_id
      AND predecessor.epoch = OLD.active_epoch
      AND predecessor.retired_at = successor.created_at
      AND successor.key_id = NEW.active_key_id
      AND successor.epoch = NEW.active_epoch
      AND successor.retired_at IS NULL
  )
BEGIN
  SELECT RAISE(ABORT, 'audit signing key state must advance through one linked rotation event');
END;

CREATE TABLE audit_export_jobs_v1 (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  operation_key TEXT NOT NULL,
  operation_hash BLOB NOT NULL CHECK (length(operation_hash) = 32),
  input_hash BLOB NOT NULL CHECK (length(input_hash) = 32),
  filter_proto BLOB NOT NULL,
  snapshot_generation INTEGER NOT NULL CHECK (snapshot_generation > 0),
  snapshot_sequence INTEGER NOT NULL CHECK (snapshot_sequence >= 0),
  snapshot_head BLOB NOT NULL CHECK (length(snapshot_head) = 32),
  destination_provider TEXT NOT NULL CHECK (length(destination_provider) BETWEEN 1 AND 64),
  evidence_provider TEXT NOT NULL CHECK (length(evidence_provider) BETWEEN 1 AND 64),
  destination_capability TEXT NOT NULL CHECK (length(destination_capability) BETWEEN 16 AND 256),
  state TEXT NOT NULL CHECK (state IN (
    'QUEUED','RUNNING','WAITING_FOR_INPUT','COMPLETED',
    'FAILED_RETRYABLE','FAILED_TERMINAL','CANCELLED'
  )),
  attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  stage TEXT NOT NULL CHECK (length(stage) BETWEEN 1 AND 64),
  checkpoint_proto BLOB,
  checkpoint_hash BLOB CHECK (checkpoint_hash IS NULL OR length(checkpoint_hash) = 32),
  progress_proto BLOB NOT NULL,
  result_ref TEXT,
  destination_hash BLOB CHECK (destination_hash IS NULL OR length(destination_hash) = 32),
  signing_key_id TEXT,
  cancellation_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancellation_requested IN (0,1)),
  rename_committed INTEGER NOT NULL DEFAULT 0 CHECK (rename_committed IN (0,1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  UNIQUE (workspace_id, operation_key),
  FOREIGN KEY (workspace_id, signing_key_id)
    REFERENCES audit_signing_keys_v1(workspace_id, key_id) ON DELETE RESTRICT,
  CHECK (rename_committed = 0 OR (state = 'COMPLETED' AND destination_hash IS NOT NULL AND result_ref IS NOT NULL))
);

CREATE INDEX audit_export_jobs_v1_election
  ON audit_export_jobs_v1(state, created_at, id);

CREATE TRIGGER audit_export_jobs_v1_immutable_input
BEFORE UPDATE ON audit_export_jobs_v1
WHEN OLD.id IS NOT NEW.id
  OR OLD.workspace_id IS NOT NEW.workspace_id
  OR OLD.operation_key IS NOT NEW.operation_key
  OR OLD.operation_hash IS NOT NEW.operation_hash
  OR OLD.input_hash IS NOT NEW.input_hash
  OR OLD.filter_proto IS NOT NEW.filter_proto
  OR OLD.snapshot_generation IS NOT NEW.snapshot_generation
  OR OLD.snapshot_sequence IS NOT NEW.snapshot_sequence
  OR OLD.snapshot_head IS NOT NEW.snapshot_head
  OR OLD.destination_provider IS NOT NEW.destination_provider
  OR OLD.evidence_provider IS NOT NEW.evidence_provider
  OR OLD.destination_capability IS NOT NEW.destination_capability
  OR OLD.created_at IS NOT NEW.created_at
BEGIN
  SELECT RAISE(ABORT, 'audit export job input is immutable');
END;

CREATE TABLE command_idempotency_v1 (
  workspace_id TEXT NOT NULL,
  actor_user_id TEXT NOT NULL,
  fully_qualified_rpc_name TEXT NOT NULL CHECK (length(fully_qualified_rpc_name) BETWEEN 1 AND 256),
  operation_key TEXT NOT NULL,
  semantic_hash_version TEXT NOT NULL CHECK (length(semantic_hash_version) BETWEEN 1 AND 32),
  request_type TEXT NOT NULL CHECK (length(request_type) BETWEEN 1 AND 256),
  normalized_hash BLOB NOT NULL CHECK (length(normalized_hash) = 32),
  result_type TEXT,
  result_proto BLOB,
  outcome TEXT NOT NULL CHECK (outcome IN ('ELECTED','COMMITTED','FAILED')),
  failure_code TEXT,
  result_resource_id TEXT,
  attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
  retention_policy TEXT NOT NULL DEFAULT 'WORKSPACE_LIFETIME' CHECK (retention_policy = 'WORKSPACE_LIFETIME'),
  created_at TEXT NOT NULL,
  completed_at TEXT,
  PRIMARY KEY (workspace_id, actor_user_id, fully_qualified_rpc_name, operation_key),
  CHECK ((outcome = 'ELECTED' AND completed_at IS NULL AND result_type IS NULL AND result_proto IS NULL)
    OR (outcome = 'COMMITTED' AND completed_at IS NOT NULL AND result_type IS NOT NULL AND result_proto IS NOT NULL)
    OR (outcome = 'FAILED' AND completed_at IS NOT NULL AND failure_code IS NOT NULL))
);
