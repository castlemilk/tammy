//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/overview"
	"google.golang.org/protobuf/proto"
)

const (
	basOverviewOrgID    = "018f0000-0000-7000-8000-00000000a001"
	basOverviewFamilyID = "018f0000-0000-7000-8000-00000000a002"
	basOverviewRootID   = "018f0000-0000-7000-8000-00000000a003"
	basOverviewHeadID   = "018f0000-0000-7000-8000-00000000a004"
	basOverviewBranchID = "018f0000-0000-7000-8000-00000000a005"
)

func basOverviewRef(kind, id string, revision uint64, fill byte) *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: kind, Id: id, Revision: revision, ContentHash: bytes.Repeat([]byte{fill}, 32)}
}
func basOverviewPreparation(id string, family *tammyv1.SourceRef, state tammyv1.BasPreparationState,
	frozen *tammyv1.SourceRef) *tammyv1.BasPreparation {
	return &tammyv1.BasPreparation{PreparationId: id, OrganisationId: basOverviewOrgID, Version: 1,
		Family: proto.Clone(family).(*tammyv1.SourceRef), State: state,
		FrozenBas: frozen, ContentHash: bytes.Repeat([]byte{9}, 32)}
}
func basOverviewHistory(root *tammyv1.BasPreparation, family *tammyv1.SourceRef) baspreparation.ExternalHistoryInput {
	return baspreparation.ExternalHistoryInput{Chain: baspreparation.ChainSnapshot{
		Family: &baspreparation.Family{Reference: proto.Clone(family).(*tammyv1.SourceRef), Version: 1,
			Scope: baspreparation.FamilyScope{OrganisationID: basOverviewOrgID,
				Start: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1}, End: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
				Window: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP}},
		RootPreparation: root, PeriodReference: basOverviewRef("bas_period", "018f0000-0000-7000-8000-00000000a006", 1, 6),
		DisputeOverlay: tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE,
	}}
}

func TestProjectBASHistoryUsesExactCurrentChainPointers(t *testing.T) {
	family := basOverviewRef("bas_family", basOverviewFamilyID, 1, 1)
	frozen := basOverviewRef("bas_frozen", "018f0000-0000-7000-8000-00000000a007", 1, 7)
	root := basOverviewPreparation(basOverviewRootID, family,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING, nil)
	t.Run("mutable root requires action", func(t *testing.T) {
		got, err := projectBASHistory(basOverviewHistory(root, family), basOverviewOrgID, root)
		if err != nil || got.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED ||
			got.Preparation.Id != root.PreparationId || got.Family.Id != family.Id || got.Frozen != nil {
			t.Fatalf("projection = %#v, %v", got, err)
		}
	})
	t.Run("review-ready root links exact root", func(t *testing.T) {
		ready := proto.Clone(root).(*tammyv1.BasPreparation)
		ready.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY
		got, err := projectBASHistory(basOverviewHistory(ready, family), basOverviewOrgID, ready)
		if err != nil || got.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_REVIEW_READY || got.Preparation.Id != ready.PreparationId {
			t.Fatalf("projection = %#v, %v", got, err)
		}
	})
	t.Run("frozen handoff links exact frozen source", func(t *testing.T) {
		manual := proto.Clone(root).(*tammyv1.BasPreparation)
		manual.State, manual.FrozenBas = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF, frozen
		got, err := projectBASHistory(basOverviewHistory(manual, family), basOverviewOrgID, manual)
		if err != nil || got.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_FROZEN_FOR_MANUAL_HANDOFF ||
			!proto.Equal(got.Frozen, frozen) {
			t.Fatalf("projection = %#v, %v", got, err)
		}
	})
	confirmed := basOverviewPreparation(basOverviewHeadID, family,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED, frozen)
	t.Run("latest confirmed head wins over root", func(t *testing.T) {
		history := basOverviewHistory(root, family)
		history.Chain.LatestHead = confirmed
		got, err := projectBASHistory(history, basOverviewOrgID, root)
		if err != nil || got.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_EXTERNAL_CONFIRMATION_RECORDED ||
			got.Preparation.Id != confirmed.PreparationId || !proto.Equal(got.Frozen, frozen) {
			t.Fatalf("projection = %#v, %v", got, err)
		}
	})
	t.Run("dispute links confirmed head and frozen proof", func(t *testing.T) {
		history := basOverviewHistory(root, family)
		history.Chain.LatestHead = confirmed
		history.Chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
		got, err := projectBASHistory(history, basOverviewOrgID, root)
		if err != nil || got.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_DISPUTED ||
			got.Preparation.Id != confirmed.PreparationId || !proto.Equal(got.Frozen, frozen) {
			t.Fatalf("projection = %#v, %v", got, err)
		}
	})
	t.Run("correction projects the active attempt lifecycle state", func(t *testing.T) {
		cases := []struct {
			name       string
			state      tammyv1.BasPreparationState
			frozen     *tammyv1.SourceRef
			wantStatus tammyv1.BasAttentionStatus
		}{
			{name: "collecting", state: tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
				wantStatus: tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED},
			{name: "review-ready", state: tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY,
				wantStatus: tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_REVIEW_READY},
			{name: "frozen", state: tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF,
				frozen:     basOverviewRef("bas_frozen", "018f0000-0000-7000-8000-00000000a00a", 1, 10),
				wantStatus: tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_FROZEN_FOR_MANUAL_HANDOFF},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				history := basOverviewHistory(root, family)
				history.Chain.LatestHead = confirmed
				active := basOverviewPreparation("018f0000-0000-7000-8000-00000000a008", family, test.state, test.frozen)
				history.Chain.Branch = &baspreparation.Branch{Reference: basOverviewRef("bas_branch", basOverviewBranchID, 1, 8),
					Version: 1, RootHead: confirmed, ActiveAttempt: active,
					Resolution: basOverviewRef("bas_dispute_resolution", "018f0000-0000-7000-8000-00000000a009", 1, 9)}
				history.Chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED
				got, err := projectBASHistory(history, basOverviewOrgID, root)
				expectedPreparation := basOverviewRef("bas_preparation", active.PreparationId, active.Version, 9)
				if err != nil || got.Status != test.wantStatus || !proto.Equal(got.Preparation, expectedPreparation) ||
					got.Family == nil || !proto.Equal(got.Family, family) || !proto.Equal(got.Frozen, test.frozen) {
					t.Fatalf("projection = %#v, %v; want status %v, preparation %v, frozen %v",
						got, err, test.wantStatus, expectedPreparation, test.frozen)
				}
			})
		}
	})
	t.Run("empty correction branch retains the confirmed head as action-needed", func(t *testing.T) {
		history := basOverviewHistory(root, family)
		history.Chain.LatestHead = confirmed
		history.Chain.Branch = &baspreparation.Branch{Reference: basOverviewRef("bas_branch", basOverviewBranchID, 1, 8),
			Version: 1, RootHead: confirmed,
			Resolution: basOverviewRef("bas_dispute_resolution", "018f0000-0000-7000-8000-00000000a009", 1, 9)}
		history.Chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED
		got, err := projectBASHistory(history, basOverviewOrgID, root)
		expectedHead := basOverviewRef("bas_preparation", confirmed.PreparationId, confirmed.Version, 9)
		if err != nil || got.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED ||
			!proto.Equal(got.Preparation, expectedHead) || got.Family == nil ||
			!proto.Equal(got.Family, family) || got.Frozen != nil {
			t.Fatalf("projection = %#v, %v; want action-needed confirmed head %v without an old frozen link",
				got, err, expectedHead)
		}
	})
	t.Run("missing disputed head fails closed", func(t *testing.T) {
		history := basOverviewHistory(root, family)
		history.Chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
		if _, err := projectBASHistory(history, basOverviewOrgID, root); err == nil {
			t.Fatal("missing exact disputed head accepted")
		}
	})
}

func projectBASAttentionInOwnReadTx(t *testing.T, server documentPageServer, projector *basOverviewProjector,
	period *tammyv1.ReportingPeriod) overview.BASAttentionProjection {
	t.Helper()
	ctx := context.Background()
	tx, err := server.module.db.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got, err := projector.ProjectBASAttention(ctx, tx, server.organisationID, period)
	if err != nil {
		t.Fatalf("ProjectBASAttention() = %v", err)
	}
	return got
}

func TestBASOverviewUsesExactCurrentPeriodAndWriterValidatedFamily(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	profile, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(&tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-00000000a101"},
		OrganisationId: server.organisationID, Profile: localBASProfileInput("Exact period overview projection fixture"),
	}, server.capability))
	if err != nil || profile.Msg.Profile == nil {
		t.Fatalf("set profile: %v, %v", profile, err)
	}
	profileRef := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: profile.Msg.Profile.ProfileId,
		Revision: profile.Msg.Profile.Revision, ContentHash: bytes.Clone(profile.Msg.Profile.ContentHash)}
	start := &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1}
	end := &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}
	period, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(&tammyv1.ConfirmBasPeriodRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-00000000a102"},
		OrganisationId: server.organisationID,
		Period: &tammyv1.BasPeriodConfirmationInput{Period: &tammyv1.BasCivilDateRange{StartDate: start, EndDate: end},
			Window: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP, Profile: profileRef,
			NominalDueDate:     &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 28},
			DueDateRuleVersion: baspreparation.DueDateRuleVersion, DueDateRuleHash: baspreparation.DueDateRuleFingerprint(),
			ConfirmedDueDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 28},
			DueDateKind:      tammyv1.BasDueDateKind_BAS_DUE_DATE_KIND_NORMAL_SUGGESTION,
			SupportState:     tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED,
			Reason:           "Confirmed actual quarterly scope for exact overview selection"},
	}, server.capability))
	if err != nil || period.Msg.Period == nil {
		t.Fatalf("confirm period: %v, %v", period, err)
	}
	projector, err := newBASOverviewProjector(server.activation.WorkspaceID, server.activation.InstallationKey)
	if err != nil {
		t.Fatal(err)
	}
	reportingPeriod := &tammyv1.ReportingPeriod{StartDate: proto.Clone(start).(*tammyv1.CivilDate), EndDate: proto.Clone(end).(*tammyv1.CivilDate)}
	beforeFamily := projectBASAttentionInOwnReadTx(t, server, projector, reportingPeriod)
	if beforeFamily.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_NOT_CREATED ||
		beforeFamily.Preparation != nil || beforeFamily.Family != nil || beforeFamily.Frozen != nil {
		t.Fatalf("period without retained family = %#v", beforeFamily)
	}
	noExactMatch := projectBASAttentionInOwnReadTx(t, server, projector, &tammyv1.ReportingPeriod{
		StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1}, EndDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 31},
	})
	if noExactMatch.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED ||
		noExactMatch.Preparation != nil || noExactMatch.Family != nil {
		t.Fatalf("overlapping dates were treated as an exact confirmed period: %#v", noExactMatch)
	}
	absent := &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
	regenerated, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(&tammyv1.RegenerateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-00000000a103"},
		OrganisationId: server.organisationID, FamilyExpectation: absent, PreparationExpectation: absent,
		Profile: profileRef, Period: &tammyv1.SourceRef{Type: "bas_period", Id: period.Msg.Period.PeriodId,
			Revision: period.Msg.Period.Version, ContentHash: bytes.Clone(period.Msg.Period.ContentHash)},
	}, server.capability))
	if err != nil || regenerated.Msg.Preparation == nil {
		t.Fatalf("regenerate family root: %v, %v", regenerated, err)
	}
	afterFamily := projectBASAttentionInOwnReadTx(t, server, projector, reportingPeriod)
	if afterFamily.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED ||
		afterFamily.Preparation == nil || afterFamily.Preparation.Id != regenerated.Msg.Preparation.PreparationId ||
		afterFamily.Preparation.Revision != regenerated.Msg.Preparation.Version || !bytes.Equal(afterFamily.Preparation.ContentHash, regenerated.Msg.Preparation.ContentHash) ||
		afterFamily.Family == nil || afterFamily.Family.Id != regenerated.Msg.Preparation.Family.Id || afterFamily.Frozen != nil {
		t.Fatalf("writer-backed family projection = %#v; preparation=%v", afterFamily, regenerated.Msg.Preparation)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	tx, err := server.module.db.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := projector.ProjectBASAttention(cancelled, tx, server.organisationID, reportingPeriod); err == nil {
		t.Fatal("cancelled BAS projection returned a partial status")
	}
}
