//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateSupportedNilBASStatementThroughRealTLSAndRejectsProfileDrift(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx := context.Background()
	preparation, calculation := createSupportedNilStatementPreparation(t, server, 0x9400)

	if calculation.Result.Outcome != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE || len(calculation.Coverage) != 0 {
		t.Fatalf("nil statement was not a complete empty source capture: outcome=%v coverage=%v findings=%v", calculation.Result.Outcome, calculation.Coverage, calculation.Findings)
	}
	var foundG1 bool
	for _, label := range calculation.Result.Labels {
		if label.Label == tammyv1.BasFormLabel_BAS_FORM_LABEL_G1 {
			foundG1 = true
			if label.State != tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO || label.GetExactCentsNumerator() != "0" {
				t.Fatalf("nil statement G1 = %v; want an exact known zero", label)
			}
		}
	}
	if !foundG1 {
		t.Fatal("complete nil statement omitted G1")
	}

	request := validateBASReviewRequest(server, preparation, calculation, 0x9410)
	validated, err := server.basPreparation.ValidateBasPreparation(ctx, brandingRequest(request, server.capability))
	if err != nil || validated.Msg.Preparation == nil || validated.Msg.Validation == nil ||
		validated.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		len(validated.Msg.Validation.Findings) != 0 || !bytes.Equal(validated.Msg.Validation.SourceSnapshotHash, calculation.SourceSnapshotHash) {
		t.Fatalf("fully supported nil-statement validation = %v, %v", validated, err)
	}

	// A real profile correction changes the source owner resolved by a fresh
	// capture while leaving the retained calculation and validation immutable.
	correctedProfile := localBASSupportedGSTProfileInput()
	correctedProfile.Reason = "Corrected same-interval profile source for stale-capture rejection"
	profile, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(&tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(0x9411)},
		OrganisationId: server.organisationID, ExpectedVersion: 1, Profile: correctedProfile,
	}, server.capability))
	if err != nil || profile.Msg.Profile == nil || profile.Msg.Profile.Revision != 2 {
		t.Fatalf("same-interval profile correction = %v, %v", profile, err)
	}
	before := basReviewCommandCounts(t, server, validated.Msg.Preparation.PreparationId)
	stale := validateBASReviewRequest(server, validated.Msg.Preparation, calculation, 0x9412)
	if _, err := server.basPreparation.ValidateBasPreparation(ctx, brandingRequest(stale, server.capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("validation against changed current profile = %v; want failed precondition", err)
	}
	assertBASPreparationCounts(t, before, basReviewCommandCounts(t, server, validated.Msg.Preparation.PreparationId))
}

func TestAcknowledgeBASWarningThroughRealTLSRequiresExactHashAndRollsBackLateAudit(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx := context.Background()
	preparation, calculation := createSupportedNilStatementPreparation(t, server, 0x9500)

	// The current production calculation/source pipeline does not emit WARNING
	// findings. Seed one valid immutable calculation revision through the typed
	// repositories so this acceptance test can exercise the real warning command.
	preparation, calculation, warning := appendBASWarningCalculationFixture(t, server, preparation, calculation)
	request := validateBASReviewRequest(server, preparation, calculation, 0x9510)
	validated, err := server.basPreparation.ValidateBasPreparation(ctx, brandingRequest(request, server.capability))
	if err != nil || validated.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		len(validated.Msg.Validation.Findings) != 1 || !proto.Equal(validated.Msg.Validation.Findings[0], warning) {
		t.Fatalf("warning-bearing validation = %v, %v", validated, err)
	}

	findingHash := basTestFindingHash(t, warning)
	ackRequest := acknowledgeBASWarningRequest(server, validated.Msg.Preparation, validated.Msg.Validation,
		warning.FindingId, findingHash, 0x9511)

	wrongHash := proto.Clone(ackRequest).(*tammyv1.AcknowledgeBasWarningsRequest)
	wrongHash.CommandContext.IdempotencyKey = testBASCommandKey(0x9512)
	wrongHash.Warnings[0].FindingHash[0] ^= 0x80
	beforeWrongHash := basReviewCommandCounts(t, server, validated.Msg.Preparation.PreparationId)
	if _, err := server.basPreparation.AcknowledgeBasWarnings(ctx, brandingRequest(wrongHash, server.capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("acknowledgement with a changed finding hash = %v; want failed precondition", err)
	}
	assertBASPreparationCounts(t, beforeWrongHash, basReviewCommandCounts(t, server, validated.Msg.Preparation.PreparationId))

	beforeAuditFailure := basReviewCommandCounts(t, server, validated.Msg.Preparation.PreparationId)
	if _, err := server.module.db.ExecContext(ctx, `CREATE TRIGGER fail_bas_preparation_audit BEFORE INSERT ON audit_events_v1 BEGIN SELECT RAISE(ABORT,'injected BAS acknowledgement audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := server.basPreparation.AcknowledgeBasWarnings(ctx, brandingRequest(ackRequest, server.capability)); connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("real warning acknowledgement audit failure = %v; want internal", err)
	}
	if _, err := server.module.db.ExecContext(ctx, `DROP TRIGGER fail_bas_preparation_audit`); err != nil {
		t.Fatal(err)
	}
	assertBASPreparationCounts(t, beforeAuditFailure, basReviewCommandCounts(t, server, validated.Msg.Preparation.PreparationId))

	acknowledged, err := server.basPreparation.AcknowledgeBasWarnings(ctx, brandingRequest(ackRequest, server.capability))
	if err != nil || acknowledged.Msg.Preparation == nil || len(acknowledged.Msg.Acknowledgements) != 1 {
		t.Fatalf("exact warning acknowledgement = %v, %v", acknowledged, err)
	}
	ack := acknowledged.Msg.Acknowledgements[0]
	if ack.Finding == nil || ack.Finding.Id != warning.FindingId || !bytes.Equal(ack.Finding.ContentHash, findingHash) ||
		ack.Validation == nil || ack.Validation.Id != validated.Msg.Validation.ValidationId ||
		!bytes.Equal(ack.SourceSnapshotHash, validated.Msg.Validation.SourceSnapshotHash) ||
		ack.ActorUserId != server.authentication.ActorUserId {
		t.Fatalf("acknowledgement lost exact finding, validation, source, or actor binding: %v", ack)
	}
	beforeReplay := basReviewCommandCounts(t, server, validated.Msg.Preparation.PreparationId)
	replay, err := server.basPreparation.AcknowledgeBasWarnings(ctx, brandingRequest(ackRequest, server.capability))
	if err != nil || !proto.Equal(acknowledged.Msg, replay.Msg) {
		t.Fatalf("exact warning acknowledgement replay = %v, %v", replay, err)
	}
	assertBASPreparationCounts(t, beforeReplay, basReviewCommandCounts(t, server, validated.Msg.Preparation.PreparationId))
}

func createSupportedNilStatementPreparation(t *testing.T, server documentPageServer, keyBase uint64) (*tammyv1.BasPreparation, *tammyv1.BasCalculation) {
	t.Helper()
	ctx := context.Background()
	profileInput := localBASSupportedGSTProfileInput()
	profileInput.Gst.GstBasis = tammyv1.BasGstBasis_BAS_GST_BASIS_CASH
	profile, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(&tammyv1.SetBasObligationsProfileRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(keyBase + 1)},
		OrganisationId: server.organisationID, Profile: profileInput,
	}, server.capability))
	if err != nil || profile.Msg.Profile == nil {
		t.Fatalf("set supported nil-statement profile = %v, %v", profile, err)
	}
	profileRef := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: profile.Msg.Profile.ProfileId,
		Revision: profile.Msg.Profile.Revision, ContentHash: bytes.Clone(profile.Msg.Profile.ContentHash)}
	period, err := server.basPreparation.ConfirmBasPeriod(ctx, brandingRequest(&tammyv1.ConfirmBasPeriodRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(keyBase + 2)},
		OrganisationId: server.organisationID,
		Period: &tammyv1.BasPeriodConfirmationInput{
			Period: &tammyv1.BasCivilDateRange{
				StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
				EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
			},
			Window: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP, Profile: profileRef,
			NominalDueDate:     &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 28},
			DueDateRuleVersion: baspreparation.DueDateRuleVersion, DueDateRuleHash: baspreparation.DueDateRuleFingerprint(),
			ConfirmedDueDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 28},
			DueDateKind:      tammyv1.BasDueDateKind_BAS_DUE_DATE_KIND_NORMAL_SUGGESTION,
			DueDateEvidence:  []*tammyv1.SourceRef{{Type: "ato_statement", Id: testBASCommandKey(keyBase + 6), Revision: 1, ContentHash: bytes.Repeat([]byte{94}, 32)}},
			SupportState:     tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED,
			Reason:           "The actual quarterly nil statement is a supported complete calculation",
		},
	}, server.capability))
	if err != nil || period.Msg.Period == nil {
		t.Fatalf("confirm supported nil-statement period = %v, %v", period, err)
	}
	absent := &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
	regenerated, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(&tammyv1.RegenerateBasPreparationRequest{
		CommandContext:    &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(keyBase + 3)},
		OrganisationId:    server.organisationID,
		FamilyExpectation: proto.Clone(absent).(*tammyv1.BasExpectedIdentity), PreparationExpectation: proto.Clone(absent).(*tammyv1.BasExpectedIdentity),
		Profile: profileRef,
		Period: &tammyv1.SourceRef{Type: "bas_period", Id: period.Msg.Period.PeriodId,
			Revision: period.Msg.Period.Version, ContentHash: bytes.Clone(period.Msg.Period.ContentHash)},
	}, server.capability))
	if err != nil || regenerated.Msg.Preparation == nil || regenerated.Msg.Calculation == nil {
		t.Fatalf("regenerate supported nil-statement preparation = %v, %v", regenerated, err)
	}

	na := func() *tammyv1.BasAmountInput {
		return &tammyv1.BasAmountInput{State: tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE, Reason: "Reviewed as not applicable for this statement"}
	}
	prep := regenerated.Msg.Preparation
	saved, err := server.basPreparation.SaveBasPaygInputs(ctx, brandingRequest(&tammyv1.SaveBasPaygInputsRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(keyBase + 4)}, OrganisationId: server.organisationID,
		PreparationId: prep.PreparationId, ExpectedVersion: prep.Version, PreparationHash: bytes.Clone(prep.ContentHash),
		Paygw: &tammyv1.BasPaygwInputs{Category: tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_UNKNOWN, W1TotalPayments: na(), W2AmountWithheld: na(), W3OtherWithholding: na(), W4NoAbnWithholding: na(), PrefillReview: tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_NOT_REQUIRED, ReviewReason: "No PAYGW obligation applies"},
		Paygi: &tammyv1.BasPaygiInputs{Method: tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE, NoticeOperativeStatus: tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_INOPERATIVE, PriorVariation: tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_NONE, UnsupportedState: tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_NONE, T1ReviewDecision: tammyv1.BasT1ReviewDecision_BAS_T1_REVIEW_DECISION_NOT_REVIEWED},
	}, server.capability))
	if err != nil {
		t.Fatalf("save explicit not-applicable PAYG answers: %v", err)
	}
	prep = saved.Msg.Preparation
	version := prep.Version
	familyVersion := prep.Family.Revision
	exact := &tammyv1.SourceRef{Type: "bas_preparation", Id: prep.PreparationId, Revision: prep.Version, ContentHash: bytes.Clone(prep.ContentHash)}
	regenerated, err = server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(&tammyv1.RegenerateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(keyBase + 5)}, OrganisationId: server.organisationID, ExpectedVersion: version,
		FamilyExpectation:      &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Retained{Retained: proto.Clone(prep.Family).(*tammyv1.SourceRef)}, ExpectedVersion: &familyVersion},
		PreparationExpectation: &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Retained{Retained: exact}, ExpectedVersion: &version}, Profile: profileRef, Period: proto.Clone(prep.Period).(*tammyv1.SourceRef),
	}, server.capability))
	if err != nil {
		t.Fatalf("regenerate explicit complete nil statement: %v", err)
	}
	return regenerated.Msg.Preparation, regenerated.Msg.Calculation
}

func validateBASReviewRequest(server documentPageServer, preparation *tammyv1.BasPreparation,
	calculation *tammyv1.BasCalculation, keySuffix uint64) *tammyv1.ValidateBasPreparationRequest {
	return &tammyv1.ValidateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(keySuffix)},
		OrganisationId: server.organisationID, ExpectedVersion: preparation.Version,
		PreparationId: preparation.PreparationId, PreparationHash: bytes.Clone(preparation.ContentHash),
		CalculationId: calculation.CalculationId, CalculationHash: bytes.Clone(calculation.ContentHash),
	}
}

func acknowledgeBASWarningRequest(server documentPageServer, preparation *tammyv1.BasPreparation,
	validation *tammyv1.BasValidationSnapshot, findingID string, findingHash []byte, keySuffix uint64) *tammyv1.AcknowledgeBasWarningsRequest {
	return &tammyv1.AcknowledgeBasWarningsRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(keySuffix)},
		OrganisationId: server.organisationID, ExpectedVersion: preparation.Version,
		PreparationId: preparation.PreparationId, PreparationHash: bytes.Clone(preparation.ContentHash),
		ValidationId: validation.ValidationId, ValidationHash: bytes.Clone(validation.ContentHash),
		Warnings:           []*tammyv1.BasWarningAcknowledgementInput{{FindingId: findingID, FindingHash: bytes.Clone(findingHash)}},
		SourceSnapshotHash: bytes.Clone(validation.SourceSnapshotHash),
	}
}

func appendBASWarningCalculationFixture(t *testing.T, server documentPageServer, preparation *tammyv1.BasPreparation,
	calculation *tammyv1.BasCalculation) (*tammyv1.BasPreparation, *tammyv1.BasCalculation, *tammyv1.BasValidationFinding) {
	t.Helper()
	ctx := context.Background()
	codec, err := paging.NewCodec(bytes.Repeat([]byte{0x63}, 32))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := server.module.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	preparations, err := baspreparation.NewPreparationRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	calculations, err := baspreparation.NewCalculationRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	current, err := preparations.GetCurrent(ctx, server.organisationID, preparation.PreparationId)
	if err != nil || current == nil || current.SavedPaygInputs == nil || current.Preparation.CurrentCalculation == nil {
		t.Fatalf("read typed nil-statement preparation for warning fixture = %v, %v", current, err)
	}
	original, err := calculations.GetExact(ctx, server.organisationID, current.Preparation.CurrentCalculation)
	if err != nil || original == nil || original.CalculationId != calculation.CalculationId || original.Version != calculation.Version {
		t.Fatalf("read exact warning fixture calculation = %v, %v", original, err)
	}
	warning := &tammyv1.BasValidationFinding{
		FindingId:  "018f0000-0000-7000-8000-009500000001",
		Severity:   tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING,
		StableCode: "SOURCE_REVIEW_REQUIRED", SafeMessage: "Review this exact retained source warning before approval",
		Sources: []*tammyv1.SourceRef{proto.Clone(original.Profile).(*tammyv1.SourceRef)},
	}
	nextCalculation := proto.Clone(original).(*tammyv1.BasCalculation)
	nextCalculation.Version++
	nextCalculation.ContentHash = nil
	nextCalculation.Preparation = &tammyv1.SourceRef{Type: "bas_preparation", Id: current.Preparation.PreparationId,
		Revision: current.Preparation.Version, ContentHash: bytes.Clone(current.Preparation.ContentHash)}
	nextCalculation.Findings = append(nextCalculation.Findings, warning)
	calculationRef, err := calculations.Append(ctx, nextCalculation, current.SavedPaygInputs)
	if err != nil {
		t.Fatalf("append typed warning calculation fixture = %v", err)
	}
	nextCalculation.ContentHash = bytes.Clone(calculationRef.ContentHash)

	nextPreparation := proto.Clone(current.Preparation).(*tammyv1.BasPreparation)
	nextPreparation.Version++
	nextPreparation.ContentHash = nil
	nextPreparation.CurrentCalculation = calculationRef
	nextPreparation.CurrentValidation = nil
	nextPreparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
	nextPreparation.UpdatedAt = timestamppb.New(time.Now().UTC())
	preparationRef, err := preparations.Append(ctx, &baspreparation.PreparationRetainedSnapshot{
		Preparation: nextPreparation, SavedPaygInputs: proto.Clone(current.SavedPaygInputs).(*tammyv1.SourceRef),
		BranchID: current.BranchID, AttemptID: current.AttemptID,
	}, &tammyv1.SourceRef{Type: "bas_preparation", Id: current.Preparation.PreparationId,
		Revision: current.Preparation.Version, ContentHash: bytes.Clone(current.Preparation.ContentHash)})
	if err != nil {
		t.Fatalf("append typed warning preparation fixture = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit typed warning fixture = %v", err)
	}
	committed = true
	nextPreparation.ContentHash = bytes.Clone(preparationRef.ContentHash)
	return nextPreparation, nextCalculation, warning
}

func basReviewCommandCounts(t *testing.T, server documentPageServer, preparationID string) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, table := range []string{
		"bas_preparation_versions_v1", "bas_validations_v1", "bas_validation_findings_v1",
		"bas_warning_acknowledgements_v1", "audit_events_v1", "command_idempotency_v1",
	} {
		var count int64
		if err := server.module.db.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	var ownerVersions, headVersion int64
	if err := server.module.db.QueryRowContext(context.Background(), `SELECT count(*) FROM bas_preparation_versions_v1 WHERE preparation_id=?`, preparationID).Scan(&ownerVersions); err != nil {
		t.Fatal(err)
	}
	if err := server.module.db.QueryRowContext(context.Background(), `SELECT current_version FROM bas_preparation_heads_v1 WHERE preparation_id=?`, preparationID).Scan(&headVersion); err != nil {
		t.Fatal(err)
	}
	counts["owner_versions"] = ownerVersions
	counts["owner_head_version"] = headVersion
	return counts
}

func basTestFindingHash(t *testing.T, finding *tammyv1.BasValidationFinding) []byte {
	t.Helper()
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(finding)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	return bytes.Clone(digest[:])
}

func testBASCommandKey(sequence uint64) string {
	return fmt.Sprintf("018f0000-0000-7000-8000-%012x", sequence)
}
