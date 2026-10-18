//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const purchaseRootOwnerLateCellBytes = 9 * 1024 * 1024

func TestPurchaseScalarRootOwnerBLOBDriftAfterFreshEmptyReservation(t *testing.T) {
	for _, tc := range []struct {
		name string
	}{
		{name: "document_owner"},
		{name: "posting_owner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			triggerDefinitions := purchaseRootOwnerTriggerDefinitions(t, tx)
			targetID := f.document.Id
			lateTable := "documents"
			lateCellColumn := "version"
			lateMutation := `UPDATE documents SET
				organisation_id=CAST(? AS BLOB),
				version=zeroblob(?)
				WHERE id=?`

			if tc.name == "document_owner" {
				// The fixture has only this one purchase document. Put it outside
				// the requested TEXT scope before reserving the empty key set.
				mutatePurchaseBASScalar(t, f, "documents",
					`UPDATE documents SET organisation_id='foreign-owner' WHERE id=?`, targetID)
			} else {
				// Use a real writer-created posting with no journal. The
				// zero-total posting keeps the physical-root ownership boundary
				// independent from manual-journal ownership.
				initialDocumentID := f.document.Id
				posted := postReportingPurchase(t, f, "ROOT-OWNER-POSTING",
					purchaseReportingDate(2026, 8, 10), 0, 0)
				if posted == nil || posted.Posting == nil {
					t.Fatal("zero-total PostReviewed fixture returned no posting")
				}
				targetID = posted.Posting.Id
				postedDocumentID := posted.Posting.DocumentId
				lateTable = "purchase_postings_v1"
				lateCellColumn = "reviewed_version"
				lateMutation = `UPDATE purchase_postings_v1 SET
					organisation_id=CAST(? AS BLOB),
					reviewed_version=zeroblob(?)
					WHERE id=?`

				// Remove every requested-organisation TEXT owner and parent
				// edge that could otherwise discover this posting. These are
				// corruption-only fixture changes in a transaction rolled back
				// below.
				const (
					foreignPostingID  = "018f0000-0000-7000-8000-000000009991"
					foreignDocumentID = "018f0000-0000-7000-8000-000000009992"
					foreignAccountID  = "018f0000-0000-7000-8000-000000009993"
				)
				mutatePurchaseBASScalar(t, f, "documents",
					`UPDATE documents SET organisation_id='foreign-owner' WHERE id IN (?,?)`,
					initialDocumentID, postedDocumentID)
				mutatePurchaseBASScalar(t, f, "accounts",
					`UPDATE accounts SET organisation_id='foreign-owner' WHERE id=?`, postingExpense)
				mutatePurchaseBASScalar(t, f, "purchase_posting_allocations_v1",
					`UPDATE purchase_posting_allocations_v1
					SET organisation_id='foreign-owner',posting_id=?,account_id=?
					WHERE posting_id=?`, foreignPostingID, foreignAccountID, targetID)
				mutatePurchaseBASScalar(t, f, "purchase_tax_evidence_v1",
					`UPDATE purchase_tax_evidence_v1
					SET organisation_id='foreign-owner',posting_id=?,document_id=?
					WHERE posting_id=?`, foreignPostingID, foreignDocumentID, targetID)
				mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1",
					`UPDATE purchase_accounting_events_v1
					SET organisation_id='foreign-owner',posting_id=?,document_id=?,scope_id=?`,
					foreignPostingID, foreignDocumentID, foreignPostingID)
				mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
					`UPDATE purchase_postings_v1
					SET organisation_id='foreign-owner',document_id=?
					WHERE id=?`, foreignDocumentID, targetID)
			}
			if !reflect.DeepEqual(triggerDefinitions, purchaseRootOwnerTriggerDefinitions(t, tx)) {
				t.Fatal("fixture setup did not restore exact trigger definitions")
			}

			probe := &purchaseBASScalarProbe{Transaction: tx}
			repo := f.repo(probe)

			// The baseline and its typed read prove that all other target TEXT
			// owner/reference arms are out of scope before the late mutation.
			before := purchaseBASScalarState(t, tx)
			summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
			wantEmpty := purchases.BASScalarSummary{OrganisationID: postingOrg}
			if err != nil || summary == nil || *summary != wantEmpty {
				t.Fatalf("foreign-scope scalar baseline=%#v err=%v, want a zero reservation", summary, err)
			}
			keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, 0, 0)
			if err != nil || len(keys) != 0 {
				t.Fatalf("foreign-scope scalar read=%#v err=%v, want an empty read", keys, err)
			}
			if !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) ||
				!reflect.DeepEqual(triggerDefinitions, purchaseRootOwnerTriggerDefinitions(t, tx)) ||
				!tx.Authenticated() {
				t.Fatal("empty summary/read changed retained state, failed to restore triggers, or ended the transaction")
			}
			if probe.summaryReads != 2 || probe.keyReads != 1 {
				t.Fatalf("empty reservation/read issued summary/key queries %d/%d, want 2/1", probe.summaryReads, probe.keyReads)
			}

			summaryReadsBeforeLate := probe.summaryReads
			keyReadsBeforeLate := probe.keyReads
			var afterMutation map[string]any
			var triggersAfterMutation map[string]string
			var beforeRead runtime.MemStats
			probe.beforeKeyRead = func() error {
				// ReadBASKeys recomputes the fresh summary before this hook.
				// The hook runs after that summary and immediately before the
				// guarded typed-key query.
				if probe.summaryReads != summaryReadsBeforeLate+1 {
					return errors.New("late mutation did not follow exactly one fresh scalar summary")
				}
				mutatePurchaseBASScalar(t, f, lateTable, lateMutation,
					postingOrg, purchaseRootOwnerLateCellBytes, targetID)
				purchaseRootOwnerAssertStoredBLOB(t, tx, lateTable, targetID, []byte(postingOrg))
				purchaseRootOwnerAssertLateCellMetadata(t, tx, lateTable, lateCellColumn, targetID)
				if got := postScalar(t, tx, `PRAGMA ignore_check_constraints`); got != 0 {
					t.Fatalf("fixture mutation left ignore_check_constraints=%d, want 0", got)
				}
				afterMutation = purchaseBASScalarState(t, tx)
				triggersAfterMutation = purchaseRootOwnerTriggerDefinitions(t, tx)
				runtime.GC()
				runtime.ReadMemStats(&beforeRead)
				return nil
			}

			keys, err = repo.ReadBASKeys(ctx, postingOrg, *summary, 0, 0)
			var afterRead runtime.MemStats
			runtime.ReadMemStats(&afterRead)

			allocated := afterRead.TotalAlloc - beforeRead.TotalAlloc
			errorBytes := 0
			if err != nil {
				errorBytes = len(err.Error())
			}
			var bound *purchases.SourceBoundError
			if keys != nil || !errors.Is(err, purchases.ErrSourceBound) ||
				!errors.As(err, &bound) || bound.Resource != "BAS scalar keys" || bound.Limit != 0 {
				t.Fatalf("late same-byte BLOB owner returned keys=%t bound=%#v error_bytes=%d alloc=%d err=%.256v; want nil keys and BAS scalar keys/0",
					keys != nil, bound, errorBytes, allocated, err)
			}
			if errorBytes >= 512 || allocated >= 2*1024*1024 {
				t.Fatalf("guarded 9 MiB integer cell reached Go: error_bytes=%d alloc=%d err=%.256v",
					errorBytes, allocated, err)
			}
			if afterMutation == nil || !reflect.DeepEqual(afterMutation, purchaseBASScalarState(t, tx)) ||
				!reflect.DeepEqual(triggerDefinitions, triggersAfterMutation) ||
				!reflect.DeepEqual(triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, tx)) ||
				!tx.Authenticated() {
				t.Fatal("late owner rejection changed retained state, failed to restore triggers, or ended the transaction")
			}
			if probe.summaryReads != summaryReadsBeforeLate+1 || probe.keyReads != keyReadsBeforeLate+1 ||
				probe.bodyReads != 0 {
				t.Fatalf("late guard queries summary/key/body=%d/%d/%d; want one fresh summary, one key query, no body reads",
					probe.summaryReads-summaryReadsBeforeLate, probe.keyReads-keyReadsBeforeLate, probe.bodyReads)
			}
		})
	}
}

func purchaseRootOwnerTriggerDefinitions(t *testing.T, tx *sqlcipher.Transaction) map[string]string {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(),
		`SELECT name,sql FROM sqlite_master WHERE type='trigger' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	definitions := make(map[string]string)
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		definitions[name] = definition
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	return definitions
}

func purchaseRootOwnerAssertStoredBLOB(t *testing.T, tx *sqlcipher.Transaction, table, id string, want []byte) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(),
		"SELECT typeof(organisation_id),length(organisation_id),organisation_id FROM "+table+" WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatalf("missing %s owner row %s", table, id)
	}
	var storageType string
	var byteLength int64
	var owner []byte
	if err := rows.Scan(&storageType, &byteLength, &owner); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatalf("non-unique %s owner row %s", table, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if storageType != "blob" || byteLength != int64(len(want)) || !bytes.Equal(owner, want) {
		t.Fatalf("%s owner=(%q,%d,%x), want exact %d-byte BLOB %x", table, storageType, byteLength, owner, len(want), want)
	}
}

func purchaseRootOwnerAssertLateCellMetadata(t *testing.T, tx *sqlcipher.Transaction, table, column, id string) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(),
		"SELECT typeof("+column+"),length("+column+") FROM "+table+" WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatalf("missing %s.%s row %s", table, column, id)
	}
	var storageType string
	var byteLength int64
	if err := rows.Scan(&storageType, &byteLength); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatalf("non-unique %s.%s row %s", table, column, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if storageType != "blob" || byteLength != purchaseRootOwnerLateCellBytes {
		t.Fatalf("late cell metadata=%s/%d, want BLOB/%d", storageType, byteLength, purchaseRootOwnerLateCellBytes)
	}
}
