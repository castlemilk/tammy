//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/purchases"
)

type purchaseRootOwnerDiscoveryProbe struct {
	*purchaseCoverageHydrationProbe
	beforeIDRead func()
	censusReads  int
	idReads      int
}

func (probe *purchaseRootOwnerDiscoveryProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "SELECT d.id AS id FROM documents d") {
		probe.censusReads++
	}
	compact := strings.ToLower(strings.Join(strings.Fields(query), ""))
	if strings.Contains(query, "/* BAS purchase owner body coverage_document_ids */") ||
		strings.HasPrefix(compact, "selectd.idfromdocumentsasdwhere") {
		probe.idReads++
		if probe.beforeIDRead != nil {
			hook := probe.beforeIDRead
			probe.beforeIDRead = nil
			hook()
		}
	}
	return probe.purchaseCoverageHydrationProbe.QueryContext(ctx, query, args...)
}

func TestPurchaseReportingRootOwnerDocumentDiscoveryRejectsDriftAfterFreshCensus(t *testing.T) {
	for _, initiallyEmpty := range []bool{false, true} {
		name := "nonempty"
		if initiallyEmpty {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			tx, fixture := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			wantRoots := 1
			if initiallyEmpty {
				mutatePurchaseBASScalar(t, fixture, "documents",
					`UPDATE documents SET organisation_id=? WHERE id=?`, purchaseScalarRootOwnerForeignUUID, fixture.document.Id)
				wantRoots = 0
			}
			baseline, err := fixture.repo(tx).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if err != nil || baseline == nil || baseline.RootCount != wantRoots ||
				(initiallyEmpty && baseline.PayloadBytes != 0) || (!initiallyEmpty && baseline.PayloadBytes <= 0) {
				t.Fatalf("canonical document preflight present=%t err=%.256v", baseline != nil, err)
			}
			probe := &purchaseRootOwnerDiscoveryProbe{purchaseCoverageHydrationProbe: &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}}
			var afterMutation map[string]any
			triggers := purchaseRootOwnerTriggerDefinitions(t, tx)
			probe.beforeIDRead = func() {
				if probe.censusReads != 1 || probe.idReads != 1 || probe.bodyReads != 0 {
					t.Fatalf("late document discovery hook census/IDs/bodies=%d/%d/%d", probe.censusReads, probe.idReads, probe.bodyReads)
				}
				mutatePurchaseBASScalar(t, fixture, "documents",
					`UPDATE documents SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, fixture.document.Id)
				assertPurchaseScalarRootOwnerStorage(t, tx, "documents", fixture.document.Id, "blob", []byte(postingOrg))
				afterMutation = purchaseBASScalarState(t, tx)
			}
			result, err := fixture.repo(probe).PreflightCoverageForBASWithPayloadBudget(ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
			if result != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Errorf("late document discovery returned result=%t err=%.256v; want nil and repository error", result != nil, err)
			}
			if probe.censusReads != 1 || probe.idReads != 1 || probe.bodyReads != 0 || afterMutation == nil {
				t.Fatalf("late discovery census/IDs/bodies=%d/%d/%d hookRan=%t", probe.censusReads, probe.idReads, probe.bodyReads, afterMutation != nil)
			}
			assertPurchaseRootOwnerNoMutation(t, tx, afterMutation, triggers)
		})
	}
}
