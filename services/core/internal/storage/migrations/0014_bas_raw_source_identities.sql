-- Additive raw-source companion for BAS calculation coverage. Migration 0013 remains unchanged.
CREATE TABLE bas_calculation_raw_coverage_v1 (
  calculation_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  family_id TEXT NOT NULL,
  calculation_version INTEGER NOT NULL CHECK(typeof(calculation_version)='integer' AND calculation_version>0),
  calculation_hash BLOB NOT NULL CHECK(typeof(calculation_hash)='blob' AND length(calculation_hash)=32),
  sequence INTEGER NOT NULL CHECK(typeof(sequence)='integer' AND sequence>0),
  candidate_kind TEXT NOT NULL CHECK(candidate_kind IN ('INVOICE_POSTING','PURCHASE_SETTLEMENT','PURCHASE_CREDIT','MANUAL_TAX_FACT','PAYG_FACT','OPENING_OR_LEGACY_FACT','OTHER_OBLIGATION')),
  disposition TEXT NOT NULL CHECK(disposition IN ('ELIGIBLE','EXCLUDED_WITH_REASON','UNRESOLVED','UNSUPPORTED','REVIEWED_ZERO','NOT_APPLICABLE')),
  raw_source_type TEXT NOT NULL CHECK(
    typeof(raw_source_type)='text'
    AND length(raw_source_type) BETWEEN 1 AND 128
    AND raw_source_type GLOB '[a-z]*'
    AND raw_source_type NOT GLOB '*[^a-z0-9_]*'
  ),
  raw_source_id TEXT NOT NULL CHECK(
    typeof(raw_source_id)='text'
    AND length(CAST(raw_source_id AS BLOB)) BETWEEN 1 AND 1048576
    AND length(trim(raw_source_id))>0
  ),
  original_revision INTEGER CHECK(original_revision IS NULL OR (typeof(original_revision)='integer' AND original_revision>=0)),
  original_content_hash BLOB CHECK(original_content_hash IS NULL OR (typeof(original_content_hash)='blob' AND length(original_content_hash)=32)),
  computed_proof_hash BLOB NOT NULL CHECK(typeof(computed_proof_hash)='blob' AND length(computed_proof_hash)=32),
  proof_scope TEXT NOT NULL CHECK(typeof(proof_scope)='text' AND proof_scope IN ('SOURCE_ROW_PROJECTION','COMPLETE_JOURNAL','REFERENCE_ONLY_PROJECTION')),
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
  FOREIGN KEY(calculation_id,organisation_id,family_id,calculation_version,calculation_hash)
    REFERENCES bas_calculations_v1(calculation_id,organisation_id,family_id,version,content_hash) ON DELETE RESTRICT,
  CHECK(disposition<>'ELIGIBLE' OR (eligible_amount_minor IS NOT NULL AND currency_code='AUD' AND tax_date IS NOT NULL))
);

CREATE TRIGGER bas_calculation_raw_coverage_v1_no_update
BEFORE UPDATE ON bas_calculation_raw_coverage_v1
BEGIN SELECT RAISE(ABORT,'BAS raw coverage is immutable'); END;

CREATE TRIGGER bas_calculation_raw_coverage_v1_no_delete
BEFORE DELETE ON bas_calculation_raw_coverage_v1
BEGIN SELECT RAISE(ABORT,'BAS raw coverage is retained'); END;

-- Reject replacement explicitly; immutable update/delete triggers alone do not
-- prevent INSERT OR REPLACE from deleting a conflicting row when recursive
-- triggers are disabled on a SQLite connection.
CREATE TRIGGER bas_calculation_raw_coverage_v1_no_conflicting_insert
BEFORE INSERT ON bas_calculation_raw_coverage_v1
WHEN EXISTS(
  SELECT 1 FROM bas_calculation_raw_coverage_v1 prior
  WHERE prior.calculation_id=NEW.calculation_id
    AND prior.calculation_version=NEW.calculation_version
    AND prior.sequence=NEW.sequence
)
BEGIN SELECT RAISE(ABORT,'BAS raw coverage cannot be replaced'); END;

-- Coverage from both tables shares the same calculation/version/sequence space.
CREATE TRIGGER bas_calculation_raw_coverage_v1_no_conflicting_canonical_insert
BEFORE INSERT ON bas_calculation_raw_coverage_v1
WHEN EXISTS(
  SELECT 1 FROM bas_calculation_coverage_v1 canonical
  WHERE canonical.calculation_id=NEW.calculation_id
    AND canonical.calculation_version=NEW.calculation_version
    AND canonical.sequence=NEW.sequence
)
BEGIN SELECT RAISE(ABORT,'BAS coverage sequence is already canonical'); END;

CREATE TRIGGER bas_calculation_coverage_v1_no_conflicting_raw_insert
BEFORE INSERT ON bas_calculation_coverage_v1
WHEN EXISTS(
  SELECT 1 FROM bas_calculation_raw_coverage_v1 raw
  WHERE raw.calculation_id=NEW.calculation_id
    AND raw.calculation_version=NEW.calculation_version
    AND raw.sequence=NEW.sequence
)
BEGIN SELECT RAISE(ABORT,'BAS coverage sequence is already raw'); END;
