CREATE TABLE documents (
  id TEXT PRIMARY KEY,
  operation_key TEXT NOT NULL UNIQUE,
  organisation_id TEXT NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL CHECK (version > 0),
  status TEXT NOT NULL CHECK (status IN ('NEEDS_REVIEW','REVIEWED')),
  source_display_name TEXT NOT NULL CHECK (length(source_display_name) BETWEEN 1 AND 255),
  mime_type TEXT NOT NULL CHECK (mime_type IN ('application/pdf','image/png','image/jpeg')),
  byte_length INTEGER NOT NULL CHECK (byte_length BETWEEN 1 AND 10485760),
  sha256 BLOB NOT NULL CHECK (length(sha256) = 32),
  original_bytes BLOB NOT NULL CHECK (length(original_bytes) = byte_length),
  extracted_text TEXT NOT NULL CHECK (length(CAST(extracted_text AS BLOB)) <= 1048576),
  supplier_name TEXT NOT NULL CHECK (length(supplier_name) <= 256),
  invoice_number TEXT NOT NULL CHECK (length(invoice_number) <= 128),
  document_date TEXT,
  subtotal_minor INTEGER NOT NULL,
  gst_minor INTEGER NOT NULL,
  total_minor INTEGER NOT NULL,
  created_by_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  reviewed_at TEXT,
  UNIQUE (organisation_id, sha256),
  CHECK ((status = 'NEEDS_REVIEW' AND reviewed_at IS NULL) OR
         (status = 'REVIEWED' AND reviewed_at IS NOT NULL))
);

CREATE INDEX documents_list_idx ON documents(organisation_id, created_at DESC, id DESC);

CREATE TRIGGER documents_immutable_source
BEFORE UPDATE ON documents
WHEN NEW.id != OLD.id OR NEW.organisation_id != OLD.organisation_id OR
     NEW.operation_key != OLD.operation_key OR
     NEW.source_display_name != OLD.source_display_name OR NEW.mime_type != OLD.mime_type OR
     NEW.byte_length != OLD.byte_length OR NEW.sha256 != OLD.sha256 OR
     NEW.original_bytes != OLD.original_bytes OR NEW.extracted_text != OLD.extracted_text OR
     NEW.created_by_user_id != OLD.created_by_user_id OR NEW.created_at != OLD.created_at
BEGIN
  SELECT RAISE(ABORT, 'document source is immutable');
END;

CREATE TRIGGER documents_review_transition
BEFORE UPDATE ON documents
WHEN NOT (OLD.status = 'NEEDS_REVIEW' AND NEW.status = 'REVIEWED' AND
          NEW.version = OLD.version + 1)
BEGIN
  SELECT RAISE(ABORT, 'invalid document review transition');
END;

CREATE TRIGGER documents_immutable_delete
BEFORE DELETE ON documents
BEGIN
  SELECT RAISE(ABORT, 'documents are immutable');
END;
