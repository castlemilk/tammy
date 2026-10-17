package purchases

// Global link metadata remains available even if there is no owned document
// row. Same-byte BLOB owner IDs are included only to reject corrupt storage;
// TEXT equality alone would silently omit them before the type checks.
// No financial/source/event body participates in this projection.
// The separate reference probe uses a bounded cast and document primary-key
// lookup; ordinary owner projection joins retain their strict identities.
const purchaseBASDocumentStateCTE = `WITH requested AS (SELECT ? AS organisation_id),
 invalid_reference_links AS (
  SELECT count(*) AS invalid_count
  FROM purchase_postings_v1 AS p CROSS JOIN requested AS target
  LEFT JOIN documents AS d ON d.id=CASE
   WHEN typeof(p.document_id)='blob' AND length(p.document_id)=36 THEN CAST(p.document_id AS TEXT) END
  WHERE d.organisation_id=target.organisation_id
   OR (typeof(d.organisation_id)='blob' AND length(d.organisation_id)=36 AND d.organisation_id=CAST(target.organisation_id AS BLOB))
 ),
 invalid_links AS (
  SELECT count(*)+(SELECT invalid_count FROM invalid_reference_links) AS invalid_count
  FROM purchase_postings_v1 AS p CROSS JOIN requested AS target
  LEFT JOIN documents AS d ON d.id=p.document_id
  LEFT JOIN organisations AS owner ON owner.id=p.organisation_id
  WHERE (p.organisation_id=target.organisation_id
    OR (typeof(p.organisation_id)='blob' AND length(p.organisation_id)=36 AND p.organisation_id=CAST(target.organisation_id AS BLOB))
    OR d.organisation_id=target.organisation_id
    OR (typeof(d.organisation_id)='blob' AND length(d.organisation_id)=36 AND d.organisation_id=CAST(target.organisation_id AS BLOB)))
   AND (typeof(p.organisation_id)!='text' OR owner.id IS NULL OR d.id IS NULL OR d.organisation_id IS NOT p.organisation_id
    OR typeof(p.id)!='text' OR length(CAST(p.id AS BLOB))!=36
    OR (SELECT count(*) FROM purchase_postings_v1 AS other WHERE other.document_id=p.document_id)>1)
 ), document_rows AS (
  SELECT d.id,d.organisation_id,d.status,d.version,d.sha256,p.id AS posting_id,
   512+coalesce(length(CAST(d.id AS BLOB)),0)+coalesce(length(CAST(d.organisation_id AS BLOB)),0)
    +coalesce(length(CAST(d.status AS BLOB)),0)+coalesce(length(d.sha256),0)
    +coalesce(length(CAST(p.id AS BLOB)),0) AS state_bytes,
   CASE WHEN owner.id IS NULL
    OR typeof(d.id)!='text' OR length(CAST(d.id AS BLOB))!=36
    OR typeof(d.organisation_id)!='text' OR length(CAST(d.organisation_id AS BLOB))!=36
    OR typeof(d.status)!='text' OR d.status NOT IN ('NEEDS_REVIEW','REVIEWED')
    OR typeof(d.version)!='integer' OR d.version<1
    OR typeof(d.sha256)!='blob' OR length(d.sha256)!=32
    OR (p.id IS NOT NULL AND (typeof(p.id)!='text' OR length(CAST(p.id AS BLOB))!=36))
    THEN 1 ELSE 0 END AS invalid_state
  FROM documents AS d CROSS JOIN requested AS target
  LEFT JOIN organisations AS owner ON owner.id=d.organisation_id
  LEFT JOIN purchase_postings_v1 AS p ON p.document_id=d.id
  WHERE d.organisation_id=target.organisation_id
   OR (typeof(d.organisation_id)='blob' AND length(d.organisation_id)=36 AND d.organisation_id=CAST(target.organisation_id AS BLOB))
 )`

const purchaseBASDocumentStateSummarySQL = `/* BAS document state summary */ ` + purchaseBASDocumentStateCTE + `,
 bounded_rows AS (SELECT * FROM document_rows ORDER BY id LIMIT ?)
 SELECT count(*),coalesce(sum(state_bytes*1.0),0),coalesce(max(invalid_state),0),
  (SELECT invalid_count FROM invalid_links) FROM bounded_rows`

const purchaseBASDocumentStateReadSQL = `/* BAS document states */ ` + purchaseBASDocumentStateCTE + `,
 bounded_rows AS (SELECT * FROM document_rows ORDER BY id LIMIT ?),
 ordered_rows AS (
  SELECT *,row_number() OVER (ORDER BY id) AS row_ordinal,
   sum(state_bytes) OVER (ORDER BY id ROWS UNBOUNDED PRECEDING) AS cumulative_bytes
  FROM bounded_rows
 ), guarded_rows AS (
  SELECT ordered_rows.*,invalid_count,
   invalid_state=0 AND invalid_count=0 AND cumulative_bytes<=? AND row_ordinal<=? AS admissible
  FROM ordered_rows CROSS JOIN invalid_links
 )
 SELECT 0 AS row_kind,0 AS row_ordinal,0 AS state_bytes,0 AS invalid_state,invalid_count,
  NULL AS id,NULL AS organisation_id,NULL AS status,NULL AS version,NULL AS sha256,NULL AS posting_id
 FROM invalid_links
 UNION ALL
 SELECT 1,row_ordinal,state_bytes,invalid_state,invalid_count,
  CASE WHEN admissible THEN id END,CASE WHEN admissible THEN organisation_id END,
  CASE WHEN admissible THEN status END,CASE WHEN admissible THEN version END,
  CASE WHEN admissible THEN sha256 END,CASE WHEN admissible THEN posting_id END
 FROM guarded_rows ORDER BY row_kind,row_ordinal`
