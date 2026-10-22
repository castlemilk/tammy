CREATE TABLE bank_statement_imports (
  id TEXT PRIMARY KEY,
  operation_key TEXT NOT NULL UNIQUE,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  opening_balance_minor INTEGER NOT NULL,
  closing_balance_minor INTEGER NOT NULL,
  line_count INTEGER NOT NULL CHECK (line_count BETWEEN 1 AND 1000),
  imported_at TEXT NOT NULL
);

CREATE TABLE bank_statement_lines (
  id TEXT PRIMARY KEY,
  statement_import_id TEXT NOT NULL REFERENCES bank_statement_imports(id) ON DELETE RESTRICT,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  transaction_date TEXT NOT NULL,
  description TEXT NOT NULL CHECK (length(description) BETWEEN 1 AND 256),
  amount_minor INTEGER NOT NULL CHECK (amount_minor <> 0),
  status TEXT NOT NULL CHECK (status IN ('UNMATCHED','MATCHED','RECONCILED')),
  match_reference TEXT NOT NULL DEFAULT '' CHECK (length(match_reference) <= 128),
  UNIQUE(statement_import_id, sequence)
);

CREATE INDEX bank_statement_lines_list_idx
ON bank_statement_lines(organisation_id, transaction_date DESC, id DESC);

CREATE TRIGGER bank_statement_line_value_immutable
BEFORE UPDATE ON bank_statement_lines
WHEN NEW.id != OLD.id OR NEW.statement_import_id != OLD.statement_import_id OR
     NEW.organisation_id != OLD.organisation_id OR NEW.sequence != OLD.sequence OR
     NEW.transaction_date != OLD.transaction_date OR NEW.description != OLD.description OR
     NEW.amount_minor != OLD.amount_minor
BEGIN
  SELECT RAISE(ABORT, 'imported bank statement values are immutable');
END;

CREATE TABLE bank_reconciliations (
  id TEXT PRIMARY KEY,
  operation_key TEXT NOT NULL UNIQUE,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  statement_import_id TEXT NOT NULL REFERENCES bank_statement_imports(id) ON DELETE RESTRICT,
  reconciled_line_count INTEGER NOT NULL CHECK (reconciled_line_count > 0),
  closing_balance_minor INTEGER NOT NULL,
  completed_at TEXT NOT NULL,
  UNIQUE(statement_import_id)
);

CREATE TABLE bas_workpapers (
  id TEXT PRIMARY KEY,
  operation_key TEXT NOT NULL UNIQUE,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL CHECK (version > 0),
  period_start TEXT NOT NULL,
  period_end TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status = 'DRAFT_NOT_LODGED'),
  sales_g1_minor INTEGER NOT NULL,
  gst_on_sales_1a_minor INTEGER NOT NULL,
  gst_credits_1b_minor INTEGER NOT NULL,
  net_gst_payable_minor INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  CHECK (period_start <= period_end)
);

CREATE TABLE bas_workpaper_sources (
  workpaper_id TEXT NOT NULL REFERENCES bas_workpapers(id) ON DELETE RESTRICT,
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE RESTRICT,
  document_version INTEGER NOT NULL CHECK (document_version > 0),
  supplier_name TEXT NOT NULL CHECK (length(supplier_name) <= 256),
  invoice_number TEXT NOT NULL CHECK (length(invoice_number) <= 128),
  document_date TEXT NOT NULL,
  gross_minor INTEGER NOT NULL,
  gst_credit_minor INTEGER NOT NULL,
  PRIMARY KEY(workpaper_id, sequence),
  UNIQUE(workpaper_id, document_id)
);

CREATE INDEX bas_workpapers_current_idx
ON bas_workpapers(organisation_id, created_at DESC, id DESC);

CREATE TRIGGER bank_statement_imports_immutable_update BEFORE UPDATE ON bank_statement_imports BEGIN
  SELECT RAISE(ABORT, 'bank statement imports are immutable');
END;
CREATE TRIGGER bank_statement_imports_immutable_delete BEFORE DELETE ON bank_statement_imports BEGIN
  SELECT RAISE(ABORT, 'bank statement imports are immutable');
END;
CREATE TRIGGER bank_reconciliations_immutable_update BEFORE UPDATE ON bank_reconciliations BEGIN
  SELECT RAISE(ABORT, 'bank reconciliations are immutable');
END;
CREATE TRIGGER bank_reconciliations_immutable_delete BEFORE DELETE ON bank_reconciliations BEGIN
  SELECT RAISE(ABORT, 'bank reconciliations are immutable');
END;
CREATE TRIGGER bas_workpapers_immutable_update BEFORE UPDATE ON bas_workpapers BEGIN
  SELECT RAISE(ABORT, 'BAS workpapers are immutable');
END;
CREATE TRIGGER bas_workpapers_immutable_delete BEFORE DELETE ON bas_workpapers BEGIN
  SELECT RAISE(ABORT, 'BAS workpapers are immutable');
END;
CREATE TRIGGER bas_workpaper_sources_immutable_update BEFORE UPDATE ON bas_workpaper_sources BEGIN
  SELECT RAISE(ABORT, 'BAS provenance is immutable');
END;
CREATE TRIGGER bas_workpaper_sources_immutable_delete BEFORE DELETE ON bas_workpaper_sources BEGIN
  SELECT RAISE(ABORT, 'BAS provenance is immutable');
END;
