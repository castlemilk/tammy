CREATE TABLE pre_restore_archives_v1 (
  archive_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  operation_id TEXT NOT NULL UNIQUE,
  version INTEGER NOT NULL CHECK (version > 0),
  state INTEGER NOT NULL CHECK (state IN (1,2,3)),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30),
  deletion_eligible_at TEXT NOT NULL CHECK (length(deletion_eligible_at) = 30 AND deletion_eligible_at >= created_at),
  content_hash BLOB NOT NULL CHECK (length(content_hash) = 32),
  source_generation INTEGER NOT NULL CHECK (source_generation > 0),
  encrypted_byte_length INTEGER NOT NULL CHECK (encrypted_byte_length > 0 AND encrypted_byte_length <= 269484032),
  deleted_at TEXT,
  deletion_reason_hash BLOB,
  CHECK ((state = 1 AND deleted_at IS NULL AND deletion_reason_hash IS NULL)
      OR (state = 3 AND deleted_at IS NULL AND length(deletion_reason_hash) = 32)
      OR (state = 2 AND length(deleted_at) = 30 AND length(deletion_reason_hash) = 32))
);

CREATE INDEX pre_restore_archives_v1_workspace_created
ON pre_restore_archives_v1(workspace_id, created_at, archive_id);

CREATE TRIGGER pre_restore_archives_v1_linked_update_only
BEFORE UPDATE ON pre_restore_archives_v1
WHEN NEW.archive_id IS NOT OLD.archive_id
  OR NEW.workspace_id IS NOT OLD.workspace_id
  OR NEW.operation_id IS NOT OLD.operation_id
  OR NEW.created_at IS NOT OLD.created_at
  OR NEW.deletion_eligible_at IS NOT OLD.deletion_eligible_at
  OR NEW.content_hash IS NOT OLD.content_hash
  OR NEW.source_generation IS NOT OLD.source_generation
  OR NEW.encrypted_byte_length IS NOT OLD.encrypted_byte_length
  OR NEW.version IS NOT OLD.version + 1
  OR NOT ((OLD.state = 1 AND NEW.state = 3 AND NEW.deleted_at IS NULL AND length(NEW.deletion_reason_hash) = 32)
    OR (OLD.state = 3 AND NEW.state = 2 AND length(NEW.deleted_at) = 30
      AND NEW.deletion_reason_hash IS OLD.deletion_reason_hash))
BEGIN
  SELECT RAISE(ABORT, 'invalid pre-restore archive transition');
END;

CREATE TRIGGER pre_restore_archives_v1_no_delete
BEFORE DELETE ON pre_restore_archives_v1
BEGIN
  SELECT RAISE(ABORT, 'pre-restore archive metadata is retained');
END;

CREATE TABLE pre_restore_archive_export_jobs_v1 (
  job_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  archive_id TEXT NOT NULL REFERENCES pre_restore_archives_v1(archive_id) ON DELETE RESTRICT,
  archive_version INTEGER NOT NULL CHECK (archive_version > 0),
  operation_key TEXT NOT NULL UNIQUE,
  retry_operation_key TEXT UNIQUE,
  version INTEGER NOT NULL CHECK (version > 0),
  state INTEGER NOT NULL CHECK (state BETWEEN 1 AND 6),
  input_hash BLOB NOT NULL CHECK (length(input_hash) = 32),
  destination_capability TEXT NOT NULL,
  destination_hash BLOB,
  progress_proto BLOB NOT NULL CHECK (length(progress_proto) BETWEEN 1 AND 4096),
  commit_point_reached INTEGER NOT NULL CHECK (commit_point_reached IN (0,1)),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30),
  completed_at TEXT,
  CHECK (destination_hash IS NULL OR length(destination_hash) = 32),
  CHECK (completed_at IS NULL OR length(completed_at) = 30),
  CHECK ((state = 4 AND commit_point_reached = 1 AND length(destination_hash) = 32 AND completed_at IS NOT NULL)
      OR state != 4)
);

CREATE INDEX pre_restore_archive_export_jobs_v1_workspace_created
ON pre_restore_archive_export_jobs_v1(workspace_id, created_at, job_id);

CREATE INDEX pre_restore_archive_export_jobs_v1_archive_state
ON pre_restore_archive_export_jobs_v1(archive_id, state);

CREATE TRIGGER pre_restore_archive_export_jobs_v1_immutable_input
BEFORE UPDATE ON pre_restore_archive_export_jobs_v1
WHEN NEW.job_id IS NOT OLD.job_id
  OR NEW.workspace_id IS NOT OLD.workspace_id
  OR NEW.archive_id IS NOT OLD.archive_id
  OR NEW.archive_version IS NOT OLD.archive_version
  OR NEW.operation_key IS NOT OLD.operation_key
  OR NEW.created_at IS NOT OLD.created_at
  OR NEW.version IS NOT OLD.version + 1
  OR ((NEW.input_hash IS NOT OLD.input_hash
      OR NEW.destination_capability IS NOT OLD.destination_capability
      OR NEW.retry_operation_key IS NOT OLD.retry_operation_key)
    AND NOT (OLD.state = 6 AND NEW.state = 1 AND OLD.commit_point_reached = 0
      AND NEW.commit_point_reached = 0 AND NEW.retry_operation_key IS NOT NULL))
BEGIN
  SELECT RAISE(ABORT, 'pre-restore export input is immutable');
END;

CREATE TRIGGER pre_restore_archive_export_jobs_v1_no_delete
BEFORE DELETE ON pre_restore_archive_export_jobs_v1
BEGIN
  SELECT RAISE(ABORT, 'pre-restore export jobs are retained');
END;

CREATE TABLE pre_restore_archive_commands_v1 (
  operation_key TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  archive_id TEXT NOT NULL REFERENCES pre_restore_archives_v1(archive_id) ON DELETE RESTRICT,
  expected_archive_version INTEGER NOT NULL CHECK (expected_archive_version > 0),
  command_type TEXT NOT NULL CHECK (command_type IN ('DELETE')),
  deletion_reason TEXT NOT NULL CHECK (length(deletion_reason) BETWEEN 1 AND 512 AND trim(deletion_reason) = deletion_reason),
  input_hash BLOB NOT NULL CHECK (length(input_hash) = 32),
  audit_event_proto BLOB NOT NULL CHECK (length(audit_event_proto) BETWEEN 1 AND 4096),
  status INTEGER NOT NULL CHECK (status IN (1,2)),
  version INTEGER NOT NULL CHECK (version > 0),
  result_proto BLOB,
  created_at TEXT NOT NULL CHECK (length(created_at) = 30),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30),
  CHECK ((status = 1 AND result_proto IS NULL) OR (status = 2 AND length(result_proto) BETWEEN 1 AND 4096))
);

CREATE TRIGGER pre_restore_archive_commands_v1_complete_only
BEFORE UPDATE ON pre_restore_archive_commands_v1
WHEN NEW.operation_key IS NOT OLD.operation_key
  OR NEW.workspace_id IS NOT OLD.workspace_id
  OR NEW.archive_id IS NOT OLD.archive_id
  OR NEW.expected_archive_version IS NOT OLD.expected_archive_version
  OR NEW.command_type IS NOT OLD.command_type
  OR NEW.deletion_reason IS NOT OLD.deletion_reason
  OR NEW.input_hash IS NOT OLD.input_hash
  OR NEW.audit_event_proto IS NOT OLD.audit_event_proto
  OR NEW.created_at IS NOT OLD.created_at
  OR NEW.version IS NOT OLD.version + 1
  OR OLD.status != 1
  OR NEW.status != 2
BEGIN
  SELECT RAISE(ABORT, 'invalid pre-restore command result transition');
END;

CREATE TRIGGER pre_restore_archive_commands_v1_no_delete
BEFORE DELETE ON pre_restore_archive_commands_v1
BEGIN
  SELECT RAISE(ABORT, 'pre-restore command results are retained');
END;

CREATE TRIGGER jobs_proto_size_insert
BEFORE INSERT ON jobs
WHEN typeof(NEW.payload_proto) != 'blob'
  OR length(NEW.payload_proto) NOT BETWEEN 1 AND 1048576
  OR (NEW.progress_proto IS NOT NULL AND (typeof(NEW.progress_proto) != 'blob'
    OR length(NEW.progress_proto) NOT BETWEEN 1 AND 1048576))
  OR (NEW.result_proto IS NOT NULL AND (typeof(NEW.result_proto) != 'blob'
    OR length(NEW.result_proto) NOT BETWEEN 1 AND 1048576))
BEGIN
  SELECT RAISE(ABORT, 'job protobuf exceeds global bound');
END;

CREATE TRIGGER jobs_proto_size_update
BEFORE UPDATE OF payload_proto, progress_proto, result_proto ON jobs
WHEN typeof(NEW.payload_proto) != 'blob'
  OR length(NEW.payload_proto) NOT BETWEEN 1 AND 1048576
  OR (NEW.progress_proto IS NOT NULL AND (typeof(NEW.progress_proto) != 'blob'
    OR length(NEW.progress_proto) NOT BETWEEN 1 AND 1048576))
  OR (NEW.result_proto IS NOT NULL AND (typeof(NEW.result_proto) != 'blob'
    OR length(NEW.result_proto) NOT BETWEEN 1 AND 1048576))
BEGIN
  SELECT RAISE(ABORT, 'job protobuf exceeds global bound');
END;

CREATE TRIGGER job_checkpoints_proto_size_insert
BEFORE INSERT ON job_checkpoints
WHEN typeof(NEW.checkpoint_proto) != 'blob'
  OR length(NEW.checkpoint_proto) NOT BETWEEN 1 AND 1048576
  OR length(NEW.checkpoint_sha256) != 64
BEGIN
  SELECT RAISE(ABORT, 'job checkpoint protobuf exceeds global bound');
END;

CREATE TRIGGER job_checkpoints_proto_size_update
BEFORE UPDATE OF checkpoint_proto, checkpoint_sha256 ON job_checkpoints
WHEN typeof(NEW.checkpoint_proto) != 'blob'
  OR length(NEW.checkpoint_proto) NOT BETWEEN 1 AND 1048576
  OR length(NEW.checkpoint_sha256) != 64
BEGIN
  SELECT RAISE(ABORT, 'job checkpoint protobuf exceeds global bound');
END;
