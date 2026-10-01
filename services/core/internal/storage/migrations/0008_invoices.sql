CREATE TABLE invoices_v1 (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL CHECK (version > 0),
  state TEXT NOT NULL CHECK (state IN ('DRAFT','ISSUED')),
  number TEXT NOT NULL CHECK (length(number) <= 40),
  invoice_proto BLOB NOT NULL CHECK (length(invoice_proto) BETWEEN 1 AND 131072),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX invoices_number_unique ON invoices_v1(organisation_id, number) WHERE number != '';
CREATE INDEX invoices_list_idx ON invoices_v1(organisation_id, created_at DESC, id DESC);

CREATE TRIGGER invoices_v1_immutable_issued
BEFORE UPDATE ON invoices_v1
WHEN OLD.state = 'ISSUED' OR NEW.id != OLD.id OR NEW.organisation_id != OLD.organisation_id OR
     NEW.created_at != OLD.created_at OR NEW.version != OLD.version + 1
BEGIN
  SELECT RAISE(ABORT, 'issued invoice or identity is immutable');
END;

CREATE TRIGGER invoices_v1_no_delete
BEFORE DELETE ON invoices_v1
BEGIN
  SELECT RAISE(ABORT, 'invoice documents are retained');
END;

-- Local invoice commands are retained atomically with their idempotency result.
-- This is command evidence, not the separate global hash-linked audit chain.
CREATE TABLE invoice_command_audit_v1 (
  operation_key TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  session_id TEXT NOT NULL,
  command_type TEXT NOT NULL CHECK (command_type IN ('tammy.v1.InvoiceService.SaveInvoiceDraft','tammy.v1.InvoiceService.IssueInvoice')),
  result_sha256 BLOB NOT NULL CHECK (length(result_sha256) = 32),
  payload_proto BLOB NOT NULL CHECK (length(payload_proto) BETWEEN 1 AND 131072),
  occurred_at TEXT NOT NULL
);
CREATE TRIGGER invoice_command_audit_v1_no_update BEFORE UPDATE ON invoice_command_audit_v1
BEGIN SELECT RAISE(ABORT, 'invoice command evidence is immutable'); END;
CREATE TRIGGER invoice_command_audit_v1_no_delete BEFORE DELETE ON invoice_command_audit_v1
BEGIN SELECT RAISE(ABORT, 'invoice command evidence is immutable'); END;
