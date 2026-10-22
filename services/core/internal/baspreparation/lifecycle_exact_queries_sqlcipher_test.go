//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestBASFrozenExactHashQuerySeparatesMissingIdentityAndCorruptProof(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 998, true, 0)
	repository := newFrozenRepositoryForTest(t, tx)
	exact, err := repository.Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := repository.GetExactByHash(ctx, org, exact); err != nil || !found || value == nil {
		t.Fatalf("exact frozen body was not returned: %v, %v, %v", value, found, err)
	}
	wrongHash := proto.Clone(exact).(*tammyv1.SourceRef)
	wrongHash.ContentHash[0] ^= 0x80
	for _, query := range []struct {
		org   string
		exact *tammyv1.SourceRef
	}{{org, wrongHash}, {otherOrg, exact}} {
		if value, found, err := repository.GetExactByHash(ctx, query.org, query.exact); value != nil || found || err != nil {
			t.Fatalf("missing exact identity must be distinguishable from corrupt proof: %v, %v, %v", value, found, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_frozen_bas_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_frozen_bas_v1 SET frozen_proto=X'01' WHERE frozen_id=?`, exact.Id); err != nil {
		t.Fatal(err)
	}
	if value, found, err := repository.GetExactByHash(ctx, org, exact); value != nil || found || err == nil {
		t.Fatalf("corrupt retained body was treated as a missing identity: %v, %v, %v", value, found, err)
	}
}
