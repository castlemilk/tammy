//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestCalculationRepositoryRoundTripsSeparateComponentIdentities(t *testing.T) {
	workspace, org := newBASRawIdentityRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputs := seedCalculationRepositoryFixture(t, tx, org)
	first := rawIdentityRepositoryCoverage(28501, true)
	first.RawSource.Type, first.RawSource.Id = "purchase_payment_allocations_v1", "opaque/payment/1"
	first.RawSource.ComponentSequence, first.RawSource.RelatedId, first.RawSource.RelatedSequence = proto.Uint64(1), proto.String("opaque/posting/1"), proto.Uint64(2)
	second := proto.Clone(first).(*tammyv1.BasSourceCoverage)
	second.RawSource.ComponentSequence, second.RawSource.RelatedSequence = proto.Uint64(2), proto.Uint64(1)
	value.Coverage = []*tammyv1.BasSourceCoverage{first, second}
	repository := newCalculationRepositoryForTest(t, tx)
	ref, err := repository.Append(context.Background(), value, inputs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repository.GetExact(context.Background(), org, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Coverage) != 2 || !proto.Equal(got.Coverage[0], first) || !proto.Equal(got.Coverage[1], second) {
		t.Fatal("retained coverage coalesced or changed the exact component tuples")
	}
	rows, err := tx.QueryContext(context.Background(), "SELECT coverage_proto,content_hash FROM bas_calculation_raw_coverage_v1 WHERE calculation_id=? ORDER BY sequence", ref.Id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var hashes [][]byte
	for rows.Next() {
		var wire, hash []byte
		if err := rows.Scan(&wire, &hash); err != nil {
			t.Fatal(err)
		}
		decoded := &tammyv1.BasSourceCoverage{}
		if proto.Unmarshal(wire, decoded) != nil || !proto.Equal(decoded, value.Coverage[len(hashes)]) {
			t.Fatal("stored component identity frame changed")
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 2 || bytes.Equal(hashes[0], hashes[1]) {
		t.Fatal("distinct component tuples share one retained proof")
	}
}
