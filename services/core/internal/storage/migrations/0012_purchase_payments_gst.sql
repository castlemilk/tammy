-- Additive retained purchase accounting. Historical documents, journals and BAS rows are not backfilled.
-- Account versions are snapshots; only stable identity and organisation form ownership keys.

CREATE UNIQUE INDEX documents_purchase_owner_key ON documents(id,organisation_id);

CREATE UNIQUE INDEX accounts_purchase_owner_key ON accounts(id,organisation_id);

CREATE UNIQUE INDEX journals_purchase_owner_key ON journals(id,organisation_id);

CREATE UNIQUE INDEX bas_workpapers_purchase_owner_key ON bas_workpapers(id,organisation_id);

ALTER TABLE journals ADD COLUMN source_kind INTEGER NOT NULL DEFAULT 0 CHECK (source_kind IN (0,6,7,8));

CREATE TRIGGER journals_purchase_source_pair BEFORE INSERT ON journals
WHEN NOT (NEW.source_kind=0 OR (NEW.source_kind=6 AND NEW.source_type='PAYABLE') OR (NEW.source_kind=7 AND NEW.source_type='BANKING') OR (NEW.source_kind=8 AND NEW.source_type='TAX'))
BEGIN SELECT RAISE(ABORT,'invalid purchase journal source pair'); END;

CREATE TRIGGER journals_purchase_source_kind_immutable BEFORE UPDATE ON journals
WHEN NEW.source_kind IS NOT OLD.source_kind
BEGIN SELECT RAISE(ABORT,'journal source kind is immutable'); END;

CREATE TRIGGER journals_purchase_source_pair_update BEFORE UPDATE ON journals
WHEN NOT (NEW.source_kind=0 OR (NEW.source_kind=6 AND NEW.source_type='PAYABLE') OR (NEW.source_kind=7 AND NEW.source_type='BANKING') OR (NEW.source_kind=8 AND NEW.source_type='TAX'))
BEGIN SELECT RAISE(ABORT,'invalid purchase journal source pair'); END;

CREATE TRIGGER journals_purchase_no_direct_reversal BEFORE INSERT ON journals
WHEN NEW.reversal_of_journal_id IS NOT NULL AND EXISTS(SELECT 1 FROM journals WHERE id=NEW.reversal_of_journal_id AND source_kind IN (6,7,8))
BEGIN SELECT RAISE(ABORT,'purchase journals require an owned correction'); END;

CREATE TRIGGER journals_purchase_no_reversal_update BEFORE UPDATE ON journals
WHEN (OLD.source_kind IN (6,7,8) AND (NEW.state='REVERSED' OR NEW.reversed_by_journal_id IS NOT NULL)) OR (NEW.reversal_of_journal_id IS NOT NULL AND EXISTS(SELECT 1 FROM journals WHERE id=NEW.reversal_of_journal_id AND source_kind IN (6,7,8)))
BEGIN SELECT RAISE(ABORT,'purchase journals require an owned correction'); END;

CREATE TRIGGER journals_purchase_no_conflicting_insert BEFORE INSERT ON journals
WHEN EXISTS(SELECT 1 FROM journals WHERE source_kind IN (6,7,8) AND (id=NEW.id OR (organisation_id=NEW.organisation_id AND source_type=NEW.source_type AND source_id=NEW.source_id AND source_revision=NEW.source_revision)))
BEGIN SELECT RAISE(ABORT,'purchase journal cannot be replaced'); END;

CREATE TABLE purchase_postings_v1 (
  id TEXT NOT NULL PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND length(trim(id))>0),
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  document_id TEXT NOT NULL UNIQUE,
  reviewed_version INTEGER NOT NULL CHECK (typeof(reviewed_version)='integer' AND reviewed_version>0),
  original_content_hash BLOB NOT NULL CHECK ((typeof(original_content_hash)='blob' AND length(original_content_hash)=32)),
  reviewed_snapshot_hash BLOB NOT NULL CHECK ((typeof(reviewed_snapshot_hash)='blob' AND length(reviewed_snapshot_hash)=32)),
  reviewed_source_proto BLOB NOT NULL CHECK (typeof(reviewed_source_proto)='blob' AND length(reviewed_source_proto) BETWEEN 1 AND 1048576),
  supplier_name TEXT NOT NULL CHECK(length(supplier_name)<=512),
  supplier_identity TEXT CHECK(supplier_identity IS NULL OR length(supplier_identity)<=512),
  invoice_number TEXT NOT NULL CHECK(length(invoice_number)<=128),
  invoice_date TEXT NOT NULL CHECK (length(invoice_date)=10 AND invoice_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(invoice_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(invoice_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(invoice_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(invoice_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(invoice_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(invoice_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(invoice_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  gst_basis INTEGER NOT NULL CHECK(gst_basis IN (0,1,2)),
  gst_profile_version INTEGER NOT NULL CHECK (typeof(gst_profile_version)='integer' AND gst_profile_version>0),
  gst_history_watermark INTEGER NOT NULL CHECK(typeof(gst_history_watermark)='integer' AND gst_history_watermark>=0),
  gross_minor INTEGER NOT NULL CHECK (typeof(gross_minor)='integer' AND gross_minor>=0),
  supplier_gst_minor INTEGER NOT NULL CHECK (typeof(supplier_gst_minor)='integer' AND supplier_gst_minor>=0),
  eligible_gst_minor INTEGER NOT NULL CHECK (typeof(eligible_gst_minor)='integer' AND eligible_gst_minor>=0),
  currency_code TEXT NOT NULL CHECK (currency_code='AUD'),
  journal_id TEXT UNIQUE,
  financial_revision INTEGER NOT NULL CHECK (typeof(financial_revision)='integer' AND financial_revision>0),
  duplicate_decision INTEGER NOT NULL CHECK(duplicate_decision IN (1,2)),
  duplicate_reason TEXT NOT NULL CHECK(length(duplicate_reason)<=512),
  prior_accounting_decision INTEGER NOT NULL CHECK(prior_accounting_decision=1),
  prior_accounting_reason TEXT NOT NULL CHECK(length(prior_accounting_reason) BETWEEN 1 AND 512),
  normalized_supplier TEXT NOT NULL CHECK(length(normalized_supplier)<=512),
  normalized_invoice TEXT NOT NULL CHECK(length(normalized_invoice)<=128),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  posting_proto BLOB NOT NULL CHECK (typeof(posting_proto)='blob' AND length(posting_proto) BETWEEN 1 AND 131072),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE(id,organisation_id),
  UNIQUE(id,organisation_id,document_id),
  FOREIGN KEY (document_id,organisation_id) REFERENCES documents(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (journal_id,organisation_id) REFERENCES journals(id,organisation_id) ON DELETE RESTRICT,
  CHECK(eligible_gst_minor<=supplier_gst_minor AND supplier_gst_minor<=gross_minor),
  CHECK(supplier_gst_minor=0 OR gst_basis<>0),
  CHECK(duplicate_decision<>2 OR length(duplicate_reason)>0),
  CHECK((gross_minor=0 AND journal_id IS NULL) OR (gross_minor>0 AND journal_id IS NOT NULL))
);

CREATE INDEX purchase_postings_period_idx ON purchase_postings_v1(organisation_id,invoice_date,id);

CREATE INDEX purchase_postings_duplicate_idx ON purchase_postings_v1(organisation_id,normalized_supplier,normalized_invoice,invoice_date,gross_minor);

CREATE TRIGGER purchase_postings_source_guard BEFORE INSERT ON purchase_postings_v1
WHEN NOT EXISTS(SELECT 1 FROM documents d WHERE d.id=NEW.document_id AND d.organisation_id=NEW.organisation_id AND d.status='REVIEWED' AND d.version=NEW.reviewed_version AND d.sha256=NEW.original_content_hash) OR (NEW.journal_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM journals WHERE id=NEW.journal_id AND organisation_id=NEW.organisation_id AND source_kind=6 AND source_id=NEW.id AND state='POSTED'))
BEGIN SELECT RAISE(ABORT,'purchase posting source is not owned and reviewed'); END;

CREATE TRIGGER purchase_postings_v1_no_update BEFORE UPDATE ON purchase_postings_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_postings_v1_no_delete BEFORE DELETE ON purchase_postings_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_postings_v1_no_conflicting_insert BEFORE INSERT ON purchase_postings_v1
WHEN EXISTS(SELECT 1 FROM purchase_postings_v1 WHERE id=NEW.id OR document_id=NEW.document_id OR (journal_id IS NOT NULL AND journal_id=NEW.journal_id))
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_posting_allocations_v1 (
  posting_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  sequence INTEGER NOT NULL CHECK (typeof(sequence)='integer' AND sequence>0),
  account_id TEXT NOT NULL,
  account_version INTEGER NOT NULL CHECK (typeof(account_version)='integer' AND account_version>0),
  gross_minor INTEGER NOT NULL CHECK (typeof(gross_minor)='integer' AND gross_minor>=0),
  supplier_gst_minor INTEGER NOT NULL CHECK (typeof(supplier_gst_minor)='integer' AND supplier_gst_minor>=0),
  eligible_gst_minor INTEGER NOT NULL CHECK (typeof(eligible_gst_minor)='integer' AND eligible_gst_minor>=0),
  use_kind INTEGER NOT NULL CHECK(use_kind IN (1,2)),
  bas_classification INTEGER NOT NULL CHECK(typeof(bas_classification)='integer' AND bas_classification BETWEEN 1 AND 3),
  cash_flow_classification INTEGER NOT NULL CHECK(typeof(cash_flow_classification)='integer' AND cash_flow_classification BETWEEN 1 AND 3),
  noncreditable_reason TEXT NOT NULL CHECK(length(noncreditable_reason)<=512),
  allocation_proto BLOB NOT NULL CHECK (typeof(allocation_proto)='blob' AND length(allocation_proto) BETWEEN 1 AND 16384),
  PRIMARY KEY(posting_id,sequence),
  UNIQUE(posting_id,organisation_id,sequence),
  FOREIGN KEY (posting_id,organisation_id) REFERENCES purchase_postings_v1(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (account_id,organisation_id) REFERENCES accounts(id,organisation_id) ON DELETE RESTRICT,
  CHECK(eligible_gst_minor<=supplier_gst_minor AND supplier_gst_minor<=gross_minor),
  CHECK(use_kind<>2 OR (eligible_gst_minor=0 AND bas_classification=3)),
  CHECK(eligible_gst_minor=supplier_gst_minor OR length(noncreditable_reason)>0)
);

CREATE INDEX purchase_posting_allocations_account_idx ON purchase_posting_allocations_v1(account_id,organisation_id);

CREATE TRIGGER purchase_posting_allocations_v1_no_update BEFORE UPDATE ON purchase_posting_allocations_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_posting_allocations_v1_no_delete BEFORE DELETE ON purchase_posting_allocations_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_posting_allocations_v1_no_conflicting_insert BEFORE INSERT ON purchase_posting_allocations_v1
WHEN EXISTS(SELECT 1 FROM purchase_posting_allocations_v1 WHERE posting_id=NEW.posting_id AND sequence=NEW.sequence)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_payments_v1 (
  id TEXT NOT NULL PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND length(trim(id))>0),
  posting_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  accounting_version INTEGER NOT NULL CHECK (typeof(accounting_version)='integer' AND accounting_version>0),
  payment_date TEXT NOT NULL CHECK (length(payment_date)=10 AND payment_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(payment_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(payment_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(payment_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(payment_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(payment_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(payment_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(payment_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  amount_minor INTEGER NOT NULL CHECK (typeof(amount_minor)='integer' AND amount_minor>0),
  currency_code TEXT NOT NULL CHECK (currency_code='AUD'),
  bank_account_id TEXT NOT NULL,
  journal_id TEXT NOT NULL UNIQUE,
  reference TEXT NOT NULL CHECK(length(reference)<=512),
  payment_proto BLOB NOT NULL CHECK (typeof(payment_proto)='blob' AND length(payment_proto) BETWEEN 1 AND 65536),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE(id,posting_id,organisation_id),
  FOREIGN KEY (posting_id,organisation_id) REFERENCES purchase_postings_v1(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (bank_account_id,organisation_id) REFERENCES accounts(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (journal_id,organisation_id) REFERENCES journals(id,organisation_id) ON DELETE RESTRICT
);

CREATE INDEX purchase_payments_period_idx ON purchase_payments_v1(organisation_id,payment_date,id);

CREATE INDEX purchase_payments_history_idx ON purchase_payments_v1(organisation_id,posting_id,created_at,id);

CREATE INDEX purchase_payments_bank_idx ON purchase_payments_v1(bank_account_id,organisation_id);

CREATE TRIGGER purchase_payments_journal_owner BEFORE INSERT ON purchase_payments_v1
WHEN NOT EXISTS(SELECT 1 FROM journals WHERE id=NEW.journal_id AND organisation_id=NEW.organisation_id AND source_kind=7 AND source_id=NEW.id AND state='POSTED')
BEGIN SELECT RAISE(ABORT,'purchase payment journal is not owned'); END;

CREATE TRIGGER purchase_payments_v1_no_update BEFORE UPDATE ON purchase_payments_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_payments_v1_no_delete BEFORE DELETE ON purchase_payments_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_payments_v1_no_conflicting_insert BEFORE INSERT ON purchase_payments_v1
WHEN EXISTS(SELECT 1 FROM purchase_payments_v1 WHERE id=NEW.id OR journal_id=NEW.journal_id)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_payment_allocations_v1 (
  payment_id TEXT NOT NULL,
  posting_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  sequence INTEGER NOT NULL CHECK (typeof(sequence)='integer' AND sequence>0),
  posting_allocation_sequence INTEGER NOT NULL CHECK (typeof(posting_allocation_sequence)='integer' AND posting_allocation_sequence>0),
  gross_minor INTEGER NOT NULL CHECK (typeof(gross_minor)='integer' AND gross_minor>=0),
  supplier_gst_minor INTEGER NOT NULL CHECK (typeof(supplier_gst_minor)='integer' AND supplier_gst_minor>=0),
  eligible_gst_minor INTEGER NOT NULL CHECK (typeof(eligible_gst_minor)='integer' AND eligible_gst_minor>=0),
  use_kind INTEGER NOT NULL CHECK(use_kind IN (1,2)),
  bas_classification INTEGER NOT NULL CHECK(typeof(bas_classification)='integer' AND bas_classification BETWEEN 1 AND 3),
  cash_flow_classification INTEGER NOT NULL CHECK(typeof(cash_flow_classification)='integer' AND cash_flow_classification BETWEEN 1 AND 3),
  allocation_proto BLOB NOT NULL CHECK (typeof(allocation_proto)='blob' AND length(allocation_proto) BETWEEN 1 AND 16384),
  PRIMARY KEY(payment_id,sequence),
  UNIQUE(payment_id,posting_id,organisation_id,sequence),
  FOREIGN KEY (payment_id,posting_id,organisation_id) REFERENCES purchase_payments_v1(id,posting_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (posting_id,organisation_id,posting_allocation_sequence) REFERENCES purchase_posting_allocations_v1(posting_id,organisation_id,sequence) ON DELETE RESTRICT,
  CHECK(eligible_gst_minor<=gross_minor AND supplier_gst_minor<=gross_minor),
  CHECK(use_kind<>2 OR (eligible_gst_minor=0 AND bas_classification=3))
);

CREATE INDEX purchase_payment_allocations_posting_idx ON purchase_payment_allocations_v1(posting_id,organisation_id,posting_allocation_sequence);

CREATE TRIGGER purchase_payment_allocations_v1_no_update BEFORE UPDATE ON purchase_payment_allocations_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_payment_allocations_v1_no_delete BEFORE DELETE ON purchase_payment_allocations_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_payment_allocations_v1_no_conflicting_insert BEFORE INSERT ON purchase_payment_allocations_v1
WHEN EXISTS(SELECT 1 FROM purchase_payment_allocations_v1 WHERE payment_id=NEW.payment_id AND sequence=NEW.sequence)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_tax_evidence_v1 (
  id TEXT NOT NULL PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND length(trim(id))>0),
  organisation_id TEXT NOT NULL,
  posting_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  accounting_version INTEGER NOT NULL CHECK (typeof(accounting_version)='integer' AND accounting_version>0),
  classification INTEGER NOT NULL CHECK(typeof(classification)='integer' AND classification BETWEEN 1 AND 3),
  source_document_id TEXT,
  source_version INTEGER CHECK(source_version IS NULL OR (typeof(source_version)='integer' AND source_version>0)),
  source_hash BLOB NULL CHECK (source_hash IS NULL OR (typeof(source_hash)='blob' AND length(source_hash)=32)),
  held_date TEXT NULL CHECK (held_date IS NULL OR (length(held_date)=10 AND held_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(held_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(held_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(held_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(held_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(held_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(held_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(held_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END)),
  registration_review_confirmed INTEGER NOT NULL CHECK(registration_review_confirmed=1),
  business_use_review_confirmed INTEGER NOT NULL CHECK(business_use_review_confirmed=1),
  evidence_review_confirmed INTEGER NOT NULL CHECK(evidence_review_confirmed=1),
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 512),
  evidence_proto BLOB NOT NULL CHECK (typeof(evidence_proto)='blob' AND length(evidence_proto) BETWEEN 1 AND 131072),
  record_hash BLOB NOT NULL CHECK ((typeof(record_hash)='blob' AND length(record_hash)=32)),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE(id,posting_id,organisation_id),
  FOREIGN KEY (posting_id,organisation_id,document_id) REFERENCES purchase_postings_v1(id,organisation_id,document_id) ON DELETE RESTRICT,
  FOREIGN KEY (source_document_id,organisation_id) REFERENCES documents(id,organisation_id) ON DELETE RESTRICT,
  CHECK((source_document_id IS NULL)=(source_version IS NULL) AND (source_document_id IS NULL)=(source_hash IS NULL)),
  CHECK(classification=1 OR (source_document_id IS NOT NULL AND held_date IS NOT NULL))
);

CREATE INDEX purchase_tax_evidence_posting_idx ON purchase_tax_evidence_v1(organisation_id,posting_id,accounting_version,id);

CREATE INDEX purchase_tax_evidence_document_idx ON purchase_tax_evidence_v1(source_document_id,organisation_id);

CREATE TRIGGER purchase_tax_evidence_v1_no_update BEFORE UPDATE ON purchase_tax_evidence_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_tax_evidence_v1_no_delete BEFORE DELETE ON purchase_tax_evidence_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_tax_evidence_v1_no_conflicting_insert BEFORE INSERT ON purchase_tax_evidence_v1
WHEN EXISTS(SELECT 1 FROM purchase_tax_evidence_v1 WHERE id=NEW.id)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_accounting_events_v1 (
  operation_key TEXT NOT NULL PRIMARY KEY CHECK(length(operation_key) BETWEEN 1 AND 256 AND length(trim(operation_key))>0) REFERENCES command_idempotency_v1(operation_key) ON DELETE RESTRICT,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  scope_kind TEXT NOT NULL CHECK(scope_kind IN ('POSTING','DOCUMENT','OPENING_PAYABLE')),
  scope_id TEXT NOT NULL,
  posting_id TEXT,
  document_id TEXT,
  opening_payable_id TEXT,
  accounting_version INTEGER NOT NULL CHECK (typeof(accounting_version)='integer' AND accounting_version>0),
  command_type TEXT NOT NULL CHECK(command_type IN ('tammy.v1.PurchaseService.PostReviewedPurchase','tammy.v1.PurchaseService.RecordPurchasePayment','tammy.v1.PurchaseService.RecordPurchaseTaxEvidence','tammy.v1.PurchaseService.AttributePurchaseGst','tammy.v1.PurchaseService.DeferPurchaseGstClaim','tammy.v1.PurchaseService.ResolveLegacyPurchaseReview')),
  financial_revision INTEGER NOT NULL CHECK (typeof(financial_revision)='integer' AND financial_revision>0),
  ledger_revision INTEGER NOT NULL CHECK(typeof(ledger_revision)='integer' AND ledger_revision>=0),
  tax_source_revision INTEGER NOT NULL CHECK(typeof(tax_source_revision)='integer' AND tax_source_revision>=0),
  settlement_revision INTEGER NOT NULL CHECK(typeof(settlement_revision)='integer' AND settlement_revision>=0),
  result_sha256 BLOB NOT NULL CHECK ((typeof(result_sha256)='blob' AND length(result_sha256)=32)),
  result_proto BLOB NOT NULL CHECK (typeof(result_proto)='blob' AND length(result_proto) BETWEEN 1 AND 1048576),
  journal_id TEXT UNIQUE,
  created_at TEXT NOT NULL,
  UNIQUE(organisation_id,scope_kind,scope_id,accounting_version),
  FOREIGN KEY (posting_id,organisation_id) REFERENCES purchase_postings_v1(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (document_id,organisation_id) REFERENCES documents(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (opening_payable_id) REFERENCES purchase_opening_payables(id) ON DELETE RESTRICT,
  FOREIGN KEY (journal_id,organisation_id) REFERENCES journals(id,organisation_id) ON DELETE RESTRICT,
  CHECK((scope_kind='POSTING' AND posting_id=scope_id AND posting_id IS NOT NULL AND document_id IS NULL AND opening_payable_id IS NULL) OR (scope_kind='DOCUMENT' AND document_id=scope_id AND document_id IS NOT NULL AND posting_id IS NULL AND opening_payable_id IS NULL) OR (scope_kind='OPENING_PAYABLE' AND opening_payable_id=scope_id AND opening_payable_id IS NOT NULL AND posting_id IS NULL AND document_id IS NULL))
);

CREATE TRIGGER purchase_accounting_events_consecutive BEFORE INSERT ON purchase_accounting_events_v1
WHEN NEW.accounting_version<>COALESCE((SELECT max(accounting_version)+1 FROM purchase_accounting_events_v1 WHERE organisation_id=NEW.organisation_id AND scope_kind=NEW.scope_kind AND scope_id=NEW.scope_id),1)
BEGIN SELECT RAISE(ABORT,'purchase accounting version must be consecutive'); END;

CREATE TRIGGER purchase_accounting_events_election BEFORE INSERT ON purchase_accounting_events_v1
WHEN NOT EXISTS(SELECT 1 FROM command_idempotency_v1 WHERE operation_key=NEW.operation_key AND fully_qualified_rpc_name=NEW.command_type AND outcome='ELECTED')
BEGIN SELECT RAISE(ABORT,'purchase event requires its elected command'); END;

CREATE TRIGGER purchase_accounting_events_opening_owner BEFORE INSERT ON purchase_accounting_events_v1
WHEN NEW.opening_payable_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id WHERE p.id=NEW.opening_payable_id AND c.organisation_id=NEW.organisation_id)
BEGIN SELECT RAISE(ABORT,'purchase opening source belongs to another organisation'); END;

CREATE TRIGGER purchase_accounting_events_journal_owner BEFORE INSERT ON purchase_accounting_events_v1
WHEN (NEW.journal_id IS NOT NULL AND NOT ((NEW.command_type='tammy.v1.PurchaseService.PostReviewedPurchase' AND EXISTS(SELECT 1 FROM purchase_postings_v1 p JOIN journals j ON j.id=p.journal_id WHERE p.id=NEW.posting_id AND p.organisation_id=NEW.organisation_id AND p.journal_id=NEW.journal_id AND j.source_kind=6 AND j.source_id=p.id)) OR (NEW.command_type='tammy.v1.PurchaseService.RecordPurchasePayment' AND EXISTS(SELECT 1 FROM purchase_payments_v1 p JOIN journals j ON j.id=p.journal_id WHERE p.posting_id=NEW.posting_id AND p.organisation_id=NEW.organisation_id AND p.accounting_version=NEW.accounting_version AND p.journal_id=NEW.journal_id AND j.source_kind=7 AND j.source_id=p.id)) OR (NEW.command_type='tammy.v1.PurchaseService.AttributePurchaseGst' AND EXISTS(SELECT 1 FROM journals WHERE id=NEW.journal_id AND organisation_id=NEW.organisation_id AND source_kind=8 AND source_id=NEW.operation_key)))) OR (NEW.journal_id IS NULL AND NEW.command_type IN ('tammy.v1.PurchaseService.RecordPurchasePayment','tammy.v1.PurchaseService.AttributePurchaseGst'))
BEGIN SELECT RAISE(ABORT,'purchase event journal is not owned by its command'); END;

CREATE INDEX purchase_accounting_events_document_idx ON purchase_accounting_events_v1(document_id,organisation_id);

CREATE INDEX purchase_accounting_events_opening_idx ON purchase_accounting_events_v1(opening_payable_id,organisation_id);

CREATE INDEX purchase_accounting_events_posting_idx ON purchase_accounting_events_v1(posting_id,organisation_id);

CREATE TRIGGER purchase_accounting_events_v1_no_update BEFORE UPDATE ON purchase_accounting_events_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_accounting_events_v1_no_delete BEFORE DELETE ON purchase_accounting_events_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_accounting_events_v1_no_conflicting_insert BEFORE INSERT ON purchase_accounting_events_v1
WHEN EXISTS(SELECT 1 FROM purchase_accounting_events_v1 WHERE operation_key=NEW.operation_key OR (organisation_id=NEW.organisation_id AND scope_kind=NEW.scope_kind AND scope_id=NEW.scope_id AND accounting_version=NEW.accounting_version) OR (journal_id IS NOT NULL AND journal_id=NEW.journal_id))
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_gst_deferrals_v1 (
  id TEXT NOT NULL PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND length(trim(id))>0),
  organisation_id TEXT NOT NULL,
  posting_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  accounting_version INTEGER NOT NULL CHECK (typeof(accounting_version)='integer' AND accounting_version>0),
  affected_period_start TEXT NOT NULL CHECK (length(affected_period_start)=10 AND affected_period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(affected_period_start,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(affected_period_start,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(affected_period_start,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(affected_period_start,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(affected_period_start,1,4) AS INTEGER)%4=0 AND (CAST(substr(affected_period_start,1,4) AS INTEGER)%100<>0 OR CAST(substr(affected_period_start,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  affected_period_end TEXT NOT NULL CHECK (length(affected_period_end)=10 AND affected_period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(affected_period_end,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(affected_period_end,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(affected_period_end,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(affected_period_end,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(affected_period_end,1,4) AS INTEGER)%4=0 AND (CAST(substr(affected_period_end,1,4) AS INTEGER)%100<>0 OR CAST(substr(affected_period_end,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  reviewed_as_of TEXT NOT NULL CHECK (length(reviewed_as_of)=10 AND reviewed_as_of GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(reviewed_as_of,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(reviewed_as_of,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(reviewed_as_of,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(reviewed_as_of,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(reviewed_as_of,1,4) AS INTEGER)%4=0 AND (CAST(substr(reviewed_as_of,1,4) AS INTEGER)%100<>0 OR CAST(substr(reviewed_as_of,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  timing_reason TEXT NOT NULL CHECK(length(timing_reason) BETWEEN 1 AND 512),
  deferred_gst_minor INTEGER NOT NULL CHECK (typeof(deferred_gst_minor)='integer' AND deferred_gst_minor>0),
  currency_code TEXT NOT NULL CHECK (currency_code='AUD'),
  ordinary_last_claim_date TEXT NOT NULL CHECK (length(ordinary_last_claim_date)=10 AND ordinary_last_claim_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(ordinary_last_claim_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(ordinary_last_claim_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(ordinary_last_claim_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 512),
  deferral_proto BLOB NOT NULL CHECK (typeof(deferral_proto)='blob' AND length(deferral_proto) BETWEEN 1 AND 262144),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  CHECK (affected_period_start<=affected_period_end),
  UNIQUE(id,posting_id,organisation_id),
  FOREIGN KEY (posting_id,organisation_id,document_id) REFERENCES purchase_postings_v1(id,organisation_id,document_id) ON DELETE RESTRICT
);

CREATE INDEX purchase_gst_deferrals_future_idx ON purchase_gst_deferrals_v1(organisation_id,ordinary_last_claim_date,posting_id,id);

CREATE INDEX purchase_gst_deferrals_period_idx ON purchase_gst_deferrals_v1(organisation_id,affected_period_start,affected_period_end,id);

CREATE TRIGGER purchase_gst_deferrals_v1_no_update BEFORE UPDATE ON purchase_gst_deferrals_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_gst_deferrals_v1_no_delete BEFORE DELETE ON purchase_gst_deferrals_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_gst_deferrals_v1_no_conflicting_insert BEFORE INSERT ON purchase_gst_deferrals_v1
WHEN EXISTS(SELECT 1 FROM purchase_gst_deferrals_v1 WHERE id=NEW.id)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_gst_deferral_portions_v1 (
  deferral_id TEXT NOT NULL,
  sequence INTEGER NOT NULL CHECK (typeof(sequence)='integer' AND sequence>0),
  organisation_id TEXT NOT NULL,
  posting_id TEXT NOT NULL,
  deferred_gst_minor INTEGER NOT NULL CHECK (typeof(deferred_gst_minor)='integer' AND deferred_gst_minor>0),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('POSTING','PAYMENT')),
  payment_id TEXT,
  allocation_sequence INTEGER NOT NULL CHECK (typeof(allocation_sequence)='integer' AND allocation_sequence>0),
  normal_event_date TEXT NOT NULL CHECK (length(normal_event_date)=10 AND normal_event_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(normal_event_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(normal_event_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(normal_event_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(normal_event_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(normal_event_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(normal_event_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(normal_event_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  earliest_period_start TEXT NOT NULL CHECK (length(earliest_period_start)=10 AND earliest_period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(earliest_period_start,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(earliest_period_start,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(earliest_period_start,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(earliest_period_start,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(earliest_period_start,1,4) AS INTEGER)%4=0 AND (CAST(substr(earliest_period_start,1,4) AS INTEGER)%100<>0 OR CAST(substr(earliest_period_start,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  earliest_period_end TEXT NOT NULL CHECK (length(earliest_period_end)=10 AND earliest_period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(earliest_period_end,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(earliest_period_end,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(earliest_period_end,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(earliest_period_end,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(earliest_period_end,1,4) AS INTEGER)%4=0 AND (CAST(substr(earliest_period_end,1,4) AS INTEGER)%100<>0 OR CAST(substr(earliest_period_end,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  required_return_due_date TEXT NOT NULL CHECK (length(required_return_due_date)=10 AND required_return_due_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(required_return_due_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(required_return_due_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(required_return_due_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(required_return_due_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(required_return_due_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(required_return_due_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(required_return_due_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  due_date_source_id TEXT NOT NULL,
  due_date_source_version INTEGER NOT NULL CHECK (typeof(due_date_source_version)='integer' AND due_date_source_version>0),
  due_date_source_hash BLOB NOT NULL CHECK ((typeof(due_date_source_hash)='blob' AND length(due_date_source_hash)=32)),
  ordinary_last_claim_date TEXT NOT NULL CHECK (length(ordinary_last_claim_date)=10 AND ordinary_last_claim_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(ordinary_last_claim_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(ordinary_last_claim_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(ordinary_last_claim_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  evidence_id TEXT NOT NULL,
  evidence_held_date TEXT NOT NULL CHECK (length(evidence_held_date)=10 AND evidence_held_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(evidence_held_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(evidence_held_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(evidence_held_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(evidence_held_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(evidence_held_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(evidence_held_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(evidence_held_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  portion_proto BLOB NOT NULL CHECK (typeof(portion_proto)='blob' AND length(portion_proto) BETWEEN 1 AND 16384),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  CHECK (earliest_period_start<=earliest_period_end),
  FOREIGN KEY (evidence_id,posting_id,organisation_id) REFERENCES purchase_tax_evidence_v1(id,posting_id,organisation_id) ON DELETE RESTRICT,
  CHECK((source_kind='POSTING' AND payment_id IS NULL) OR (source_kind='PAYMENT' AND payment_id IS NOT NULL)),
  PRIMARY KEY(deferral_id,sequence),
  FOREIGN KEY (deferral_id,posting_id,organisation_id) REFERENCES purchase_gst_deferrals_v1(id,posting_id,organisation_id) ON DELETE RESTRICT
);

CREATE TRIGGER purchase_gst_deferral_portions_v1_posting_owner BEFORE INSERT ON purchase_gst_deferral_portions_v1
WHEN NEW.source_kind='POSTING' AND NOT EXISTS(SELECT 1 FROM purchase_posting_allocations_v1 WHERE posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND sequence=NEW.allocation_sequence)
BEGIN SELECT RAISE(ABORT,'GST source posting allocation is not owned'); END;

CREATE TRIGGER purchase_gst_deferral_portions_v1_payment_owner BEFORE INSERT ON purchase_gst_deferral_portions_v1
WHEN NEW.source_kind='PAYMENT' AND NOT EXISTS(SELECT 1 FROM purchase_payment_allocations_v1 WHERE payment_id=NEW.payment_id AND posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND sequence=NEW.allocation_sequence)
BEGIN SELECT RAISE(ABORT,'GST source payment allocation is not owned'); END;

CREATE INDEX purchase_gst_deferral_portions_v1_evidence_idx ON purchase_gst_deferral_portions_v1(evidence_id,posting_id,organisation_id);

CREATE INDEX purchase_gst_deferral_portions_v1_payment_idx ON purchase_gst_deferral_portions_v1(payment_id,allocation_sequence);

CREATE INDEX purchase_gst_deferral_portions_v1_posting_idx ON purchase_gst_deferral_portions_v1(posting_id,organisation_id,allocation_sequence);

CREATE TRIGGER purchase_gst_deferral_portions_v1_evidence_held_guard BEFORE INSERT ON purchase_gst_deferral_portions_v1
WHEN NOT EXISTS(SELECT 1 FROM purchase_tax_evidence_v1 WHERE id=NEW.evidence_id AND posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND held_date=NEW.evidence_held_date AND classification IN (2,3))
BEGIN SELECT RAISE(ABORT,'GST source does not match held evidence'); END;

CREATE UNIQUE INDEX purchase_gst_deferral_portions_source_unique ON purchase_gst_deferral_portions_v1(deferral_id,source_kind,COALESCE(payment_id,''),allocation_sequence);

CREATE TRIGGER purchase_gst_deferral_portions_v1_no_update BEFORE UPDATE ON purchase_gst_deferral_portions_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_gst_deferral_portions_v1_no_delete BEFORE DELETE ON purchase_gst_deferral_portions_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_gst_deferral_portions_v1_no_conflicting_insert BEFORE INSERT ON purchase_gst_deferral_portions_v1
WHEN EXISTS(SELECT 1 FROM purchase_gst_deferral_portions_v1 WHERE (deferral_id=NEW.deferral_id AND sequence=NEW.sequence) OR (deferral_id=NEW.deferral_id AND source_kind=NEW.source_kind AND payment_id IS NEW.payment_id AND allocation_sequence=NEW.allocation_sequence))
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_gst_attributions_v1 (
  id TEXT NOT NULL PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND length(trim(id))>0),
  operation_key TEXT NOT NULL REFERENCES command_idempotency_v1(operation_key) ON DELETE RESTRICT,
  organisation_id TEXT NOT NULL,
  posting_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  accounting_version INTEGER NOT NULL CHECK (typeof(accounting_version)='integer' AND accounting_version>0),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('POSTING','PAYMENT')),
  payment_id TEXT,
  allocation_sequence INTEGER NOT NULL CHECK (typeof(allocation_sequence)='integer' AND allocation_sequence>0),
  normal_event_date TEXT NOT NULL CHECK (length(normal_event_date)=10 AND normal_event_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(normal_event_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(normal_event_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(normal_event_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(normal_event_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(normal_event_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(normal_event_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(normal_event_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  earliest_period_start TEXT NOT NULL CHECK (length(earliest_period_start)=10 AND earliest_period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(earliest_period_start,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(earliest_period_start,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(earliest_period_start,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(earliest_period_start,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(earliest_period_start,1,4) AS INTEGER)%4=0 AND (CAST(substr(earliest_period_start,1,4) AS INTEGER)%100<>0 OR CAST(substr(earliest_period_start,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  earliest_period_end TEXT NOT NULL CHECK (length(earliest_period_end)=10 AND earliest_period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(earliest_period_end,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(earliest_period_end,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(earliest_period_end,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(earliest_period_end,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(earliest_period_end,1,4) AS INTEGER)%4=0 AND (CAST(substr(earliest_period_end,1,4) AS INTEGER)%100<>0 OR CAST(substr(earliest_period_end,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  required_return_due_date TEXT NOT NULL CHECK (length(required_return_due_date)=10 AND required_return_due_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(required_return_due_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(required_return_due_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(required_return_due_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(required_return_due_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(required_return_due_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(required_return_due_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(required_return_due_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  due_date_source_id TEXT NOT NULL,
  due_date_source_version INTEGER NOT NULL CHECK (typeof(due_date_source_version)='integer' AND due_date_source_version>0),
  due_date_source_hash BLOB NOT NULL CHECK ((typeof(due_date_source_hash)='blob' AND length(due_date_source_hash)=32)),
  ordinary_last_claim_date TEXT NOT NULL CHECK (length(ordinary_last_claim_date)=10 AND ordinary_last_claim_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(ordinary_last_claim_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(ordinary_last_claim_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(ordinary_last_claim_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(ordinary_last_claim_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  evidence_id TEXT NOT NULL,
  evidence_held_date TEXT NOT NULL CHECK (length(evidence_held_date)=10 AND evidence_held_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(evidence_held_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(evidence_held_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(evidence_held_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(evidence_held_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(evidence_held_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(evidence_held_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(evidence_held_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  claim_period_start TEXT NOT NULL CHECK (length(claim_period_start)=10 AND claim_period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(claim_period_start,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(claim_period_start,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(claim_period_start,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(claim_period_start,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(claim_period_start,1,4) AS INTEGER)%4=0 AND (CAST(substr(claim_period_start,1,4) AS INTEGER)%100<>0 OR CAST(substr(claim_period_start,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  claim_period_end TEXT NOT NULL CHECK (length(claim_period_end)=10 AND claim_period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(claim_period_end,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(claim_period_end,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(claim_period_end,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(claim_period_end,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(claim_period_end,1,4) AS INTEGER)%4=0 AND (CAST(substr(claim_period_end,1,4) AS INTEGER)%100<>0 OR CAST(substr(claim_period_end,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  release_posting_date TEXT NOT NULL CHECK (length(release_posting_date)=10 AND release_posting_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(release_posting_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(release_posting_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(release_posting_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(release_posting_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(release_posting_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(release_posting_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(release_posting_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  reviewed_as_of TEXT NOT NULL CHECK (length(reviewed_as_of)=10 AND reviewed_as_of GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(reviewed_as_of,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(reviewed_as_of,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(reviewed_as_of,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(reviewed_as_of,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(reviewed_as_of,1,4) AS INTEGER)%4=0 AND (CAST(substr(reviewed_as_of,1,4) AS INTEGER)%100<>0 OR CAST(substr(reviewed_as_of,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  timing_reason TEXT NOT NULL CHECK(length(timing_reason) BETWEEN 1 AND 512),
  claimed_gst_minor INTEGER NOT NULL CHECK (typeof(claimed_gst_minor)='integer' AND claimed_gst_minor>0),
  currency_code TEXT NOT NULL CHECK (currency_code='AUD'),
  closes_deferral_id TEXT,
  journal_id TEXT NOT NULL,
  financial_revision INTEGER NOT NULL CHECK (typeof(financial_revision)='integer' AND financial_revision>0),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  attribution_proto BLOB NOT NULL CHECK (typeof(attribution_proto)='blob' AND length(attribution_proto) BETWEEN 1 AND 32768),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  CHECK (earliest_period_start<=earliest_period_end),
  FOREIGN KEY (evidence_id,posting_id,organisation_id) REFERENCES purchase_tax_evidence_v1(id,posting_id,organisation_id) ON DELETE RESTRICT,
  CHECK((source_kind='POSTING' AND payment_id IS NULL) OR (source_kind='PAYMENT' AND payment_id IS NOT NULL)),
  CHECK (claim_period_start<=claim_period_end),
  FOREIGN KEY (posting_id,organisation_id,document_id) REFERENCES purchase_postings_v1(id,organisation_id,document_id) ON DELETE RESTRICT,
  FOREIGN KEY (journal_id,organisation_id) REFERENCES journals(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (closes_deferral_id,posting_id,organisation_id) REFERENCES purchase_gst_deferrals_v1(id,posting_id,organisation_id) ON DELETE RESTRICT
);

CREATE TRIGGER purchase_gst_attributions_v1_posting_owner BEFORE INSERT ON purchase_gst_attributions_v1
WHEN NEW.source_kind='POSTING' AND NOT EXISTS(SELECT 1 FROM purchase_posting_allocations_v1 WHERE posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND sequence=NEW.allocation_sequence)
BEGIN SELECT RAISE(ABORT,'GST source posting allocation is not owned'); END;

CREATE TRIGGER purchase_gst_attributions_v1_payment_owner BEFORE INSERT ON purchase_gst_attributions_v1
WHEN NEW.source_kind='PAYMENT' AND NOT EXISTS(SELECT 1 FROM purchase_payment_allocations_v1 WHERE payment_id=NEW.payment_id AND posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND sequence=NEW.allocation_sequence)
BEGIN SELECT RAISE(ABORT,'GST source payment allocation is not owned'); END;

CREATE INDEX purchase_gst_attributions_v1_evidence_idx ON purchase_gst_attributions_v1(evidence_id,posting_id,organisation_id);

CREATE INDEX purchase_gst_attributions_v1_payment_idx ON purchase_gst_attributions_v1(payment_id,allocation_sequence);

CREATE INDEX purchase_gst_attributions_v1_posting_idx ON purchase_gst_attributions_v1(posting_id,organisation_id,allocation_sequence);

CREATE TRIGGER purchase_gst_attributions_v1_evidence_held_guard BEFORE INSERT ON purchase_gst_attributions_v1
WHEN NOT EXISTS(SELECT 1 FROM purchase_tax_evidence_v1 WHERE id=NEW.evidence_id AND posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND held_date=NEW.evidence_held_date AND classification IN (2,3))
BEGIN SELECT RAISE(ABORT,'GST source does not match held evidence'); END;

CREATE INDEX purchase_gst_attributions_period_idx ON purchase_gst_attributions_v1(organisation_id,claim_period_start,claim_period_end,id);

CREATE INDEX purchase_gst_attributions_deferral_idx ON purchase_gst_attributions_v1(closes_deferral_id,posting_id,organisation_id);

CREATE INDEX purchase_gst_attributions_journal_idx ON purchase_gst_attributions_v1(journal_id,organisation_id,operation_key);

CREATE UNIQUE INDEX purchase_gst_attributions_portion_unique ON purchase_gst_attributions_v1(operation_key,source_kind,COALESCE(payment_id,''),allocation_sequence);

CREATE TRIGGER purchase_gst_attributions_journal_owner BEFORE INSERT ON purchase_gst_attributions_v1
WHEN NOT EXISTS(SELECT 1 FROM journals WHERE id=NEW.journal_id AND organisation_id=NEW.organisation_id AND source_kind=8 AND source_id=NEW.operation_key AND state='POSTED') OR EXISTS(SELECT 1 FROM purchase_gst_attributions_v1 WHERE journal_id=NEW.journal_id AND operation_key<>NEW.operation_key)
BEGIN SELECT RAISE(ABORT,'GST attribution command does not own journal'); END;

CREATE TRIGGER purchase_gst_attributions_deferral_portion BEFORE INSERT ON purchase_gst_attributions_v1
WHEN NEW.closes_deferral_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM purchase_gst_deferral_portions_v1 WHERE deferral_id=NEW.closes_deferral_id AND posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND source_kind=NEW.source_kind AND payment_id IS NEW.payment_id AND allocation_sequence=NEW.allocation_sequence)
BEGIN SELECT RAISE(ABORT,'GST claim does not close its owned deferral portion'); END;

CREATE TRIGGER purchase_gst_attributions_v1_no_update BEFORE UPDATE ON purchase_gst_attributions_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_gst_attributions_v1_no_delete BEFORE DELETE ON purchase_gst_attributions_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_gst_attributions_v1_no_conflicting_insert BEFORE INSERT ON purchase_gst_attributions_v1
WHEN EXISTS(SELECT 1 FROM purchase_gst_attributions_v1 WHERE id=NEW.id OR (operation_key=NEW.operation_key AND source_kind=NEW.source_kind AND payment_id IS NEW.payment_id AND allocation_sequence=NEW.allocation_sequence))
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_legacy_reviews_v1 (
  id TEXT NOT NULL PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND length(trim(id))>0),
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('DOCUMENT','OPENING_PAYABLE')),
  document_id TEXT,
  opening_payable_id TEXT,
  source_version INTEGER NOT NULL CHECK (typeof(source_version)='integer' AND source_version>0),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  reviewed_snapshot_hash BLOB NULL CHECK (reviewed_snapshot_hash IS NULL OR (typeof(reviewed_snapshot_hash)='blob' AND length(reviewed_snapshot_hash)=32)),
  accounting_version INTEGER NOT NULL CHECK (typeof(accounting_version)='integer' AND accounting_version>0),
  affected_period_start TEXT NOT NULL CHECK (length(affected_period_start)=10 AND affected_period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(affected_period_start,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(affected_period_start,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(affected_period_start,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(affected_period_start,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(affected_period_start,1,4) AS INTEGER)%4=0 AND (CAST(substr(affected_period_start,1,4) AS INTEGER)%100<>0 OR CAST(substr(affected_period_start,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  affected_period_end TEXT NOT NULL CHECK (length(affected_period_end)=10 AND affected_period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(affected_period_end,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(affected_period_end,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(affected_period_end,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(affected_period_end,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(affected_period_end,1,4) AS INTEGER)%4=0 AND (CAST(substr(affected_period_end,1,4) AS INTEGER)%100<>0 OR CAST(substr(affected_period_end,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  reporting_balance_date TEXT NOT NULL CHECK (length(reporting_balance_date)=10 AND reporting_balance_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(reporting_balance_date,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(reporting_balance_date,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(reporting_balance_date,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(reporting_balance_date,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(reporting_balance_date,1,4) AS INTEGER)%4=0 AND (CAST(substr(reporting_balance_date,1,4) AS INTEGER)%100<>0 OR CAST(substr(reporting_balance_date,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  disposition INTEGER NOT NULL CHECK(typeof(disposition)='integer' AND disposition BETWEEN 1 AND 6),
  previously_claimed_period_start TEXT NULL CHECK (previously_claimed_period_start IS NULL OR (length(previously_claimed_period_start)=10 AND previously_claimed_period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(previously_claimed_period_start,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(previously_claimed_period_start,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(previously_claimed_period_start,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(previously_claimed_period_start,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(previously_claimed_period_start,1,4) AS INTEGER)%4=0 AND (CAST(substr(previously_claimed_period_start,1,4) AS INTEGER)%100<>0 OR CAST(substr(previously_claimed_period_start,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END)),
  previously_claimed_period_end TEXT NULL CHECK (previously_claimed_period_end IS NULL OR (length(previously_claimed_period_end)=10 AND previously_claimed_period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(previously_claimed_period_end,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(previously_claimed_period_end,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(previously_claimed_period_end,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(previously_claimed_period_end,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(previously_claimed_period_end,1,4) AS INTEGER)%4=0 AND (CAST(substr(previously_claimed_period_end,1,4) AS INTEGER)%100<>0 OR CAST(substr(previously_claimed_period_end,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END)),
  claim_evidence_source_id TEXT,
  claim_evidence_source_version INTEGER CHECK(claim_evidence_source_version IS NULL OR (typeof(claim_evidence_source_version)='integer' AND claim_evidence_source_version>0)),
  claim_evidence_source_hash BLOB NULL CHECK (claim_evidence_source_hash IS NULL OR (typeof(claim_evidence_source_hash)='blob' AND length(claim_evidence_source_hash)=32)),
  linked_posting_id TEXT,
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 512),
  review_proto BLOB NOT NULL CHECK (typeof(review_proto)='blob' AND length(review_proto) BETWEEN 1 AND 131072),
  record_hash BLOB NOT NULL CHECK ((typeof(record_hash)='blob' AND length(record_hash)=32)),
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  CHECK (affected_period_start<=affected_period_end),
  UNIQUE(id,organisation_id),
  FOREIGN KEY (document_id,organisation_id) REFERENCES documents(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (opening_payable_id) REFERENCES purchase_opening_payables(id) ON DELETE RESTRICT,
  FOREIGN KEY (linked_posting_id,organisation_id) REFERENCES purchase_postings_v1(id,organisation_id) ON DELETE RESTRICT,
  CHECK((source_kind='DOCUMENT' AND document_id IS NOT NULL AND opening_payable_id IS NULL AND reviewed_snapshot_hash IS NOT NULL) OR (source_kind='OPENING_PAYABLE' AND opening_payable_id IS NOT NULL AND document_id IS NULL AND reviewed_snapshot_hash IS NULL)),
  CHECK((previously_claimed_period_start IS NULL)=(previously_claimed_period_end IS NULL)),
  CHECK(previously_claimed_period_start IS NULL OR previously_claimed_period_start<=previously_claimed_period_end),
  CHECK((claim_evidence_source_id IS NULL)=(claim_evidence_source_version IS NULL) AND (claim_evidence_source_id IS NULL)=(claim_evidence_source_hash IS NULL)),
  CHECK(disposition<>2 OR (previously_claimed_period_start IS NOT NULL AND claim_evidence_source_id IS NOT NULL)),
  CHECK(disposition<>5 OR linked_posting_id IS NOT NULL)
);

CREATE TRIGGER purchase_legacy_reviews_opening_owner BEFORE INSERT ON purchase_legacy_reviews_v1
WHEN NEW.opening_payable_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id WHERE p.id=NEW.opening_payable_id AND c.organisation_id=NEW.organisation_id AND c.version=NEW.source_version)
BEGIN SELECT RAISE(ABORT,'legacy opening source is not owned'); END;

CREATE TRIGGER purchase_legacy_reviews_document_source BEFORE INSERT ON purchase_legacy_reviews_v1
WHEN NEW.document_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM documents WHERE id=NEW.document_id AND organisation_id=NEW.organisation_id AND status='REVIEWED' AND version=NEW.source_version AND sha256=NEW.source_hash)
BEGIN SELECT RAISE(ABORT,'legacy document source is not reviewed'); END;

CREATE INDEX purchase_legacy_reviews_context_idx ON purchase_legacy_reviews_v1(organisation_id,source_kind,COALESCE(document_id,opening_payable_id),source_version,affected_period_start,affected_period_end,reporting_balance_date);

CREATE UNIQUE INDEX purchase_legacy_reviews_version_unique ON purchase_legacy_reviews_v1(organisation_id,source_kind,COALESCE(document_id,opening_payable_id),accounting_version);

CREATE INDEX purchase_legacy_reviews_document_idx ON purchase_legacy_reviews_v1(document_id,organisation_id);

CREATE INDEX purchase_legacy_reviews_opening_idx ON purchase_legacy_reviews_v1(opening_payable_id,organisation_id);

CREATE INDEX purchase_legacy_reviews_posting_idx ON purchase_legacy_reviews_v1(linked_posting_id,organisation_id);

CREATE TRIGGER purchase_legacy_reviews_v1_no_update BEFORE UPDATE ON purchase_legacy_reviews_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_legacy_reviews_v1_no_delete BEFORE DELETE ON purchase_legacy_reviews_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_legacy_reviews_v1_no_conflicting_insert BEFORE INSERT ON purchase_legacy_reviews_v1
WHEN EXISTS(SELECT 1 FROM purchase_legacy_reviews_v1 WHERE id=NEW.id OR (organisation_id=NEW.organisation_id AND source_kind=NEW.source_kind AND document_id IS NEW.document_id AND opening_payable_id IS NEW.opening_payable_id AND accounting_version=NEW.accounting_version))
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE purchase_command_audit_v1 (
  operation_key TEXT NOT NULL PRIMARY KEY CHECK(length(operation_key) BETWEEN 1 AND 256 AND length(trim(operation_key))>0) REFERENCES purchase_accounting_events_v1(operation_key) ON DELETE RESTRICT,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  workspace_id TEXT NOT NULL,
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  session_id TEXT NOT NULL,
  command_type TEXT NOT NULL,
  result_sha256 BLOB NOT NULL CHECK ((typeof(result_sha256)='blob' AND length(result_sha256)=32)),
  payload_proto BLOB NOT NULL CHECK (typeof(payload_proto)='blob' AND length(payload_proto) BETWEEN 1 AND 1048576),
  occurred_at TEXT NOT NULL,
  FOREIGN KEY (workspace_id,actor_user_id,command_type,operation_key) REFERENCES command_idempotency_v1(workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key) ON DELETE RESTRICT
);

CREATE TRIGGER purchase_command_audit_event_owner BEFORE INSERT ON purchase_command_audit_v1
WHEN NOT EXISTS(SELECT 1 FROM purchase_accounting_events_v1 WHERE operation_key=NEW.operation_key AND organisation_id=NEW.organisation_id AND command_type=NEW.command_type AND result_sha256=NEW.result_sha256)
BEGIN SELECT RAISE(ABORT,'purchase audit does not match accepted event'); END;

CREATE INDEX purchase_command_audit_actor_idx ON purchase_command_audit_v1(actor_user_id);

CREATE INDEX purchase_command_audit_org_idx ON purchase_command_audit_v1(organisation_id,occurred_at);

CREATE TRIGGER purchase_command_audit_v1_no_update BEFORE UPDATE ON purchase_command_audit_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER purchase_command_audit_v1_no_delete BEFORE DELETE ON purchase_command_audit_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER purchase_command_audit_v1_no_conflicting_insert BEFORE INSERT ON purchase_command_audit_v1
WHEN EXISTS(SELECT 1 FROM purchase_command_audit_v1 WHERE operation_key=NEW.operation_key)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE bas_workpaper_purchase_metadata_v1 (
  workpaper_id TEXT NOT NULL PRIMARY KEY CHECK(length(workpaper_id) BETWEEN 1 AND 128 AND length(trim(workpaper_id))>0),
  organisation_id TEXT NOT NULL,
  purchase_source_format TEXT NOT NULL CHECK(length(purchase_source_format) BETWEEN 1 AND 32 AND purchase_source_format='RECONCILED_V1'),
  legacy_candidate_gst_minor INTEGER NOT NULL CHECK (typeof(legacy_candidate_gst_minor)='integer' AND legacy_candidate_gst_minor>=0),
  currency_code TEXT NOT NULL CHECK (currency_code='AUD'),
  FOREIGN KEY (workpaper_id,organisation_id) REFERENCES bas_workpapers(id,organisation_id) ON DELETE RESTRICT
);

CREATE INDEX bas_workpaper_purchase_metadata_owner_idx ON bas_workpaper_purchase_metadata_v1(organisation_id,workpaper_id);

CREATE TRIGGER bas_workpaper_purchase_metadata_v1_no_update BEFORE UPDATE ON bas_workpaper_purchase_metadata_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER bas_workpaper_purchase_metadata_v1_no_delete BEFORE DELETE ON bas_workpaper_purchase_metadata_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER bas_workpaper_purchase_metadata_v1_no_conflicting_insert BEFORE INSERT ON bas_workpaper_purchase_metadata_v1
WHEN EXISTS(SELECT 1 FROM bas_workpaper_purchase_metadata_v1 WHERE workpaper_id=NEW.workpaper_id)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE bas_workpaper_purchase_sources_v1 (
  workpaper_id TEXT NOT NULL,
  sequence INTEGER NOT NULL CHECK (typeof(sequence)='integer' AND sequence>0),
  organisation_id TEXT NOT NULL,
  posting_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  allocation_sequence INTEGER NOT NULL CHECK (typeof(allocation_sequence)='integer' AND allocation_sequence>0),
  payment_id TEXT,
  attribution_id TEXT,
  claim_period_start TEXT NOT NULL CHECK (length(claim_period_start)=10 AND claim_period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(claim_period_start,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(claim_period_start,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(claim_period_start,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(claim_period_start,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(claim_period_start,1,4) AS INTEGER)%4=0 AND (CAST(substr(claim_period_start,1,4) AS INTEGER)%100<>0 OR CAST(substr(claim_period_start,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  claim_period_end TEXT NOT NULL CHECK (length(claim_period_end)=10 AND claim_period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(claim_period_end,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(claim_period_end,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(claim_period_end,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(claim_period_end,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(claim_period_end,1,4) AS INTEGER)%4=0 AND (CAST(substr(claim_period_end,1,4) AS INTEGER)%100<>0 OR CAST(substr(claim_period_end,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  gross_minor INTEGER NOT NULL CHECK (typeof(gross_minor)='integer' AND gross_minor>=0),
  supplier_gst_minor INTEGER NOT NULL CHECK (typeof(supplier_gst_minor)='integer' AND supplier_gst_minor>=0),
  eligible_gst_minor INTEGER NOT NULL CHECK (typeof(eligible_gst_minor)='integer' AND eligible_gst_minor>=0),
  claimed_gst_minor INTEGER NOT NULL CHECK (typeof(claimed_gst_minor)='integer' AND claimed_gst_minor>=0),
  currency_code TEXT NOT NULL CHECK (currency_code='AUD'),
  source_revision INTEGER NOT NULL CHECK (typeof(source_revision)='integer' AND source_revision>0),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  snapshot_proto BLOB NOT NULL CHECK (typeof(snapshot_proto)='blob' AND length(snapshot_proto) BETWEEN 1 AND 32768),
  CHECK (claim_period_start<=claim_period_end),
  PRIMARY KEY(workpaper_id,sequence),
  FOREIGN KEY (workpaper_id,organisation_id) REFERENCES bas_workpapers(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (posting_id,organisation_id,document_id) REFERENCES purchase_postings_v1(id,organisation_id,document_id) ON DELETE RESTRICT,
  FOREIGN KEY (payment_id,posting_id,organisation_id) REFERENCES purchase_payments_v1(id,posting_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (attribution_id) REFERENCES purchase_gst_attributions_v1(id) ON DELETE RESTRICT,
  CHECK(claimed_gst_minor<=eligible_gst_minor AND eligible_gst_minor<=gross_minor AND supplier_gst_minor<=gross_minor),
  CHECK(payment_id IS NOT NULL OR eligible_gst_minor<=supplier_gst_minor)
);

CREATE INDEX bas_workpaper_purchase_sources_posting_idx ON bas_workpaper_purchase_sources_v1(posting_id,organisation_id,document_id);

CREATE INDEX bas_workpaper_purchase_sources_payment_idx ON bas_workpaper_purchase_sources_v1(payment_id,posting_id,organisation_id);

CREATE INDEX bas_workpaper_purchase_sources_attribution_idx ON bas_workpaper_purchase_sources_v1(attribution_id);

CREATE TRIGGER bas_workpaper_purchase_sources_portion_owner BEFORE INSERT ON bas_workpaper_purchase_sources_v1
WHEN (NEW.payment_id IS NULL AND NOT EXISTS(SELECT 1 FROM purchase_posting_allocations_v1 WHERE posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND sequence=NEW.allocation_sequence)) OR (NEW.payment_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM purchase_payment_allocations_v1 WHERE payment_id=NEW.payment_id AND posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND sequence=NEW.allocation_sequence)) OR (NEW.attribution_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM purchase_gst_attributions_v1 WHERE id=NEW.attribution_id AND posting_id=NEW.posting_id AND organisation_id=NEW.organisation_id AND document_id=NEW.document_id AND payment_id IS NEW.payment_id AND allocation_sequence=NEW.allocation_sequence))
BEGIN SELECT RAISE(ABORT,'saved GST source portion is not owned'); END;

CREATE TRIGGER bas_workpaper_purchase_sources_v1_no_update BEFORE UPDATE ON bas_workpaper_purchase_sources_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER bas_workpaper_purchase_sources_v1_no_delete BEFORE DELETE ON bas_workpaper_purchase_sources_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER bas_workpaper_purchase_sources_v1_no_conflicting_insert BEFORE INSERT ON bas_workpaper_purchase_sources_v1
WHEN EXISTS(SELECT 1 FROM bas_workpaper_purchase_sources_v1 WHERE workpaper_id=NEW.workpaper_id AND sequence=NEW.sequence)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE bas_workpaper_purchase_reviews_v1 (
  workpaper_id TEXT NOT NULL,
  sequence INTEGER NOT NULL CHECK (typeof(sequence)='integer' AND sequence>0),
  organisation_id TEXT NOT NULL,
  kind INTEGER NOT NULL CHECK(typeof(kind)='integer' AND kind BETWEEN 1 AND 12),
  status INTEGER NOT NULL CHECK(typeof(status)='integer' AND status BETWEEN 1 AND 6),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('POSTING','DOCUMENT','OPENING_PAYABLE')),
  posting_id TEXT,
  document_id TEXT,
  opening_payable_id TEXT,
  payment_id TEXT,
  deferral_id TEXT,
  legacy_review_id TEXT,
  affected_period_start TEXT NOT NULL CHECK (length(affected_period_start)=10 AND affected_period_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(affected_period_start,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(affected_period_start,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(affected_period_start,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(affected_period_start,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(affected_period_start,1,4) AS INTEGER)%4=0 AND (CAST(substr(affected_period_start,1,4) AS INTEGER)%100<>0 OR CAST(substr(affected_period_start,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  affected_period_end TEXT NOT NULL CHECK (length(affected_period_end)=10 AND affected_period_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND CAST(substr(affected_period_end,1,4) AS INTEGER) BETWEEN 1900 AND 9999 AND CAST(substr(affected_period_end,6,2) AS INTEGER) BETWEEN 1 AND 12 AND CAST(substr(affected_period_end,9,2) AS INTEGER) BETWEEN 1 AND CASE CAST(substr(affected_period_end,6,2) AS INTEGER) WHEN 2 THEN CASE WHEN CAST(substr(affected_period_end,1,4) AS INTEGER)%4=0 AND (CAST(substr(affected_period_end,1,4) AS INTEGER)%100<>0 OR CAST(substr(affected_period_end,1,4) AS INTEGER)%400=0) THEN 29 ELSE 28 END WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30 ELSE 31 END),
  eligible_gst_minor INTEGER NOT NULL CHECK (typeof(eligible_gst_minor)='integer' AND eligible_gst_minor>=0),
  unclaimed_gst_minor INTEGER NOT NULL CHECK (typeof(unclaimed_gst_minor)='integer' AND unclaimed_gst_minor>=0),
  currency_code TEXT NOT NULL CHECK (currency_code='AUD'),
  source_revision INTEGER NOT NULL CHECK (typeof(source_revision)='integer' AND source_revision>0),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  snapshot_proto BLOB NOT NULL CHECK (typeof(snapshot_proto)='blob' AND length(snapshot_proto) BETWEEN 1 AND 65536),
  CHECK (affected_period_start<=affected_period_end),
  PRIMARY KEY(workpaper_id,sequence),
  FOREIGN KEY (workpaper_id,organisation_id) REFERENCES bas_workpapers(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (posting_id,organisation_id) REFERENCES purchase_postings_v1(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (document_id,organisation_id) REFERENCES documents(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (opening_payable_id) REFERENCES purchase_opening_payables(id) ON DELETE RESTRICT,
  FOREIGN KEY (legacy_review_id,organisation_id) REFERENCES purchase_legacy_reviews_v1(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (payment_id,posting_id,organisation_id) REFERENCES purchase_payments_v1(id,posting_id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (deferral_id,posting_id,organisation_id) REFERENCES purchase_gst_deferrals_v1(id,posting_id,organisation_id) ON DELETE RESTRICT,
  CHECK((source_kind='POSTING' AND posting_id IS NOT NULL AND opening_payable_id IS NULL) OR (source_kind='DOCUMENT' AND document_id IS NOT NULL AND posting_id IS NULL AND opening_payable_id IS NULL) OR (source_kind='OPENING_PAYABLE' AND opening_payable_id IS NOT NULL AND document_id IS NULL AND posting_id IS NULL)),
  CHECK(payment_id IS NULL OR posting_id IS NOT NULL),
  CHECK(deferral_id IS NULL OR posting_id IS NOT NULL),
  CHECK(unclaimed_gst_minor<=eligible_gst_minor)
);

CREATE TRIGGER bas_workpaper_purchase_reviews_opening_owner BEFORE INSERT ON bas_workpaper_purchase_reviews_v1
WHEN NEW.opening_payable_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id WHERE p.id=NEW.opening_payable_id AND c.organisation_id=NEW.organisation_id)
BEGIN SELECT RAISE(ABORT,'saved opening review is not owned'); END;

CREATE INDEX bas_workpaper_purchase_reviews_posting_id_idx ON bas_workpaper_purchase_reviews_v1(posting_id,organisation_id);

CREATE INDEX bas_workpaper_purchase_reviews_document_id_idx ON bas_workpaper_purchase_reviews_v1(document_id,organisation_id);

CREATE INDEX bas_workpaper_purchase_reviews_opening_payable_id_idx ON bas_workpaper_purchase_reviews_v1(opening_payable_id,organisation_id);

CREATE INDEX bas_workpaper_purchase_reviews_legacy_review_id_idx ON bas_workpaper_purchase_reviews_v1(legacy_review_id,organisation_id);

CREATE INDEX bas_workpaper_purchase_reviews_payment_id_idx ON bas_workpaper_purchase_reviews_v1(payment_id,organisation_id);

CREATE INDEX bas_workpaper_purchase_reviews_deferral_id_idx ON bas_workpaper_purchase_reviews_v1(deferral_id,organisation_id);

CREATE TRIGGER bas_workpaper_purchase_reviews_v1_no_update BEFORE UPDATE ON bas_workpaper_purchase_reviews_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER bas_workpaper_purchase_reviews_v1_no_delete BEFORE DELETE ON bas_workpaper_purchase_reviews_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER bas_workpaper_purchase_reviews_v1_no_conflicting_insert BEFORE INSERT ON bas_workpaper_purchase_reviews_v1
WHEN EXISTS(SELECT 1 FROM bas_workpaper_purchase_reviews_v1 WHERE workpaper_id=NEW.workpaper_id AND sequence=NEW.sequence)
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TABLE bas_workpaper_purchase_support_v1 (
  workpaper_id TEXT NOT NULL,
  organisation_id TEXT NOT NULL,
  review_id TEXT NOT NULL,
  kind INTEGER NOT NULL CHECK(kind IN (1,2)),
  sequence INTEGER NOT NULL CHECK (typeof(sequence)='integer' AND sequence>0),
  source_id TEXT NOT NULL,
  source_version INTEGER NOT NULL CHECK (typeof(source_version)='integer' AND source_version>0),
  source_hash BLOB NOT NULL CHECK ((typeof(source_hash)='blob' AND length(source_hash)=32)),
  snapshot_proto BLOB NOT NULL CHECK (typeof(snapshot_proto)='blob' AND length(snapshot_proto) BETWEEN 1 AND 2048),
  PRIMARY KEY(workpaper_id,review_id,kind,sequence),
  UNIQUE(workpaper_id,review_id,kind,source_id,source_version),
  FOREIGN KEY (workpaper_id,organisation_id) REFERENCES bas_workpapers(id,organisation_id) ON DELETE RESTRICT,
  FOREIGN KEY (review_id,organisation_id) REFERENCES purchase_legacy_reviews_v1(id,organisation_id) ON DELETE RESTRICT
);

CREATE INDEX bas_workpaper_purchase_support_review_idx ON bas_workpaper_purchase_support_v1(review_id,organisation_id);

CREATE TRIGGER bas_workpaper_purchase_support_saved_review BEFORE INSERT ON bas_workpaper_purchase_support_v1
WHEN NOT EXISTS(SELECT 1 FROM bas_workpaper_purchase_reviews_v1 WHERE workpaper_id=NEW.workpaper_id AND organisation_id=NEW.organisation_id AND legacy_review_id=NEW.review_id)
BEGIN SELECT RAISE(ABORT,'saved supporting source requires saved legacy review'); END;

CREATE TRIGGER bas_workpaper_purchase_support_v1_no_update BEFORE UPDATE ON bas_workpaper_purchase_support_v1 BEGIN SELECT RAISE(ABORT,'purchase history is immutable'); END;

CREATE TRIGGER bas_workpaper_purchase_support_v1_no_delete BEFORE DELETE ON bas_workpaper_purchase_support_v1 BEGIN SELECT RAISE(ABORT,'purchase history is retained'); END;

CREATE TRIGGER bas_workpaper_purchase_support_v1_no_conflicting_insert BEFORE INSERT ON bas_workpaper_purchase_support_v1
WHEN EXISTS(SELECT 1 FROM bas_workpaper_purchase_support_v1 WHERE (workpaper_id=NEW.workpaper_id AND review_id=NEW.review_id AND kind=NEW.kind AND sequence=NEW.sequence) OR (workpaper_id=NEW.workpaper_id AND review_id=NEW.review_id AND kind=NEW.kind AND source_id=NEW.source_id AND source_version=NEW.source_version))
BEGIN SELECT RAISE(ABORT,'purchase history cannot be replaced'); END;

CREATE TRIGGER bas_workpaper_purchase_sources_v1_bounded BEFORE INSERT ON bas_workpaper_purchase_sources_v1
WHEN (SELECT count(*) FROM bas_workpaper_purchase_sources_v1 WHERE workpaper_id=NEW.workpaper_id)>=1000
BEGIN SELECT RAISE(ABORT,'saved purchase source limit exceeded'); END;

CREATE TRIGGER bas_workpaper_purchase_reviews_v1_bounded BEFORE INSERT ON bas_workpaper_purchase_reviews_v1
WHEN (SELECT count(*) FROM bas_workpaper_purchase_reviews_v1 WHERE workpaper_id=NEW.workpaper_id)>=1000
BEGIN SELECT RAISE(ABORT,'saved purchase source limit exceeded'); END;

CREATE TRIGGER bas_workpaper_purchase_support_v1_bounded BEFORE INSERT ON bas_workpaper_purchase_support_v1
WHEN (SELECT count(*) FROM bas_workpaper_purchase_support_v1 WHERE workpaper_id=NEW.workpaper_id)>=1000
BEGIN SELECT RAISE(ABORT,'saved purchase source limit exceeded'); END;

CREATE INDEX purchase_postings_v1_actor_idx ON purchase_postings_v1(actor_user_id);

CREATE INDEX purchase_payments_v1_actor_idx ON purchase_payments_v1(actor_user_id);

CREATE INDEX purchase_tax_evidence_v1_actor_idx ON purchase_tax_evidence_v1(actor_user_id);

CREATE INDEX purchase_gst_attributions_v1_actor_idx ON purchase_gst_attributions_v1(actor_user_id);

CREATE INDEX purchase_gst_deferrals_v1_actor_idx ON purchase_gst_deferrals_v1(actor_user_id);

CREATE INDEX purchase_legacy_reviews_v1_actor_idx ON purchase_legacy_reviews_v1(actor_user_id);
