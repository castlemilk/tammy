//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	v1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"testing"
)

func TestBASPhysicalKeysReconcileFullModernOpeningLegacyBodies(t *testing.T) {
	for _, name := range []string{"modern", "opening-manual", "legacy-history"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			var f *postingFixture
			switch name {
			case "modern":
				f = newPurchaseModernOwnerFixture(t).fixture
			case "opening-manual":
				tx := newPurchaseReferenceDiscoveryTx(t)
				f = newPostingFixture(t, tx, v1.GstBasis_GST_BASIS_CASH)
				legacyOpeningFixture(t, f)
				seedCoverageLegacyCashPayment(t, f, f.document.Id, "1011")
			case "legacy-history":
				_, f, _, _ = purchaseBASLegacyDecisionFixture(t)
			}
			repo := f.repo(f.executor)
			summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
			if err != nil {
				t.Fatal(err)
			}
			keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
			if err != nil {
				t.Fatal(err)
			}
			capture, err := repo.CaptureCoverageForBAS(ctx, postingOrg)
			if err != nil {
				t.Fatal(err)
			}
			if err := purchases.ReconcileBASPhysicalKeys(ctx, postingOrg, keys, capture); err != nil {
				t.Fatalf("full %s body/key reconciliation: %v", name, err)
			}
			for index, key := range keys {
				changed := append([]purchases.BASScalarKey(nil), keys...)
				changed[index].IsRoot = !key.IsRoot
				if err := purchases.ReconcileBASPhysicalKeys(ctx, postingOrg, changed, capture); err == nil {
					t.Fatalf("same-count family %d root-flag drift accepted", key.Kind)
				}
			}
			// Retain the physical primary key and change only referenced proof data.
			for _, field := range []string{"source hash", "record hash", "operation key"} {
				changed := append([]purchases.BASScalarKey(nil), keys...)
				mutated := false
				for index := range changed {
					switch field {
					case "source hash":
						if len(changed[index].SourceHash) != 32 {
							continue
						}
						changed[index].SourceHash = append([]byte(nil), changed[index].SourceHash...)
						changed[index].SourceHash[0] ^= 0xff
					case "record hash":
						if len(changed[index].RecordHash) != 32 {
							continue
						}
						changed[index].RecordHash = append([]byte(nil), changed[index].RecordHash...)
						changed[index].RecordHash[0] ^= 0xff
					case "operation key":
						if changed[index].OperationKey == nil {
							continue
						}
						value := *changed[index].OperationKey + "-different-reference"
						changed[index].OperationKey = &value
					}
					mutated = true
					break
				}
				if mutated && purchases.ReconcileBASPhysicalKeys(ctx, postingOrg, changed, capture) == nil {
					t.Fatalf("same-key %s reference drift accepted", field)
				}
			}
			if len(keys) > 0 {
				if err := purchases.ReconcileBASPhysicalKeys(ctx, postingOrg, keys[:len(keys)-1], capture); err == nil {
					t.Fatal("missing selected key accepted")
				}
				duplicate := append(append([]purchases.BASScalarKey(nil), keys...), keys[0])
				if err := purchases.ReconcileBASPhysicalKeys(ctx, postingOrg, duplicate, capture); err == nil {
					t.Fatal("duplicate selected key accepted")
				}
			}
		})
	}
}
