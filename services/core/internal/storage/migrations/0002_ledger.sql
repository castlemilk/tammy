CREATE TABLE organisations (
  id TEXT PRIMARY KEY,
  legal_name TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  entity_type TEXT NOT NULL DEFAULT '',
  trading_name TEXT,
  abn TEXT UNIQUE,
  gst_basis INTEGER NOT NULL DEFAULT 0 CHECK (gst_basis BETWEEN 0 AND 2),
  gst_reporting_frequency INTEGER NOT NULL DEFAULT 0 CHECK (gst_reporting_frequency BETWEEN 0 AND 3),
  financial_year_end_month INTEGER NOT NULL DEFAULT 12 CHECK (financial_year_end_month BETWEEN 1 AND 12),
  owner_user_id TEXT NOT NULL DEFAULT '',
  active_tax_rule_type TEXT NOT NULL DEFAULT '',
  active_tax_rule_id TEXT NOT NULL DEFAULT '',
  active_tax_rule_revision INTEGER NOT NULL DEFAULT 0 CHECK (active_tax_rule_revision >= 0),
  active_tax_rule_content_hash BLOB NOT NULL DEFAULT X'' CHECK (length(active_tax_rule_content_hash) IN (0,32)),
  status TEXT NOT NULL CHECK (status IN ('ACTIVE','INACTIVE')),
  verification_state TEXT NOT NULL DEFAULT 'UNVERIFIED' CHECK (verification_state IN ('UNVERIFIED','PENDING','VERIFIED','FAILED','EXPIRED','SUPERSEDED')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT
);

CREATE TABLE accounts (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  code TEXT NOT NULL,
  name TEXT NOT NULL,
  account_type TEXT NOT NULL CHECK (account_type IN ('ASSET','LIABILITY','EQUITY','REVENUE','OTHER_REVENUE','EXPENSE','OTHER_EXPENSE','CONTRA')),
  subtype TEXT,
  normal_balance TEXT NOT NULL CHECK (normal_balance IN ('DEBIT','CREDIT')),
  status TEXT NOT NULL CHECK (status IN ('ACTIVE','ARCHIVED')),
  designation TEXT NOT NULL CHECK (designation IN ('ORDINARY','SYSTEM','CONTROL')),
  default_tax_code_id TEXT,
  report_classification TEXT NOT NULL DEFAULT '',
  cash_flow_classification TEXT NOT NULL DEFAULT '',
  owner_module TEXT NOT NULL DEFAULT 'ledger' CHECK (owner_module = 'ledger'),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (organisation_id, code)
);

CREATE TRIGGER organisations_singleton_insert
BEFORE INSERT ON organisations
WHEN EXISTS (SELECT 1 FROM organisations)
BEGIN
  SELECT RAISE(ABORT, 'workspace already has an organisation');
END;

CREATE TRIGGER accounts_control_fields_immutable
BEFORE UPDATE ON accounts
WHEN OLD.designation IN ('SYSTEM','CONTROL') AND (
  NEW.organisation_id <> OLD.organisation_id
  OR NEW.code <> OLD.code
  OR NEW.name <> OLD.name
  OR NEW.account_type <> OLD.account_type
  OR NEW.subtype IS NOT OLD.subtype
  OR NEW.normal_balance <> OLD.normal_balance
  OR NEW.status <> OLD.status
  OR NEW.designation <> OLD.designation
  OR NEW.default_tax_code_id IS NOT OLD.default_tax_code_id
  OR NEW.report_classification <> OLD.report_classification
  OR NEW.cash_flow_classification <> OLD.cash_flow_classification
)
BEGIN
  SELECT RAISE(ABORT, 'system and control accounts cannot be repurposed');
END;

CREATE TRIGGER accounts_protected_no_delete
BEFORE DELETE ON accounts
WHEN OLD.designation IN ('SYSTEM','CONTROL')
BEGIN
  SELECT RAISE(ABORT, 'system and control accounts cannot be deleted');
END;

CREATE TABLE accounting_periods (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  start_date TEXT NOT NULL,
  end_date TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('OPEN','CLOSED')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  closed_at TEXT NOT NULL,
  reopened_at TEXT,
  CHECK (start_date <= end_date),
  UNIQUE (organisation_id, start_date, end_date)
);

CREATE TRIGGER accounting_period_transition_guard
BEFORE UPDATE ON accounting_periods
WHEN NOT (
  OLD.state = 'CLOSED' AND NEW.state = 'OPEN' AND NEW.version = OLD.version + 1
  AND NEW.id = OLD.id AND NEW.organisation_id = OLD.organisation_id
  AND NEW.start_date = OLD.start_date AND NEW.end_date = OLD.end_date
  AND NEW.closed_at = OLD.closed_at AND OLD.reopened_at IS NULL AND NEW.reopened_at IS NOT NULL
)
BEGIN
  SELECT RAISE(ABORT, 'invalid accounting period transition');
END;

CREATE TRIGGER accounting_period_no_delete BEFORE DELETE ON accounting_periods BEGIN
  SELECT RAISE(ABORT, 'accounting periods are immutable history');
END;

CREATE UNIQUE INDEX accounting_period_one_closed
ON accounting_periods(organisation_id) WHERE state = 'CLOSED';

CREATE TABLE opening_conversions (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  conversion_date TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('DRAFT','POSTED','REPLACED')),
  source_sha256 TEXT NOT NULL CHECK (length(source_sha256) = 64),
  journal_id TEXT UNIQUE REFERENCES journals(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  replaced_by_id TEXT UNIQUE REFERENCES opening_conversions(id) ON DELETE RESTRICT,
  financial_revision INTEGER CHECK (financial_revision > 0),
  created_at TEXT NOT NULL
);

CREATE TABLE opening_items (
  id TEXT PRIMARY KEY,
  conversion_id TEXT NOT NULL REFERENCES opening_conversions(id) ON DELETE CASCADE,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  item_kind TEXT NOT NULL CHECK (item_kind IN ('ORDINARY','CUSTOMER_OPEN_ITEM','SUPPLIER_OPEN_ITEM','FINANCIAL_ACCOUNT','UNALLOCATED_CREDIT','OPENING_EQUITY')),
  debit_minor INTEGER NOT NULL DEFAULT 0 CHECK (debit_minor >= 0),
  credit_minor INTEGER NOT NULL DEFAULT 0 CHECK (credit_minor >= 0),
  currency_code TEXT NOT NULL CHECK (currency_code = 'AUD'),
  source_type TEXT,
  source_id TEXT,
  source_revision INTEGER CHECK (source_revision IS NULL OR source_revision > 0),
  source_content_hash BLOB CHECK (source_content_hash IS NULL OR length(source_content_hash) = 32),
  original_issue_date TEXT,
  original_due_date TEXT,
  outstanding_gst_minor INTEGER,
  prior_gst_attributed_minor INTEGER,
  latest_statement_date TEXT,
  latest_statement_balance_minor INTEGER,
  CHECK ((debit_minor > 0 AND credit_minor = 0) OR (credit_minor > 0 AND debit_minor = 0))
);

CREATE UNIQUE INDEX opening_conversions_one_current
ON opening_conversions(organisation_id) WHERE state = 'POSTED';

CREATE TRIGGER opening_conversion_transition_guard
BEFORE UPDATE ON opening_conversions
WHEN NOT (
  OLD.state = 'DRAFT' AND NEW.state = 'POSTED'
  AND NEW.version = OLD.version AND NEW.id = OLD.id AND NEW.organisation_id = OLD.organisation_id
  AND NEW.conversion_date = OLD.conversion_date AND NEW.source_sha256 = OLD.source_sha256
  AND OLD.journal_id IS NULL AND NEW.journal_id IS NOT NULL
  AND OLD.financial_revision IS NULL AND NEW.financial_revision IS NOT NULL
  AND NEW.replaced_by_id IS NULL
) AND NOT (
  OLD.state = 'POSTED' AND NEW.state = 'REPLACED' AND NEW.version = OLD.version + 1
  AND NEW.id = OLD.id AND NEW.organisation_id = OLD.organisation_id
  AND NEW.conversion_date = OLD.conversion_date AND NEW.source_sha256 = OLD.source_sha256
  AND NEW.journal_id = OLD.journal_id AND NEW.financial_revision = OLD.financial_revision
  AND OLD.replaced_by_id IS NULL AND NEW.replaced_by_id IS NOT NULL
)
BEGIN
  SELECT RAISE(ABORT, 'invalid opening conversion transition');
END;

CREATE TRIGGER opening_items_immutable_update BEFORE UPDATE ON opening_items BEGIN
  SELECT RAISE(ABORT, 'opening items are immutable');
END;
CREATE TRIGGER opening_items_immutable_delete BEFORE DELETE ON opening_items BEGIN
  SELECT RAISE(ABORT, 'opening items are immutable');
END;

CREATE TABLE sales_opening_receivables (
  id TEXT PRIMARY KEY,
  conversion_id TEXT NOT NULL REFERENCES opening_conversions(id) ON DELETE RESTRICT,
  retained_input_proto BLOB NOT NULL CHECK (length(retained_input_proto) BETWEEN 1 AND 1048576),
  created_at TEXT NOT NULL
);
CREATE TABLE purchase_opening_payables (
  id TEXT PRIMARY KEY,
  conversion_id TEXT NOT NULL REFERENCES opening_conversions(id) ON DELETE RESTRICT,
  retained_input_proto BLOB NOT NULL CHECK (length(retained_input_proto) BETWEEN 1 AND 1048576),
  created_at TEXT NOT NULL
);
CREATE TABLE banking_opening_accounts (
  id TEXT PRIMARY KEY,
  conversion_id TEXT NOT NULL REFERENCES opening_conversions(id) ON DELETE RESTRICT,
  retained_input_proto BLOB NOT NULL CHECK (length(retained_input_proto) BETWEEN 1 AND 1048576),
  created_at TEXT NOT NULL
);

CREATE TRIGGER sales_opening_receivables_immutable_update BEFORE UPDATE ON sales_opening_receivables BEGIN
  SELECT RAISE(ABORT, 'sales opening rows are immutable');
END;
CREATE TRIGGER purchase_opening_payables_immutable_update BEFORE UPDATE ON purchase_opening_payables BEGIN
  SELECT RAISE(ABORT, 'purchase opening rows are immutable');
END;
CREATE TRIGGER banking_opening_accounts_immutable_update BEFORE UPDATE ON banking_opening_accounts BEGIN
  SELECT RAISE(ABORT, 'banking opening rows are immutable');
END;

CREATE TABLE journals (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  source_type TEXT NOT NULL CHECK (source_type IN ('MANUAL','REVERSAL','OPENING','RECEIVABLE','PAYABLE','BANKING','TAX','SYSTEM')),
  source_id TEXT NOT NULL,
  source_revision INTEGER NOT NULL CHECK (source_revision > 0),
  state TEXT NOT NULL CHECK (state IN ('DRAFT','POSTED','REVERSED')),
  journal_date TEXT NOT NULL,
  description TEXT NOT NULL,
  reversal_of_journal_id TEXT UNIQUE REFERENCES journals(id) ON DELETE RESTRICT,
  reversed_by_journal_id TEXT UNIQUE REFERENCES journals(id) ON DELETE RESTRICT,
  total_debits_minor INTEGER NOT NULL CHECK (total_debits_minor > 0),
  total_credits_minor INTEGER NOT NULL CHECK (total_credits_minor > 0),
  currency_code TEXT NOT NULL CHECK (currency_code = 'AUD'),
  financial_revision INTEGER NOT NULL CHECK (financial_revision > 0),
  posted_at TEXT,
  created_at TEXT NOT NULL,
  CHECK ((state = 'DRAFT' AND posted_at IS NULL) OR (state IN ('POSTED','REVERSED') AND posted_at IS NOT NULL)),
  CHECK ((source_type = 'REVERSAL') = (reversal_of_journal_id IS NOT NULL)),
  CHECK ((state = 'REVERSED') = (reversed_by_journal_id IS NOT NULL)),
  UNIQUE (organisation_id, source_type, source_id, source_revision)
);

CREATE TABLE journal_lines (
  id TEXT PRIMARY KEY,
  journal_id TEXT NOT NULL REFERENCES journals(id) ON DELETE CASCADE,
  line_number INTEGER NOT NULL CHECK (line_number > 0),
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  debit_minor INTEGER NOT NULL DEFAULT 0 CHECK (debit_minor >= 0),
  credit_minor INTEGER NOT NULL DEFAULT 0 CHECK (credit_minor >= 0),
  currency_code TEXT NOT NULL CHECK (currency_code = 'AUD'),
  memo TEXT,
  tax_code_id TEXT,
  tax_amount_minor INTEGER,
  tax_rule_type TEXT,
  tax_rule_id TEXT,
  tax_rule_revision INTEGER CHECK (tax_rule_revision IS NULL OR tax_rule_revision > 0),
  tax_rule_content_hash BLOB CHECK (tax_rule_content_hash IS NULL OR length(tax_rule_content_hash) = 32),
  CHECK ((debit_minor > 0 AND credit_minor = 0) OR (credit_minor > 0 AND debit_minor = 0)),
  CHECK ((tax_code_id IS NULL) = (tax_amount_minor IS NULL)),
  CHECK ((tax_code_id IS NULL) = (tax_rule_id IS NULL)),
  UNIQUE (journal_id, line_number)
);

CREATE TABLE tax_facts (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  journal_line_id TEXT NOT NULL UNIQUE REFERENCES journal_lines(id) ON DELETE RESTRICT,
  tax_code TEXT NOT NULL,
  treatment TEXT NOT NULL CHECK (treatment IN ('TAXABLE','GST_FREE','INPUT_TAXED','OUT_OF_SCOPE','ADJUSTMENT')),
  original_gross_minor INTEGER NOT NULL,
  original_net_minor INTEGER NOT NULL,
  original_gst_minor INTEGER NOT NULL,
  attributed_gross_minor INTEGER NOT NULL,
  attributed_net_minor INTEGER NOT NULL,
  attributed_gst_minor INTEGER NOT NULL,
  remaining_gross_minor INTEGER NOT NULL,
  remaining_net_minor INTEGER NOT NULL,
  remaining_gst_minor INTEGER NOT NULL,
  tax_rule_type TEXT NOT NULL,
  tax_rule_id TEXT NOT NULL,
  tax_rule_revision INTEGER NOT NULL CHECK (tax_rule_revision > 0),
  tax_rule_content_hash BLOB NOT NULL CHECK (length(tax_rule_content_hash) = 32),
  source_type TEXT NOT NULL,
  source_id TEXT NOT NULL,
  source_revision INTEGER NOT NULL CHECK (source_revision > 0),
  created_at TEXT NOT NULL
);

CREATE TABLE cash_flow_facts (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  journal_line_id TEXT NOT NULL REFERENCES journal_lines(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  category TEXT NOT NULL CHECK (category IN ('OPERATING','INVESTING','FINANCING','TRANSFER','NONCASH')),
  amount_minor INTEGER NOT NULL CHECK (amount_minor <> 0),
  source_revision INTEGER NOT NULL CHECK (source_revision > 0),
  created_at TEXT NOT NULL,
  UNIQUE (journal_line_id, sequence)
);

CREATE TRIGGER journals_posted_immutable
BEFORE UPDATE ON journals
WHEN OLD.state IN ('POSTED','REVERSED') AND NOT (
  OLD.state = 'POSTED' AND NEW.state = 'REVERSED'
  AND OLD.reversed_by_journal_id IS NULL AND NEW.reversed_by_journal_id IS NOT NULL
  AND NEW.version = OLD.version + 1
  AND NEW.id = OLD.id AND NEW.organisation_id = OLD.organisation_id
  AND NEW.source_type = OLD.source_type AND NEW.source_id = OLD.source_id
  AND NEW.source_revision = OLD.source_revision AND NEW.journal_date = OLD.journal_date
  AND NEW.description = OLD.description AND NEW.reversal_of_journal_id IS OLD.reversal_of_journal_id
  AND NEW.total_debits_minor = OLD.total_debits_minor
  AND NEW.total_credits_minor = OLD.total_credits_minor
  AND NEW.currency_code = OLD.currency_code
  AND NEW.financial_revision = OLD.financial_revision
  AND NEW.posted_at = OLD.posted_at AND NEW.created_at = OLD.created_at
)
BEGIN
  SELECT RAISE(ABORT, 'posted journals are immutable except direct reversal link');
END;

CREATE TRIGGER journal_lines_immutable_update BEFORE UPDATE ON journal_lines BEGIN
  SELECT RAISE(ABORT, 'journal lines are immutable');
END;
CREATE TRIGGER journal_lines_immutable_delete BEFORE DELETE ON journal_lines BEGIN
  SELECT RAISE(ABORT, 'journal lines are immutable');
END;
CREATE TRIGGER tax_facts_immutable_update BEFORE UPDATE ON tax_facts BEGIN
  SELECT RAISE(ABORT, 'tax facts are immutable');
END;
CREATE TRIGGER tax_facts_immutable_delete BEFORE DELETE ON tax_facts BEGIN
  SELECT RAISE(ABORT, 'tax facts are immutable');
END;
CREATE TRIGGER cash_flow_facts_immutable_update BEFORE UPDATE ON cash_flow_facts BEGIN
  SELECT RAISE(ABORT, 'cash flow facts are immutable');
END;
CREATE TRIGGER cash_flow_facts_immutable_delete BEFORE DELETE ON cash_flow_facts BEGIN
  SELECT RAISE(ABORT, 'cash flow facts are immutable');
END;

CREATE TABLE rule_bundles (
  id TEXT PRIMARY KEY,
  organisation_id TEXT REFERENCES organisations(id) ON DELETE RESTRICT,
  bundle_type TEXT NOT NULL,
  version TEXT NOT NULL,
  semantic_sha256 TEXT NOT NULL CHECK (length(semantic_sha256) = 64),
  rules_proto BLOB NOT NULL,
  effective_from TEXT NOT NULL,
  effective_to TEXT,
  retained_at TEXT NOT NULL,
  UNIQUE (bundle_type, version, semantic_sha256)
);

CREATE TABLE tax_code_catalogue (
  id TEXT NOT NULL UNIQUE,
  code TEXT NOT NULL,
  rule_bundle_id TEXT NOT NULL REFERENCES rule_bundles(id) ON DELETE RESTRICT,
  label TEXT NOT NULL,
  rate_millionths INTEGER NOT NULL CHECK (rate_millionths BETWEEN 0 AND 1000000),
  treatment TEXT NOT NULL CHECK (treatment IN ('TAXABLE','GST_FREE','INPUT_TAXED','OUT_OF_SCOPE','ADJUSTMENT')),
  effective_from TEXT NOT NULL,
  effective_to TEXT,
  PRIMARY KEY (code, rule_bundle_id)
);

CREATE TRIGGER rule_bundles_immutable_update
BEFORE UPDATE ON rule_bundles
BEGIN
  SELECT RAISE(ABORT, 'retained rule bundles are immutable');
END;

CREATE TRIGGER rule_bundles_immutable_delete
BEFORE DELETE ON rule_bundles
BEGIN
  SELECT RAISE(ABORT, 'retained rule bundles are immutable');
END;

CREATE TRIGGER tax_code_catalogue_immutable_update
BEFORE UPDATE ON tax_code_catalogue
BEGIN
  SELECT RAISE(ABORT, 'retained tax codes are immutable');
END;

CREATE TRIGGER tax_code_catalogue_immutable_delete
BEFORE DELETE ON tax_code_catalogue
BEGIN
  SELECT RAISE(ABORT, 'retained tax codes are immutable');
END;

CREATE TABLE financial_revisions (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  financial_revision INTEGER NOT NULL DEFAULT 0 CHECK (financial_revision >= 0),
  ledger_revision INTEGER NOT NULL DEFAULT 0 CHECK (ledger_revision >= 0),
  settlement_revision INTEGER NOT NULL DEFAULT 0 CHECK (settlement_revision >= 0),
  banking_revision INTEGER NOT NULL DEFAULT 0 CHECK (banking_revision >= 0),
  tax_source_revision INTEGER NOT NULL DEFAULT 0 CHECK (tax_source_revision >= 0),
  organisation_profile_revision INTEGER NOT NULL DEFAULT 0 CHECK (organisation_profile_revision >= 0),
  rule_bundle_revision INTEGER NOT NULL DEFAULT 0 CHECK (rule_bundle_revision >= 0),
  updated_at TEXT NOT NULL
);

INSERT INTO financial_revisions(id, updated_at) VALUES (1, '1970-01-01T00:00:00Z');

CREATE TABLE financial_revision_claims (
  operation_key TEXT PRIMARY KEY REFERENCES idempotency_records(operation_key) ON DELETE RESTRICT,
  domain_mask INTEGER NOT NULL CHECK (domain_mask BETWEEN 1 AND 63),
  financial_revision INTEGER NOT NULL CHECK (financial_revision > 0),
  ledger_revision INTEGER NOT NULL CHECK (ledger_revision >= 0),
  settlement_revision INTEGER NOT NULL CHECK (settlement_revision >= 0),
  banking_revision INTEGER NOT NULL CHECK (banking_revision >= 0),
  tax_source_revision INTEGER NOT NULL CHECK (tax_source_revision >= 0),
  organisation_profile_revision INTEGER NOT NULL CHECK (organisation_profile_revision >= 0),
  rule_bundle_revision INTEGER NOT NULL CHECK (rule_bundle_revision >= 0),
  updated_at TEXT NOT NULL
);

CREATE TRIGGER journal_posting_must_balance
BEFORE UPDATE OF state ON journals
WHEN OLD.state = 'DRAFT' AND NEW.state = 'POSTED'
BEGIN
  SELECT CASE WHEN (
    SELECT count(*) < 2
      OR sum(debit_minor) <> sum(credit_minor)
      OR sum(debit_minor) <= 0
    FROM journal_lines
    WHERE journal_id = NEW.id
  ) THEN RAISE(ABORT, 'journal is not balanced') END;
END;

CREATE TRIGGER journal_insert_must_be_draft
BEFORE INSERT ON journals
WHEN NEW.state <> 'DRAFT'
BEGIN
  SELECT RAISE(ABORT, 'journals must be posted through the guarded transition');
END;

CREATE TRIGGER journal_state_transition_guard
BEFORE UPDATE OF state ON journals
WHEN OLD.state = 'DRAFT' AND NEW.state NOT IN ('DRAFT','POSTED')
BEGIN
  SELECT RAISE(ABORT, 'invalid journal state transition');
END;

CREATE TRIGGER journal_organisation_immutable
BEFORE UPDATE OF organisation_id ON journals
WHEN NEW.organisation_id <> OLD.organisation_id
BEGIN
  SELECT RAISE(ABORT, 'journal organisation ownership is immutable');
END;

CREATE TRIGGER account_organisation_immutable
BEFORE UPDATE OF organisation_id ON accounts
WHEN NEW.organisation_id <> OLD.organisation_id
BEGIN
  SELECT RAISE(ABORT, 'account organisation ownership is immutable');
END;

CREATE TRIGGER opening_conversion_organisation_immutable
BEFORE UPDATE OF organisation_id ON opening_conversions
WHEN NEW.organisation_id <> OLD.organisation_id
BEGIN
  SELECT RAISE(ABORT, 'opening conversion organisation ownership is immutable');
END;

CREATE TRIGGER journal_line_account_owner_insert
BEFORE INSERT ON journal_lines
WHEN NOT EXISTS (
  SELECT 1 FROM journals j
  JOIN accounts a ON a.id = NEW.account_id
  WHERE j.id = NEW.journal_id AND a.organisation_id = j.organisation_id
)
BEGIN
  SELECT RAISE(ABORT, 'journal line account belongs to another organisation');
END;

CREATE TRIGGER journal_line_account_owner_update
BEFORE UPDATE OF journal_id, account_id ON journal_lines
WHEN NOT EXISTS (
  SELECT 1 FROM journals j
  JOIN accounts a ON a.id = NEW.account_id
  WHERE j.id = NEW.journal_id AND a.organisation_id = j.organisation_id
)
BEGIN
  SELECT RAISE(ABORT, 'journal line account belongs to another organisation');
END;

CREATE TRIGGER journal_line_with_facts_journal_immutable
BEFORE UPDATE OF journal_id ON journal_lines
WHEN NEW.journal_id <> OLD.journal_id AND (
  EXISTS (SELECT 1 FROM tax_facts WHERE journal_line_id = OLD.id)
  OR EXISTS (SELECT 1 FROM cash_flow_facts WHERE journal_line_id = OLD.id)
)
BEGIN
  SELECT RAISE(ABORT, 'journal lines carrying financial facts cannot change journals');
END;

CREATE TRIGGER posted_journal_lines_no_insert
BEFORE INSERT ON journal_lines
WHEN EXISTS (SELECT 1 FROM journals WHERE id = NEW.journal_id AND state IN ('POSTED','REVERSED'))
BEGIN
  SELECT RAISE(ABORT, 'posted journal lines are immutable');
END;

CREATE TRIGGER posted_journal_lines_no_update
BEFORE UPDATE ON journal_lines
WHEN EXISTS (SELECT 1 FROM journals WHERE id IN (OLD.journal_id, NEW.journal_id) AND state IN ('POSTED','REVERSED'))
BEGIN
  SELECT RAISE(ABORT, 'posted journal lines are immutable');
END;

CREATE TRIGGER posted_journal_lines_no_delete
BEFORE DELETE ON journal_lines
WHEN EXISTS (SELECT 1 FROM journals WHERE id = OLD.journal_id AND state IN ('POSTED','REVERSED'))
BEGIN
  SELECT RAISE(ABORT, 'posted journal lines are immutable');
END;

CREATE TRIGGER posted_journal_no_delete
BEFORE DELETE ON journals
WHEN OLD.state IN ('POSTED','REVERSED')
BEGIN
  SELECT RAISE(ABORT, 'posted journals are immutable');
END;

CREATE TRIGGER posted_journal_update_guard
BEFORE UPDATE ON journals
WHEN OLD.state IN ('POSTED','REVERSED') AND NOT (
  OLD.state = 'POSTED'
  AND NEW.state = 'REVERSED'
  AND NEW.reversed_by_journal_id IS NOT NULL
  AND NEW.id = OLD.id
  AND NEW.organisation_id = OLD.organisation_id
  AND NEW.source_type = OLD.source_type
  AND NEW.source_id = OLD.source_id
  AND NEW.source_revision = OLD.source_revision
  AND NEW.journal_date = OLD.journal_date
  AND NEW.description = OLD.description
  AND NEW.reversal_of_journal_id IS OLD.reversal_of_journal_id
  AND NEW.posted_at = OLD.posted_at
  AND NEW.created_at = OLD.created_at
)
BEGIN
  SELECT RAISE(ABORT, 'posted journals are immutable');
END;

CREATE TRIGGER reversal_links_original
BEFORE INSERT ON journals
WHEN NEW.reversal_of_journal_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM journals original
    WHERE original.id = NEW.reversal_of_journal_id
      AND original.organisation_id = NEW.organisation_id
      AND original.state = 'POSTED'
  ) THEN RAISE(ABORT, 'invalid direct reversal') END;
END;

CREATE TRIGGER reversal_completion_links_direct_reversal
BEFORE UPDATE OF state, reversed_by_journal_id ON journals
WHEN OLD.state = 'POSTED' AND NEW.state = 'REVERSED'
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM journals reversal
    WHERE reversal.id = NEW.reversed_by_journal_id
      AND reversal.reversal_of_journal_id = OLD.id
      AND reversal.organisation_id = OLD.organisation_id
      AND reversal.state = 'POSTED'
  ) THEN RAISE(ABORT, 'reversal completion does not link a posted direct reversal') END;
END;

CREATE TRIGGER opening_conversion_journal_owner_insert
BEFORE INSERT ON opening_conversions
WHEN NEW.journal_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM journals
  WHERE id = NEW.journal_id
    AND organisation_id = NEW.organisation_id
    AND source_type = 'OPENING'
)
BEGIN
  SELECT RAISE(ABORT, 'opening conversion journal belongs to another owner');
END;

CREATE TRIGGER opening_conversion_insert_must_be_unreplaced_draft
BEFORE INSERT ON opening_conversions
WHEN NEW.state <> 'DRAFT'
  OR NEW.replaced_by_id IS NOT NULL
  OR NEW.financial_revision IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'opening conversions must be inserted as unreplaced drafts');
END;

CREATE TRIGGER opening_conversion_journal_owner_update
BEFORE UPDATE OF organisation_id, journal_id ON opening_conversions
WHEN NEW.journal_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM journals
  WHERE id = NEW.journal_id
    AND organisation_id = NEW.organisation_id
    AND source_type = 'OPENING'
)
BEGIN
  SELECT RAISE(ABORT, 'opening conversion journal belongs to another owner');
END;

CREATE TRIGGER opening_conversion_replacement_owner_guard
BEFORE UPDATE OF replaced_by_id ON opening_conversions
WHEN NEW.replaced_by_id IS NOT NULL AND (
  NEW.replaced_by_id = OLD.id OR NOT EXISTS (
    SELECT 1 FROM opening_conversions replacement
    WHERE replacement.id = NEW.replaced_by_id
      AND replacement.organisation_id = OLD.organisation_id
  )
)
BEGIN
  SELECT RAISE(ABORT, 'opening replacement must be a distinct conversion for the same organisation');
END;

CREATE TRIGGER opening_item_account_owner_insert
BEFORE INSERT ON opening_items
WHEN NOT EXISTS (
  SELECT 1 FROM opening_conversions conversion
  JOIN accounts account ON account.id = NEW.account_id
  WHERE conversion.id = NEW.conversion_id
    AND account.organisation_id = conversion.organisation_id
)
BEGIN
  SELECT RAISE(ABORT, 'opening item account belongs to another organisation');
END;

CREATE TRIGGER opening_item_account_owner_update
BEFORE UPDATE OF conversion_id, account_id ON opening_items
WHEN NOT EXISTS (
  SELECT 1 FROM opening_conversions conversion
  JOIN accounts account ON account.id = NEW.account_id
  WHERE conversion.id = NEW.conversion_id
    AND account.organisation_id = conversion.organisation_id
)
BEGIN
  SELECT RAISE(ABORT, 'opening item account belongs to another organisation');
END;

CREATE TRIGGER opening_conversion_post_guard
BEFORE UPDATE OF state ON opening_conversions
WHEN OLD.state = 'DRAFT' AND NEW.state = 'POSTED' AND (
  NEW.financial_revision IS NULL OR NOT EXISTS (
    SELECT 1 FROM journals
    WHERE id = NEW.journal_id
      AND organisation_id = NEW.organisation_id
      AND source_type = 'OPENING'
      AND state = 'POSTED'
  )
)
BEGIN
  SELECT RAISE(ABORT, 'opening conversion requires a posted owned journal');
END;

CREATE TRIGGER posted_opening_items_no_insert
BEFORE INSERT ON opening_items
WHEN EXISTS (SELECT 1 FROM opening_conversions WHERE id = NEW.conversion_id AND state IN ('POSTED','REPLACED'))
BEGIN
  SELECT RAISE(ABORT, 'posted opening items are immutable');
END;

CREATE TRIGGER posted_opening_items_no_update
BEFORE UPDATE ON opening_items
WHEN EXISTS (SELECT 1 FROM opening_conversions WHERE id IN (OLD.conversion_id, NEW.conversion_id) AND state IN ('POSTED','REPLACED'))
BEGIN
  SELECT RAISE(ABORT, 'posted opening items are immutable');
END;

CREATE TRIGGER posted_opening_items_no_delete
BEFORE DELETE ON opening_items
WHEN EXISTS (SELECT 1 FROM opening_conversions WHERE id = OLD.conversion_id AND state IN ('POSTED','REPLACED'))
BEGIN
  SELECT RAISE(ABORT, 'posted opening items are immutable');
END;

CREATE TRIGGER posted_opening_conversion_no_delete
BEFORE DELETE ON opening_conversions
WHEN OLD.state IN ('POSTED','REPLACED')
BEGIN
  SELECT RAISE(ABORT, 'posted opening conversions are immutable');
END;

CREATE TRIGGER posted_opening_conversion_update_guard
BEFORE UPDATE ON opening_conversions
WHEN OLD.state IN ('POSTED','REPLACED') AND NOT (
  OLD.state = 'POSTED'
  AND NEW.state = 'REPLACED'
  AND NEW.replaced_by_id IS NOT NULL
  AND NEW.version = OLD.version + 1
  AND NEW.id = OLD.id
  AND NEW.organisation_id = OLD.organisation_id
  AND NEW.conversion_date = OLD.conversion_date
  AND NEW.source_sha256 = OLD.source_sha256
  AND NEW.journal_id = OLD.journal_id
  AND NEW.financial_revision = OLD.financial_revision
  AND NEW.created_at = OLD.created_at
)
BEGIN
  SELECT RAISE(ABORT, 'posted opening conversions are immutable');
END;

CREATE TRIGGER financial_fact_owner_insert
BEFORE INSERT ON tax_facts
WHEN NOT EXISTS (
  SELECT 1 FROM journal_lines line
  JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id = NEW.journal_line_id AND journal.organisation_id = NEW.organisation_id
)
BEGIN
  SELECT RAISE(ABORT, 'tax fact belongs to another organisation');
END;

CREATE TRIGGER financial_fact_owner_update
BEFORE UPDATE OF organisation_id, journal_line_id ON tax_facts
WHEN NOT EXISTS (
  SELECT 1 FROM journal_lines line
  JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id = NEW.journal_line_id AND journal.organisation_id = NEW.organisation_id
)
BEGIN
  SELECT RAISE(ABORT, 'tax fact belongs to another organisation');
END;

CREATE TRIGGER cash_flow_fact_owner_insert
BEFORE INSERT ON cash_flow_facts
WHEN NOT EXISTS (
  SELECT 1 FROM journal_lines line
  JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id = NEW.journal_line_id AND journal.organisation_id = NEW.organisation_id
)
BEGIN
  SELECT RAISE(ABORT, 'cash-flow fact belongs to another organisation');
END;

CREATE TRIGGER cash_flow_fact_owner_update
BEFORE UPDATE OF organisation_id, journal_line_id ON cash_flow_facts
WHEN NOT EXISTS (
  SELECT 1 FROM journal_lines line
  JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id = NEW.journal_line_id AND journal.organisation_id = NEW.organisation_id
)
BEGIN
  SELECT RAISE(ABORT, 'cash-flow fact belongs to another organisation');
END;

CREATE TRIGGER posted_tax_facts_no_insert
BEFORE INSERT ON tax_facts
WHEN EXISTS (
  SELECT 1 FROM journal_lines line JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id = NEW.journal_line_id AND journal.state IN ('POSTED','REVERSED')
)
BEGIN
  SELECT RAISE(ABORT, 'posted tax facts are immutable');
END;

CREATE TRIGGER posted_tax_facts_no_update
BEFORE UPDATE ON tax_facts
WHEN EXISTS (
  SELECT 1 FROM journal_lines line JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id IN (OLD.journal_line_id, NEW.journal_line_id) AND journal.state IN ('POSTED','REVERSED')
)
BEGIN
  SELECT RAISE(ABORT, 'posted tax facts are immutable');
END;

CREATE TRIGGER posted_tax_facts_no_delete
BEFORE DELETE ON tax_facts
WHEN EXISTS (
  SELECT 1 FROM journal_lines line JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id = OLD.journal_line_id AND journal.state IN ('POSTED','REVERSED')
)
BEGIN
  SELECT RAISE(ABORT, 'posted tax facts are immutable');
END;

CREATE TRIGGER posted_cash_flow_facts_no_insert
BEFORE INSERT ON cash_flow_facts
WHEN EXISTS (
  SELECT 1 FROM journal_lines line JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id = NEW.journal_line_id AND journal.state IN ('POSTED','REVERSED')
)
BEGIN
  SELECT RAISE(ABORT, 'posted cash-flow facts are immutable');
END;

CREATE TRIGGER posted_cash_flow_facts_no_update
BEFORE UPDATE ON cash_flow_facts
WHEN EXISTS (
  SELECT 1 FROM journal_lines line JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id IN (OLD.journal_line_id, NEW.journal_line_id) AND journal.state IN ('POSTED','REVERSED')
)
BEGIN
  SELECT RAISE(ABORT, 'posted cash-flow facts are immutable');
END;

CREATE TRIGGER posted_cash_flow_facts_no_delete
BEFORE DELETE ON cash_flow_facts
WHEN EXISTS (
  SELECT 1 FROM journal_lines line JOIN journals journal ON journal.id = line.journal_id
  WHERE line.id = OLD.journal_line_id AND journal.state IN ('POSTED','REVERSED')
)
BEGIN
  SELECT RAISE(ABORT, 'posted cash-flow facts are immutable');
END;
