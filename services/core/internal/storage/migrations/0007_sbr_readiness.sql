CREATE UNIQUE INDEX sbr_organisations_id_abn_v1
ON organisations(id, abn);

CREATE TABLE sbr_credential_bindings_v1 (
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  credential_fingerprint BLOB NOT NULL CHECK (length(credential_fingerprint) = 32 AND credential_fingerprint != zeroblob(32)),
  component_version TEXT NOT NULL CHECK (length(component_version) BETWEEN 1 AND 64),
  subject_hash BLOB NOT NULL CHECK (length(subject_hash) = 32 AND subject_hash != zeroblob(32)),
  expires_at TEXT NOT NULL CHECK (length(expires_at) = 30),
  binding_state TEXT NOT NULL CHECK (binding_state IN ('ACTIVE','REIMPORT_REQUIRED','REMOVED')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30),
  PRIMARY KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint),
  FOREIGN KEY (organisation_id, canonical_abn) REFERENCES organisations(id, abn) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX sbr_credential_bindings_v1_one_current
ON sbr_credential_bindings_v1(workspace_id, organisation_id, canonical_abn, schema_version)
WHERE binding_state IN ('ACTIVE','REIMPORT_REQUIRED');

CREATE TABLE sbr_authenticated_profiles_v1 (
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  credential_fingerprint BLOB NOT NULL CHECK (length(credential_fingerprint) = 32 AND credential_fingerprint != zeroblob(32)),
  environment TEXT NOT NULL CHECK (environment IN ('SIMULATOR','EVTE')),
  profile_fingerprint BLOB NOT NULL CHECK (length(profile_fingerprint) = 32 AND profile_fingerprint != zeroblob(32)),
  registration_fingerprint BLOB NOT NULL CHECK (length(registration_fingerprint) = 32 AND registration_fingerprint != zeroblob(32)),
  component_fingerprint BLOB NOT NULL CHECK (length(component_fingerprint) = 32 AND component_fingerprint != zeroblob(32)),
  conformance TEXT NOT NULL CHECK (conformance IN ('SIMULATOR','PRE_CONFORMANCE','POST_CONFORMANCE')),
  evidence_sequence INTEGER NOT NULL CHECK (evidence_sequence > 0),
  authenticated_at TEXT NOT NULL CHECK (length(authenticated_at) = 30),
  PRIMARY KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, environment, evidence_sequence),
  UNIQUE (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, environment,
          profile_fingerprint, registration_fingerprint, component_fingerprint, conformance),
  FOREIGN KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    REFERENCES sbr_credential_bindings_v1(workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    ON DELETE RESTRICT,
  FOREIGN KEY (organisation_id, canonical_abn) REFERENCES organisations(id, abn) ON DELETE RESTRICT
);

CREATE TABLE sbr_readiness_transitions_v1 (
  transition_id TEXT PRIMARY KEY CHECK (length(transition_id) = 36),
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  credential_fingerprint BLOB NOT NULL CHECK (length(credential_fingerprint) = 32 AND credential_fingerprint != zeroblob(32)),
  readiness_state TEXT NOT NULL CHECK (readiness_state IN ('UNAVAILABLE','READY_FOR_SIMULATOR','READY_FOR_EVTE_PRE_CONFORMANCE','READY_FOR_EVTE_POST_CONFORMANCE')),
  reason_code TEXT NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 128),
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  occurred_at TEXT NOT NULL CHECK (length(occurred_at) = 30),
  UNIQUE (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, transition_id),
  UNIQUE (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, sequence),
  FOREIGN KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    REFERENCES sbr_credential_bindings_v1(workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    ON DELETE RESTRICT,
  FOREIGN KEY (organisation_id, canonical_abn) REFERENCES organisations(id, abn) ON DELETE RESTRICT
);

CREATE TABLE sbr_mutations_v1 (
  operation_id TEXT PRIMARY KEY CHECK (length(operation_id) = 36),
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  credential_fingerprint BLOB CHECK (credential_fingerprint IS NULL OR (length(credential_fingerprint) = 32 AND credential_fingerprint != zeroblob(32))),
  mutation_kind TEXT NOT NULL CHECK (mutation_kind IN ('IMPORT_CREDENTIAL','REPLACE_CREDENTIAL','REMOVE_CREDENTIAL','IMPORT_PRODUCT_ID','REMOVE_PRODUCT_ID')),
  mutation_state TEXT NOT NULL CHECK (mutation_state IN ('PREPARED','STAGED','CORE_COMMITTED','HELPER_COMMITTED','ABORT_REQUIRED','ABORTING','ABORTED','RECONCILE_REQUIRED')),
  pending_id TEXT CHECK (pending_id IS NULL OR length(pending_id) = 36),
  metadata_hash BLOB NOT NULL CHECK (length(metadata_hash) = 32 AND metadata_hash != zeroblob(32)),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30),
  UNIQUE (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, operation_id),
  CHECK ((mutation_state IN ('PREPARED','ABORTED','HELPER_COMMITTED') AND pending_id IS NULL) OR
         (mutation_state IN ('STAGED','CORE_COMMITTED','ABORT_REQUIRED','ABORTING','RECONCILE_REQUIRED') AND pending_id IS NOT NULL)),
  CHECK (credential_fingerprint IS NOT NULL OR (mutation_kind = 'IMPORT_CREDENTIAL' AND mutation_state IN ('PREPARED','ABORTED'))),
  FOREIGN KEY (organisation_id, canonical_abn) REFERENCES organisations(id, abn) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX sbr_mutations_v1_owned_pending
ON sbr_mutations_v1(workspace_id, organisation_id, canonical_abn, pending_id)
WHERE pending_id IS NOT NULL AND mutation_state NOT IN ('ABORTED','HELPER_COMMITTED');

-- Core commits retain only redacted projection data. Nothing in this payload
-- may contain a credential, password, bookmark, Product ID value, or endpoint.
-- The row is immutable history so a restart can finish the exact projection
-- only after the helper acknowledges COMMIT.
CREATE TABLE sbr_pending_mutation_effects_v1 (
  operation_id TEXT PRIMARY KEY REFERENCES sbr_mutations_v1(operation_id) ON DELETE RESTRICT,
  effect_json BLOB NOT NULL CHECK (length(effect_json) BETWEEN 2 AND 32768),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30)
);

CREATE TRIGGER sbr_pending_mutation_effects_v1_no_update BEFORE UPDATE ON sbr_pending_mutation_effects_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR pending mutation effects are immutable');
END;
CREATE TRIGGER sbr_pending_mutation_effects_v1_no_delete BEFORE DELETE ON sbr_pending_mutation_effects_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR pending mutation effects are retained redacted history');
END;

CREATE TABLE sbr_idempotency_v1 (
  idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  credential_fingerprint BLOB NOT NULL CHECK (length(credential_fingerprint) = 32 AND credential_fingerprint != zeroblob(32)),
  semantic_hash BLOB NOT NULL CHECK (length(semantic_hash) = 32 AND semantic_hash != zeroblob(32)),
  result_hash BLOB CHECK (result_hash IS NULL OR (length(result_hash) = 32 AND result_hash != zeroblob(32))),
  original_operation_id TEXT NOT NULL UNIQUE CHECK (length(original_operation_id) = 36),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30),
  PRIMARY KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, idempotency_key),
  FOREIGN KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    REFERENCES sbr_credential_bindings_v1(workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    ON DELETE RESTRICT,
  FOREIGN KEY (organisation_id, canonical_abn) REFERENCES organisations(id, abn) ON DELETE RESTRICT
);

CREATE TABLE sbr_simulator_transports_v1 (
  operation_id TEXT PRIMARY KEY CHECK (length(operation_id) = 36),
  actor_user_id TEXT NOT NULL CHECK (length(actor_user_id) = 36),
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  credential_fingerprint BLOB NOT NULL CHECK (length(credential_fingerprint) = 32 AND credential_fingerprint != zeroblob(32)),
  idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
  semantic_hash BLOB NOT NULL CHECK (length(semantic_hash) = 32 AND semantic_hash != zeroblob(32)),
  result_hash BLOB CHECK (result_hash IS NULL OR (length(result_hash) = 32 AND result_hash != zeroblob(32))),
  pending_terminal_state TEXT CHECK (pending_terminal_state IS NULL OR pending_terminal_state IN ('ACCEPTED','FAILED')),
  pending_result_hash BLOB CHECK (pending_result_hash IS NULL OR (length(pending_result_hash) = 32 AND pending_result_hash != zeroblob(32))),
  retry_of_operation_id TEXT UNIQUE REFERENCES sbr_simulator_transports_v1(operation_id) ON DELETE RESTRICT,
  state TEXT NOT NULL CHECK (state IN ('PREPARED','DISPATCHING','NOT_STARTED','MAYBE_SENT','RESPONSE_RECEIVED','ACCEPTED','FAILED','UNKNOWN')),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30),
  CHECK ((state = 'RESPONSE_RECEIVED' AND ((pending_terminal_state IS NULL AND pending_result_hash IS NULL) OR
         (pending_terminal_state IS NOT NULL AND pending_result_hash IS NOT NULL))) OR
         (state != 'RESPONSE_RECEIVED' AND pending_terminal_state IS NULL AND pending_result_hash IS NULL)),
  UNIQUE (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, idempotency_key),
  FOREIGN KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, idempotency_key)
    REFERENCES sbr_idempotency_v1(workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, idempotency_key)
    ON DELETE RESTRICT,
  FOREIGN KEY (organisation_id, canonical_abn) REFERENCES organisations(id, abn) ON DELETE RESTRICT
);

CREATE TABLE sbr_helper_dispatches_v1 (
  operation_id TEXT PRIMARY KEY CHECK (length(operation_id) = 36),
  actor_user_id TEXT NOT NULL CHECK (length(actor_user_id) = 36),
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  credential_fingerprint BLOB NOT NULL CHECK (length(credential_fingerprint) = 32 AND credential_fingerprint != zeroblob(32)),
  idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
  semantic_hash BLOB NOT NULL CHECK (length(semantic_hash) = 32 AND semantic_hash != zeroblob(32)),
  state TEXT NOT NULL CHECK (state IN ('DISPATCHING','COMPLETED','FAILED','UNKNOWN')),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30),
  UNIQUE (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, idempotency_key),
  FOREIGN KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    REFERENCES sbr_credential_bindings_v1(workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    ON DELETE RESTRICT,
  FOREIGN KEY (organisation_id, canonical_abn) REFERENCES organisations(id, abn) ON DELETE RESTRICT
);

CREATE TRIGGER sbr_bindings_v1_identity_immutable
BEFORE UPDATE ON sbr_credential_bindings_v1
WHEN NEW.workspace_id IS NOT OLD.workspace_id OR NEW.organisation_id IS NOT OLD.organisation_id OR
     NEW.canonical_abn IS NOT OLD.canonical_abn OR NEW.schema_version IS NOT OLD.schema_version OR
     NEW.credential_fingerprint IS NOT OLD.credential_fingerprint OR NEW.component_version IS NOT OLD.component_version OR
     NEW.subject_hash IS NOT OLD.subject_hash OR NEW.expires_at IS NOT OLD.expires_at OR
     NEW.revision IS NOT OLD.revision + 1 OR NEW.binding_state = OLD.binding_state
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR binding transition');
END;

CREATE TRIGGER sbr_bindings_v1_transition
BEFORE UPDATE ON sbr_credential_bindings_v1
WHEN NOT (
  (OLD.binding_state = 'ACTIVE' AND NEW.binding_state IN ('REIMPORT_REQUIRED','REMOVED')) OR
  (OLD.binding_state = 'REIMPORT_REQUIRED' AND NEW.binding_state IN ('ACTIVE','REMOVED'))
)
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR binding state transition');
END;

CREATE TRIGGER sbr_bindings_v1_no_delete BEFORE DELETE ON sbr_credential_bindings_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR bindings are retained redacted history');
END;

CREATE TRIGGER sbr_profiles_v1_no_update BEFORE UPDATE ON sbr_authenticated_profiles_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR authenticated profiles are immutable');
END;
CREATE TRIGGER sbr_profiles_v1_no_delete BEFORE DELETE ON sbr_authenticated_profiles_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR authenticated profiles are immutable');
END;
CREATE TRIGGER sbr_readiness_v1_no_update BEFORE UPDATE ON sbr_readiness_transitions_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR readiness transitions are immutable');
END;
CREATE TRIGGER sbr_readiness_v1_no_delete BEFORE DELETE ON sbr_readiness_transitions_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR readiness transitions are immutable');
END;

CREATE TRIGGER sbr_mutations_v1_transition
BEFORE UPDATE ON sbr_mutations_v1
WHEN NEW.operation_id IS NOT OLD.operation_id OR NEW.workspace_id IS NOT OLD.workspace_id OR
     NEW.organisation_id IS NOT OLD.organisation_id OR NEW.canonical_abn IS NOT OLD.canonical_abn OR
     NEW.schema_version IS NOT OLD.schema_version OR
     (NEW.credential_fingerprint IS NOT OLD.credential_fingerprint AND NOT (
       OLD.mutation_kind = 'IMPORT_CREDENTIAL' AND OLD.mutation_state = 'PREPARED' AND NEW.mutation_state = 'STAGED' AND
       OLD.credential_fingerprint IS NULL AND NEW.credential_fingerprint IS NOT NULL
     )) OR
     NEW.mutation_kind IS NOT OLD.mutation_kind OR NEW.metadata_hash IS NOT OLD.metadata_hash OR
     NEW.created_at IS NOT OLD.created_at OR
     NOT (
       (OLD.mutation_state = 'PREPARED' AND NEW.mutation_state IN ('STAGED','ABORTED')) OR
       (OLD.mutation_state = 'STAGED' AND NEW.mutation_state IN ('CORE_COMMITTED','ABORT_REQUIRED','ABORTED')) OR
       (OLD.mutation_state = 'CORE_COMMITTED' AND NEW.mutation_state IN ('HELPER_COMMITTED','RECONCILE_REQUIRED','ABORTED')) OR
       (OLD.mutation_state = 'RECONCILE_REQUIRED' AND NEW.mutation_state IN ('HELPER_COMMITTED','ABORTED')) OR
       (OLD.mutation_state = 'ABORT_REQUIRED' AND NEW.mutation_state IN ('ABORTING','ABORTED')) OR
       (OLD.mutation_state = 'ABORTING' AND NEW.mutation_state = 'ABORTED')
     )
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR mutation transition');
END;

CREATE TRIGGER sbr_helper_dispatch_v1_transition
BEFORE UPDATE ON sbr_helper_dispatches_v1
WHEN NEW.operation_id IS NOT OLD.operation_id OR NEW.actor_user_id IS NOT OLD.actor_user_id OR
     NEW.workspace_id IS NOT OLD.workspace_id OR NEW.organisation_id IS NOT OLD.organisation_id OR
     NEW.canonical_abn IS NOT OLD.canonical_abn OR NEW.schema_version IS NOT OLD.schema_version OR
     NEW.credential_fingerprint IS NOT OLD.credential_fingerprint OR NEW.idempotency_key IS NOT OLD.idempotency_key OR
     NEW.semantic_hash IS NOT OLD.semantic_hash OR NEW.created_at IS NOT OLD.created_at OR
     OLD.state != 'DISPATCHING' OR NEW.state NOT IN ('COMPLETED','FAILED','UNKNOWN')
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR helper dispatch transition');
END;

CREATE TRIGGER sbr_mutations_v1_no_delete BEFORE DELETE ON sbr_mutations_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR mutation history is immutable');
END;

CREATE TRIGGER sbr_helper_dispatch_v1_no_delete BEFORE DELETE ON sbr_helper_dispatches_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR helper dispatch history is immutable');
END;

CREATE TRIGGER sbr_idempotency_v1_result_once
BEFORE UPDATE ON sbr_idempotency_v1
WHEN NEW.idempotency_key IS NOT OLD.idempotency_key OR NEW.workspace_id IS NOT OLD.workspace_id OR
     NEW.organisation_id IS NOT OLD.organisation_id OR NEW.canonical_abn IS NOT OLD.canonical_abn OR
     NEW.schema_version IS NOT OLD.schema_version OR NEW.credential_fingerprint IS NOT OLD.credential_fingerprint OR
     NEW.semantic_hash IS NOT OLD.semantic_hash OR NEW.original_operation_id IS NOT OLD.original_operation_id OR
     NEW.created_at IS NOT OLD.created_at OR OLD.result_hash IS NOT NULL OR NEW.result_hash IS NULL
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR idempotency result transition');
END;

CREATE TRIGGER sbr_idempotency_v1_no_delete BEFORE DELETE ON sbr_idempotency_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR idempotency history is immutable');
END;

CREATE TABLE sbr_commands_v1 (
  operation_id TEXT NOT NULL UNIQUE CHECK (length(operation_id) = 36),
  actor_user_id TEXT NOT NULL CHECK (length(actor_user_id) = 36),
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
  semantic_hash BLOB NOT NULL CHECK (length(semantic_hash) = 32 AND semantic_hash != zeroblob(32)),
  mutation_kind TEXT NOT NULL CHECK (mutation_kind IN ('IMPORT_CREDENTIAL','REPLACE_CREDENTIAL','REMOVE_CREDENTIAL','IMPORT_PRODUCT_ID','REMOVE_PRODUCT_ID')),
  command_state TEXT NOT NULL CHECK (command_state IN ('PREPARED','COMPLETED')),
  result_credential_state INTEGER CHECK (result_credential_state IS NULL OR result_credential_state BETWEEN 0 AND 6),
  result_credential_fingerprint BLOB CHECK (result_credential_fingerprint IS NULL OR (length(result_credential_fingerprint) = 32 AND result_credential_fingerprint != zeroblob(32))),
  result_credential_issuer TEXT CHECK (result_credential_issuer IS NULL OR length(result_credential_issuer) <= 512),
  result_credential_serial TEXT CHECK (result_credential_serial IS NULL OR length(result_credential_serial) <= 128),
  result_credential_created_at TEXT CHECK (result_credential_created_at IS NULL OR length(result_credential_created_at) = 30),
  result_credential_expires_at TEXT CHECK (result_credential_expires_at IS NULL OR length(result_credential_expires_at) = 30),
  result_component_version TEXT CHECK (result_component_version IS NULL OR length(result_component_version) BETWEEN 1 AND 128),
  result_product_state TEXT CHECK (result_product_state IS NULL OR result_product_state IN ('MISSING','PRESENT','INACCESSIBLE')),
  created_at TEXT NOT NULL CHECK (length(created_at) = 30),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30),
  PRIMARY KEY (workspace_id, organisation_id, canonical_abn, schema_version, idempotency_key),
  CHECK ((command_state = 'PREPARED' AND result_credential_state IS NULL AND result_credential_fingerprint IS NULL AND
          result_credential_issuer IS NULL AND result_credential_serial IS NULL AND result_credential_created_at IS NULL AND
          result_credential_expires_at IS NULL AND result_component_version IS NULL AND result_product_state IS NULL) OR
         command_state = 'COMPLETED'),
  FOREIGN KEY (organisation_id, canonical_abn) REFERENCES organisations(id, abn) ON DELETE RESTRICT
);

CREATE TRIGGER sbr_commands_v1_complete_once
BEFORE UPDATE ON sbr_commands_v1
WHEN NEW.operation_id IS NOT OLD.operation_id OR NEW.workspace_id IS NOT OLD.workspace_id OR
     NEW.actor_user_id IS NOT OLD.actor_user_id OR
     NEW.organisation_id IS NOT OLD.organisation_id OR NEW.canonical_abn IS NOT OLD.canonical_abn OR
     NEW.schema_version IS NOT OLD.schema_version OR NEW.idempotency_key IS NOT OLD.idempotency_key OR
     NEW.semantic_hash IS NOT OLD.semantic_hash OR NEW.mutation_kind IS NOT OLD.mutation_kind OR
     NEW.created_at IS NOT OLD.created_at OR OLD.command_state != 'PREPARED' OR NEW.command_state != 'COMPLETED'
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR command transition');
END;

CREATE TRIGGER sbr_commands_v1_no_delete BEFORE DELETE ON sbr_commands_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR command history is immutable');
END;

CREATE TABLE sbr_product_states_v1 (
  workspace_id TEXT NOT NULL CHECK (length(workspace_id) = 36),
  organisation_id TEXT NOT NULL,
  canonical_abn TEXT NOT NULL CHECK (length(canonical_abn) = 11 AND canonical_abn NOT GLOB '*[^0-9]*'),
  schema_version INTEGER NOT NULL CHECK (schema_version = 1),
  credential_fingerprint BLOB NOT NULL CHECK (length(credential_fingerprint) = 32 AND credential_fingerprint != zeroblob(32)),
  environment TEXT NOT NULL CHECK (environment IN ('SIMULATOR','EVTE')),
  scope_fingerprint BLOB NOT NULL CHECK (length(scope_fingerprint) = 32 AND scope_fingerprint != zeroblob(32)),
  expected_product_identifier TEXT NOT NULL CHECK (length(expected_product_identifier) BETWEEN 1 AND 128),
  expected_service_id TEXT NOT NULL CHECK (length(expected_service_id) BETWEEN 1 AND 128),
  product_state TEXT NOT NULL CHECK (product_state IN ('MISSING','PRESENT','INACCESSIBLE')),
  product_fingerprint BLOB CHECK (product_fingerprint IS NULL OR (length(product_fingerprint) = 32 AND product_fingerprint != zeroblob(32))),
  revision INTEGER NOT NULL CHECK (revision > 0),
  updated_at TEXT NOT NULL CHECK (length(updated_at) = 30),
  PRIMARY KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint, environment,
               scope_fingerprint, expected_product_identifier, expected_service_id),
  CHECK ((product_state = 'MISSING' AND product_fingerprint IS NULL) OR
         (product_state IN ('PRESENT','INACCESSIBLE') AND product_fingerprint IS NOT NULL)),
  FOREIGN KEY (workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    REFERENCES sbr_credential_bindings_v1(workspace_id, organisation_id, canonical_abn, schema_version, credential_fingerprint)
    ON DELETE RESTRICT
);

CREATE TRIGGER sbr_product_states_v1_transition
BEFORE UPDATE ON sbr_product_states_v1
WHEN NEW.workspace_id IS NOT OLD.workspace_id OR NEW.organisation_id IS NOT OLD.organisation_id OR
     NEW.canonical_abn IS NOT OLD.canonical_abn OR NEW.schema_version IS NOT OLD.schema_version OR
     NEW.credential_fingerprint IS NOT OLD.credential_fingerprint OR NEW.environment IS NOT OLD.environment OR
     NEW.scope_fingerprint IS NOT OLD.scope_fingerprint OR
     NEW.expected_product_identifier IS NOT OLD.expected_product_identifier OR
     NEW.expected_service_id IS NOT OLD.expected_service_id OR
     NEW.revision IS NOT OLD.revision + 1
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR Product state transition');
END;

CREATE TRIGGER sbr_product_states_v1_no_delete BEFORE DELETE ON sbr_product_states_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR Product state history is retained');
END;

CREATE TABLE sbr_audit_events_v1 (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  action INTEGER NOT NULL CHECK (action BETWEEN 1 AND 15),
  status_code TEXT NOT NULL CHECK (length(status_code) BETWEEN 1 AND 96),
  credential_fingerprint BLOB CHECK (credential_fingerprint IS NULL OR length(credential_fingerprint) = 32),
  profile_fingerprint BLOB CHECK (profile_fingerprint IS NULL OR length(profile_fingerprint) = 32),
  component_fingerprint BLOB CHECK (component_fingerprint IS NULL OR length(component_fingerprint) = 32),
  payload_proto BLOB NOT NULL CHECK (length(payload_proto) BETWEEN 1 AND 1024),
  occurred_at TEXT NOT NULL CHECK (length(occurred_at) = 30)
);

CREATE TRIGGER sbr_audit_events_v1_no_update BEFORE UPDATE ON sbr_audit_events_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR audit history is immutable');
END;

CREATE TRIGGER sbr_audit_events_v1_no_delete BEFORE DELETE ON sbr_audit_events_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR audit history is immutable');
END;

CREATE TRIGGER sbr_transport_v1_transition
BEFORE UPDATE ON sbr_simulator_transports_v1
WHEN NEW.operation_id IS NOT OLD.operation_id OR NEW.workspace_id IS NOT OLD.workspace_id OR
     NEW.actor_user_id IS NOT OLD.actor_user_id OR
     NEW.organisation_id IS NOT OLD.organisation_id OR NEW.canonical_abn IS NOT OLD.canonical_abn OR
     NEW.schema_version IS NOT OLD.schema_version OR NEW.credential_fingerprint IS NOT OLD.credential_fingerprint OR
     NEW.idempotency_key IS NOT OLD.idempotency_key OR NEW.semantic_hash IS NOT OLD.semantic_hash OR
     NEW.retry_of_operation_id IS NOT OLD.retry_of_operation_id OR
     NEW.created_at IS NOT OLD.created_at OR
     NOT (
       (OLD.state = 'PREPARED' AND NEW.state IN ('DISPATCHING','NOT_STARTED')) OR
       (OLD.state = 'DISPATCHING' AND NEW.state IN ('MAYBE_SENT','RESPONSE_RECEIVED','UNKNOWN')) OR
       (OLD.state = 'MAYBE_SENT' AND NEW.state = 'UNKNOWN') OR
       (OLD.state = 'RESPONSE_RECEIVED' AND NEW.state = 'RESPONSE_RECEIVED' AND
        OLD.pending_terminal_state IS NULL AND NEW.pending_terminal_state IS NOT NULL) OR
       (OLD.state = 'RESPONSE_RECEIVED' AND NEW.state IN ('ACCEPTED','FAILED') AND
        OLD.pending_terminal_state = NEW.state AND OLD.pending_result_hash = NEW.result_hash)
     ) OR
     ((NEW.state IN ('ACCEPTED','FAILED')) != (NEW.result_hash IS NOT NULL)) OR
     (NEW.state = 'RESPONSE_RECEIVED' AND OLD.state != 'RESPONSE_RECEIVED' AND
      NOT ((NEW.pending_terminal_state IS NULL AND NEW.pending_result_hash IS NULL) OR
           (NEW.pending_terminal_state IS NOT NULL AND NEW.pending_result_hash IS NOT NULL))) OR
     (OLD.state = 'RESPONSE_RECEIVED' AND NEW.state = 'RESPONSE_RECEIVED' AND
      (OLD.result_hash IS NOT NEW.result_hash OR NEW.pending_terminal_state IS NULL OR NEW.pending_result_hash IS NULL)) OR
     (OLD.state = 'RESPONSE_RECEIVED' AND NEW.state IN ('ACCEPTED','FAILED') AND
      (NEW.pending_terminal_state IS NOT NULL OR NEW.pending_result_hash IS NOT NULL)) OR
     (NEW.state NOT IN ('RESPONSE_RECEIVED','ACCEPTED','FAILED') AND
      (NEW.pending_terminal_state IS NOT OLD.pending_terminal_state OR NEW.pending_result_hash IS NOT OLD.pending_result_hash OR
       NEW.result_hash IS NOT OLD.result_hash))
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR simulator transport transition');
END;

CREATE TRIGGER sbr_transport_v1_retry_guard
BEFORE INSERT ON sbr_simulator_transports_v1
WHEN NEW.retry_of_operation_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM sbr_simulator_transports_v1 AS original
  WHERE original.operation_id = NEW.retry_of_operation_id AND original.workspace_id = NEW.workspace_id AND
        original.organisation_id = NEW.organisation_id AND original.canonical_abn = NEW.canonical_abn AND
        original.schema_version = NEW.schema_version AND original.credential_fingerprint = NEW.credential_fingerprint AND
        original.semantic_hash = NEW.semantic_hash AND original.state = 'NOT_STARTED' AND NEW.state = 'PREPARED'
)
BEGIN
  SELECT RAISE(ABORT, 'invalid SBR simulator retry edge');
END;

CREATE TRIGGER sbr_transport_v1_no_delete BEFORE DELETE ON sbr_simulator_transports_v1 BEGIN
  SELECT RAISE(ABORT, 'SBR simulator transport history is immutable');
END;
