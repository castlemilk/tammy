# Company invoice template and logo

Extend the approved expense-to-invoice editor in Tammy's restrained accounting style. The user requested company logos and management of invoice templates, payment instructions, and related reusable fields.

Use one company template initially. It stores a logo, seller address, payment instructions, notes, and payment terms (0–365 days, initially 14). A compact expandable template form on the invoice page allows previewing, uploading or removing the logo and saving defaults. New invoices inherit saved defaults. Applying a saved template to an existing editable draft is an explicit action; it changes only those fields and due date. Draft fields remain individually editable. Issued invoices cannot apply templates.

PNG and JPEG logos are selected through a file input, capped at 1 MiB, 4096 pixels per dimension, and four million pixels. Validate actual content and MIME with a full bounded decode in the core. Store immutable organisation-scoped assets in SQLCipher. Templates and invoices reference asset IDs; invoice lists never inline image bytes. Uploaded assets remain available to invoices after the template logo is replaced or removed.

Invoices already snapshot seller identity and editable text. Add an optional logo reference to the invoice record and save request, leaving the existing invoice draft/MCP proposal schema intact. Omitting the save-request logo preserves a saved logo; an explicit empty value removes it. Existing invoices remain unbranded. Export and issue preflight fetch the saved logo under the current organisation and session guard, never the current company template.

The PDF places a contained logo in the header with seller name and ABN still visible. Use local data only and preserve aspect ratio within 120×56 points. Reject malformed logo content before rendering and verify an image was embedded, since the renderer can silently ignore corrupt input. Payment instructions and notes retain their existing PDF sections. Private cost, profit, source IDs and template internals remain excluded.

Template saves use optimistic versions and ordinary audited commands; authenticated invoice writers manage defaults and assets. Reads and writes enforce organisation ownership. A failed load, upload or save preserves user edits and exposes retry. Asynchronous defaults load cannot replace a draft that the user has started editing.

Validation covers bounded PNG/JPEG uploads, MIME mismatch, malformed and oversized files, cross-organisation references, optimistic conflicts, idempotency, migrations, restart persistence and issued immutability. UI tests cover defaults, explicit apply, logo removal, pending-load races and file errors. Real packaged Electron acceptance uploads a logo, saves defaults, creates and exports an invoice, restarts, changes the template, and confirms the earlier invoice exports its original logo and payment instructions.
