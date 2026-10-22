-- Record the PAYGI method selected by the first confirmed BAS statement in each
-- externally confirmed income year. The exact retained period/profile pair is
-- the immutable proof for that decision.
CREATE TABLE bas_income_year_pins_v1 (
  organisation_id TEXT NOT NULL CHECK(typeof(organisation_id)='text') REFERENCES organisations(id) ON DELETE RESTRICT,
  income_year_id TEXT NOT NULL CHECK(typeof(income_year_id)='text' AND length(income_year_id)=36),
  income_year_start TEXT NOT NULL CHECK(
    typeof(income_year_start)='text'
    AND length(income_year_start)=10
    AND income_year_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    AND income_year_start>='0001-01-01' AND date(income_year_start,'+0 days') IS NOT NULL
    AND date(income_year_start,'+0 days')=income_year_start
  ),
  income_year_end TEXT NOT NULL CHECK(
    typeof(income_year_end)='text'
    AND length(income_year_end)=10
    AND income_year_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    AND income_year_end>='0001-01-01' AND date(income_year_end,'+0 days') IS NOT NULL
    AND date(income_year_end,'+0 days')=income_year_end
  ),
  method INTEGER NOT NULL CHECK(typeof(method)='integer' AND method IN (2,3)),
  first_period_id TEXT NOT NULL CHECK(typeof(first_period_id)='text'),
  first_period_version INTEGER NOT NULL CHECK(typeof(first_period_version)='integer' AND first_period_version>0),
  first_period_hash BLOB NOT NULL CHECK(typeof(first_period_hash)='blob' AND length(first_period_hash)=32),
  first_profile_id TEXT NOT NULL CHECK(typeof(first_profile_id)='text'),
  first_profile_version INTEGER NOT NULL CHECK(typeof(first_profile_version)='integer' AND first_profile_version>0),
  first_profile_hash BLOB NOT NULL CHECK(typeof(first_profile_hash)='blob' AND length(first_profile_hash)=32),
  pinned_at TEXT NOT NULL CHECK(typeof(pinned_at)='text' AND length(pinned_at)>0),
  PRIMARY KEY(organisation_id,income_year_id),
  UNIQUE(organisation_id,income_year_start,income_year_end),
  UNIQUE(organisation_id,first_period_id,first_period_version),
  FOREIGN KEY(first_period_id,organisation_id,first_period_version,first_period_hash)
    REFERENCES bas_period_versions_v1(period_id,organisation_id,version,content_hash) ON DELETE RESTRICT,
  FOREIGN KEY(first_profile_id,organisation_id,first_profile_version,first_profile_hash)
    REFERENCES bas_profile_versions_v1(profile_id,organisation_id,version,content_hash) ON DELETE RESTRICT,
  CHECK(income_year_start<=income_year_end)
);

-- The overlap query used by confirmation is organization- and range-scoped.
CREATE INDEX bas_income_year_pins_overlap_idx
  ON bas_income_year_pins_v1(organisation_id,income_year_start,income_year_end);

CREATE TRIGGER bas_income_year_pins_v1_no_update
BEFORE UPDATE ON bas_income_year_pins_v1
BEGIN SELECT RAISE(ABORT,'BAS income-year method pins are immutable'); END;

CREATE TRIGGER bas_income_year_pins_v1_no_delete
BEFORE DELETE ON bas_income_year_pins_v1
BEGIN SELECT RAISE(ABORT,'BAS income-year method pins are retained'); END;

-- INSERT OR REPLACE must not bypass append-only semantics if recursive triggers
-- are disabled by a caller's SQLite configuration.
CREATE TRIGGER bas_income_year_pins_v1_no_conflicting_insert
BEFORE INSERT ON bas_income_year_pins_v1
WHEN EXISTS(
  SELECT 1 FROM bas_income_year_pins_v1 prior
  WHERE prior.organisation_id=NEW.organisation_id
    AND (prior.income_year_id=NEW.income_year_id
      OR (prior.income_year_start=NEW.income_year_start AND prior.income_year_end=NEW.income_year_end))
)
BEGIN SELECT RAISE(ABORT,'BAS income-year method pin cannot be replaced'); END;

-- Different identities may not claim overlapping dates, even if their methods
-- happen to agree. A later nonoverlapping income year remains independently pin-able.
CREATE TRIGGER bas_income_year_pins_v1_no_overlapping_identity
BEFORE INSERT ON bas_income_year_pins_v1
WHEN EXISTS(
  SELECT 1 FROM bas_income_year_pins_v1 prior
  WHERE prior.organisation_id=NEW.organisation_id
    AND prior.income_year_id<>NEW.income_year_id
    AND prior.income_year_start<=NEW.income_year_end
    AND prior.income_year_end>=NEW.income_year_start
)
BEGIN SELECT RAISE(ABORT,'BAS income-year identity overlaps another retained pin'); END;

-- The period version itself carries the exact profile reference. Require the
-- normalized pin columns to repeat that same reciprocal edge.
CREATE TRIGGER bas_income_year_pins_v1_require_period_profile_pair
BEFORE INSERT ON bas_income_year_pins_v1
WHEN NOT EXISTS(
  SELECT 1 FROM bas_period_versions_v1 period
  WHERE period.period_id=NEW.first_period_id
    AND period.organisation_id=NEW.organisation_id
    AND period.version=NEW.first_period_version
    AND period.content_hash=NEW.first_period_hash
    AND period.profile_id=NEW.first_profile_id
    AND period.profile_version=NEW.first_profile_version
    AND period.profile_hash=NEW.first_profile_hash
)
BEGIN SELECT RAISE(ABORT,'BAS income-year pin must reference its period exact profile'); END;
