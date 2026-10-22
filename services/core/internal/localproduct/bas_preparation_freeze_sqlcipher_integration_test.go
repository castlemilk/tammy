//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"reflect"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/identity"
	"google.golang.org/protobuf/proto"
)

const basFreezeApprovalPassword = "administrator-password-long-enough"

func TestFreezeBASForManualHandoffThroughRealTLSRequiresLodgerAndReplaysOnce(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx := context.Background()
	preparation, calculation, validation := prepareBASFreezeReadyNilStatement(t, server, 0x9700)
	chain := readBASFreezeChain(t, server, server.authentication, preparation)
	marker := assertBASFreezeApprovalFactor(t, server, server.authentication, basFreezeApprovalPassword, 0x9710)

	// A valid, purpose-bound TOTP does not make an administrator-only actor a
	// lodger. The denied command must leave both the assertion and BAS state
	// untouched so the same authorization-bound assertion may be retried only
	// after the role correction.
	denied := makeBASFreezeRequest(server, server.authentication, preparation, calculation, validation,
		chain, marker, 0x9711)
	beforeDeniedAudit, beforeDeniedIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	beforeDenied := basReviewCommandCounts(t, server, preparation.PreparationId)
	if _, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(denied, server.capability)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("workspace-admin-only freeze = %v; want permission denied", err)
	}
	if basFreezeAssertionConsumed(t, server, marker.AssertionId) {
		t.Fatal("role-denied freeze consumed its fresh TOTP assertion")
	}
	assertBASPreparationCounts(t, beforeDenied, basReviewCommandCounts(t, server, preparation.PreparationId))
	assertBASApprovalCountsUnchanged(t, beforeDeniedAudit, beforeDeniedIdempotency, server)

	assigned := addBusinessLodgerRolePreservingExistingRoles(t, server, server.authentication, 0x9712)
	if !basFreezeHasRole(assigned.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) ||
		!basFreezeHasRole(assigned.Roles, tammyv1.Role_ROLE_BUSINESS_LODGER) {
		t.Fatalf("AssignRoles did not preserve admin while adding business_lodger: %v", assigned.Roles)
	}
	// AssignRoles changed only the identity role set. The request must still
	// bind the exact current calculation, validation, and chain observed above.
	if !proto.Equal(readBASFreezeChain(t, server, server.authentication, preparation), chain) {
		t.Fatal("role assignment unexpectedly changed the BAS approval chain")
	}

	request := makeBASFreezeRequest(server, server.authentication, preparation, calculation, validation,
		chain, marker, 0x9713)
	beforeAudit, beforeIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	beforeState := basReviewCommandCounts(t, server, preparation.PreparationId)
	frozen, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(request, server.capability))
	if err != nil || frozen.Msg.Preparation == nil || frozen.Msg.FrozenBas == nil {
		t.Fatalf("freeze reviewed nil statement = %v, %v", frozen, err)
	}
	assertBASFrozenOutput(t, frozen.Msg, preparation, calculation, validation, marker, server.authentication.ActorUserId)
	if frozen.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF {
		t.Fatalf("freeze retained preparation state = %v", frozen.Msg.Preparation.State)
	}
	assertBASFreezeCountsIncrementedOnce(t, server, beforeAudit, beforeIdempotency)
	if !basFreezeAssertionConsumed(t, server, marker.AssertionId) {
		t.Fatal("successful freeze did not consume its exact fresh TOTP assertion")
	}
	if reflect.DeepEqual(beforeState, basReviewCommandCounts(t, server, preparation.PreparationId)) {
		t.Fatal("successful freeze did not append the frozen preparation version")
	}

	beforeReplayAudit, beforeReplayIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	beforeReplayState := basReviewCommandCounts(t, server, preparation.PreparationId)
	replayed, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(request, server.capability))
	if err != nil || !proto.Equal(frozen.Msg, replayed.Msg) {
		t.Fatalf("lost-response freeze replay = %v, %v", replayed, err)
	}
	assertBASApprovalCountsUnchanged(t, beforeReplayAudit, beforeReplayIdempotency, server)
	assertBASPreparationCounts(t, beforeReplayState, basReviewCommandCounts(t, server, preparation.PreparationId))
	if !basFreezeAssertionConsumed(t, server, marker.AssertionId) {
		t.Fatal("idempotent replay changed the consumed assertion state")
	}
}

func TestFreezeBASRejectsChangedSourceWithoutConsumingFreshApprovalFactor(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx := context.Background()
	preparation, calculation, validation := prepareBASFreezeReadyNilStatement(t, server, 0x9800)
	assigned := addBusinessLodgerRolePreservingExistingRoles(t, server, server.authentication, 0x980f)
	if !basFreezeHasRole(assigned.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) ||
		!basFreezeHasRole(assigned.Roles, tammyv1.Role_ROLE_BUSINESS_LODGER) {
		t.Fatalf("AssignRoles did not preserve admin while adding business_lodger: %v", assigned.Roles)
	}
	chain := readBASFreezeChain(t, server, server.authentication, preparation)
	marker := assertBASFreezeApprovalFactor(t, server, server.authentication, basFreezeApprovalPassword, 0x9810)

	// Correct the current profile at the same effective interval. The reviewed
	// calculation and validation remain immutable, but their source is stale.
	corrected := localBASSupportedGSTProfileInput()
	corrected.Gst.GstBasis = tammyv1.BasGstBasis_BAS_GST_BASIS_CASH
	corrected.Reason = "Corrected current profile after BAS review"
	profile, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(&tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(0x9811)},
		OrganisationId: server.organisationID, ExpectedVersion: 1, Profile: corrected,
	}, server.capability))
	if err != nil || profile.Msg.Profile == nil || profile.Msg.Profile.Revision != 2 {
		t.Fatalf("same-interval current-profile correction = %v, %v", profile, err)
	}

	request := makeBASFreezeRequest(server, server.authentication, preparation, calculation, validation,
		chain, marker, 0x9812)
	beforeAudit, beforeIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	beforeState := basReviewCommandCounts(t, server, preparation.PreparationId)
	if _, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(request, server.capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("freeze with changed current source = %v; want failed precondition", err)
	}
	if basFreezeAssertionConsumed(t, server, marker.AssertionId) {
		t.Fatal("stale-source rejection consumed its fresh TOTP assertion")
	}
	assertBASApprovalCountsUnchanged(t, beforeAudit, beforeIdempotency, server)
	assertBASPreparationCounts(t, beforeState, basReviewCommandCounts(t, server, preparation.PreparationId))
	read := readBASFreezePreparation(t, server, server.authentication, preparation)
	if read.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		!proto.Equal(read.Preparation.CurrentCalculation, preparation.CurrentCalculation) ||
		!proto.Equal(read.Preparation.CurrentValidation, preparation.CurrentValidation) {
		t.Fatalf("stale-source freeze changed the reviewed preparation: %v", read.Preparation)
	}
}

func TestFreezeBASFinalAuditFailureRollsBackAndAllowsFreshFactorRetry(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx := context.Background()
	preparation, calculation, validation := prepareBASFreezeReadyNilStatement(t, server, 0x9900)
	assigned := addBusinessLodgerRolePreservingExistingRoles(t, server, server.authentication, 0x990f)
	if !basFreezeHasRole(assigned.Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) ||
		!basFreezeHasRole(assigned.Roles, tammyv1.Role_ROLE_BUSINESS_LODGER) {
		t.Fatalf("AssignRoles did not preserve admin while adding business_lodger: %v", assigned.Roles)
	}
	chain := readBASFreezeChain(t, server, server.authentication, preparation)
	marker := assertBASFreezeApprovalFactor(t, server, server.authentication, basFreezeApprovalPassword, 0x9910)
	request := makeBASFreezeRequest(server, server.authentication, preparation, calculation, validation,
		chain, marker, 0x9911)

	beforeAudit, beforeIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	beforeState := basReviewCommandCounts(t, server, preparation.PreparationId)
	if _, err := server.module.db.ExecContext(ctx, `CREATE TRIGGER reject_bas_freeze_test_audit
		BEFORE INSERT ON audit_events_v1 BEGIN SELECT RAISE(ABORT, 'injected final BAS freeze audit failure'); END`); err != nil {
		t.Fatalf("install final audit failure trigger: %v", err)
	}
	triggerInstalled := true
	t.Cleanup(func() {
		if triggerInstalled {
			_, _ = server.module.db.ExecContext(context.Background(), `DROP TRIGGER reject_bas_freeze_test_audit`)
		}
	})
	if _, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(request, server.capability)); connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("freeze with injected final audit failure = %v; want internal", err)
	}
	if _, err := server.module.db.ExecContext(ctx, `DROP TRIGGER reject_bas_freeze_test_audit`); err != nil {
		t.Fatalf("remove final audit failure trigger: %v", err)
	}
	triggerInstalled = false

	if basFreezeAssertionConsumed(t, server, marker.AssertionId) {
		t.Fatal("final audit rollback committed fresh-factor consumption")
	}
	assertBASApprovalCountsUnchanged(t, beforeAudit, beforeIdempotency, server)
	assertBASPreparationCounts(t, beforeState, basReviewCommandCounts(t, server, preparation.PreparationId))
	read := readBASFreezePreparation(t, server, server.authentication, preparation)
	if read.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY || read.Preparation.FrozenBas != nil {
		t.Fatalf("final audit rollback left a partial freeze: %v", read.Preparation)
	}

	frozen, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(request, server.capability))
	if err != nil || frozen.Msg.FrozenBas == nil {
		t.Fatalf("same request and factor after audit rollback = %v, %v", frozen, err)
	}
	assertBASFrozenOutput(t, frozen.Msg, preparation, calculation, validation, marker, server.authentication.ActorUserId)
	assertBASFreezeCountsIncrementedOnce(t, server, beforeAudit, beforeIdempotency)
	if !basFreezeAssertionConsumed(t, server, marker.AssertionId) {
		t.Fatal("successful retry did not consume the rolled-back fresh factor")
	}
	beforeReplayAudit, beforeReplayIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	replayed, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(request, server.capability))
	if err != nil || !proto.Equal(frozen.Msg, replayed.Msg) {
		t.Fatalf("replay after audit rollback/retry = %v, %v", replayed, err)
	}
	assertBASApprovalCountsUnchanged(t, beforeReplayAudit, beforeReplayIdempotency, server)
}

func prepareBASFreezeReadyNilStatement(t *testing.T, server documentPageServer, keyBase uint64) (
	*tammyv1.BasPreparation, *tammyv1.BasCalculation, *tammyv1.BasValidationSnapshot) {
	t.Helper()
	preparation, calculation := createSupportedNilStatementPreparation(t, server, keyBase)
	validated, err := server.basPreparation.ValidateBasPreparation(context.Background(), brandingRequest(
		validateBASReviewRequest(server, preparation, calculation, keyBase+0x10), server.capability))
	if err != nil || validated.Msg.Preparation == nil || validated.Msg.Validation == nil ||
		validated.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		len(validated.Msg.Validation.Findings) != 0 {
		t.Fatalf("validate supported nil statement before freeze = %v, %v", validated, err)
	}
	return validated.Msg.Preparation, calculation, validated.Msg.Validation
}

func readBASFreezePreparation(t *testing.T, server documentPageServer, actor *tammyv1.AuthenticationContext,
	preparation *tammyv1.BasPreparation) *tammyv1.GetBasPreparationResponse {
	t.Helper()
	read, err := server.basPreparation.GetBasPreparation(context.Background(), brandingRequest(&tammyv1.GetBasPreparationRequest{
		Authentication: actor, OrganisationId: server.organisationID,
		PreparationId: preparation.PreparationId, PreparationHash: bytes.Clone(preparation.ContentHash),
	}, server.capability))
	if err != nil || read.Msg.Preparation == nil || read.Msg.CurrentChain == nil {
		t.Fatalf("read exact BAS preparation and current chain = %v, %v", read, err)
	}
	return read.Msg
}

func readBASFreezeChain(t *testing.T, server documentPageServer, actor *tammyv1.AuthenticationContext,
	preparation *tammyv1.BasPreparation) *tammyv1.BasChainExpectation {
	t.Helper()
	read := readBASFreezePreparation(t, server, actor, preparation)
	chain := read.CurrentChain
	if chain.Family == nil || preparation.Family == nil || !proto.Equal(chain.Family, preparation.Family) ||
		chain.ExpectedFamilyVersion == 0 || chain.RootPreparation == nil ||
		chain.RootPreparation.Type != "bas_preparation" || chain.RootPreparation.Revision != 1 ||
		!basFreezeExplicitAbsence(chain.LatestHead) || !basFreezeExplicitAbsence(chain.Branch) ||
		!basFreezeExplicitAbsence(chain.ActiveAttempt) {
		t.Fatalf("root freeze must bind immutable root v1 and explicit no-head/no-branch/no-active-attempt family state: %v", chain)
	}
	return proto.Clone(chain).(*tammyv1.BasChainExpectation)
}

func basFreezeExplicitAbsence(value *tammyv1.BasExpectedIdentity) bool {
	return value != nil && value.GetAbsent() && value.ExpectedVersion == nil
}

func makeBASFreezeRequest(server documentPageServer, actor *tammyv1.AuthenticationContext,
	preparation *tammyv1.BasPreparation, calculation *tammyv1.BasCalculation,
	validation *tammyv1.BasValidationSnapshot, chain *tammyv1.BasChainExpectation,
	marker *tammyv1.FreshFactorContext, key uint64) *tammyv1.FreezeBasForManualHandoffRequest {
	return &tammyv1.FreezeBasForManualHandoffRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			FreshFactor: proto.Clone(marker).(*tammyv1.FreshFactorContext), IdempotencyKey: testBASCommandKey(key)},
		OrganisationId: server.organisationID, ExpectedVersion: preparation.Version,
		Chain: proto.Clone(chain).(*tammyv1.BasChainExpectation), PreparationId: preparation.PreparationId,
		PreparationHash: bytes.Clone(preparation.ContentHash), CalculationId: calculation.CalculationId,
		CalculationHash: bytes.Clone(calculation.ContentHash), ValidationId: validation.ValidationId,
		ValidationHash: bytes.Clone(validation.ContentHash), ApprovalReason: "Reviewed exact supported BAS statement for manual handoff",
	}
}

func assertBASFreezeApprovalFactor(t *testing.T, server documentPageServer,
	actor *tammyv1.AuthenticationContext, password string, keyBase uint64) *tammyv1.FreshFactorContext {
	t.Helper()
	ctx := context.Background()
	enrolled, err := server.identity.EnrolTOTP(ctx, brandingRequest(&tammyv1.EnrolTOTPRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(keyBase)},
		CurrentPassword: &tammyv1.SecretInput{Utf8: []byte(password)},
	}, server.capability))
	if err != nil || enrolled.Msg.Factor == nil || enrolled.Msg.ProvisioningSecret == nil {
		t.Fatalf("enrol real BAS freeze TOTP factor = %v, %v", enrolled, err)
	}
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(string(enrolled.Msg.ProvisioningSecret.Utf8))
	if err != nil {
		t.Fatalf("decode real TOTP enrollment secret: %v", err)
	}
	defer clear(secretBytes)
	confirmAt := time.Now().UTC()
	confirmCounter := confirmAt.Unix() / 30
	confirmed, err := server.identity.ConfirmTOTP(ctx, brandingRequest(&tammyv1.ConfirmTOTPRequest{
		Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext), FactorId: enrolled.Msg.Factor.Id,
		Code: &tammyv1.TotpCodeInput{Value: identity.TOTPCode(secretBytes, confirmAt)},
	}, server.capability))
	if err != nil || confirmed.Msg.Factor.GetState() != tammyv1.FactorState_FACTOR_STATE_ENABLED {
		t.Fatalf("confirm real BAS freeze TOTP factor = %v, %v", confirmed, err)
	}
	for time.Now().UTC().Unix()/30 <= confirmCounter {
		time.Sleep(100 * time.Millisecond)
	}
	asserted, err := server.identity.AssertTOTP(ctx, brandingRequest(&tammyv1.AssertTOTPRequest{
		Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
		Code:           &tammyv1.TotpCodeInput{Value: identity.TOTPCode(secretBytes, time.Now().UTC())},
		Purpose:        "bas_freeze_manual_handoff",
	}, server.capability))
	if err != nil || asserted.Msg.FreshFactor == nil || asserted.Msg.FreshFactor.Purpose != "bas_freeze_manual_handoff" {
		t.Fatalf("assert real purpose-bound BAS freeze TOTP factor = %v, %v", asserted, err)
	}
	return proto.Clone(asserted.Msg.FreshFactor).(*tammyv1.FreshFactorContext)
}

func addBusinessLodgerRolePreservingExistingRoles(t *testing.T, server documentPageServer,
	actor *tammyv1.AuthenticationContext, key uint64) *tammyv1.User {
	t.Helper()
	ctx := context.Background()
	current, err := server.identity.GetCurrentUser(ctx, brandingRequest(&tammyv1.GetCurrentUserRequest{
		Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
	}, server.capability))
	if err != nil || current.Msg.User == nil || current.Msg.User.Id != actor.ActorUserId {
		t.Fatalf("read current user before AssignRoles: %v, %v", current, err)
	}
	roles := append([]tammyv1.Role(nil), current.Msg.User.Roles...)
	if !basFreezeHasRole(roles, tammyv1.Role_ROLE_BUSINESS_LODGER) {
		roles = append(roles, tammyv1.Role_ROLE_BUSINESS_LODGER)
	}
	assigned, err := server.identity.AssignRoles(ctx, brandingRequest(&tammyv1.AssignRolesRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(key)},
		UserId: current.Msg.User.Id, ExpectedVersion: current.Msg.User.Version, Roles: roles,
	}, server.capability))
	if err != nil || assigned.Msg.User == nil {
		t.Fatalf("AssignRoles add business_lodger while retaining existing roles: %v, %v", assigned, err)
	}
	return assigned.Msg.User
}

func basFreezeHasRole(roles []tammyv1.Role, want tammyv1.Role) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}
	return false
}

func assertBASFrozenOutput(t *testing.T, response *tammyv1.FreezeBasForManualHandoffResponse,
	preparation *tammyv1.BasPreparation, calculation *tammyv1.BasCalculation,
	validation *tammyv1.BasValidationSnapshot, marker *tammyv1.FreshFactorContext, actorUserID string) {
	t.Helper()
	frozen := response.GetFrozenBas()
	if frozen == nil || frozen.Calculation == nil || frozen.Validation == nil || frozen.Preparation == nil ||
		frozen.ApprovalActorUserId == "" || frozen.ApprovalActorUserId != actorUserID ||
		frozen.FreshFactorAssertionId != marker.AssertionId || len(frozen.ApprovedSnapshotHash) != sha256.Size ||
		response.Preparation == nil || response.Preparation.Version != preparation.Version+1 ||
		response.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF ||
		response.Preparation.FrozenBas == nil || response.Preparation.FrozenBas.Id != frozen.FrozenId ||
		frozen.Calculation.Id != calculation.CalculationId || !bytes.Equal(frozen.Calculation.ContentHash, calculation.ContentHash) ||
		frozen.Validation.Id != validation.ValidationId || !bytes.Equal(frozen.Validation.ContentHash, validation.ContentHash) ||
		frozen.Preparation.Id != preparation.PreparationId || !bytes.Equal(frozen.Preparation.ContentHash, preparation.ContentHash) ||
		!proto.Equal(frozen.CalculationSnapshot, calculation) || len(frozen.Acknowledgements) != 0 {
		t.Fatalf("frozen BAS does not bind exact reviewed calculation/validation/preparation and fresh factor: %v", frozen)
	}
}

func basFreezeAssertionConsumed(t *testing.T, server documentPageServer, assertionID string) bool {
	t.Helper()
	var consumed int
	if err := server.module.db.QueryRowContext(context.Background(),
		`SELECT consumed FROM factor_assertions WHERE id = ?`, assertionID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	return consumed == 1
}

func assertBASApprovalCountsUnchanged(t *testing.T, wantAudit, wantIdempotency int, server documentPageServer) {
	t.Helper()
	gotAudit, gotIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	if gotAudit != wantAudit || gotIdempotency != wantIdempotency {
		t.Fatalf("BAS approval side effects changed unexpectedly: audit %d→%d idempotency %d→%d",
			wantAudit, gotAudit, wantIdempotency, gotIdempotency)
	}
}

func assertBASFreezeCountsIncrementedOnce(t *testing.T, server documentPageServer, beforeAudit, beforeIdempotency int) {
	t.Helper()
	afterAudit, afterIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	if afterAudit != beforeAudit+1 || afterIdempotency != beforeIdempotency+1 {
		t.Fatalf("freeze should append exactly one audit and idempotency result; audit %d→%d idempotency %d→%d",
			beforeAudit, afterAudit, beforeIdempotency, afterIdempotency)
	}
}

func basApprovalAuditAndIdempotencyCounts(t *testing.T, server documentPageServer) (int, int) {
	t.Helper()
	var auditCount, idempotencyCount int
	if err := server.module.db.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_events_v1`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := server.module.db.QueryRowContext(context.Background(), `SELECT count(*) FROM command_idempotency_v1`).Scan(&idempotencyCount); err != nil {
		t.Fatal(err)
	}
	return auditCount, idempotencyCount
}
