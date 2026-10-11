-- Retain exact BAS inputs, calculations, approvals and user-recorded follow-up.
-- No existing accounting fact or historical workpaper is rewritten or backfilled.

ALTER TABLE tax_facts ADD COLUMN source_content_hash BLOB
  CHECK (source_content_hash IS NULL OR (typeof(source_content_hash)='blob' AND length(source_content_hash)=32));

CREATE UNIQUE INDEX factor_assertions_bas_actor_key ON factor_assertions(id,user_id);

CREATE TABLE bas_profiles_v1 (
  profile_id TEXT NOT NULL PRIMARY KEY CHECK(length(profile_id) BETWEEN 1 AND 128 AND length(trim(profile_id))>0),
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE(profile_id,organisation_id)
);

CREATE TABLE bas_profile_versions_v1 (
  append_ordinal INTEGER PRIMARY KEY AUTOINCREMENT CHECK(append_ordinal>0),
  profile_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
  previous_version INTEGER CHECK(previous_version IS NULL OR (typeof(previous_version)='integer' AND previous_version>0)),
  previous_hash BLOB CHECK(previous_hash IS NULL OR (typeof(previous_hash)='blob' AND length(previous_hash)=32)),
  effective_start TEXT NOT NULL CHECK(length(effective_start)=10 AND effective_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(effective_start,'+0 days')=effective_start),
  effective_end TEXT NOT NULL CHECK(length(effective_end)=10 AND effective_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(effective_end,'+0 days')=effective_end),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  profile_proto BLOB NOT NULL CHECK(typeof(profile_proto)='blob' AND length(profile_proto) BETWEEN 1 AND 65536),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE(profile_id,version),
  UNIQUE(profile_id,organisation_id,version),
  UNIQUE(profile_id,organisation_id,version,content_hash),
  FOREIGN KEY(profile_id,organisation_id) REFERENCES bas_profiles_v1(profile_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(profile_id,organisation_id,previous_version,previous_hash) REFERENCES bas_profile_versions_v1(profile_id,organisation_id,version,content_hash) ON DELETE RESTRICT,
  CHECK((previous_version IS NULL AND previous_hash IS NULL) OR (previous_version IS NOT NULL AND previous_hash IS NOT NULL)),
  CHECK((version=1 AND previous_version IS NULL) OR (version>1 AND previous_version=version-1)),
  CHECK(effective_start<=effective_end)
);

CREATE TABLE bas_profile_heads_v1 (
  profile_id TEXT NOT NULL PRIMARY KEY,
  organisation_id TEXT NOT NULL,
  current_version INTEGER NOT NULL CHECK(typeof(current_version)='integer' AND current_version>0),
  current_hash BLOB NOT NULL CHECK(typeof(current_hash)='blob' AND length(current_hash)=32),
  cas_version INTEGER NOT NULL CHECK(typeof(cas_version)='integer' AND cas_version>0),
  updated_at TEXT NOT NULL,
  FOREIGN KEY(profile_id,organisation_id,current_version,current_hash) REFERENCES bas_profile_versions_v1(profile_id,organisation_id,version,content_hash) ON DELETE RESTRICT
);
CREATE INDEX bas_profile_versions_history_idx ON bas_profile_versions_v1(profile_id,organisation_id,version);
CREATE INDEX bas_profile_versions_append_idx ON bas_profile_versions_v1(organisation_id,append_ordinal);

CREATE TABLE bas_periods_v1 (
  period_id TEXT NOT NULL PRIMARY KEY CHECK(length(period_id) BETWEEN 1 AND 128 AND length(trim(period_id))>0),
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  period_start TEXT NOT NULL CHECK(length(period_start)=10 AND period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(period_start,'+0 days')=period_start),
  period_end TEXT NOT NULL CHECK(length(period_end)=10 AND period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(period_end,'+0 days')=period_end),
  reporting_window TEXT NOT NULL CHECK(reporting_window IN ('MONTHLY','JUL_SEP','OCT_DEC','JAN_MAR','APR_JUN','CUSTOM')),
  identity_hash BLOB NOT NULL CHECK(typeof(identity_hash)='blob' AND length(identity_hash)=32),
  created_at TEXT NOT NULL,
  UNIQUE(period_id,organisation_id),
  UNIQUE(period_id,organisation_id,identity_hash),
  UNIQUE(organisation_id,period_start,period_end,reporting_window),
  CHECK(period_start<=period_end)
);

CREATE TABLE bas_period_versions_v1 (
  append_ordinal INTEGER PRIMARY KEY AUTOINCREMENT CHECK(append_ordinal>0),
  period_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
  profile_id TEXT NOT NULL,
  profile_version INTEGER NOT NULL CHECK(typeof(profile_version)='integer' AND profile_version>0),
  profile_hash BLOB NOT NULL CHECK(typeof(profile_hash)='blob' AND length(profile_hash)=32),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  period_proto BLOB NOT NULL CHECK(typeof(period_proto)='blob' AND length(period_proto) BETWEEN 1 AND 65536),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE(period_id,version),
  UNIQUE(period_id,organisation_id,version),
  UNIQUE(period_id,organisation_id,version,content_hash),
  FOREIGN KEY(period_id,organisation_id) REFERENCES bas_periods_v1(period_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(profile_id,organisation_id,profile_version,profile_hash) REFERENCES bas_profile_versions_v1(profile_id,organisation_id,version,content_hash) ON DELETE RESTRICT
);

CREATE TABLE bas_period_heads_v1 (
  period_id TEXT NOT NULL PRIMARY KEY,
  organisation_id TEXT NOT NULL,
  current_version INTEGER NOT NULL CHECK(typeof(current_version)='integer' AND current_version>0),
  current_hash BLOB NOT NULL CHECK(typeof(current_hash)='blob' AND length(current_hash)=32),
  cas_version INTEGER NOT NULL CHECK(typeof(cas_version)='integer' AND cas_version>0),
  updated_at TEXT NOT NULL,
  FOREIGN KEY(period_id,organisation_id,current_version,current_hash) REFERENCES bas_period_versions_v1(period_id,organisation_id,version,content_hash) ON DELETE RESTRICT
);
CREATE INDEX bas_period_versions_history_idx ON bas_period_versions_v1(period_id,organisation_id,version);
CREATE INDEX bas_period_versions_append_idx ON bas_period_versions_v1(organisation_id,append_ordinal);

-- Family identity is only organisation plus actual civil scope. Profile/rule
-- corrections therefore reuse it; a changed period or window gets a new row.
CREATE TABLE bas_family_identities_v1 (
  family_id TEXT NOT NULL PRIMARY KEY CHECK(length(family_id) BETWEEN 1 AND 128 AND length(trim(family_id))>0),
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  period_start TEXT NOT NULL CHECK(length(period_start)=10 AND period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(period_start,'+0 days')=period_start),
  period_end TEXT NOT NULL CHECK(length(period_end)=10 AND period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(period_end,'+0 days')=period_end),
  reporting_window TEXT NOT NULL CHECK(reporting_window IN ('MONTHLY','JUL_SEP','OCT_DEC','JAN_MAR','APR_JUN','CUSTOM')),
  identity_hash BLOB NOT NULL CHECK(typeof(identity_hash)='blob' AND length(identity_hash)=32),
  identity_proto BLOB NOT NULL CHECK(typeof(identity_proto)='blob' AND length(identity_proto) BETWEEN 1 AND 65536),
  created_at TEXT NOT NULL,
  UNIQUE(family_id,organisation_id),
  UNIQUE(family_id,organisation_id,identity_hash),
  UNIQUE(organisation_id,period_start,period_end,reporting_window),
  CHECK(period_start<=period_end)
);

CREATE TABLE bas_preparation_identities_v1 (
  preparation_id TEXT NOT NULL PRIMARY KEY CHECK(length(preparation_id) BETWEEN 1 AND 128 AND length(trim(preparation_id))>0),
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  branch_id TEXT,
  attempt_id TEXT NOT NULL CHECK(length(attempt_id) BETWEEN 1 AND 128 AND length(trim(attempt_id))>0),
  predecessor_id TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(preparation_id,organisation_id,family_id),
  UNIQUE(preparation_id,organisation_id,family_id,branch_id,attempt_id),
  UNIQUE(attempt_id,organisation_id,family_id,branch_id),
  UNIQUE(organisation_id,family_id,attempt_id),
  FOREIGN KEY(family_id,organisation_id) REFERENCES bas_family_identities_v1(family_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(branch_id,organisation_id,family_id) REFERENCES bas_branch_identities_v1(branch_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(predecessor_id,organisation_id,family_id) REFERENCES bas_preparation_identities_v1(preparation_id,organisation_id,family_id) ON DELETE RESTRICT
);

CREATE TABLE bas_payg_input_versions_v1 (
  input_snapshot_id TEXT NOT NULL PRIMARY KEY CHECK(length(input_snapshot_id)=36 AND input_snapshot_id GLOB '????????-????-7???-????-????????????'),
  preparation_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  preparation_version INTEGER NOT NULL CHECK(typeof(preparation_version)='integer' AND preparation_version>0),
  preparation_hash BLOB NOT NULL CHECK(typeof(preparation_hash)='blob' AND length(preparation_hash)=32),
  input_revision INTEGER NOT NULL CHECK(typeof(input_revision)='integer' AND input_revision>0),
  input_hash BLOB NOT NULL CHECK(typeof(input_hash)='blob' AND length(input_hash)=32),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  input_proto BLOB NOT NULL CHECK(typeof(input_proto)='blob' AND length(input_proto) BETWEEN 1 AND 2097152),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE(preparation_id,organisation_id,family_id,input_revision),
  UNIQUE(preparation_id,organisation_id,family_id,input_revision,input_hash,content_hash),
  UNIQUE(input_snapshot_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash,input_revision,input_hash,content_hash),
  UNIQUE(input_snapshot_id,organisation_id,family_id,preparation_id,input_revision,input_hash,content_hash),
  FOREIGN KEY(preparation_id,organisation_id,family_id) REFERENCES bas_preparation_identities_v1(preparation_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(preparation_id,organisation_id,family_id,preparation_version,preparation_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE bas_calculations_v1 (
  append_ordinal INTEGER PRIMARY KEY AUTOINCREMENT CHECK(append_ordinal>0),
  calculation_id TEXT NOT NULL CHECK(length(calculation_id) BETWEEN 1 AND 128 AND length(trim(calculation_id))>0),
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  preparation_id TEXT NOT NULL,
  preparation_version INTEGER NOT NULL CHECK(typeof(preparation_version)='integer' AND preparation_version>0),
  preparation_hash BLOB NOT NULL CHECK(typeof(preparation_hash)='blob' AND length(preparation_hash)=32),
  version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  input_snapshot_id TEXT NOT NULL,
  input_revision INTEGER NOT NULL CHECK(typeof(input_revision)='integer' AND input_revision>0),
  input_hash BLOB NOT NULL CHECK(typeof(input_hash)='blob' AND length(input_hash)=32),
  input_content_hash BLOB NOT NULL CHECK(typeof(input_content_hash)='blob' AND length(input_content_hash)=32),
  profile_id TEXT NOT NULL,
  profile_version INTEGER NOT NULL CHECK(typeof(profile_version)='integer' AND profile_version>0),
  profile_hash BLOB NOT NULL CHECK(typeof(profile_hash)='blob' AND length(profile_hash)=32),
  period_id TEXT NOT NULL,
  period_version INTEGER NOT NULL CHECK(typeof(period_version)='integer' AND period_version>0),
  period_hash BLOB NOT NULL CHECK(typeof(period_hash)='blob' AND length(period_hash)=32),
  outcome TEXT NOT NULL CHECK(outcome IN ('COMPLETE','INCOMPLETE','UNSUPPORTED')),
  source_snapshot_hash BLOB NOT NULL CHECK(typeof(source_snapshot_hash)='blob' AND length(source_snapshot_hash)=32),
  coverage_manifest_hash BLOB NOT NULL CHECK(typeof(coverage_manifest_hash)='blob' AND length(coverage_manifest_hash)=32),
  revision_vector_hash BLOB NOT NULL CHECK(typeof(revision_vector_hash)='blob' AND length(revision_vector_hash)=32),
  rule_version TEXT NOT NULL CHECK(length(rule_version) BETWEEN 1 AND 128),
  rule_hash BLOB NOT NULL CHECK(typeof(rule_hash)='blob' AND length(rule_hash)=32),
  calculator_version TEXT NOT NULL CHECK(length(calculator_version) BETWEEN 1 AND 128),
  calculator_hash BLOB NOT NULL CHECK(typeof(calculator_hash)='blob' AND length(calculator_hash)=32),
  form_version TEXT NOT NULL CHECK(length(form_version) BETWEEN 1 AND 128),
  form_hash BLOB NOT NULL CHECK(typeof(form_hash)='blob' AND length(form_hash)=32),
  schema_version TEXT NOT NULL CHECK(length(schema_version) BETWEEN 1 AND 128),
  schema_hash BLOB NOT NULL CHECK(typeof(schema_hash)='blob' AND length(schema_hash)=32),
  descriptor_version TEXT NOT NULL CHECK(length(descriptor_version) BETWEEN 1 AND 128),
  descriptor_hash BLOB NOT NULL CHECK(typeof(descriptor_hash)='blob' AND length(descriptor_hash)=32),
  organisation_snapshot_hash BLOB NOT NULL CHECK(typeof(organisation_snapshot_hash)='blob' AND length(organisation_snapshot_hash)=32),
  result_proto BLOB NOT NULL CHECK(typeof(result_proto)='blob' AND length(result_proto) BETWEEN 1 AND 2097152),
  workpaper_snapshot_proto BLOB NOT NULL CHECK(typeof(workpaper_snapshot_proto)='blob' AND length(workpaper_snapshot_proto) BETWEEN 1 AND 8388608),
  revision_vector_proto BLOB NOT NULL CHECK(typeof(revision_vector_proto)='blob' AND length(revision_vector_proto) BETWEEN 1 AND 2097152),
  organisation_snapshot_proto BLOB NOT NULL CHECK(typeof(organisation_snapshot_proto)='blob' AND length(organisation_snapshot_proto) BETWEEN 1 AND 1048576),
  calculation_proto BLOB NOT NULL CHECK(typeof(calculation_proto)='blob' AND length(calculation_proto) BETWEEN 1 AND 4194304),
  created_at TEXT NOT NULL,
  UNIQUE(calculation_id,version),
  UNIQUE(calculation_id,organisation_id,family_id,preparation_id,version,content_hash),
  UNIQUE(calculation_id,organisation_id,family_id,version,content_hash),
  UNIQUE(calculation_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash,version,content_hash),
  FOREIGN KEY(preparation_id,organisation_id,family_id) REFERENCES bas_preparation_identities_v1(preparation_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(preparation_id,organisation_id,family_id,preparation_version,preparation_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(input_snapshot_id,organisation_id,family_id,preparation_id,input_revision,input_hash,input_content_hash) REFERENCES bas_payg_input_versions_v1(input_snapshot_id,organisation_id,family_id,preparation_id,input_revision,input_hash,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(profile_id,organisation_id,profile_version,profile_hash) REFERENCES bas_profile_versions_v1(profile_id,organisation_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(period_id,organisation_id,period_version,period_hash) REFERENCES bas_period_versions_v1(period_id,organisation_id,version,content_hash) ON DELETE RESTRICT
);

CREATE INDEX bas_calculations_append_idx ON bas_calculations_v1(organisation_id,preparation_id,append_ordinal);

CREATE TABLE bas_calculation_coverage_v1 (
  calculation_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  calculation_version INTEGER NOT NULL CHECK(typeof(calculation_version)='integer' AND calculation_version>0),
  calculation_hash BLOB NOT NULL CHECK(typeof(calculation_hash)='blob' AND length(calculation_hash)=32),
  sequence INTEGER NOT NULL CHECK(typeof(sequence)='integer' AND sequence>0),
  candidate_kind TEXT NOT NULL CHECK(candidate_kind IN ('INVOICE_POSTING','PURCHASE_SETTLEMENT','PURCHASE_CREDIT','MANUAL_TAX_FACT','PAYG_FACT','OPENING_OR_LEGACY_FACT','OTHER_OBLIGATION')),
  disposition TEXT NOT NULL CHECK(disposition IN ('ELIGIBLE','EXCLUDED_WITH_REASON','UNRESOLVED','UNSUPPORTED','REVIEWED_ZERO','NOT_APPLICABLE')),
  source_type TEXT NOT NULL CHECK(length(source_type) BETWEEN 1 AND 128),
  source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 256),
  source_revision INTEGER NOT NULL CHECK(typeof(source_revision)='integer' AND source_revision>0),
  source_hash BLOB NOT NULL CHECK(typeof(source_hash)='blob' AND length(source_hash)=32),
  eligible_amount_minor INTEGER CHECK(eligible_amount_minor IS NULL OR (typeof(eligible_amount_minor)='integer' AND eligible_amount_minor>=0)),
  currency_code TEXT CHECK(currency_code IS NULL OR currency_code='AUD'),
  tax_date TEXT CHECK(tax_date IS NULL OR (length(tax_date)=10 AND tax_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(tax_date,'+0 days')=tax_date)),
  classification_code TEXT NOT NULL CHECK(length(classification_code)<=128),
  reason TEXT NOT NULL CHECK(length(reason)<=1024),
  lineage_proto BLOB NOT NULL CHECK(typeof(lineage_proto)='blob' AND length(lineage_proto) BETWEEN 1 AND 2097152),
  evidence_proto BLOB NOT NULL CHECK(typeof(evidence_proto)='blob' AND length(evidence_proto) BETWEEN 1 AND 2097152),
  coverage_proto BLOB NOT NULL CHECK(typeof(coverage_proto)='blob' AND length(coverage_proto) BETWEEN 1 AND 1048576),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  PRIMARY KEY(calculation_id,calculation_version,sequence),
  FOREIGN KEY(calculation_id,organisation_id,family_id,calculation_version,calculation_hash) REFERENCES bas_calculations_v1(calculation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT,
  CHECK(disposition<>'ELIGIBLE' OR (eligible_amount_minor IS NOT NULL AND currency_code='AUD' AND tax_date IS NOT NULL))
);

CREATE TABLE bas_validations_v1 (
  validation_id TEXT NOT NULL CHECK(length(validation_id) BETWEEN 1 AND 128 AND length(trim(validation_id))>0),
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  preparation_id TEXT NOT NULL,
  preparation_version INTEGER NOT NULL CHECK(typeof(preparation_version)='integer' AND preparation_version>0),
  revision INTEGER NOT NULL CHECK(typeof(revision)='integer' AND revision>0),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  preparation_hash BLOB NOT NULL CHECK(typeof(preparation_hash)='blob' AND length(preparation_hash)=32),
  calculation_id TEXT NOT NULL,
  calculation_version INTEGER NOT NULL CHECK(typeof(calculation_version)='integer' AND calculation_version>0),
  calculation_hash BLOB NOT NULL CHECK(typeof(calculation_hash)='blob' AND length(calculation_hash)=32),
  source_snapshot_hash BLOB NOT NULL CHECK(typeof(source_snapshot_hash)='blob' AND length(source_snapshot_hash)=32),
  input_revision INTEGER NOT NULL CHECK(typeof(input_revision)='integer' AND input_revision>0),
  input_hash BLOB NOT NULL CHECK(typeof(input_hash)='blob' AND length(input_hash)=32),
  input_snapshot_id TEXT NOT NULL,
  input_content_hash BLOB NOT NULL CHECK(typeof(input_content_hash)='blob' AND length(input_content_hash)=32),
  validation_proto BLOB NOT NULL CHECK(typeof(validation_proto)='blob' AND length(validation_proto) BETWEEN 1 AND 1048576),
  created_at TEXT NOT NULL,
  PRIMARY KEY(validation_id,revision),
  UNIQUE(validation_id,organisation_id,family_id,preparation_id,preparation_version,revision,content_hash),
  UNIQUE(validation_id,organisation_id,family_id,revision),
  UNIQUE(validation_id,organisation_id,family_id,revision,content_hash),
  UNIQUE(validation_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash,revision,content_hash),
  UNIQUE(validation_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash,revision,content_hash,calculation_id,calculation_version,calculation_hash,source_snapshot_hash),
  FOREIGN KEY(preparation_id,organisation_id,family_id) REFERENCES bas_preparation_identities_v1(preparation_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(input_snapshot_id,organisation_id,family_id,preparation_id,input_revision,input_hash,input_content_hash) REFERENCES bas_payg_input_versions_v1(input_snapshot_id,organisation_id,family_id,preparation_id,input_revision,input_hash,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(calculation_id,organisation_id,family_id,calculation_version,calculation_hash) REFERENCES bas_calculations_v1(calculation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(preparation_id,organisation_id,family_id,preparation_version,preparation_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE bas_validation_findings_v1 (
  validation_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  revision INTEGER NOT NULL CHECK(typeof(revision)='integer' AND revision>0),
  validation_hash BLOB NOT NULL CHECK(typeof(validation_hash)='blob' AND length(validation_hash)=32),
  finding_id TEXT NOT NULL CHECK(length(finding_id) BETWEEN 1 AND 128 AND length(trim(finding_id))>0),
  finding_hash BLOB NOT NULL CHECK(typeof(finding_hash)='blob' AND length(finding_hash)=32),
  severity TEXT NOT NULL CHECK(severity IN ('BLOCKER','WARNING','INFORMATION','UNSUPPORTED')),
  stable_code TEXT NOT NULL CHECK(length(stable_code) BETWEEN 1 AND 128),
  safe_message TEXT NOT NULL CHECK(length(safe_message)<=2048),
  finding_proto BLOB NOT NULL CHECK(typeof(finding_proto)='blob' AND length(finding_proto) BETWEEN 1 AND 65536),
  PRIMARY KEY(validation_id,revision,finding_id),
  UNIQUE(validation_id,organisation_id,family_id,revision,validation_hash,finding_id,finding_hash),
  FOREIGN KEY(validation_id,organisation_id,family_id,revision,validation_hash) REFERENCES bas_validations_v1(validation_id,organisation_id,family_id,revision,content_hash) ON DELETE RESTRICT
);

CREATE TABLE bas_warning_acknowledgements_v1 (
  acknowledgement_id TEXT NOT NULL PRIMARY KEY CHECK(length(acknowledgement_id) BETWEEN 1 AND 128 AND length(trim(acknowledgement_id))>0),
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  preparation_id TEXT NOT NULL,
  validation_id TEXT NOT NULL,
  validation_revision INTEGER NOT NULL CHECK(typeof(validation_revision)='integer' AND validation_revision>0),
  validation_hash BLOB NOT NULL CHECK(typeof(validation_hash)='blob' AND length(validation_hash)=32),
  finding_id TEXT NOT NULL,
  finding_hash BLOB NOT NULL CHECK(typeof(finding_hash)='blob' AND length(finding_hash)=32),
  source_snapshot_hash BLOB NOT NULL CHECK(typeof(source_snapshot_hash)='blob' AND length(source_snapshot_hash)=32),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  acknowledged_at TEXT NOT NULL,
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  acknowledgement_proto BLOB NOT NULL CHECK(typeof(acknowledgement_proto)='blob' AND length(acknowledgement_proto) BETWEEN 1 AND 65536),
  UNIQUE(acknowledgement_id,organisation_id,family_id,content_hash),
  UNIQUE(organisation_id,family_id,validation_id,validation_revision,finding_id),
  FOREIGN KEY(preparation_id,organisation_id,family_id) REFERENCES bas_preparation_identities_v1(preparation_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(validation_id,organisation_id,family_id,validation_revision,validation_hash) REFERENCES bas_validations_v1(validation_id,organisation_id,family_id,revision,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(validation_id,organisation_id,family_id,validation_revision,validation_hash,finding_id,finding_hash) REFERENCES bas_validation_findings_v1(validation_id,organisation_id,family_id,revision,validation_hash,finding_id,finding_hash) ON DELETE RESTRICT
);

CREATE TABLE bas_frozen_bas_v1 (
  append_ordinal INTEGER PRIMARY KEY AUTOINCREMENT CHECK(append_ordinal>0),
  frozen_id TEXT NOT NULL UNIQUE CHECK(length(frozen_id) BETWEEN 1 AND 128 AND length(trim(frozen_id))>0),
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  preparation_id TEXT NOT NULL,
  preparation_version INTEGER NOT NULL CHECK(typeof(preparation_version)='integer' AND preparation_version>0),
  preparation_hash BLOB NOT NULL CHECK(typeof(preparation_hash)='blob' AND length(preparation_hash)=32),
  profile_id TEXT NOT NULL,
  profile_version INTEGER NOT NULL CHECK(typeof(profile_version)='integer' AND profile_version>0),
  profile_hash BLOB NOT NULL CHECK(typeof(profile_hash)='blob' AND length(profile_hash)=32),
  period_id TEXT NOT NULL,
  period_version INTEGER NOT NULL CHECK(typeof(period_version)='integer' AND period_version>0),
  period_hash BLOB NOT NULL CHECK(typeof(period_hash)='blob' AND length(period_hash)=32),
  calculation_id TEXT NOT NULL,
  calculation_version INTEGER NOT NULL CHECK(typeof(calculation_version)='integer' AND calculation_version>0),
  calculation_hash BLOB NOT NULL CHECK(typeof(calculation_hash)='blob' AND length(calculation_hash)=32),
  validation_id TEXT NOT NULL,
  validation_revision INTEGER NOT NULL CHECK(typeof(validation_revision)='integer' AND validation_revision>0),
  validation_hash BLOB NOT NULL CHECK(typeof(validation_hash)='blob' AND length(validation_hash)=32),
  source_snapshot_hash BLOB NOT NULL CHECK(typeof(source_snapshot_hash)='blob' AND length(source_snapshot_hash)=32),
  approved_snapshot_hash BLOB NOT NULL CHECK(typeof(approved_snapshot_hash)='blob' AND length(approved_snapshot_hash)=32),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  calculation_snapshot_proto BLOB NOT NULL CHECK(typeof(calculation_snapshot_proto)='blob' AND length(calculation_snapshot_proto) BETWEEN 1 AND 8388608),
  frozen_proto BLOB NOT NULL CHECK(typeof(frozen_proto)='blob' AND length(frozen_proto) BETWEEN 1 AND 8388608),
  approval_actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  fresh_factor_assertion_id TEXT NOT NULL,
  approved_at TEXT NOT NULL,
  approval_reason TEXT NOT NULL CHECK(length(approval_reason) BETWEEN 1 AND 512),
  UNIQUE(frozen_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash,content_hash),
  UNIQUE(frozen_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash),
  UNIQUE(frozen_id,organisation_id,family_id,preparation_id,content_hash),
  UNIQUE(frozen_id,organisation_id,family_id),
  UNIQUE(frozen_id,organisation_id,family_id,content_hash),
  UNIQUE(preparation_id,preparation_version),
  UNIQUE(fresh_factor_assertion_id),
  UNIQUE(frozen_id,organisation_id,approval_actor_user_id),
  FOREIGN KEY(preparation_id,organisation_id,family_id,preparation_version,preparation_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(profile_id,organisation_id,profile_version,profile_hash) REFERENCES bas_profile_versions_v1(profile_id,organisation_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(period_id,organisation_id,period_version,period_hash) REFERENCES bas_period_versions_v1(period_id,organisation_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(calculation_id,organisation_id,family_id,preparation_id,calculation_version,calculation_hash) REFERENCES bas_calculations_v1(calculation_id,organisation_id,family_id,preparation_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(validation_id,organisation_id,family_id,validation_revision,validation_hash) REFERENCES bas_validations_v1(validation_id,organisation_id,family_id,revision,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(fresh_factor_assertion_id,approval_actor_user_id) REFERENCES factor_assertions(id,user_id) ON DELETE RESTRICT
);
CREATE INDEX bas_frozen_bas_append_idx ON bas_frozen_bas_v1(organisation_id,append_ordinal);
CREATE INDEX bas_frozen_bas_family_append_idx ON bas_frozen_bas_v1(organisation_id,family_id,append_ordinal);

CREATE TABLE bas_frozen_acknowledgements_v1 (
  frozen_id TEXT NOT NULL,
  frozen_hash BLOB NOT NULL CHECK(typeof(frozen_hash)='blob' AND length(frozen_hash)=32),
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  acknowledgement_id TEXT NOT NULL,
  acknowledgement_hash BLOB NOT NULL CHECK(typeof(acknowledgement_hash)='blob' AND length(acknowledgement_hash)=32),
  PRIMARY KEY(frozen_id,acknowledgement_id),
  UNIQUE(frozen_id,organisation_id,family_id,acknowledgement_id,acknowledgement_hash),
  FOREIGN KEY(frozen_id,organisation_id,family_id,frozen_hash) REFERENCES bas_frozen_bas_v1(frozen_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(acknowledgement_id,organisation_id,family_id,acknowledgement_hash) REFERENCES bas_warning_acknowledgements_v1(acknowledgement_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT
);

-- The current family root/effective head are exact retained confirmed versions.
-- Dispute overlays and later metadata snapshots never rewrite those identities.
CREATE TABLE bas_family_state_v1 (
  family_id TEXT NOT NULL PRIMARY KEY,
  organisation_id TEXT NOT NULL,
  root_preparation_id TEXT NOT NULL,
  root_version INTEGER NOT NULL CHECK(typeof(root_version)='integer' AND root_version>0),
  root_hash BLOB NOT NULL CHECK(typeof(root_hash)='blob' AND length(root_hash)=32),
  effective_original_preparation_id TEXT,
  effective_original_version INTEGER CHECK(effective_original_version IS NULL OR (typeof(effective_original_version)='integer' AND effective_original_version>0)),
  effective_original_hash BLOB CHECK(effective_original_hash IS NULL OR (typeof(effective_original_hash)='blob' AND length(effective_original_hash)=32)),
  head_preparation_id TEXT,
  head_version INTEGER CHECK(head_version IS NULL OR (typeof(head_version)='integer' AND head_version>0)),
  head_hash BLOB CHECK(head_hash IS NULL OR (typeof(head_hash)='blob' AND length(head_hash)=32)),
  active_branch_id TEXT,
  active_branch_identity_hash BLOB CHECK(active_branch_identity_hash IS NULL OR (typeof(active_branch_identity_hash)='blob' AND length(active_branch_identity_hash)=32)),
  active_branch_version INTEGER CHECK(active_branch_version IS NULL OR (typeof(active_branch_version)='integer' AND active_branch_version>0)),
  active_branch_hash BLOB CHECK(active_branch_hash IS NULL OR (typeof(active_branch_hash)='blob' AND length(active_branch_hash)=32)),
  overlay_state TEXT NOT NULL CHECK(overlay_state IN ('NONE','DISPUTED','CORRECTION_REQUIRED','RESOLVED_UPHELD')),
  family_state_proto BLOB NOT NULL CHECK(typeof(family_state_proto)='blob' AND length(family_state_proto) BETWEEN 1 AND 65536),
  state_hash BLOB NOT NULL CHECK(typeof(state_hash)='blob' AND length(state_hash)=32),
  cas_version INTEGER NOT NULL CHECK(typeof(cas_version)='integer' AND cas_version>0),
  last_family_event_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(typeof(last_family_event_ordinal)='integer' AND last_family_event_ordinal>=0),
  updated_at TEXT NOT NULL,
  UNIQUE(family_id,organisation_id,state_hash),
  FOREIGN KEY(family_id,organisation_id) REFERENCES bas_family_identities_v1(family_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(root_preparation_id,organisation_id,family_id,root_version,root_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(effective_original_preparation_id,organisation_id,family_id,effective_original_version,effective_original_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(head_preparation_id,organisation_id,family_id,head_version,head_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(active_branch_id,organisation_id,family_id,active_branch_identity_hash) REFERENCES bas_branch_identities_v1(branch_id,organisation_id,family_id,identity_hash) ON DELETE RESTRICT,
  FOREIGN KEY(active_branch_id,organisation_id,family_id,active_branch_version,active_branch_hash) REFERENCES bas_branch_versions_v1(branch_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK((root_preparation_id IS NULL AND root_version IS NULL AND root_hash IS NULL) OR (root_preparation_id IS NOT NULL AND root_version IS NOT NULL AND root_hash IS NOT NULL)),
  CHECK((effective_original_preparation_id IS NULL AND effective_original_version IS NULL AND effective_original_hash IS NULL) OR (effective_original_preparation_id IS NOT NULL AND effective_original_version IS NOT NULL AND effective_original_hash IS NOT NULL)),
  CHECK((head_preparation_id IS NULL AND head_version IS NULL AND head_hash IS NULL) OR (head_preparation_id IS NOT NULL AND head_version IS NOT NULL AND head_hash IS NOT NULL)),
  CHECK((active_branch_id IS NULL AND active_branch_identity_hash IS NULL AND active_branch_version IS NULL AND active_branch_hash IS NULL) OR (active_branch_id IS NOT NULL AND active_branch_identity_hash IS NOT NULL AND active_branch_version IS NOT NULL AND active_branch_hash IS NOT NULL))
);

CREATE TABLE bas_preparation_versions_v1 (
  append_ordinal INTEGER PRIMARY KEY AUTOINCREMENT CHECK(append_ordinal>0),
  preparation_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
  profile_id TEXT NOT NULL,
  profile_version INTEGER NOT NULL CHECK(typeof(profile_version)='integer' AND profile_version>0),
  profile_hash BLOB NOT NULL CHECK(typeof(profile_hash)='blob' AND length(profile_hash)=32),
  period_id TEXT NOT NULL,
  period_version INTEGER NOT NULL CHECK(typeof(period_version)='integer' AND period_version>0),
  period_hash BLOB NOT NULL CHECK(typeof(period_hash)='blob' AND length(period_hash)=32),
  input_snapshot_id TEXT,
  input_revision INTEGER NOT NULL CHECK(typeof(input_revision)='integer' AND input_revision>0),
  input_hash BLOB NOT NULL CHECK(typeof(input_hash)='blob' AND length(input_hash)=32),
  input_content_hash BLOB CHECK(input_content_hash IS NULL OR (typeof(input_content_hash)='blob' AND length(input_content_hash)=32)),
  calculation_id TEXT,
  calculation_version INTEGER CHECK(calculation_version IS NULL OR (typeof(calculation_version)='integer' AND calculation_version>0)),
  calculation_hash BLOB CHECK(calculation_hash IS NULL OR (typeof(calculation_hash)='blob' AND length(calculation_hash)=32)),
  validation_id TEXT,
  validation_revision INTEGER CHECK(validation_revision IS NULL OR (typeof(validation_revision)='integer' AND validation_revision>0)),
  validation_hash BLOB CHECK(validation_hash IS NULL OR (typeof(validation_hash)='blob' AND length(validation_hash)=32)),
  predecessor_id TEXT,
  predecessor_version INTEGER CHECK(predecessor_version IS NULL OR (typeof(predecessor_version)='integer' AND predecessor_version>0)),
  predecessor_hash BLOB CHECK(predecessor_hash IS NULL OR (typeof(predecessor_hash)='blob' AND length(predecessor_hash)=32)),
  superseded_by_preparation_id TEXT,
  superseded_by_version INTEGER CHECK(superseded_by_version IS NULL OR (typeof(superseded_by_version)='integer' AND superseded_by_version>0)),
  superseded_by_hash BLOB CHECK(superseded_by_hash IS NULL OR (typeof(superseded_by_hash)='blob' AND length(superseded_by_hash)=32)),
  dispute_overlay TEXT NOT NULL CHECK(dispute_overlay IN ('NONE','DISPUTED','CORRECTION_REQUIRED','RESOLVED_UPHELD')),
  source_snapshot_hash BLOB NOT NULL CHECK(typeof(source_snapshot_hash)='blob' AND length(source_snapshot_hash)=32),
  state TEXT NOT NULL CHECK(state IN ('COLLECTING','BLOCKED','REVIEW_READY','FROZEN_FOR_MANUAL_HANDOFF','EXTERNAL_CONFIRMATION_RECORDED','SUPERSEDED_BEFORE_CONFIRMATION','SUPERSEDED_AFTER_RECORDED_REJECTION','SUPERSEDED_BY_CONFIRMED_REVISION')),
  freeze_id TEXT,
  freeze_hash BLOB CHECK(freeze_hash IS NULL OR (typeof(freeze_hash)='blob' AND length(freeze_hash)=32)),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  preparation_proto BLOB NOT NULL CHECK(typeof(preparation_proto)='blob' AND length(preparation_proto) BETWEEN 1 AND 65536),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(preparation_id,version),
  UNIQUE(preparation_id,organisation_id,family_id,version,content_hash),
  UNIQUE(preparation_id,organisation_id,version,content_hash),
  UNIQUE(preparation_id,organisation_id,family_id,version,content_hash,freeze_id,freeze_hash),
  FOREIGN KEY(preparation_id,organisation_id,family_id) REFERENCES bas_preparation_identities_v1(preparation_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(profile_id,organisation_id,profile_version,profile_hash) REFERENCES bas_profile_versions_v1(profile_id,organisation_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(period_id,organisation_id,period_version,period_hash) REFERENCES bas_period_versions_v1(period_id,organisation_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(input_snapshot_id,organisation_id,family_id,preparation_id,input_revision,input_hash,input_content_hash) REFERENCES bas_payg_input_versions_v1(input_snapshot_id,organisation_id,family_id,preparation_id,input_revision,input_hash,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(calculation_id,organisation_id,family_id,preparation_id,calculation_version,calculation_hash) REFERENCES bas_calculations_v1(calculation_id,organisation_id,family_id,preparation_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(validation_id,organisation_id,family_id,validation_revision,validation_hash) REFERENCES bas_validations_v1(validation_id,organisation_id,family_id,revision,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(predecessor_id,organisation_id,family_id,predecessor_version,predecessor_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(superseded_by_preparation_id,organisation_id,family_id,superseded_by_version,superseded_by_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(freeze_id,organisation_id,family_id,preparation_id,freeze_hash) REFERENCES bas_frozen_bas_v1(frozen_id,organisation_id,family_id,preparation_id,content_hash) ON DELETE RESTRICT,
  CHECK((calculation_id IS NULL AND calculation_version IS NULL AND calculation_hash IS NULL) OR (calculation_id IS NOT NULL AND calculation_version IS NOT NULL AND calculation_hash IS NOT NULL)),
  CHECK((validation_id IS NULL AND validation_revision IS NULL AND validation_hash IS NULL) OR (validation_id IS NOT NULL AND validation_revision IS NOT NULL AND validation_hash IS NOT NULL)),
  CHECK(validation_id IS NULL OR calculation_id IS NOT NULL),
  CHECK((input_snapshot_id IS NULL AND input_content_hash IS NULL) OR (input_snapshot_id IS NOT NULL AND input_content_hash IS NOT NULL)),
  CHECK((calculation_id IS NULL AND validation_id IS NULL) OR input_snapshot_id IS NOT NULL),
  CHECK((predecessor_id IS NULL AND predecessor_version IS NULL AND predecessor_hash IS NULL) OR (predecessor_id IS NOT NULL AND predecessor_version IS NOT NULL AND predecessor_hash IS NOT NULL)),
  CHECK((superseded_by_preparation_id IS NULL AND superseded_by_version IS NULL AND superseded_by_hash IS NULL) OR (superseded_by_preparation_id IS NOT NULL AND superseded_by_version IS NOT NULL AND superseded_by_hash IS NOT NULL)),
  CHECK(predecessor_id IS NULL OR predecessor_id<>preparation_id),
  CHECK(superseded_by_preparation_id IS NULL OR superseded_by_preparation_id<>preparation_id),
  CHECK((freeze_id IS NULL AND freeze_hash IS NULL) OR (freeze_id IS NOT NULL AND freeze_hash IS NOT NULL)),
  CHECK(state NOT IN ('FROZEN_FOR_MANUAL_HANDOFF','EXTERNAL_CONFIRMATION_RECORDED') OR (calculation_id IS NOT NULL AND validation_id IS NOT NULL AND freeze_id IS NOT NULL)),
  CHECK(state NOT IN ('SUPERSEDED_BEFORE_CONFIRMATION','SUPERSEDED_AFTER_RECORDED_REJECTION','SUPERSEDED_BY_CONFIRMED_REVISION') OR (calculation_id IS NOT NULL AND validation_id IS NOT NULL AND freeze_id IS NOT NULL)),
  CHECK(state<>'REVIEW_READY' OR (calculation_id IS NOT NULL AND validation_id IS NOT NULL)),
  CHECK(state NOT IN ('COLLECTING','BLOCKED','REVIEW_READY') OR freeze_id IS NULL)
);

CREATE TABLE bas_preparation_heads_v1 (
  preparation_id TEXT NOT NULL PRIMARY KEY,
  organisation_id TEXT NOT NULL,
  current_version INTEGER NOT NULL CHECK(typeof(current_version)='integer' AND current_version>0),
  current_hash BLOB NOT NULL CHECK(typeof(current_hash)='blob' AND length(current_hash)=32),
  cas_version INTEGER NOT NULL CHECK(typeof(cas_version)='integer' AND cas_version>0),
  updated_at TEXT NOT NULL,
  FOREIGN KEY(preparation_id,organisation_id,current_version,current_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,version,content_hash) ON DELETE RESTRICT
);
CREATE INDEX bas_preparation_versions_history_idx ON bas_preparation_versions_v1(organisation_id,family_id,preparation_id,version);
CREATE INDEX bas_preparation_versions_append_idx ON bas_preparation_versions_v1(organisation_id,append_ordinal);

CREATE TABLE bas_branch_identities_v1 (
  branch_id TEXT NOT NULL PRIMARY KEY CHECK(length(branch_id) BETWEEN 1 AND 128 AND length(trim(branch_id))>0),
  family_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  original_purpose TEXT NOT NULL CHECK(original_purpose IN ('REPLACEMENT','REVISION')),
  identity_hash BLOB NOT NULL CHECK(typeof(identity_hash)='blob' AND length(identity_hash)=32),
  branch_proto BLOB NOT NULL CHECK(typeof(branch_proto)='blob' AND length(branch_proto) BETWEEN 1 AND 65536),
  family_root_preparation_id TEXT NOT NULL,
  family_root_version INTEGER NOT NULL CHECK(typeof(family_root_version)='integer' AND family_root_version>0),
  family_root_hash BLOB NOT NULL CHECK(typeof(family_root_hash)='blob' AND length(family_root_hash)=32),
  root_preparation_id TEXT,
  root_version INTEGER CHECK(root_version IS NULL OR (typeof(root_version)='integer' AND root_version>0)),
  root_hash BLOB CHECK(root_hash IS NULL OR (typeof(root_hash)='blob' AND length(root_hash)=32)),
  effective_original_preparation_id TEXT,
  effective_original_version INTEGER CHECK(effective_original_version IS NULL OR (typeof(effective_original_version)='integer' AND effective_original_version>0)),
  effective_original_hash BLOB CHECK(effective_original_hash IS NULL OR (typeof(effective_original_hash)='blob' AND length(effective_original_hash)=32)),
  created_at TEXT NOT NULL,
  UNIQUE(branch_id,organisation_id,family_id),
  UNIQUE(branch_id,organisation_id,family_id,identity_hash),
  FOREIGN KEY(family_id,organisation_id) REFERENCES bas_family_identities_v1(family_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(family_root_preparation_id,organisation_id,family_id,family_root_version,family_root_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(root_preparation_id,organisation_id,family_id,root_version,root_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(effective_original_preparation_id,organisation_id,family_id,effective_original_version,effective_original_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT,
  CHECK((root_preparation_id IS NULL AND root_version IS NULL AND root_hash IS NULL) OR (root_preparation_id IS NOT NULL AND root_version IS NOT NULL AND root_hash IS NOT NULL)),
  CHECK((effective_original_preparation_id IS NULL AND effective_original_version IS NULL AND effective_original_hash IS NULL) OR (effective_original_preparation_id IS NOT NULL AND effective_original_version IS NOT NULL AND effective_original_hash IS NOT NULL))
);

CREATE TABLE bas_branch_versions_v1 (
  append_ordinal INTEGER PRIMARY KEY AUTOINCREMENT CHECK(append_ordinal>0),
  branch_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
  workflow_state TEXT NOT NULL CHECK(workflow_state IN ('ACTIVE','CORRECTIVE','RESOLVED','CLOSED')),
  active_attempt_id TEXT,
  active_preparation_id TEXT,
  active_attempt_version INTEGER CHECK(active_attempt_version IS NULL OR (typeof(active_attempt_version)='integer' AND active_attempt_version>0)),
  active_attempt_hash BLOB CHECK(active_attempt_hash IS NULL OR (typeof(active_attempt_hash)='blob' AND length(active_attempt_hash)=32)),
  corrective_resolution_id TEXT,
  corrective_resolution_hash BLOB CHECK(corrective_resolution_hash IS NULL OR (typeof(corrective_resolution_hash)='blob' AND length(corrective_resolution_hash)=32)),
  barrier_preparation_id TEXT,
  barrier_preparation_version INTEGER CHECK(barrier_preparation_version IS NULL OR (typeof(barrier_preparation_version)='integer' AND barrier_preparation_version>0)),
  barrier_preparation_hash BLOB CHECK(barrier_preparation_hash IS NULL OR (typeof(barrier_preparation_hash)='blob' AND length(barrier_preparation_hash)=32)),
  barrier_attempt_id TEXT,
  barrier_frozen_id TEXT,
  barrier_frozen_hash BLOB CHECK(barrier_frozen_hash IS NULL OR (typeof(barrier_frozen_hash)='blob' AND length(barrier_frozen_hash)=32)),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  branch_state_proto BLOB NOT NULL CHECK(typeof(branch_state_proto)='blob' AND length(branch_state_proto) BETWEEN 1 AND 65536),
  created_at TEXT NOT NULL,
  UNIQUE(branch_id,version),
  UNIQUE(branch_id,organisation_id,family_id,version,content_hash),
  FOREIGN KEY(branch_id,organisation_id,family_id) REFERENCES bas_branch_identities_v1(branch_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(active_preparation_id,organisation_id,family_id,active_attempt_version,active_attempt_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(active_preparation_id,organisation_id,family_id,branch_id,active_attempt_id) REFERENCES bas_preparation_identities_v1(preparation_id,organisation_id,family_id,branch_id,attempt_id) ON DELETE RESTRICT,
  FOREIGN KEY(corrective_resolution_id,organisation_id,family_id,corrective_resolution_hash) REFERENCES bas_dispute_resolutions_v1(resolution_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(barrier_preparation_id,organisation_id,family_id,barrier_preparation_version,barrier_preparation_hash,barrier_frozen_id,barrier_frozen_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash,freeze_id,freeze_hash) ON DELETE RESTRICT,
  FOREIGN KEY(barrier_frozen_id,organisation_id,family_id,barrier_preparation_id,barrier_frozen_hash) REFERENCES bas_frozen_bas_v1(frozen_id,organisation_id,family_id,preparation_id,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(barrier_preparation_id,organisation_id,family_id,branch_id,barrier_attempt_id) REFERENCES bas_preparation_identities_v1(preparation_id,organisation_id,family_id,branch_id,attempt_id) ON DELETE RESTRICT,
  CHECK((active_attempt_id IS NULL AND active_preparation_id IS NULL AND active_attempt_version IS NULL AND active_attempt_hash IS NULL) OR (active_attempt_id IS NOT NULL AND active_preparation_id IS NOT NULL AND active_attempt_version IS NOT NULL AND active_attempt_hash IS NOT NULL)),
  CHECK((corrective_resolution_id IS NULL AND corrective_resolution_hash IS NULL) OR (corrective_resolution_id IS NOT NULL AND corrective_resolution_hash IS NOT NULL)),
  CHECK((barrier_preparation_id IS NULL AND barrier_preparation_version IS NULL AND barrier_preparation_hash IS NULL AND barrier_attempt_id IS NULL AND barrier_frozen_id IS NULL AND barrier_frozen_hash IS NULL) OR (barrier_preparation_id IS NOT NULL AND barrier_preparation_version IS NOT NULL AND barrier_preparation_hash IS NOT NULL AND barrier_attempt_id IS NOT NULL AND barrier_frozen_id IS NOT NULL AND barrier_frozen_hash IS NOT NULL)),
  CHECK(workflow_state<>'CORRECTIVE' OR corrective_resolution_id IS NOT NULL)
);

CREATE TABLE bas_branch_heads_v1 (
  branch_id TEXT NOT NULL PRIMARY KEY,
  family_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  current_version INTEGER NOT NULL CHECK(typeof(current_version)='integer' AND current_version>0),
  current_hash BLOB NOT NULL CHECK(typeof(current_hash)='blob' AND length(current_hash)=32),
  cas_version INTEGER NOT NULL CHECK(typeof(cas_version)='integer' AND cas_version>0),
  updated_at TEXT NOT NULL,
  FOREIGN KEY(branch_id,organisation_id,family_id,current_version,current_hash) REFERENCES bas_branch_versions_v1(branch_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT
);
CREATE INDEX bas_branch_versions_history_idx ON bas_branch_versions_v1(organisation_id,family_id,branch_id,version);

CREATE TABLE bas_family_events_v1 (
  event_ordinal INTEGER PRIMARY KEY AUTOINCREMENT CHECK(typeof(event_ordinal)='integer' AND event_ordinal>0),
  event_id TEXT NOT NULL UNIQUE CHECK(length(event_id) BETWEEN 1 AND 128 AND length(trim(event_id))>0),
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  event_kind TEXT NOT NULL CHECK(event_kind IN ('OBSERVATION','CORRECTION','RESOLUTION')),
  operation_key TEXT NOT NULL CHECK(length(operation_key) BETWEEN 1 AND 256 AND length(trim(operation_key))>0),
  claimed_at TEXT NOT NULL,
  event_proto BLOB NOT NULL CHECK(typeof(event_proto)='blob' AND length(event_proto) BETWEEN 1 AND 131072),
  body_hash BLOB NOT NULL CHECK(typeof(body_hash)='blob' AND length(body_hash)=32),
  created_at TEXT NOT NULL,
  UNIQUE(event_id,organisation_id,family_id,event_kind),
  UNIQUE(event_id,organisation_id,family_id,event_kind,body_hash),
  UNIQUE(organisation_id,family_id,operation_key),
  UNIQUE(organisation_id,family_id,event_ordinal),
  FOREIGN KEY(family_id,organisation_id) REFERENCES bas_family_identities_v1(family_id,organisation_id) ON DELETE RESTRICT
);

CREATE INDEX bas_family_events_order_idx ON bas_family_events_v1(organisation_id,family_id,event_ordinal);

CREATE TABLE bas_export_events_v1 (
  append_ordinal INTEGER PRIMARY KEY AUTOINCREMENT CHECK(append_ordinal>0),
  export_event_id TEXT NOT NULL UNIQUE CHECK(length(export_event_id) BETWEEN 1 AND 128 AND length(trim(export_event_id))>0),
  operation_key TEXT NOT NULL CHECK(length(operation_key) BETWEEN 1 AND 256 AND length(trim(operation_key))>0),
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  target_kind TEXT NOT NULL CHECK(target_kind IN ('CALCULATION','FROZEN')),
  event_version INTEGER NOT NULL CHECK(typeof(event_version)='integer' AND event_version>0),
  calculation_id TEXT,
  calculation_version INTEGER CHECK(calculation_version IS NULL OR (typeof(calculation_version)='integer' AND calculation_version>0)),
  calculation_hash BLOB CHECK(calculation_hash IS NULL OR (typeof(calculation_hash)='blob' AND length(calculation_hash)=32)),
  frozen_id TEXT,
  frozen_hash BLOB CHECK(frozen_hash IS NULL OR (typeof(frozen_hash)='blob' AND length(frozen_hash)=32)),
  format TEXT NOT NULL CHECK(format IN ('PDF','CSV')),
  phase TEXT NOT NULL CHECK(phase IN ('STARTED','FINISHED')),
  outcome TEXT CHECK(outcome IS NULL OR outcome IN ('CANCELLED','FAILED','SUCCEEDED')),
  artifact_hash BLOB CHECK(artifact_hash IS NULL OR (typeof(artifact_hash)='blob' AND length(artifact_hash)=32)),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  reason TEXT NOT NULL CHECK(length(reason)<=1024),
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  export_proto BLOB NOT NULL CHECK(typeof(export_proto)='blob' AND length(export_proto) BETWEEN 1 AND 131072),
  recorded_at TEXT NOT NULL,
  UNIQUE(organisation_id,family_id,operation_key,phase),
  UNIQUE(export_event_id,organisation_id,family_id,content_hash),
  FOREIGN KEY(family_id,organisation_id) REFERENCES bas_family_identities_v1(family_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(calculation_id,organisation_id,family_id,calculation_version,calculation_hash) REFERENCES bas_calculations_v1(calculation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(frozen_id,organisation_id,family_id,frozen_hash) REFERENCES bas_frozen_bas_v1(frozen_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  CHECK((target_kind='CALCULATION' AND calculation_id IS NOT NULL AND calculation_version IS NOT NULL AND calculation_hash IS NOT NULL AND frozen_id IS NULL AND frozen_hash IS NULL) OR (target_kind='FROZEN' AND calculation_id IS NULL AND calculation_version IS NULL AND calculation_hash IS NULL AND frozen_id IS NOT NULL AND frozen_hash IS NOT NULL)),
  CHECK((phase='STARTED' AND outcome IS NULL AND artifact_hash IS NULL) OR (phase='FINISHED' AND outcome IS NOT NULL AND ((outcome='SUCCEEDED' AND artifact_hash IS NOT NULL) OR (outcome<>'SUCCEEDED' AND artifact_hash IS NULL))))
);
CREATE INDEX bas_export_events_page_idx ON bas_export_events_v1(organisation_id,family_id,recorded_at,export_event_id);
CREATE INDEX bas_export_events_family_scan_idx ON bas_export_events_v1(organisation_id,family_id,append_ordinal);
CREATE INDEX bas_export_events_calculation_page_idx ON bas_export_events_v1(organisation_id,calculation_id,calculation_version,calculation_hash,append_ordinal) WHERE target_kind='CALCULATION';
CREATE INDEX bas_export_events_frozen_page_idx ON bas_export_events_v1(organisation_id,frozen_id,frozen_hash,append_ordinal) WHERE target_kind='FROZEN';
CREATE UNIQUE INDEX bas_export_events_calculation_version_unique ON bas_export_events_v1(organisation_id,calculation_id,calculation_version,calculation_hash,event_version) WHERE target_kind='CALCULATION';
CREATE UNIQUE INDEX bas_export_events_frozen_version_unique ON bas_export_events_v1(organisation_id,frozen_id,frozen_hash,event_version) WHERE target_kind='FROZEN';

CREATE TABLE bas_external_observations_v1 (
  observation_id TEXT NOT NULL PRIMARY KEY CHECK(length(observation_id) BETWEEN 1 AND 128 AND length(trim(observation_id))>0),
  event_id TEXT NOT NULL UNIQUE,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  family_event_kind TEXT NOT NULL DEFAULT 'OBSERVATION' CHECK(family_event_kind='OBSERVATION'),
  frozen_id TEXT NOT NULL,
  frozen_hash BLOB NOT NULL CHECK(typeof(frozen_hash)='blob' AND length(frozen_hash)=32),
  event_kind TEXT NOT NULL CHECK(event_kind IN ('COMPLETED_LODGEMENT','ACCEPTANCE','PENDING','UNKNOWN','REJECTION','REVIEWER_HANDOFF','PAYMENT','REFUND')),
  channel TEXT NOT NULL CHECK(channel IN ('ONLINE_PORTAL','TAX_AGENT','PAPER','PHONE','OTHER')),
  claimed_event_at TEXT NOT NULL,
  external_reference TEXT NOT NULL CHECK(length(external_reference)<=512),
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 1024),
  trust TEXT NOT NULL CHECK(trust='USER_RECORDED_UNVERIFIED'),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  recorded_at TEXT NOT NULL,
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  observation_proto BLOB NOT NULL CHECK(typeof(observation_proto)='blob' AND length(observation_proto) BETWEEN 1 AND 131072),
  UNIQUE(observation_id,organisation_id,family_id,content_hash),
  UNIQUE(observation_id,organisation_id,family_id),
  FOREIGN KEY(family_id,organisation_id) REFERENCES bas_family_identities_v1(family_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(frozen_id,organisation_id,family_id,frozen_hash) REFERENCES bas_frozen_bas_v1(frozen_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(event_id,organisation_id,family_id,family_event_kind,content_hash) REFERENCES bas_family_events_v1(event_id,organisation_id,family_id,event_kind,body_hash) ON DELETE RESTRICT
);

CREATE TABLE bas_external_observation_corrections_v1 (
  correction_id TEXT NOT NULL PRIMARY KEY CHECK(length(correction_id) BETWEEN 1 AND 128 AND length(trim(correction_id))>0),
  event_id TEXT NOT NULL UNIQUE,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  family_event_kind TEXT NOT NULL DEFAULT 'CORRECTION' CHECK(family_event_kind='CORRECTION'),
  frozen_id TEXT NOT NULL,
  frozen_hash BLOB NOT NULL CHECK(typeof(frozen_hash)='blob' AND length(frozen_hash)=32),
  disputed_observation_id TEXT,
  disputed_observation_hash BLOB CHECK(disputed_observation_hash IS NULL OR (typeof(disputed_observation_hash)='blob' AND length(disputed_observation_hash)=32)),
  disputed_correction_id TEXT,
  disputed_correction_hash BLOB CHECK(disputed_correction_hash IS NULL OR (typeof(disputed_correction_hash)='blob' AND length(disputed_correction_hash)=32)),
  correction_kind TEXT NOT NULL CHECK(correction_kind IN ('RETRACT_AS_INCORRECT','REPLACE_ASSERTION')),
  corrected_event_kind TEXT CHECK(corrected_event_kind IS NULL OR corrected_event_kind IN ('COMPLETED_LODGEMENT','ACCEPTANCE','PENDING','UNKNOWN','REJECTION','REVIEWER_HANDOFF','PAYMENT','REFUND')),
  corrected_claimed_event_at TEXT,
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 1024),
  trust TEXT NOT NULL CHECK(trust='USER_RECORDED_UNVERIFIED'),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  recorded_at TEXT NOT NULL,
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  correction_proto BLOB NOT NULL CHECK(typeof(correction_proto)='blob' AND length(correction_proto) BETWEEN 1 AND 131072),
  UNIQUE(correction_id,organisation_id,family_id,content_hash),
  UNIQUE(correction_id,organisation_id,family_id),
  FOREIGN KEY(family_id,organisation_id) REFERENCES bas_family_identities_v1(family_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(frozen_id,organisation_id,family_id,frozen_hash) REFERENCES bas_frozen_bas_v1(frozen_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(disputed_observation_id,organisation_id,family_id,disputed_observation_hash) REFERENCES bas_external_observations_v1(observation_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(event_id,organisation_id,family_id,family_event_kind,content_hash) REFERENCES bas_family_events_v1(event_id,organisation_id,family_id,event_kind,body_hash) ON DELETE RESTRICT,
  FOREIGN KEY(disputed_correction_id,organisation_id,family_id,disputed_correction_hash) REFERENCES bas_external_observation_corrections_v1(correction_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  CHECK((disputed_observation_id IS NOT NULL AND disputed_observation_hash IS NOT NULL AND disputed_correction_id IS NULL AND disputed_correction_hash IS NULL) OR (disputed_observation_id IS NULL AND disputed_observation_hash IS NULL AND disputed_correction_id IS NOT NULL AND disputed_correction_hash IS NOT NULL)),
  CHECK(correction_id IS NOT disputed_observation_id AND correction_id IS NOT disputed_correction_id),
  CHECK((correction_kind='RETRACT_AS_INCORRECT' AND corrected_event_kind IS NULL AND corrected_claimed_event_at IS NULL) OR (correction_kind='REPLACE_ASSERTION' AND (corrected_event_kind IS NOT NULL OR corrected_claimed_event_at IS NOT NULL)))
);

CREATE INDEX bas_external_observations_frozen_idx ON bas_external_observations_v1(organisation_id,frozen_id,frozen_hash,event_id);
CREATE INDEX bas_external_corrections_frozen_idx ON bas_external_observation_corrections_v1(organisation_id,frozen_id,frozen_hash,event_id);

CREATE TABLE bas_dispute_resolutions_v1 (
  resolution_id TEXT NOT NULL PRIMARY KEY CHECK(length(resolution_id) BETWEEN 1 AND 128 AND length(trim(resolution_id))>0),
  event_id TEXT NOT NULL UNIQUE,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  family_event_kind TEXT NOT NULL DEFAULT 'RESOLUTION' CHECK(family_event_kind='RESOLUTION'),
  outcome TEXT NOT NULL CHECK(outcome IN ('UPHOLD_RECORDED_CHAIN','REQUIRE_CORRECTIVE_REVISION')),
  dependent_chain_manifest_hash BLOB NOT NULL CHECK(typeof(dependent_chain_manifest_hash)='blob' AND length(dependent_chain_manifest_hash)=32),
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 1024),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  resolved_at TEXT NOT NULL,
  content_hash BLOB NOT NULL CHECK(typeof(content_hash)='blob' AND length(content_hash)=32),
  resolution_proto BLOB NOT NULL CHECK(typeof(resolution_proto)='blob' AND length(resolution_proto) BETWEEN 1 AND 131072),
  UNIQUE(resolution_id,organisation_id,family_id,content_hash),
  UNIQUE(resolution_id,organisation_id,family_id),
  FOREIGN KEY(family_id,organisation_id) REFERENCES bas_family_identities_v1(family_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY(event_id,organisation_id,family_id,family_event_kind,content_hash) REFERENCES bas_family_events_v1(event_id,organisation_id,family_id,event_kind,body_hash) ON DELETE RESTRICT
);

CREATE TABLE bas_dispute_resolution_observations_v1 (
  resolution_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  subject_kind TEXT NOT NULL CHECK(subject_kind IN ('OBSERVATION','CORRECTION')),
  subject_id TEXT NOT NULL CHECK(length(subject_id) BETWEEN 1 AND 128 AND length(trim(subject_id))>0),
  subject_hash BLOB NOT NULL CHECK(typeof(subject_hash)='blob' AND length(subject_hash)=32),
  observation_id TEXT,
  observation_hash BLOB CHECK(observation_hash IS NULL OR (typeof(observation_hash)='blob' AND length(observation_hash)=32)),
  correction_id TEXT,
  correction_hash BLOB CHECK(correction_hash IS NULL OR (typeof(correction_hash)='blob' AND length(correction_hash)=32)),
  PRIMARY KEY(resolution_id,subject_kind,subject_id),
  FOREIGN KEY(resolution_id,organisation_id,family_id) REFERENCES bas_dispute_resolutions_v1(resolution_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(observation_id,organisation_id,family_id,observation_hash) REFERENCES bas_external_observations_v1(observation_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(correction_id,organisation_id,family_id,correction_hash) REFERENCES bas_external_observation_corrections_v1(correction_id,organisation_id,family_id,content_hash) ON DELETE RESTRICT,
  CHECK((subject_kind='OBSERVATION' AND observation_id IS NOT NULL AND observation_hash IS NOT NULL AND subject_id=observation_id AND subject_hash=observation_hash AND correction_id IS NULL AND correction_hash IS NULL)
    OR (subject_kind='CORRECTION' AND correction_id IS NOT NULL AND correction_hash IS NOT NULL AND subject_id=correction_id AND subject_hash=correction_hash AND observation_id IS NULL AND observation_hash IS NULL))
);

CREATE TABLE bas_dispute_resolution_preparations_v1 (
  resolution_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  preparation_id TEXT NOT NULL,
  preparation_version INTEGER NOT NULL CHECK(typeof(preparation_version)='integer' AND preparation_version>0),
  preparation_hash BLOB NOT NULL CHECK(typeof(preparation_hash)='blob' AND length(preparation_hash)=32),
  PRIMARY KEY(resolution_id,preparation_id,preparation_version),
  UNIQUE(resolution_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash),
  FOREIGN KEY(resolution_id,organisation_id,family_id) REFERENCES bas_dispute_resolutions_v1(resolution_id,organisation_id,family_id) ON DELETE RESTRICT,
  FOREIGN KEY(preparation_id,organisation_id,family_id,preparation_version,preparation_hash) REFERENCES bas_preparation_versions_v1(preparation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT
);

CREATE TRIGGER bas_profiles_v1_no_update BEFORE UPDATE ON bas_profiles_v1 BEGIN SELECT RAISE(ABORT,'BAS retained identity is immutable'); END;
CREATE TRIGGER bas_profiles_v1_no_delete BEFORE DELETE ON bas_profiles_v1 BEGIN SELECT RAISE(ABORT,'BAS retained identity is retained'); END;
CREATE TRIGGER bas_profiles_v1_no_conflicting_insert BEFORE INSERT ON bas_profiles_v1
WHEN EXISTS(SELECT 1 FROM bas_profiles_v1 WHERE profile_id=NEW.profile_id)
BEGIN SELECT RAISE(ABORT,'BAS retained identity cannot be replaced'); END;

CREATE TRIGGER bas_profile_versions_v1_no_update BEFORE UPDATE ON bas_profile_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS retained history is immutable'); END;
CREATE TRIGGER bas_profile_versions_v1_no_delete BEFORE DELETE ON bas_profile_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS retained history is retained'); END;
CREATE TRIGGER bas_profile_versions_v1_no_conflicting_insert BEFORE INSERT ON bas_profile_versions_v1
WHEN EXISTS(SELECT 1 FROM bas_profile_versions_v1 WHERE append_ordinal=NEW.append_ordinal OR (profile_id=NEW.profile_id AND version=NEW.version) OR (profile_id=NEW.profile_id AND organisation_id=NEW.organisation_id AND version=NEW.version AND content_hash=NEW.content_hash))
BEGIN SELECT RAISE(ABORT,'BAS retained history cannot be replaced'); END;
CREATE TRIGGER bas_profile_versions_v1_append_guard BEFORE INSERT ON bas_profile_versions_v1
WHEN NEW.version<>COALESCE((SELECT max(version)+1 FROM bas_profile_versions_v1 WHERE profile_id=NEW.profile_id),1)
BEGIN SELECT RAISE(ABORT,'BAS profile revisions must be consecutive'); END;

CREATE TRIGGER bas_profile_heads_v1_no_delete BEFORE DELETE ON bas_profile_heads_v1 BEGIN SELECT RAISE(ABORT,'BAS current pointer is retained'); END;
CREATE TRIGGER bas_profile_heads_v1_no_conflicting_insert BEFORE INSERT ON bas_profile_heads_v1
WHEN EXISTS(SELECT 1 FROM bas_profile_heads_v1 WHERE profile_id=NEW.profile_id)
BEGIN SELECT RAISE(ABORT,'BAS current pointer cannot be replaced'); END;
CREATE TRIGGER bas_profile_heads_v1_initial_guard BEFORE INSERT ON bas_profile_heads_v1
WHEN NEW.current_version<>1 OR NEW.cas_version<>1
BEGIN SELECT RAISE(ABORT,'BAS profile head must start at version one'); END;
CREATE TRIGGER bas_profile_heads_v1_cas_guard BEFORE UPDATE ON bas_profile_heads_v1
WHEN NEW.profile_id IS NOT OLD.profile_id OR NEW.organisation_id IS NOT OLD.organisation_id OR NEW.cas_version<>OLD.cas_version+1 OR NEW.current_version<>OLD.current_version+1 OR NEW.current_hash IS OLD.current_hash
BEGIN SELECT RAISE(ABORT,'BAS profile head compare-and-swap required'); END;

CREATE TRIGGER bas_periods_v1_no_update BEFORE UPDATE ON bas_periods_v1 BEGIN SELECT RAISE(ABORT,'BAS period identity is immutable'); END;
CREATE TRIGGER bas_periods_v1_no_delete BEFORE DELETE ON bas_periods_v1 BEGIN SELECT RAISE(ABORT,'BAS period identity is retained'); END;
CREATE TRIGGER bas_periods_v1_no_conflicting_insert BEFORE INSERT ON bas_periods_v1
WHEN EXISTS(SELECT 1 FROM bas_periods_v1 WHERE period_id=NEW.period_id OR (organisation_id=NEW.organisation_id AND period_start=NEW.period_start AND period_end=NEW.period_end AND reporting_window=NEW.reporting_window))
BEGIN SELECT RAISE(ABORT,'BAS period scope cannot be replaced'); END;

CREATE TRIGGER bas_period_versions_v1_no_update BEFORE UPDATE ON bas_period_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS retained history is immutable'); END;
CREATE TRIGGER bas_period_versions_v1_no_delete BEFORE DELETE ON bas_period_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS retained history is retained'); END;
CREATE TRIGGER bas_period_versions_v1_no_conflicting_insert BEFORE INSERT ON bas_period_versions_v1
WHEN EXISTS(SELECT 1 FROM bas_period_versions_v1 WHERE append_ordinal=NEW.append_ordinal OR (period_id=NEW.period_id AND version=NEW.version) OR (period_id=NEW.period_id AND organisation_id=NEW.organisation_id AND version=NEW.version AND content_hash=NEW.content_hash))
BEGIN SELECT RAISE(ABORT,'BAS retained history cannot be replaced'); END;
CREATE TRIGGER bas_period_versions_v1_append_guard BEFORE INSERT ON bas_period_versions_v1
WHEN NEW.version<>COALESCE((SELECT max(version)+1 FROM bas_period_versions_v1 WHERE period_id=NEW.period_id),1)
BEGIN SELECT RAISE(ABORT,'BAS period revisions must be consecutive'); END;

CREATE TRIGGER bas_period_heads_v1_no_delete BEFORE DELETE ON bas_period_heads_v1 BEGIN SELECT RAISE(ABORT,'BAS current pointer is retained'); END;
CREATE TRIGGER bas_period_heads_v1_no_conflicting_insert BEFORE INSERT ON bas_period_heads_v1
WHEN EXISTS(SELECT 1 FROM bas_period_heads_v1 WHERE period_id=NEW.period_id)
BEGIN SELECT RAISE(ABORT,'BAS current pointer cannot be replaced'); END;
CREATE TRIGGER bas_period_heads_v1_initial_guard BEFORE INSERT ON bas_period_heads_v1
WHEN NEW.current_version<>1 OR NEW.cas_version<>1
BEGIN SELECT RAISE(ABORT,'BAS period head must start at version one'); END;
CREATE TRIGGER bas_period_heads_v1_cas_guard BEFORE UPDATE ON bas_period_heads_v1
WHEN NEW.period_id IS NOT OLD.period_id OR NEW.organisation_id IS NOT OLD.organisation_id OR NEW.cas_version<>OLD.cas_version+1 OR NEW.current_version<>OLD.current_version+1 OR NEW.current_hash IS OLD.current_hash
BEGIN SELECT RAISE(ABORT,'BAS period head compare-and-swap required'); END;

CREATE TRIGGER bas_family_identities_v1_no_update BEFORE UPDATE ON bas_family_identities_v1 BEGIN SELECT RAISE(ABORT,'BAS family identity is immutable'); END;
CREATE TRIGGER bas_family_identities_v1_no_delete BEFORE DELETE ON bas_family_identities_v1 BEGIN SELECT RAISE(ABORT,'BAS family identity is retained'); END;
CREATE TRIGGER bas_family_identities_v1_no_conflicting_insert BEFORE INSERT ON bas_family_identities_v1
WHEN EXISTS(SELECT 1 FROM bas_family_identities_v1 WHERE family_id=NEW.family_id OR (organisation_id=NEW.organisation_id AND period_start=NEW.period_start AND period_end=NEW.period_end AND reporting_window=NEW.reporting_window))
BEGIN SELECT RAISE(ABORT,'BAS family scope cannot be replaced'); END;

CREATE TRIGGER bas_preparation_identities_v1_no_update BEFORE UPDATE ON bas_preparation_identities_v1 BEGIN SELECT RAISE(ABORT,'BAS preparation identity is immutable'); END;
CREATE TRIGGER bas_preparation_identities_v1_no_delete BEFORE DELETE ON bas_preparation_identities_v1 BEGIN SELECT RAISE(ABORT,'BAS preparation identity is retained'); END;
CREATE TRIGGER bas_preparation_identities_v1_no_conflicting_insert BEFORE INSERT ON bas_preparation_identities_v1
WHEN EXISTS(SELECT 1 FROM bas_preparation_identities_v1 WHERE preparation_id=NEW.preparation_id OR (organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND attempt_id=NEW.attempt_id))
BEGIN SELECT RAISE(ABORT,'BAS preparation attempt cannot be replaced'); END;

CREATE TRIGGER bas_payg_input_versions_v1_no_update BEFORE UPDATE ON bas_payg_input_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS retained input is immutable'); END;
CREATE TRIGGER bas_payg_input_versions_v1_no_delete BEFORE DELETE ON bas_payg_input_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS retained input is retained'); END;
CREATE TRIGGER bas_payg_input_versions_v1_no_conflicting_insert BEFORE INSERT ON bas_payg_input_versions_v1
WHEN EXISTS(SELECT 1 FROM bas_payg_input_versions_v1 WHERE input_snapshot_id=NEW.input_snapshot_id OR (preparation_id=NEW.preparation_id AND input_revision=NEW.input_revision))
BEGIN SELECT RAISE(ABORT,'BAS retained input cannot be replaced'); END;
CREATE TRIGGER bas_payg_input_versions_v1_append_guard BEFORE INSERT ON bas_payg_input_versions_v1
WHEN NEW.input_revision<>COALESCE((SELECT max(input_revision)+1 FROM bas_payg_input_versions_v1 WHERE preparation_id=NEW.preparation_id),1)
BEGIN SELECT RAISE(ABORT,'BAS PAYG input revisions must be consecutive'); END;
CREATE TRIGGER bas_payg_input_versions_v1_preparation_guard BEFORE INSERT ON bas_payg_input_versions_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_preparation_versions_v1 p
  WHERE p.preparation_id=NEW.preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id
    AND p.version=NEW.preparation_version AND p.content_hash=NEW.preparation_hash
    AND p.input_revision=NEW.input_revision AND p.input_hash=NEW.input_hash
)
BEGIN SELECT RAISE(ABORT,'BAS PAYG input envelope must pin its exact input-bearing preparation version'); END;

CREATE TRIGGER bas_calculations_v1_no_update BEFORE UPDATE ON bas_calculations_v1 BEGIN SELECT RAISE(ABORT,'BAS calculation is immutable'); END;
CREATE TRIGGER bas_calculations_v1_no_delete BEFORE DELETE ON bas_calculations_v1 BEGIN SELECT RAISE(ABORT,'BAS calculation is retained'); END;
CREATE TRIGGER bas_calculations_v1_no_conflicting_insert BEFORE INSERT ON bas_calculations_v1
WHEN EXISTS(SELECT 1 FROM bas_calculations_v1 WHERE append_ordinal=NEW.append_ordinal OR (calculation_id=NEW.calculation_id AND version=NEW.version) OR (calculation_id=NEW.calculation_id AND organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND preparation_id=NEW.preparation_id AND version=NEW.version AND content_hash=NEW.content_hash))
BEGIN SELECT RAISE(ABORT,'BAS calculation cannot be replaced'); END;
CREATE TRIGGER bas_calculations_v1_append_guard BEFORE INSERT ON bas_calculations_v1
WHEN NEW.version<>COALESCE((SELECT max(version)+1 FROM bas_calculations_v1 WHERE calculation_id=NEW.calculation_id),1)
BEGIN SELECT RAISE(ABORT,'BAS calculation revisions must be consecutive'); END;
CREATE TRIGGER bas_calculations_v1_owner_guard BEFORE INSERT ON bas_calculations_v1
WHEN EXISTS(SELECT 1 FROM bas_calculations_v1 prior WHERE prior.calculation_id=NEW.calculation_id
  AND (prior.organisation_id<>NEW.organisation_id OR prior.family_id<>NEW.family_id OR prior.preparation_id<>NEW.preparation_id))
BEGIN SELECT RAISE(ABORT,'BAS calculation identity owner cannot change'); END;
CREATE TRIGGER bas_calculations_v1_anchor_guard BEFORE INSERT ON bas_calculations_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_preparation_versions_v1 p
  WHERE p.preparation_id=NEW.preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id
    AND p.version=NEW.preparation_version AND p.content_hash=NEW.preparation_hash
    AND p.profile_id=NEW.profile_id AND p.profile_version=NEW.profile_version AND p.profile_hash=NEW.profile_hash
    AND p.period_id=NEW.period_id AND p.period_version=NEW.period_version AND p.period_hash=NEW.period_hash
    AND p.input_snapshot_id=NEW.input_snapshot_id AND p.input_revision=NEW.input_revision
    AND p.input_hash=NEW.input_hash AND p.input_content_hash=NEW.input_content_hash
    AND p.source_snapshot_hash=NEW.source_snapshot_hash
)
BEGIN SELECT RAISE(ABORT,'BAS calculation must bind the exact preparation, saved inputs, profile, period, and source snapshot'); END;

CREATE TRIGGER bas_calculation_coverage_v1_no_update BEFORE UPDATE ON bas_calculation_coverage_v1 BEGIN SELECT RAISE(ABORT,'BAS coverage is immutable'); END;
CREATE TRIGGER bas_calculation_coverage_v1_no_delete BEFORE DELETE ON bas_calculation_coverage_v1 BEGIN SELECT RAISE(ABORT,'BAS coverage is retained'); END;
CREATE TRIGGER bas_calculation_coverage_v1_no_conflicting_insert BEFORE INSERT ON bas_calculation_coverage_v1
WHEN EXISTS(SELECT 1 FROM bas_calculation_coverage_v1 WHERE calculation_id=NEW.calculation_id AND calculation_version=NEW.calculation_version AND sequence=NEW.sequence)
BEGIN SELECT RAISE(ABORT,'BAS coverage cannot be replaced'); END;

CREATE TRIGGER bas_validations_v1_no_update BEFORE UPDATE ON bas_validations_v1 BEGIN SELECT RAISE(ABORT,'BAS validation is immutable'); END;
CREATE TRIGGER bas_validations_v1_no_delete BEFORE DELETE ON bas_validations_v1 BEGIN SELECT RAISE(ABORT,'BAS validation is retained'); END;
CREATE TRIGGER bas_validations_v1_no_conflicting_insert BEFORE INSERT ON bas_validations_v1
WHEN EXISTS(SELECT 1 FROM bas_validations_v1 WHERE (validation_id=NEW.validation_id AND revision=NEW.revision) OR (validation_id=NEW.validation_id AND organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND preparation_id=NEW.preparation_id AND preparation_version=NEW.preparation_version AND revision=NEW.revision AND content_hash=NEW.content_hash))
BEGIN SELECT RAISE(ABORT,'BAS validation cannot be replaced'); END;
CREATE TRIGGER bas_validations_v1_append_guard BEFORE INSERT ON bas_validations_v1
WHEN NEW.revision<>COALESCE((SELECT max(revision)+1 FROM bas_validations_v1 WHERE validation_id=NEW.validation_id),1)
BEGIN SELECT RAISE(ABORT,'BAS validation revisions must be consecutive'); END;
CREATE TRIGGER bas_validations_v1_owner_guard BEFORE INSERT ON bas_validations_v1
WHEN EXISTS(SELECT 1 FROM bas_validations_v1 WHERE validation_id=NEW.validation_id
  AND (organisation_id<>NEW.organisation_id OR family_id<>NEW.family_id OR preparation_id<>NEW.preparation_id))
BEGIN SELECT RAISE(ABORT,'BAS validation identity owner cannot change'); END;
CREATE TRIGGER bas_validations_v1_anchor_guard BEFORE INSERT ON bas_validations_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_preparation_versions_v1 p JOIN bas_calculations_v1 c
    ON c.calculation_id=NEW.calculation_id AND c.organisation_id=NEW.organisation_id AND c.family_id=NEW.family_id
      AND c.preparation_id=NEW.preparation_id AND c.version=NEW.calculation_version AND c.content_hash=NEW.calculation_hash
  WHERE p.preparation_id=NEW.preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id
    AND p.version=NEW.preparation_version AND p.content_hash=NEW.preparation_hash
    AND p.calculation_id=NEW.calculation_id AND p.calculation_version=NEW.calculation_version AND p.calculation_hash=NEW.calculation_hash
    AND p.input_snapshot_id=NEW.input_snapshot_id AND p.input_revision=NEW.input_revision
    AND p.input_hash=NEW.input_hash AND p.input_content_hash=NEW.input_content_hash
    AND p.source_snapshot_hash=NEW.source_snapshot_hash
    AND c.preparation_version<NEW.preparation_version
    AND c.input_snapshot_id=NEW.input_snapshot_id AND c.input_revision=NEW.input_revision
    AND c.input_hash=NEW.input_hash AND c.input_content_hash=NEW.input_content_hash
    AND c.source_snapshot_hash=NEW.source_snapshot_hash
)
BEGIN SELECT RAISE(ABORT,'BAS validation must pin a later preparation version and its exact calculation, inputs, and source snapshot'); END;

CREATE TRIGGER bas_validation_findings_v1_no_update BEFORE UPDATE ON bas_validation_findings_v1 BEGIN SELECT RAISE(ABORT,'BAS validation finding is immutable'); END;
CREATE TRIGGER bas_validation_findings_v1_no_delete BEFORE DELETE ON bas_validation_findings_v1 BEGIN SELECT RAISE(ABORT,'BAS validation finding is retained'); END;
CREATE TRIGGER bas_validation_findings_v1_no_conflicting_insert BEFORE INSERT ON bas_validation_findings_v1
WHEN EXISTS(SELECT 1 FROM bas_validation_findings_v1 WHERE validation_id=NEW.validation_id AND revision=NEW.revision AND finding_id=NEW.finding_id)
BEGIN SELECT RAISE(ABORT,'BAS validation finding cannot be replaced'); END;

CREATE TRIGGER bas_warning_acknowledgements_v1_no_update BEFORE UPDATE ON bas_warning_acknowledgements_v1 BEGIN SELECT RAISE(ABORT,'BAS warning acknowledgement is immutable'); END;
CREATE TRIGGER bas_warning_acknowledgements_v1_no_delete BEFORE DELETE ON bas_warning_acknowledgements_v1 BEGIN SELECT RAISE(ABORT,'BAS warning acknowledgement is retained'); END;
CREATE TRIGGER bas_warning_acknowledgements_v1_no_conflicting_insert BEFORE INSERT ON bas_warning_acknowledgements_v1
WHEN EXISTS(SELECT 1 FROM bas_warning_acknowledgements_v1 WHERE acknowledgement_id=NEW.acknowledgement_id OR (organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND validation_id=NEW.validation_id AND validation_revision=NEW.validation_revision AND finding_id=NEW.finding_id))
BEGIN SELECT RAISE(ABORT,'BAS warning acknowledgement cannot be replaced'); END;
CREATE TRIGGER bas_warning_acknowledgements_v1_exact_warning_guard BEFORE INSERT ON bas_warning_acknowledgements_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_validations_v1 v JOIN bas_validation_findings_v1 f
    ON f.validation_id=v.validation_id AND f.organisation_id=v.organisation_id AND f.family_id=v.family_id
      AND f.revision=v.revision AND f.validation_hash=v.content_hash
  WHERE v.validation_id=NEW.validation_id AND v.organisation_id=NEW.organisation_id AND v.family_id=NEW.family_id
    AND v.revision=NEW.validation_revision AND v.content_hash=NEW.validation_hash
    AND v.preparation_id=NEW.preparation_id AND v.source_snapshot_hash=NEW.source_snapshot_hash
    AND f.finding_id=NEW.finding_id AND f.finding_hash=NEW.finding_hash AND f.severity='WARNING'
)
BEGIN SELECT RAISE(ABORT,'BAS acknowledgement must pin an exact warning in the same preparation snapshot'); END;

CREATE TRIGGER bas_frozen_bas_v1_no_update BEFORE UPDATE ON bas_frozen_bas_v1 BEGIN SELECT RAISE(ABORT,'BAS frozen approval is immutable'); END;
CREATE TRIGGER bas_frozen_bas_v1_no_delete BEFORE DELETE ON bas_frozen_bas_v1 BEGIN SELECT RAISE(ABORT,'BAS frozen approval is retained'); END;
CREATE TRIGGER bas_frozen_bas_v1_no_conflicting_insert BEFORE INSERT ON bas_frozen_bas_v1
WHEN EXISTS(SELECT 1 FROM bas_frozen_bas_v1 WHERE append_ordinal=NEW.append_ordinal OR frozen_id=NEW.frozen_id OR (preparation_id=NEW.preparation_id AND preparation_version=NEW.preparation_version) OR fresh_factor_assertion_id=NEW.fresh_factor_assertion_id)
BEGIN SELECT RAISE(ABORT,'BAS frozen approval cannot be replaced'); END;
CREATE TRIGGER bas_frozen_bas_v1_fresh_factor_guard BEFORE INSERT ON bas_frozen_bas_v1
WHEN NOT EXISTS(SELECT 1 FROM factor_assertions WHERE id=NEW.fresh_factor_assertion_id AND user_id=NEW.approval_actor_user_id AND purpose='bas_freeze_manual_handoff' AND consumed=0)
BEGIN SELECT RAISE(ABORT,'BAS freeze requires a fresh actor-bound assertion'); END;
CREATE TRIGGER bas_frozen_bas_v1_anchor_guard BEFORE INSERT ON bas_frozen_bas_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_preparation_versions_v1 p
  JOIN bas_calculations_v1 c ON c.calculation_id=NEW.calculation_id AND c.organisation_id=NEW.organisation_id
    AND c.family_id=NEW.family_id AND c.preparation_id=NEW.preparation_id
    AND c.version=NEW.calculation_version AND c.content_hash=NEW.calculation_hash
  JOIN bas_validations_v1 v ON v.validation_id=NEW.validation_id AND v.organisation_id=NEW.organisation_id
    AND v.family_id=NEW.family_id AND v.revision=NEW.validation_revision AND v.content_hash=NEW.validation_hash
  WHERE p.preparation_id=NEW.preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id
    AND p.version=NEW.preparation_version AND p.content_hash=NEW.preparation_hash
    AND p.state='REVIEW_READY' AND p.calculation_id=NEW.calculation_id
    AND p.calculation_version=NEW.calculation_version AND p.calculation_hash=NEW.calculation_hash
    AND p.validation_id=NEW.validation_id AND p.validation_revision=NEW.validation_revision AND p.validation_hash=NEW.validation_hash
    AND p.profile_id=NEW.profile_id AND p.profile_version=NEW.profile_version AND p.profile_hash=NEW.profile_hash
    AND p.period_id=NEW.period_id AND p.period_version=NEW.period_version AND p.period_hash=NEW.period_hash
    AND p.source_snapshot_hash=NEW.source_snapshot_hash
    AND c.preparation_version<NEW.preparation_version
    AND c.preparation_version<v.preparation_version AND v.preparation_version<p.version
    AND v.preparation_id=NEW.preparation_id AND v.calculation_id=NEW.calculation_id
    AND v.calculation_version=NEW.calculation_version AND v.calculation_hash=NEW.calculation_hash
    AND v.source_snapshot_hash=NEW.source_snapshot_hash AND c.source_snapshot_hash=NEW.source_snapshot_hash
)
BEGIN SELECT RAISE(ABORT,'BAS freeze must pin an exact review-ready preparation, prior calculation, and validation'); END;

CREATE TRIGGER bas_frozen_acknowledgements_v1_no_update BEFORE UPDATE ON bas_frozen_acknowledgements_v1 BEGIN SELECT RAISE(ABORT,'BAS frozen acknowledgement is immutable'); END;
CREATE TRIGGER bas_frozen_acknowledgements_v1_no_delete BEFORE DELETE ON bas_frozen_acknowledgements_v1 BEGIN SELECT RAISE(ABORT,'BAS frozen acknowledgement is retained'); END;
CREATE TRIGGER bas_frozen_acknowledgements_v1_no_conflicting_insert BEFORE INSERT ON bas_frozen_acknowledgements_v1
WHEN EXISTS(SELECT 1 FROM bas_frozen_acknowledgements_v1 WHERE frozen_id=NEW.frozen_id AND acknowledgement_id=NEW.acknowledgement_id)
BEGIN SELECT RAISE(ABORT,'BAS frozen acknowledgement cannot be replaced'); END;
CREATE TRIGGER bas_frozen_acknowledgements_v1_exact_snapshot_guard BEFORE INSERT ON bas_frozen_acknowledgements_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_frozen_bas_v1 f JOIN bas_warning_acknowledgements_v1 a
    ON a.acknowledgement_id=NEW.acknowledgement_id AND a.organisation_id=NEW.organisation_id
      AND a.family_id=NEW.family_id AND a.content_hash=NEW.acknowledgement_hash
  WHERE f.frozen_id=NEW.frozen_id AND f.organisation_id=NEW.organisation_id
    AND f.family_id=NEW.family_id AND f.content_hash=NEW.frozen_hash
    AND f.preparation_id=a.preparation_id AND f.validation_id=a.validation_id
    AND f.validation_revision=a.validation_revision AND f.validation_hash=a.validation_hash
    AND f.source_snapshot_hash=a.source_snapshot_hash
)
BEGIN SELECT RAISE(ABORT,'BAS frozen acknowledgement must match its approved validation and source snapshot'); END;

CREATE TRIGGER bas_preparation_versions_v1_no_update BEFORE UPDATE ON bas_preparation_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS preparation snapshot is immutable'); END;
CREATE TRIGGER bas_preparation_versions_v1_no_delete BEFORE DELETE ON bas_preparation_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS preparation snapshot is retained'); END;
CREATE TRIGGER bas_preparation_versions_v1_no_conflicting_insert BEFORE INSERT ON bas_preparation_versions_v1
WHEN EXISTS(SELECT 1 FROM bas_preparation_versions_v1 WHERE append_ordinal=NEW.append_ordinal OR (preparation_id=NEW.preparation_id AND version=NEW.version) OR (preparation_id=NEW.preparation_id AND organisation_id=NEW.organisation_id AND version=NEW.version AND content_hash=NEW.content_hash))
BEGIN SELECT RAISE(ABORT,'BAS preparation snapshot cannot be replaced'); END;
CREATE TRIGGER bas_preparation_versions_v1_append_guard BEFORE INSERT ON bas_preparation_versions_v1
WHEN NEW.version<>COALESCE((SELECT max(version)+1 FROM bas_preparation_versions_v1 WHERE preparation_id=NEW.preparation_id),1)
BEGIN SELECT RAISE(ABORT,'BAS preparation versions must be consecutive'); END;
CREATE TRIGGER bas_preparation_versions_v1_frozen_inputs_guard BEFORE INSERT ON bas_preparation_versions_v1
WHEN EXISTS(
  SELECT 1 FROM bas_preparation_versions_v1 prior
  WHERE prior.preparation_id=NEW.preparation_id
    AND prior.version=(SELECT max(version) FROM bas_preparation_versions_v1 WHERE preparation_id=NEW.preparation_id)
    AND (prior.freeze_id IS NOT NULL OR prior.state IN ('SUPERSEDED_BEFORE_CONFIRMATION','SUPERSEDED_AFTER_RECORDED_REJECTION','SUPERSEDED_BY_CONFIRMED_REVISION'))
    AND (NEW.profile_id IS NOT prior.profile_id OR NEW.profile_version IS NOT prior.profile_version OR NEW.profile_hash IS NOT prior.profile_hash
      OR NEW.period_id IS NOT prior.period_id OR NEW.period_version IS NOT prior.period_version OR NEW.period_hash IS NOT prior.period_hash
      OR NEW.input_snapshot_id IS NOT prior.input_snapshot_id OR NEW.input_revision IS NOT prior.input_revision OR NEW.input_hash IS NOT prior.input_hash OR NEW.input_content_hash IS NOT prior.input_content_hash
      OR NEW.calculation_id IS NOT prior.calculation_id OR NEW.calculation_version IS NOT prior.calculation_version OR NEW.calculation_hash IS NOT prior.calculation_hash
      OR NEW.validation_id IS NOT prior.validation_id OR NEW.validation_revision IS NOT prior.validation_revision OR NEW.validation_hash IS NOT prior.validation_hash
      OR NEW.freeze_id IS NOT prior.freeze_id OR NEW.freeze_hash IS NOT prior.freeze_hash
      OR NEW.source_snapshot_hash IS NOT prior.source_snapshot_hash)
)
BEGIN SELECT RAISE(ABORT,'BAS frozen preparation inputs cannot be changed'); END;
CREATE TRIGGER bas_preparation_versions_v1_anchor_guard BEFORE INSERT ON bas_preparation_versions_v1
WHEN (NEW.calculation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bas_calculations_v1 c
  WHERE c.calculation_id=NEW.calculation_id AND c.organisation_id=NEW.organisation_id AND c.family_id=NEW.family_id
    AND c.preparation_id=NEW.preparation_id AND c.version=NEW.calculation_version AND c.content_hash=NEW.calculation_hash
    AND c.profile_id=NEW.profile_id AND c.profile_version=NEW.profile_version AND c.profile_hash=NEW.profile_hash
    AND c.period_id=NEW.period_id AND c.period_version=NEW.period_version AND c.period_hash=NEW.period_hash
    AND c.input_snapshot_id=NEW.input_snapshot_id AND c.input_revision=NEW.input_revision
    AND c.input_hash=NEW.input_hash AND c.input_content_hash=NEW.input_content_hash
    AND c.source_snapshot_hash=NEW.source_snapshot_hash
    AND c.preparation_version<NEW.version
)) OR (NEW.validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bas_validations_v1 v
  WHERE v.validation_id=NEW.validation_id AND v.organisation_id=NEW.organisation_id AND v.family_id=NEW.family_id
    AND v.revision=NEW.validation_revision AND v.content_hash=NEW.validation_hash
    AND v.preparation_id=NEW.preparation_id AND v.calculation_id=NEW.calculation_id
    AND v.calculation_version=NEW.calculation_version AND v.calculation_hash=NEW.calculation_hash
    AND v.input_snapshot_id=NEW.input_snapshot_id AND v.input_revision=NEW.input_revision
    AND v.input_hash=NEW.input_hash AND v.input_content_hash=NEW.input_content_hash
    AND v.source_snapshot_hash=NEW.source_snapshot_hash
    AND v.preparation_version<NEW.version
)) OR NOT EXISTS(
  SELECT 1 FROM bas_periods_v1 p JOIN bas_family_identities_v1 f
    ON f.family_id=NEW.family_id AND f.organisation_id=NEW.organisation_id
  WHERE p.period_id=NEW.period_id AND p.organisation_id=NEW.organisation_id
    AND p.period_start=f.period_start AND p.period_end=f.period_end AND p.reporting_window=f.reporting_window
)
BEGIN SELECT RAISE(ABORT,'BAS preparation snapshot anchors do not match its exact calculation, validation, inputs, or family period'); END;
CREATE TRIGGER bas_preparation_versions_v1_freeze_link_guard BEFORE INSERT ON bas_preparation_versions_v1
WHEN NEW.freeze_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bas_frozen_bas_v1 f
  WHERE f.frozen_id=NEW.freeze_id AND f.organisation_id=NEW.organisation_id AND f.family_id=NEW.family_id
    AND f.preparation_id=NEW.preparation_id AND f.content_hash=NEW.freeze_hash AND f.preparation_version<NEW.version
)
BEGIN SELECT RAISE(ABORT,'BAS frozen preparation versions must point at an exact earlier retained approval'); END;

CREATE TRIGGER bas_preparation_heads_v1_no_delete BEFORE DELETE ON bas_preparation_heads_v1 BEGIN SELECT RAISE(ABORT,'BAS current pointer is retained'); END;
CREATE TRIGGER bas_preparation_heads_v1_no_conflicting_insert BEFORE INSERT ON bas_preparation_heads_v1
WHEN EXISTS(SELECT 1 FROM bas_preparation_heads_v1 WHERE preparation_id=NEW.preparation_id)
BEGIN SELECT RAISE(ABORT,'BAS current pointer cannot be replaced'); END;
CREATE TRIGGER bas_preparation_heads_v1_initial_guard BEFORE INSERT ON bas_preparation_heads_v1
WHEN NEW.current_version<>1 OR NEW.cas_version<>1
BEGIN SELECT RAISE(ABORT,'BAS preparation head must start at version one'); END;
CREATE TRIGGER bas_preparation_heads_v1_cas_guard BEFORE UPDATE ON bas_preparation_heads_v1
WHEN NEW.preparation_id IS NOT OLD.preparation_id OR NEW.organisation_id IS NOT OLD.organisation_id OR NEW.cas_version<>OLD.cas_version+1 OR NEW.current_version<>OLD.current_version+1 OR NEW.current_hash IS OLD.current_hash
BEGIN SELECT RAISE(ABORT,'BAS preparation head compare-and-swap required'); END;

CREATE TRIGGER bas_branch_identities_v1_no_update BEFORE UPDATE ON bas_branch_identities_v1 BEGIN SELECT RAISE(ABORT,'BAS branch identity is immutable'); END;
CREATE TRIGGER bas_branch_identities_v1_no_delete BEFORE DELETE ON bas_branch_identities_v1 BEGIN SELECT RAISE(ABORT,'BAS branch identity is retained'); END;
CREATE TRIGGER bas_branch_identities_v1_no_conflicting_insert BEFORE INSERT ON bas_branch_identities_v1
WHEN EXISTS(SELECT 1 FROM bas_branch_identities_v1 WHERE branch_id=NEW.branch_id)
BEGIN SELECT RAISE(ABORT,'BAS branch identity cannot be replaced'); END;
CREATE TRIGGER bas_branch_identities_v1_exact_head_guard BEFORE INSERT ON bas_branch_identities_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_family_state_v1 f
  WHERE f.family_id=NEW.family_id AND f.organisation_id=NEW.organisation_id
    AND f.root_preparation_id=NEW.family_root_preparation_id AND f.root_version=NEW.family_root_version AND f.root_hash=NEW.family_root_hash
    AND ((NEW.root_preparation_id IS NULL AND f.head_preparation_id IS NULL) OR (f.head_preparation_id=NEW.root_preparation_id AND f.head_version=NEW.root_version AND f.head_hash=NEW.root_hash))
    AND ((NEW.effective_original_preparation_id IS NULL AND f.effective_original_preparation_id IS NULL) OR (f.effective_original_preparation_id=NEW.effective_original_preparation_id AND f.effective_original_version=NEW.effective_original_version AND f.effective_original_hash=NEW.effective_original_hash))
)
BEGIN SELECT RAISE(ABORT,'BAS branch must pin the exact family root, current head, and effective original'); END;

CREATE TRIGGER bas_branch_versions_v1_no_update BEFORE UPDATE ON bas_branch_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS branch workflow snapshot is immutable'); END;
CREATE TRIGGER bas_branch_versions_v1_no_delete BEFORE DELETE ON bas_branch_versions_v1 BEGIN SELECT RAISE(ABORT,'BAS branch workflow snapshot is retained'); END;
CREATE TRIGGER bas_branch_versions_v1_no_conflicting_insert BEFORE INSERT ON bas_branch_versions_v1
WHEN EXISTS(SELECT 1 FROM bas_branch_versions_v1 WHERE append_ordinal=NEW.append_ordinal OR (branch_id=NEW.branch_id AND version=NEW.version) OR (branch_id=NEW.branch_id AND organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND version=NEW.version AND content_hash=NEW.content_hash))
BEGIN SELECT RAISE(ABORT,'BAS branch workflow snapshot cannot be replaced'); END;
CREATE TRIGGER bas_branch_versions_v1_append_guard BEFORE INSERT ON bas_branch_versions_v1
WHEN NEW.version<>COALESCE((SELECT max(version)+1 FROM bas_branch_versions_v1 WHERE branch_id=NEW.branch_id),1)
  OR (NEW.version=1 AND NEW.workflow_state<>'ACTIVE')
BEGIN SELECT RAISE(ABORT,'BAS branch workflow versions must be consecutive'); END;
CREATE TRIGGER bas_branch_versions_v1_anchor_guard BEFORE INSERT ON bas_branch_versions_v1
WHEN (NEW.active_attempt_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bas_preparation_identities_v1 i JOIN bas_preparation_versions_v1 p
    ON p.preparation_id=NEW.active_preparation_id AND p.organisation_id=NEW.organisation_id
      AND p.family_id=NEW.family_id AND p.version=NEW.active_attempt_version AND p.content_hash=NEW.active_attempt_hash
  WHERE i.preparation_id=NEW.active_preparation_id AND i.organisation_id=NEW.organisation_id
    AND i.family_id=NEW.family_id AND i.branch_id=NEW.branch_id AND i.attempt_id=NEW.active_attempt_id
)) OR (NEW.workflow_state='CORRECTIVE' AND NOT EXISTS(
  SELECT 1 FROM bas_dispute_resolutions_v1 r
  WHERE r.resolution_id=NEW.corrective_resolution_id AND r.organisation_id=NEW.organisation_id
    AND r.family_id=NEW.family_id AND r.content_hash=NEW.corrective_resolution_hash
    AND r.outcome='REQUIRE_CORRECTIVE_REVISION'
)) OR (NEW.barrier_preparation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bas_preparation_versions_v1 p JOIN bas_frozen_bas_v1 f
    ON f.frozen_id=NEW.barrier_frozen_id AND f.organisation_id=NEW.organisation_id AND f.family_id=NEW.family_id
      AND f.preparation_id=NEW.barrier_preparation_id AND f.preparation_version<NEW.barrier_preparation_version
      AND f.content_hash=NEW.barrier_frozen_hash
  WHERE p.preparation_id=NEW.barrier_preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id
    AND p.version=NEW.barrier_preparation_version AND p.content_hash=NEW.barrier_preparation_hash
    AND p.freeze_id=NEW.barrier_frozen_id AND p.freeze_hash=NEW.barrier_frozen_hash
    AND p.state='FROZEN_FOR_MANUAL_HANDOFF'
))
BEGIN SELECT RAISE(ABORT,'BAS branch workflow must retain exact attempt, corrective resolution, and optional frozen barrier references'); END;
CREATE TRIGGER bas_branch_versions_v1_corrective_adoption_guard BEFORE INSERT ON bas_branch_versions_v1
WHEN EXISTS(
  SELECT 1 FROM bas_branch_versions_v1 prior
  WHERE prior.branch_id=NEW.branch_id AND prior.version=(SELECT max(version) FROM bas_branch_versions_v1 WHERE branch_id=NEW.branch_id)
    AND ((prior.workflow_state='ACTIVE' AND NEW.workflow_state='CORRECTIVE' AND NOT (
      -- A resolution can reserve an empty branch, reset a mutable attempt, or
      -- retain the exact already-frozen attempt behind its own barrier.
      (prior.active_preparation_id IS NULL AND NEW.active_preparation_id IS NULL AND NEW.barrier_preparation_id IS NULL)
      OR (NEW.active_preparation_id=prior.active_preparation_id AND NEW.active_attempt_id=prior.active_attempt_id AND EXISTS(
        SELECT 1 FROM bas_preparation_versions_v1 oldp JOIN bas_preparation_versions_v1 newp
          ON newp.preparation_id=NEW.active_preparation_id AND newp.organisation_id=NEW.organisation_id AND newp.family_id=NEW.family_id
            AND newp.version=NEW.active_attempt_version AND newp.content_hash=NEW.active_attempt_hash
        WHERE oldp.preparation_id=prior.active_preparation_id AND oldp.organisation_id=NEW.organisation_id AND oldp.family_id=NEW.family_id
          AND oldp.version=prior.active_attempt_version AND oldp.content_hash=prior.active_attempt_hash
          AND ((oldp.state IN ('COLLECTING','BLOCKED','REVIEW_READY') AND newp.state='COLLECTING'
            AND newp.version>=oldp.version AND newp.calculation_id IS NULL AND newp.validation_id IS NULL AND NEW.barrier_preparation_id IS NULL)
          OR (oldp.state='FROZEN_FOR_MANUAL_HANDOFF' AND NEW.active_attempt_version=prior.active_attempt_version
            AND NEW.active_attempt_hash=prior.active_attempt_hash AND NEW.barrier_preparation_id=prior.active_preparation_id
            AND NEW.barrier_preparation_version=prior.active_attempt_version AND NEW.barrier_preparation_hash=prior.active_attempt_hash
            AND NEW.barrier_attempt_id=prior.active_attempt_id))
      ))
    )) OR (prior.workflow_state='CORRECTIVE' AND (
      NEW.corrective_resolution_id IS NOT prior.corrective_resolution_id
      OR NEW.corrective_resolution_hash IS NOT prior.corrective_resolution_hash
      OR NEW.workflow_state NOT IN ('CORRECTIVE','RESOLVED','CLOSED')
      OR NOT (
        (NEW.barrier_preparation_id IS prior.barrier_preparation_id AND NEW.barrier_preparation_version IS prior.barrier_preparation_version
          AND NEW.barrier_preparation_hash IS prior.barrier_preparation_hash AND NEW.barrier_attempt_id IS prior.barrier_attempt_id
          AND NEW.barrier_frozen_id IS prior.barrier_frozen_id AND NEW.barrier_frozen_hash IS prior.barrier_frozen_hash)
        OR (prior.barrier_preparation_id IS NOT NULL AND NEW.barrier_preparation_id IS NULL
          AND NEW.active_preparation_id<>prior.active_preparation_id AND EXISTS(
            SELECT 1 FROM bas_preparation_versions_v1 successor JOIN bas_preparation_versions_v1 terminal
              ON terminal.preparation_id=successor.predecessor_id AND terminal.organisation_id=NEW.organisation_id AND terminal.family_id=NEW.family_id
                AND terminal.version=successor.predecessor_version AND terminal.content_hash=successor.predecessor_hash
              JOIN bas_preparation_heads_v1 h ON h.preparation_id=terminal.preparation_id AND h.organisation_id=terminal.organisation_id
                AND h.current_version=terminal.version AND h.current_hash=terminal.content_hash
            WHERE successor.preparation_id=NEW.active_preparation_id AND successor.organisation_id=NEW.organisation_id AND successor.family_id=NEW.family_id
              AND successor.version=NEW.active_attempt_version AND successor.content_hash=NEW.active_attempt_hash
              AND successor.state='COLLECTING' AND successor.freeze_id IS NULL
              AND terminal.preparation_id=prior.active_preparation_id AND terminal.version>prior.barrier_preparation_version
              AND terminal.state IN ('SUPERSEDED_BEFORE_CONFIRMATION','SUPERSEDED_AFTER_RECORDED_REJECTION')
          ))
      )
    )) OR (prior.workflow_state IN ('RESOLVED','CLOSED') AND NEW.workflow_state IS NOT prior.workflow_state))
)
BEGIN SELECT RAISE(ABORT,'BAS corrective workflow must preserve its resolution and clear a barrier only for an exact retained retry'); END;

CREATE TRIGGER bas_branch_heads_v1_no_delete BEFORE DELETE ON bas_branch_heads_v1 BEGIN SELECT RAISE(ABORT,'BAS current pointer is retained'); END;
CREATE TRIGGER bas_branch_heads_v1_no_conflicting_insert BEFORE INSERT ON bas_branch_heads_v1
WHEN EXISTS(SELECT 1 FROM bas_branch_heads_v1 WHERE branch_id=NEW.branch_id)
BEGIN SELECT RAISE(ABORT,'BAS current pointer cannot be replaced'); END;
CREATE TRIGGER bas_branch_heads_v1_initial_guard BEFORE INSERT ON bas_branch_heads_v1
WHEN NEW.current_version<>1 OR NEW.cas_version<>1
BEGIN SELECT RAISE(ABORT,'BAS branch head must start at version one'); END;
CREATE TRIGGER bas_branch_heads_v1_one_live_branch_guard BEFORE INSERT ON bas_branch_heads_v1
WHEN EXISTS(
  SELECT 1 FROM bas_branch_heads_v1 h JOIN bas_branch_versions_v1 v
    ON v.branch_id=h.branch_id AND v.organisation_id=h.organisation_id AND v.family_id=h.family_id
      AND v.version=h.current_version AND v.content_hash=h.current_hash
  WHERE h.organisation_id=NEW.organisation_id AND h.family_id=NEW.family_id
    AND v.workflow_state IN ('ACTIVE','CORRECTIVE')
) AND EXISTS(
  SELECT 1 FROM bas_branch_versions_v1 v WHERE v.branch_id=NEW.branch_id
    AND v.organisation_id=NEW.organisation_id AND v.family_id=NEW.family_id
    AND v.version=NEW.current_version AND v.content_hash=NEW.current_hash
    AND v.workflow_state IN ('ACTIVE','CORRECTIVE')
)
BEGIN SELECT RAISE(ABORT,'BAS family can have only one active branch'); END;
CREATE TRIGGER bas_branch_heads_v1_cas_guard BEFORE UPDATE ON bas_branch_heads_v1
WHEN NEW.branch_id IS NOT OLD.branch_id OR NEW.family_id IS NOT OLD.family_id OR NEW.organisation_id IS NOT OLD.organisation_id OR NEW.cas_version<>OLD.cas_version+1 OR NEW.current_version<>OLD.current_version+1 OR NEW.current_hash IS OLD.current_hash
BEGIN SELECT RAISE(ABORT,'BAS branch head compare-and-swap required'); END;
CREATE TRIGGER bas_branch_heads_v1_one_live_branch_update_guard BEFORE UPDATE ON bas_branch_heads_v1
WHEN EXISTS(
  SELECT 1 FROM bas_branch_heads_v1 h JOIN bas_branch_versions_v1 v
    ON v.branch_id=h.branch_id AND v.organisation_id=h.organisation_id AND v.family_id=h.family_id
      AND v.version=h.current_version AND v.content_hash=h.current_hash
  WHERE h.organisation_id=NEW.organisation_id AND h.family_id=NEW.family_id AND h.branch_id<>NEW.branch_id
    AND v.workflow_state IN ('ACTIVE','CORRECTIVE')
) AND EXISTS(
  SELECT 1 FROM bas_branch_versions_v1 v WHERE v.branch_id=NEW.branch_id
    AND v.organisation_id=NEW.organisation_id AND v.family_id=NEW.family_id
    AND v.version=NEW.current_version AND v.content_hash=NEW.current_hash
    AND v.workflow_state IN ('ACTIVE','CORRECTIVE')
)
BEGIN SELECT RAISE(ABORT,'BAS family can have only one active branch'); END;

CREATE TRIGGER bas_family_state_v1_no_delete BEFORE DELETE ON bas_family_state_v1 BEGIN SELECT RAISE(ABORT,'BAS family state is retained'); END;
CREATE TRIGGER bas_family_state_v1_no_conflicting_insert BEFORE INSERT ON bas_family_state_v1
WHEN EXISTS(SELECT 1 FROM bas_family_state_v1 WHERE family_id=NEW.family_id)
BEGIN SELECT RAISE(ABORT,'BAS family state cannot be replaced'); END;
CREATE TRIGGER bas_family_state_v1_initial_guard BEFORE INSERT ON bas_family_state_v1
WHEN NEW.cas_version<>1 OR NEW.active_branch_id IS NOT NULL OR NEW.last_family_event_ordinal<>0 OR NEW.overlay_state<>'NONE'
  OR NOT EXISTS(SELECT 1 FROM bas_preparation_versions_v1 p WHERE p.preparation_id=NEW.root_preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id AND p.version=NEW.root_version AND p.content_hash=NEW.root_hash AND p.version=1 AND p.state='COLLECTING' AND p.freeze_id IS NULL)
  OR (NEW.effective_original_preparation_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM bas_preparation_versions_v1 p WHERE p.preparation_id=NEW.effective_original_preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id AND p.version=NEW.effective_original_version AND p.content_hash=NEW.effective_original_hash AND p.state='EXTERNAL_CONFIRMATION_RECORDED' AND p.freeze_id IS NOT NULL))
  OR (NEW.head_preparation_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM bas_preparation_versions_v1 p WHERE p.preparation_id=NEW.head_preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id AND p.version=NEW.head_version AND p.content_hash=NEW.head_hash AND p.state='EXTERNAL_CONFIRMATION_RECORDED' AND p.freeze_id IS NOT NULL))
BEGIN SELECT RAISE(ABORT,'BAS family state must retain its initial collecting root with a matching CAS'); END;
CREATE TRIGGER bas_family_state_v1_cas_guard BEFORE UPDATE ON bas_family_state_v1
WHEN NEW.family_id IS NOT OLD.family_id OR NEW.organisation_id IS NOT OLD.organisation_id OR NEW.cas_version<>OLD.cas_version+1 OR NEW.family_state_proto IS OLD.family_state_proto OR NEW.state_hash IS OLD.state_hash OR NEW.last_family_event_ordinal<OLD.last_family_event_ordinal
BEGIN SELECT RAISE(ABORT,'BAS family state compare-and-swap required'); END;
CREATE TRIGGER bas_family_state_v1_root_immutable BEFORE UPDATE ON bas_family_state_v1
WHEN NEW.root_preparation_id IS NOT OLD.root_preparation_id OR NEW.root_version IS NOT OLD.root_version OR NEW.root_hash IS NOT OLD.root_hash
BEGIN SELECT RAISE(ABORT,'BAS initial preparation root is immutable'); END;
CREATE TRIGGER bas_family_state_v1_confirmed_head_guard BEFORE UPDATE ON bas_family_state_v1
WHEN NOT EXISTS(SELECT 1 FROM bas_preparation_versions_v1 p WHERE p.preparation_id=NEW.root_preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id AND p.version=NEW.root_version AND p.content_hash=NEW.root_hash AND p.version=1 AND p.state='COLLECTING' AND p.freeze_id IS NULL)
  OR (NEW.effective_original_preparation_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM bas_preparation_versions_v1 p WHERE p.preparation_id=NEW.effective_original_preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id AND p.version=NEW.effective_original_version AND p.content_hash=NEW.effective_original_hash AND p.state='EXTERNAL_CONFIRMATION_RECORDED' AND p.freeze_id IS NOT NULL))
  OR (NEW.head_preparation_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM bas_preparation_versions_v1 p WHERE p.preparation_id=NEW.head_preparation_id AND p.organisation_id=NEW.organisation_id AND p.family_id=NEW.family_id AND p.version=NEW.head_version AND p.content_hash=NEW.head_hash AND p.state='EXTERNAL_CONFIRMATION_RECORDED' AND p.freeze_id IS NOT NULL))
  OR (OLD.head_preparation_id IS NOT NULL AND NEW.head_preparation_id IS NOT NULL AND NEW.head_preparation_id=OLD.head_preparation_id AND NEW.head_version<OLD.head_version)
  OR (OLD.head_preparation_id IS NOT NULL AND NEW.head_preparation_id IS NULL)
  OR (OLD.effective_original_preparation_id IS NOT NULL AND (NEW.effective_original_preparation_id IS NOT OLD.effective_original_preparation_id OR NEW.effective_original_version IS NOT OLD.effective_original_version OR NEW.effective_original_hash IS NOT OLD.effective_original_hash))
BEGIN SELECT RAISE(ABORT,'BAS family root must retain its initial snapshot and effective heads must pin confirmed versions'); END;
CREATE TRIGGER bas_family_state_v1_active_branch_guard BEFORE UPDATE ON bas_family_state_v1
WHEN NEW.active_branch_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bas_branch_identities_v1 i JOIN bas_branch_versions_v1 v
    ON v.branch_id=NEW.active_branch_id AND v.organisation_id=NEW.organisation_id AND v.family_id=NEW.family_id
      AND v.version=NEW.active_branch_version AND v.content_hash=NEW.active_branch_hash
  WHERE i.branch_id=NEW.active_branch_id AND i.organisation_id=NEW.organisation_id AND i.family_id=NEW.family_id
    AND i.identity_hash=NEW.active_branch_identity_hash AND v.workflow_state IN ('ACTIVE','CORRECTIVE')
)
BEGIN SELECT RAISE(ABORT,'BAS active branch must point at a retained live workflow version'); END;
CREATE TRIGGER bas_family_state_v1_event_ordinal_guard BEFORE UPDATE ON bas_family_state_v1
WHEN NEW.last_family_event_ordinal>OLD.last_family_event_ordinal AND NOT EXISTS(
  SELECT 1 FROM bas_family_events_v1 e WHERE e.organisation_id=NEW.organisation_id AND e.family_id=NEW.family_id AND e.event_ordinal=NEW.last_family_event_ordinal
)
BEGIN SELECT RAISE(ABORT,'BAS family state event cursor must point at a retained event'); END;

CREATE TRIGGER bas_family_events_v1_no_update BEFORE UPDATE ON bas_family_events_v1 BEGIN SELECT RAISE(ABORT,'BAS family event is immutable'); END;
CREATE TRIGGER bas_family_events_v1_no_delete BEFORE DELETE ON bas_family_events_v1 BEGIN SELECT RAISE(ABORT,'BAS family event is retained'); END;
CREATE TRIGGER bas_family_events_v1_no_conflicting_insert BEFORE INSERT ON bas_family_events_v1
WHEN EXISTS(SELECT 1 FROM bas_family_events_v1 WHERE event_id=NEW.event_id OR (organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND operation_key=NEW.operation_key) OR (NEW.event_ordinal IS NOT NULL AND event_ordinal=NEW.event_ordinal))
BEGIN SELECT RAISE(ABORT,'BAS family event cannot be replaced or replayed'); END;

CREATE TRIGGER bas_export_events_v1_no_update BEFORE UPDATE ON bas_export_events_v1 BEGIN SELECT RAISE(ABORT,'BAS export event is immutable'); END;
CREATE TRIGGER bas_export_events_v1_no_delete BEFORE DELETE ON bas_export_events_v1 BEGIN SELECT RAISE(ABORT,'BAS export event is retained'); END;
CREATE TRIGGER bas_export_events_v1_no_conflicting_insert BEFORE INSERT ON bas_export_events_v1
WHEN EXISTS(SELECT 1 FROM bas_export_events_v1 WHERE export_event_id=NEW.export_event_id OR append_ordinal=NEW.append_ordinal
  OR (organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND operation_key=NEW.operation_key AND phase=NEW.phase)
  OR (organisation_id=NEW.organisation_id AND target_kind=NEW.target_kind AND event_version=NEW.event_version
    AND calculation_id IS NEW.calculation_id AND calculation_version IS NEW.calculation_version AND calculation_hash IS NEW.calculation_hash
    AND frozen_id IS NEW.frozen_id AND frozen_hash IS NEW.frozen_hash))
BEGIN SELECT RAISE(ABORT,'BAS export event cannot be replaced or replayed'); END;
CREATE TRIGGER bas_export_events_v1_consecutive_version BEFORE INSERT ON bas_export_events_v1
WHEN NEW.event_version<>COALESCE((SELECT MAX(event_version) FROM bas_export_events_v1
  WHERE organisation_id=NEW.organisation_id AND target_kind=NEW.target_kind
    AND calculation_id IS NEW.calculation_id AND calculation_version IS NEW.calculation_version AND calculation_hash IS NEW.calculation_hash
    AND frozen_id IS NEW.frozen_id AND frozen_hash IS NEW.frozen_hash),0)+1
BEGIN SELECT RAISE(ABORT,'BAS export target history must be consecutive'); END;

CREATE TRIGGER bas_external_observations_v1_no_update BEFORE UPDATE ON bas_external_observations_v1 BEGIN SELECT RAISE(ABORT,'BAS external observation is immutable'); END;
CREATE TRIGGER bas_external_observations_v1_no_delete BEFORE DELETE ON bas_external_observations_v1 BEGIN SELECT RAISE(ABORT,'BAS external observation is retained'); END;
CREATE TRIGGER bas_external_observations_v1_no_conflicting_insert BEFORE INSERT ON bas_external_observations_v1
WHEN EXISTS(SELECT 1 FROM bas_external_observations_v1 WHERE observation_id=NEW.observation_id OR event_id=NEW.event_id) OR EXISTS(SELECT 1 FROM bas_external_observation_corrections_v1 WHERE correction_id=NEW.observation_id)
BEGIN SELECT RAISE(ABORT,'BAS external observation cannot be replaced'); END;

CREATE TRIGGER bas_external_observation_corrections_v1_no_update BEFORE UPDATE ON bas_external_observation_corrections_v1 BEGIN SELECT RAISE(ABORT,'BAS observation correction is immutable'); END;
CREATE TRIGGER bas_external_observation_corrections_v1_no_delete BEFORE DELETE ON bas_external_observation_corrections_v1 BEGIN SELECT RAISE(ABORT,'BAS observation correction is retained'); END;
CREATE TRIGGER bas_external_observation_corrections_v1_no_conflicting_insert BEFORE INSERT ON bas_external_observation_corrections_v1
WHEN EXISTS(SELECT 1 FROM bas_external_observation_corrections_v1 WHERE correction_id=NEW.correction_id OR event_id=NEW.event_id) OR EXISTS(SELECT 1 FROM bas_external_observations_v1 WHERE observation_id=NEW.correction_id)
BEGIN SELECT RAISE(ABORT,'BAS observation correction cannot be replaced'); END;

CREATE TRIGGER bas_dispute_resolutions_v1_no_update BEFORE UPDATE ON bas_dispute_resolutions_v1 BEGIN SELECT RAISE(ABORT,'BAS dispute resolution is immutable'); END;
CREATE TRIGGER bas_dispute_resolutions_v1_no_delete BEFORE DELETE ON bas_dispute_resolutions_v1 BEGIN SELECT RAISE(ABORT,'BAS dispute resolution is retained'); END;
CREATE TRIGGER bas_dispute_resolutions_v1_no_conflicting_insert BEFORE INSERT ON bas_dispute_resolutions_v1
WHEN EXISTS(SELECT 1 FROM bas_dispute_resolutions_v1 WHERE resolution_id=NEW.resolution_id OR event_id=NEW.event_id)
BEGIN SELECT RAISE(ABORT,'BAS dispute resolution cannot be replaced'); END;

CREATE TRIGGER bas_dispute_resolution_observations_v1_no_update BEFORE UPDATE ON bas_dispute_resolution_observations_v1 BEGIN SELECT RAISE(ABORT,'BAS resolution link is immutable'); END;
CREATE TRIGGER bas_dispute_resolution_observations_v1_no_delete BEFORE DELETE ON bas_dispute_resolution_observations_v1 BEGIN SELECT RAISE(ABORT,'BAS resolution link is retained'); END;
CREATE TRIGGER bas_dispute_resolution_observations_v1_no_conflicting_insert BEFORE INSERT ON bas_dispute_resolution_observations_v1
WHEN EXISTS(SELECT 1 FROM bas_dispute_resolution_observations_v1 WHERE resolution_id=NEW.resolution_id AND subject_kind=NEW.subject_kind AND subject_id=NEW.subject_id)
BEGIN SELECT RAISE(ABORT,'BAS resolution link cannot be replaced'); END;

CREATE TRIGGER bas_dispute_resolution_preparations_v1_no_update BEFORE UPDATE ON bas_dispute_resolution_preparations_v1 BEGIN SELECT RAISE(ABORT,'BAS resolution preparation link is immutable'); END;
CREATE TRIGGER bas_dispute_resolution_preparations_v1_no_delete BEFORE DELETE ON bas_dispute_resolution_preparations_v1 BEGIN SELECT RAISE(ABORT,'BAS resolution preparation link is retained'); END;
CREATE TRIGGER bas_dispute_resolution_preparations_v1_no_conflicting_insert BEFORE INSERT ON bas_dispute_resolution_preparations_v1
WHEN EXISTS(SELECT 1 FROM bas_dispute_resolution_preparations_v1 WHERE resolution_id=NEW.resolution_id AND preparation_id=NEW.preparation_id AND preparation_version=NEW.preparation_version)
BEGIN SELECT RAISE(ABORT,'BAS resolution preparation link cannot be replaced'); END;


CREATE TRIGGER bas_external_observation_corrections_v1_subject_order_guard BEFORE INSERT ON bas_external_observation_corrections_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_family_events_v1 current_event JOIN bas_family_events_v1 subject_event
    ON subject_event.organisation_id=NEW.organisation_id AND subject_event.family_id=NEW.family_id
  WHERE current_event.event_id=NEW.event_id AND current_event.organisation_id=NEW.organisation_id
    AND current_event.family_id=NEW.family_id AND subject_event.event_ordinal<current_event.event_ordinal
    AND (subject_event.event_id=(SELECT event_id FROM bas_external_observations_v1 WHERE observation_id=NEW.disputed_observation_id AND organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND content_hash=NEW.disputed_observation_hash AND frozen_id=NEW.frozen_id AND frozen_hash=NEW.frozen_hash)
      OR subject_event.event_id=(SELECT event_id FROM bas_external_observation_corrections_v1 WHERE correction_id=NEW.disputed_correction_id AND organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND content_hash=NEW.disputed_correction_hash AND frozen_id=NEW.frozen_id AND frozen_hash=NEW.frozen_hash))
)
BEGIN SELECT RAISE(ABORT,'BAS correction must reference an exact earlier retained subject'); END;

CREATE TRIGGER bas_dispute_resolution_observations_v1_subject_order_guard BEFORE INSERT ON bas_dispute_resolution_observations_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_dispute_resolutions_v1 r JOIN bas_family_events_v1 resolution_event
    ON resolution_event.event_id=r.event_id AND resolution_event.organisation_id=NEW.organisation_id AND resolution_event.family_id=NEW.family_id
  JOIN bas_family_events_v1 subject_event ON subject_event.organisation_id=NEW.organisation_id AND subject_event.family_id=NEW.family_id
  WHERE r.resolution_id=NEW.resolution_id AND r.organisation_id=NEW.organisation_id AND r.family_id=NEW.family_id
    AND subject_event.event_ordinal<resolution_event.event_ordinal
    AND (subject_event.event_id=(SELECT event_id FROM bas_external_observations_v1 WHERE observation_id=NEW.observation_id AND organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND content_hash=NEW.observation_hash)
      OR subject_event.event_id=(SELECT event_id FROM bas_external_observation_corrections_v1 WHERE correction_id=NEW.correction_id AND organisation_id=NEW.organisation_id AND family_id=NEW.family_id AND content_hash=NEW.correction_hash))
)
BEGIN SELECT RAISE(ABORT,'BAS resolution must reference an exact earlier retained subject'); END;
