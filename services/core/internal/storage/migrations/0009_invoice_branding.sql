CREATE TABLE invoice_logo_assets_v1 (
  id TEXT PRIMARY KEY,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  mime_type TEXT NOT NULL CHECK (mime_type IN ('image/png','image/jpeg')),
  byte_length INTEGER NOT NULL CHECK (byte_length BETWEEN 1 AND 1048576),
  sha256 BLOB NOT NULL CHECK (length(sha256) = 32),
  width INTEGER NOT NULL CHECK (width BETWEEN 1 AND 4096),
  height INTEGER NOT NULL CHECK (height BETWEEN 1 AND 4096 AND width * height <= 4000000),
  content BLOB NOT NULL CHECK (length(content) = byte_length),
  created_by_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE(id, organisation_id)
);
CREATE TRIGGER invoice_logo_assets_v1_no_update BEFORE UPDATE ON invoice_logo_assets_v1
BEGIN SELECT RAISE(ABORT, 'invoice logo snapshots are immutable'); END;
CREATE TRIGGER invoice_logo_assets_v1_no_delete BEFORE DELETE ON invoice_logo_assets_v1
BEGIN SELECT RAISE(ABORT, 'invoice logo snapshots are retained'); END;

CREATE TABLE invoice_templates_v1 (
  organisation_id TEXT PRIMARY KEY REFERENCES organisations(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL CHECK (version > 0),
  logo_asset_id TEXT,
  template_proto BLOB NOT NULL CHECK (length(template_proto) BETWEEN 1 AND 32768),
  updated_at TEXT NOT NULL,
  FOREIGN KEY (logo_asset_id, organisation_id) REFERENCES invoice_logo_assets_v1(id, organisation_id) ON DELETE RESTRICT
);
CREATE TRIGGER invoice_templates_v1_version_guard BEFORE UPDATE ON invoice_templates_v1
WHEN NEW.organisation_id != OLD.organisation_id OR NEW.version != OLD.version + 1
BEGIN SELECT RAISE(ABORT, 'invoice template requires the next version'); END;
CREATE TRIGGER invoice_templates_v1_no_delete BEFORE DELETE ON invoice_templates_v1
BEGIN SELECT RAISE(ABORT, 'invoice template version is retained'); END;

-- Small command results retain template values or logo metadata, never image bytes.
CREATE TABLE invoice_branding_command_audit_v1 (
  operation_key TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  session_id TEXT NOT NULL,
  command_type TEXT NOT NULL CHECK (command_type IN ('tammy.v1.InvoiceService.SaveInvoiceTemplate','tammy.v1.InvoiceService.UploadInvoiceLogo')),
  result_sha256 BLOB NOT NULL CHECK (length(result_sha256) = 32),
  payload_proto BLOB NOT NULL CHECK (length(payload_proto) BETWEEN 1 AND 32768),
  occurred_at TEXT NOT NULL
);
CREATE TRIGGER invoice_branding_command_audit_v1_no_update BEFORE UPDATE ON invoice_branding_command_audit_v1
BEGIN SELECT RAISE(ABORT, 'invoice branding command evidence is immutable'); END;
CREATE TRIGGER invoice_branding_command_audit_v1_no_delete BEFORE DELETE ON invoice_branding_command_audit_v1
BEGIN SELECT RAISE(ABORT, 'invoice branding command evidence is immutable'); END;
