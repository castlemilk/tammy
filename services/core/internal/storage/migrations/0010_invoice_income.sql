-- Accounting links and receipts retain the issued document's financial source.
CREATE TABLE invoice_income_settings_v1 (
  organisation_id TEXT PRIMARY KEY REFERENCES organisations(id) ON DELETE RESTRICT,
  revenue_account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT
);
CREATE TABLE invoice_income_postings_v1 (
  invoice_id TEXT PRIMARY KEY REFERENCES invoices_v1(id) ON DELETE RESTRICT,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  invoice_version INTEGER NOT NULL CHECK (invoice_version > 0),
  invoice_date TEXT NOT NULL,
  gst_basis INTEGER NOT NULL CHECK (gst_basis BETWEEN 0 AND 2),
  revenue_account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  journal_id TEXT REFERENCES journals(id) ON DELETE RESTRICT,
  invoice_net_minor INTEGER NOT NULL CHECK (invoice_net_minor >= 0),
  invoice_gst_minor INTEGER NOT NULL CHECK (invoice_gst_minor >= 0),
  invoice_total_minor INTEGER NOT NULL CHECK (invoice_total_minor = invoice_net_minor + invoice_gst_minor),
  bas_sales_gross_minor INTEGER NOT NULL CHECK (bas_sales_gross_minor BETWEEN 0 AND invoice_total_minor),
  profile_version INTEGER NOT NULL CHECK (profile_version > 0),
  basis_audit_sequence INTEGER NOT NULL CHECK (basis_audit_sequence >= 0),
  financial_revision INTEGER NOT NULL CHECK (financial_revision > 0),
  source_hash BLOB NOT NULL CHECK (length(source_hash) = 32),
  created_at TEXT NOT NULL,
  UNIQUE(invoice_id, organisation_id),
  CHECK ((invoice_total_minor = 0 AND journal_id IS NULL) OR (invoice_total_minor > 0 AND journal_id IS NOT NULL))
);
CREATE INDEX invoice_income_postings_period_idx ON invoice_income_postings_v1(organisation_id,invoice_date);
CREATE TABLE invoice_income_lines_v1 (
  invoice_id TEXT NOT NULL REFERENCES invoice_income_postings_v1(invoice_id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  gross_minor INTEGER NOT NULL CHECK (gross_minor >= 0),
  net_minor INTEGER NOT NULL CHECK (net_minor >= 0),
  gst_minor INTEGER NOT NULL CHECK (gst_minor >= 0 AND gross_minor = net_minor + gst_minor),
  treatment INTEGER NOT NULL CHECK (treatment BETWEEN 1 AND 4),
  PRIMARY KEY(invoice_id,sequence)
);
CREATE TABLE invoice_payment_v1 (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL,
  invoice_id TEXT NOT NULL,
  payment_date TEXT NOT NULL,
  amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
  allocated_gst_minor INTEGER NOT NULL CHECK (allocated_gst_minor BETWEEN 0 AND amount_minor),
  bas_sales_gross_minor INTEGER NOT NULL CHECK (bas_sales_gross_minor BETWEEN 0 AND amount_minor),
  bank_account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  journal_id TEXT NOT NULL UNIQUE REFERENCES journals(id) ON DELETE RESTRICT,
  reference TEXT NOT NULL CHECK (length(reference) <= 200),
  payment_proto BLOB NOT NULL CHECK (length(payment_proto) BETWEEN 1 AND 4096),
  source_hash BLOB NOT NULL CHECK (length(source_hash) = 32),
  created_at TEXT NOT NULL,
  FOREIGN KEY(invoice_id,organisation_id) REFERENCES invoice_income_postings_v1(invoice_id,organisation_id) ON DELETE RESTRICT
);
CREATE INDEX invoice_payment_period_idx ON invoice_payment_v1(organisation_id,payment_date);
CREATE INDEX invoice_payment_invoice_idx ON invoice_payment_v1(invoice_id,created_at,id);
CREATE TABLE invoice_payment_lines_v1 (
  payment_id TEXT NOT NULL REFERENCES invoice_payment_v1(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  gross_minor INTEGER NOT NULL CHECK (gross_minor >= 0),
  gst_minor INTEGER NOT NULL CHECK (gst_minor BETWEEN 0 AND gross_minor),
  PRIMARY KEY(payment_id,sequence)
);
CREATE TABLE invoice_income_command_audit_v1 (
  operation_key TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  session_id TEXT NOT NULL,
  command_type TEXT NOT NULL CHECK (command_type IN ('tammy.v1.InvoiceService.PostInvoiceIncome','tammy.v1.InvoiceService.RecordInvoicePayment')),
  result_sha256 BLOB NOT NULL CHECK (length(result_sha256) = 32),
  payload_proto BLOB NOT NULL CHECK (length(payload_proto) BETWEEN 1 AND 131072),
  occurred_at TEXT NOT NULL
);
CREATE TRIGGER invoice_income_settings_no_update BEFORE UPDATE ON invoice_income_settings_v1 BEGIN SELECT RAISE(ABORT,'invoice income settings are retained'); END;
CREATE TRIGGER invoice_income_settings_no_delete BEFORE DELETE ON invoice_income_settings_v1 BEGIN SELECT RAISE(ABORT,'invoice income settings are retained'); END;
CREATE TRIGGER invoice_income_postings_no_update BEFORE UPDATE ON invoice_income_postings_v1 BEGIN SELECT RAISE(ABORT,'invoice accounting source is immutable'); END;
CREATE TRIGGER invoice_income_postings_no_delete BEFORE DELETE ON invoice_income_postings_v1 BEGIN SELECT RAISE(ABORT,'invoice accounting source is retained'); END;
CREATE TRIGGER invoice_income_lines_no_update BEFORE UPDATE ON invoice_income_lines_v1 BEGIN SELECT RAISE(ABORT,'invoice accounting lines are immutable'); END;
CREATE TRIGGER invoice_income_lines_no_delete BEFORE DELETE ON invoice_income_lines_v1 BEGIN SELECT RAISE(ABORT,'invoice accounting lines are retained'); END;
CREATE TRIGGER invoice_payment_no_update BEFORE UPDATE ON invoice_payment_v1 BEGIN SELECT RAISE(ABORT,'invoice receipts are immutable'); END;
CREATE TRIGGER invoice_payment_no_delete BEFORE DELETE ON invoice_payment_v1 BEGIN SELECT RAISE(ABORT,'invoice receipts are retained'); END;
CREATE TRIGGER invoice_payment_lines_no_update BEFORE UPDATE ON invoice_payment_lines_v1 BEGIN SELECT RAISE(ABORT,'invoice receipt allocations are immutable'); END;
CREATE TRIGGER invoice_payment_lines_no_delete BEFORE DELETE ON invoice_payment_lines_v1 BEGIN SELECT RAISE(ABORT,'invoice receipt allocations are retained'); END;
CREATE TRIGGER invoice_income_command_audit_no_update BEFORE UPDATE ON invoice_income_command_audit_v1 BEGIN SELECT RAISE(ABORT,'invoice accounting evidence is immutable'); END;
CREATE TRIGGER invoice_income_command_audit_no_delete BEFORE DELETE ON invoice_income_command_audit_v1 BEGIN SELECT RAISE(ABORT,'invoice accounting evidence is retained'); END;

ALTER TABLE bas_workpapers ADD COLUMN gst_basis INTEGER NOT NULL DEFAULT 0 CHECK (gst_basis BETWEEN 0 AND 2);
ALTER TABLE bas_workpapers ADD COLUMN source_fingerprint BLOB CHECK (source_fingerprint IS NULL OR length(source_fingerprint)=32);
ALTER TABLE bas_workpapers ADD COLUMN review_warnings_json TEXT NOT NULL DEFAULT '[]' CHECK (length(review_warnings_json)<=8192);
ALTER TABLE bas_workpapers ADD COLUMN excluded_cash_purchase_count INTEGER NOT NULL DEFAULT 0 CHECK (excluded_cash_purchase_count>=0);
CREATE TABLE bas_workpaper_sales_sources_v1 (
  workpaper_id TEXT NOT NULL REFERENCES bas_workpapers(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK (sequence>0),
  invoice_id TEXT NOT NULL REFERENCES invoices_v1(id) ON DELETE RESTRICT,
  invoice_number TEXT NOT NULL CHECK (length(invoice_number)<=40),
  customer_name TEXT NOT NULL CHECK (length(customer_name)<=256),
  tax_date TEXT NOT NULL,
  gross_minor INTEGER NOT NULL CHECK (gross_minor>=0),
  gst_minor INTEGER NOT NULL CHECK (gst_minor>=0),
  payment_id TEXT REFERENCES invoice_payment_v1(id) ON DELETE RESTRICT,
  source_revision INTEGER NOT NULL CHECK (source_revision>0),
  source_hash BLOB NOT NULL CHECK (length(source_hash)=32),
  PRIMARY KEY(workpaper_id,sequence)
);
CREATE TRIGGER bas_workpaper_sales_sources_no_update BEFORE UPDATE ON bas_workpaper_sales_sources_v1 BEGIN SELECT RAISE(ABORT,'BAS sales sources are immutable'); END;
CREATE TRIGGER bas_workpaper_sales_sources_no_delete BEFORE DELETE ON bas_workpaper_sales_sources_v1 BEGIN SELECT RAISE(ABORT,'BAS sales sources are retained'); END;
