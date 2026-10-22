//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package overview

import (
	"context"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"path/filepath"
	"testing"
)

type overviewProjectorProbe struct {
	called      bool
	gotOrg      string
	gotPeriod   *tammyv1.ReportingPeriod
	gotExecutor SnapshotExecutor
}

func (probe *overviewProjectorProbe) ProjectBASAttention(_ context.Context, executor SnapshotExecutor,
	organisationID string, period *tammyv1.ReportingPeriod) (BASAttentionProjection, error) {
	probe.called, probe.gotExecutor, probe.gotOrg = true, executor, organisationID
	probe.gotPeriod = proto.Clone(period).(*tammyv1.ReportingPeriod)
	return BASAttentionProjection{Status: tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED}, nil
}

func TestSQLCipherSnapshotPortRegistersBASProjectorOnExistingReadTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overview-projector.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	if _, err := sqlcipher.MigrateWorkspace(context.Background(), path, key, 9); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(context.Background(), path, key)
	clear(key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	port, err := NewSQLCipherSnapshotPort(database, overviewOrganisationID)
	if err != nil {
		t.Fatal(err)
	}
	probe := &overviewProjectorProbe{}
	if err := port.RegisterBASAttentionProjector(probe); err != nil {
		t.Fatal(err)
	}
	period := &tammyv1.ReportingPeriod{
		StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
		EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
	}
	snapshot, err := port.Attention(context.Background(), overviewOrganisationID, period)
	if err != nil {
		t.Fatal(err)
	}
	if !probe.called || probe.gotExecutor == nil || probe.gotOrg != overviewOrganisationID ||
		!proto.Equal(probe.gotPeriod, period) || snapshot.BASStatus != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED {
		t.Fatalf("projector call/snapshot = called:%v executor:%T org:%q period:%v snapshot:%#v", probe.called, probe.gotExecutor, probe.gotOrg, probe.gotPeriod, snapshot)
	}
	if err := port.RegisterBASAttentionProjector(&overviewProjectorProbe{}); err == nil {
		t.Fatal("duplicate projector registration succeeded")
	}
}
