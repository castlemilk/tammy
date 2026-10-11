//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestLifecycleRepositoryFrozenBarrierVerifiesRetainedApprovalAndResolutionBodies(t *testing.T) {
	for _, kind := range []string{"valid", "approval_body", "approval_actor", "approval_calculation", "resolution_body", "resolution_actor", "resolution_event", "resolution_claimed_at", "resolution_operation", "missing_subject", "subject_body", "subject_operation", "correction_body", "correction_reason"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			families, _ := NewFamilyStateRepository(tx, codec)
			identities, _ := NewBranchIdentityRepository(tx, codec)
			branches, _ := NewBranchStateRepository(tx, codec)
			rootPreparation, root := seedTypedCollectingPreparation(t, tx, org)
			if _, err := families.Append(ctx, org, initialTypedFamilyState(rootPreparation, root), nil); err != nil {
				t.Fatal(err)
			}
			identityRef, err := identities.Append(ctx, typedReplacementIdentity(rootPreparation, root, 9961))
			if err != nil {
				t.Fatal(err)
			}
			frozenPreparation, frozenRef := seedTypedFrozenWorkflowAttempt(t, tx, rootPreparation, identityRef.Id)
			state := &tammyv1.BasBranchState{BranchIdentity: identityRef, Version: 1,
				WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
				ActiveAttempt: retainedExpectedIdentity(preparationReference(frozenPreparation), frozenPreparation.Version), UpdatedAt: frozenPreparation.UpdatedAt}
			stateRef, err := branches.Append(ctx, org, state, nil)
			if err != nil {
				t.Fatal(err)
			}
			resolutionRef, ordinal := seedTypedWorkflowResolution(t, tx, rootPreparation.Family, frozenPreparation)
			var statements []string
			switch kind {
			case "approval_body":
				statements = []string{`DROP TRIGGER bas_frozen_bas_v1_no_update`, `UPDATE bas_frozen_bas_v1 SET frozen_proto=X'01'`}
			case "approval_actor":
				statements = []string{`DROP TRIGGER bas_frozen_bas_v1_no_update`, `UPDATE bas_frozen_bas_v1 SET approval_reason='Changed approval reason'`}
			case "approval_calculation":
				statements = []string{`DROP TRIGGER bas_frozen_bas_v1_no_update`, `UPDATE bas_frozen_bas_v1 SET calculation_snapshot_proto=X'01'`}
			case "resolution_body":
				statements = []string{`DROP TRIGGER bas_dispute_resolutions_v1_no_update`, `UPDATE bas_dispute_resolutions_v1 SET resolution_proto=X'01'`}
			case "resolution_actor":
				statements = []string{`DROP TRIGGER bas_dispute_resolutions_v1_no_update`, `UPDATE bas_dispute_resolutions_v1 SET reason='Changed resolution reason'`}
			case "resolution_event":
				statements = []string{`DROP TRIGGER bas_family_events_v1_no_update`, `UPDATE bas_family_events_v1 SET event_proto=X'01'`}
			case "resolution_claimed_at":
				statements = []string{`DROP TRIGGER bas_family_events_v1_no_update`, `UPDATE bas_family_events_v1 SET claimed_at='2026-10-01T00:00:00Z' WHERE event_kind='RESOLUTION'`}
			case "resolution_operation":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_family_events_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_family_events_v1 SET operation_key=? WHERE event_kind='RESOLUTION'`, strings.Repeat("é", 129)); err != nil {
					t.Fatal(err)
				}
			case "missing_subject":
				statements = []string{`DROP TRIGGER bas_dispute_resolution_observations_v1_no_delete`, `DELETE FROM bas_dispute_resolution_observations_v1`}
			case "subject_body":
				statements = []string{`DROP TRIGGER bas_external_observations_v1_no_update`, `UPDATE bas_external_observations_v1 SET observation_proto=X'01'`}
			case "subject_operation":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_family_events_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_family_events_v1 SET operation_key=? WHERE event_kind='OBSERVATION'`, strings.Repeat("é", 129)); err != nil {
					t.Fatal(err)
				}
			case "correction_body":
				statements = []string{`DROP TRIGGER bas_external_observation_corrections_v1_no_update`, `UPDATE bas_external_observation_corrections_v1 SET correction_proto=X'01'`}
			case "correction_reason":
				statements = []string{`DROP TRIGGER bas_external_observation_corrections_v1_no_update`, `UPDATE bas_external_observation_corrections_v1 SET reason='Changed correction reason'`}
			}
			for _, statement := range statements {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			corrective := proto.Clone(state).(*tammyv1.BasBranchState)
			corrective.Version++
			corrective.WorkflowState = tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE
			corrective.Resolution, corrective.BarrierPreparation, corrective.FrozenBarrier = resolutionRef, preparationReference(frozenPreparation), frozenRef
			correctiveRef, err := branches.Append(ctx, org, corrective, stateRef)
			if kind != "valid" {
				if correctiveRef != nil || !errors.Is(err, ErrSnapshotCorrupt) {
					t.Fatalf("corrupt %s authorized corrective workflow: %v %v", kind, correctiveRef, err)
				}
				assertBASRepositoryCount(t, tx, "bas_branch_versions_v1", 1)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := branches.GetExact(ctx, org, correctiveRef)
			if err != nil || !sameRef(got.FrozenBarrier, frozenRef) || !sameRef(got.Resolution, resolutionRef) {
				t.Fatalf("corrective approval read: %v %v", got, err)
			}
			if err := branches.verifyFamilyEvent(ctx, org, rootPreparation.Family, ordinal); err != nil {
				t.Fatalf("retained resolution event: %v", err)
			}
		})
	}
}

func TestFamilyStateRepositoryPinsConfirmedHeadAndVerifiesItsRetainedApproval(t *testing.T) {
	for _, kind := range []string{"valid", "approval_body"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			families, _ := NewFamilyStateRepository(tx, codec)
			identities, _ := NewBranchIdentityRepository(tx, codec)
			rootPreparation, root := seedTypedCollectingPreparation(t, tx, org)
			family := initialTypedFamilyState(rootPreparation, root)
			familyRef, err := families.Append(ctx, org, family, nil)
			if err != nil {
				t.Fatal(err)
			}
			branchRef, err := identities.Append(ctx, typedReplacementIdentity(rootPreparation, root, 9961))
			if err != nil {
				t.Fatal(err)
			}
			frozen, _ := seedTypedFrozenWorkflowAttempt(t, tx, rootPreparation, branchRef.Id)
			reader, _ := NewPreparationReader(tx, codec)
			retained, err := reader.GetExact(ctx, org, preparationReference(frozen))
			if err != nil {
				t.Fatal(err)
			}
			confirmed := proto.Clone(frozen).(*tammyv1.BasPreparation)
			confirmed.Version++
			confirmed.ContentHash = nil
			confirmed.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
			confirmedRef := appendTypedWorkflowPreparation(t, tx, confirmed, retained.SavedPaygInputs)
			family.Version++
			family.EffectiveOriginal = cloneRef(confirmedRef)
			family.LatestHead = retainedExpectedIdentity(confirmedRef, confirmed.Version)
			if _, err := families.Append(ctx, org, family, familyRef); err != nil {
				t.Fatal(err)
			}
			if kind == "approval_body" {
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_frozen_bas_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_frozen_bas_v1 SET frozen_proto=X'01'`); err != nil {
					t.Fatal(err)
				}
				if body, err := families.GetCurrent(ctx, org, rootPreparation.Family); body != nil || !errors.Is(err, ErrSnapshotCorrupt) {
					t.Fatalf("confirmed family hid corrupt exact approval: %v %v", body, err)
				}
				return
			}
			got, err := families.GetCurrent(ctx, org, rootPreparation.Family)
			if err != nil || !sameRef(got.RootPreparation, root) || !sameRef(got.EffectiveOriginal, confirmedRef) || !sameRef(got.LatestHead.GetRetained(), confirmedRef) {
				t.Fatalf("confirmed family identities: %v %v", got, err)
			}
			// A resolution retains its own historical affected-chain manifest.
			resolutionRef, _ := seedTypedWorkflowResolution(t, tx, rootPreparation.Family, frozen, confirmedRef)
			if _, err := families.readResolutionProof(ctx, org, rootPreparation.Family, resolutionRef); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_dispute_resolution_preparations_v1_no_delete`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM bas_dispute_resolution_preparations_v1`); err != nil {
				t.Fatal(err)
			}
			if body, err := families.readResolutionProof(ctx, org, rootPreparation.Family, resolutionRef); body != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("missing affected-chain link accepted: %v %v", body, err)
			}
		})
	}
}

// This fixture supplies typed storage parents, not an authorised freeze or
// resolution command. Command-level role, fresh factor, policy and audit tests
// belong to the mounted lifecycle service.
func seedTypedFrozenWorkflowAttempt(t *testing.T, executor app.CommandSQLExecutor, root *tammyv1.BasPreparation, branchID string) (*tammyv1.BasPreparation, *tammyv1.SourceRef) {
	t.Helper()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	owner, ownerRef := seedTypedBranchPreparation(t, executor, root, branchID, 9962, 9963)
	inputs, _ := NewSavedPaygRepository(executor, codec)
	input := savedPaygInputsForOwner(t, owner, ownerRef)
	inputRef, err := inputs.Append(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	owner = proto.Clone(owner).(*tammyv1.BasPreparation)
	owner.Version++
	owner.ContentHash = nil
	ownerRef = appendTypedWorkflowPreparation(t, executor, owner, inputRef)
	calculation := proto.Clone(readyReviewInput().Calculation).(*tammyv1.BasCalculation)
	calculation.OrganisationId, calculation.Preparation, calculation.Family = owner.OrganisationId, ownerRef, cloneRef(owner.Family)
	calculation.Profile, calculation.Period, calculation.ContentHash = cloneRef(owner.Profile), cloneRef(owner.Period), nil
	calculation.InputRevision, calculation.InputHash, calculation.SourceSnapshotHash = owner.InputRevision, owner.InputHash, owner.SourceSnapshotHash
	profile := newProfileRepositoryForTest(t, executor, codec)
	calculation.ProfileSnapshot, err = profile.GetExact(ctx, owner.OrganisationId, owner.Profile)
	if err != nil {
		t.Fatal(err)
	}
	period := newPeriodRepositoryForTest(t, executor, codec)
	calculation.PeriodSnapshot, err = period.GetExact(ctx, owner.OrganisationId, owner.Period)
	if err != nil {
		t.Fatal(err)
	}
	calculation.OrganisationSnapshot = organisationSnapshot(owner.OrganisationId)
	calculation.OrganisationIdentity.Id = owner.OrganisationId
	calculation.WorkpaperSnapshot.OrganisationId = owner.OrganisationId
	calculationWire, calculationHash, err := sealCalculation(calculation)
	if err != nil {
		t.Fatal(err)
	}
	calculation.ContentHash = calculationHash
	createdAt, _ := basTimestampSQLText(calculation.CreatedAt)
	columns := []string{"calculation_id", "organisation_id", "family_id", "preparation_id", "preparation_version", "preparation_hash", "version", "content_hash", "input_snapshot_id", "input_revision", "input_hash", "input_content_hash", "profile_id", "profile_version", "profile_hash", "period_id", "period_version", "period_hash", "outcome", "source_snapshot_hash", "coverage_manifest_hash", "revision_vector_hash", "rule_version", "rule_hash", "calculator_version", "calculator_hash", "form_version", "form_hash", "schema_version", "schema_hash", "descriptor_version", "descriptor_hash", "organisation_snapshot_hash", "result_proto", "workpaper_snapshot_proto", "revision_vector_proto", "organisation_snapshot_proto", "calculation_proto", "created_at"}
	values := []any{calculation.CalculationId, owner.OrganisationId, owner.Family.Id, owner.PreparationId, owner.Version, ownerRef.ContentHash, calculation.Version, calculationHash, inputRef.Id, owner.InputRevision, owner.InputHash, inputRef.ContentHash, owner.Profile.Id, owner.Profile.Revision, owner.Profile.ContentHash, owner.Period.Id, owner.Period.Revision, owner.Period.ContentHash, "COMPLETE", owner.SourceSnapshotHash, calculation.CoverageManifestHash, calculation.RevisionVectorHash, calculation.RuleVersion, calculation.RuleHash, calculation.CalculatorVersion, calculation.CalculatorHash, calculation.FormVersion, calculation.FormHash, calculation.SchemaVersion, calculation.SchemaHash, calculation.DescriptorVersion, calculation.DescriptorHash, calculation.OrganisationSnapshotHash, typedWorkflowWire(t, calculation.Result), typedWorkflowWire(t, calculation.WorkpaperSnapshot), typedWorkflowWire(t, calculation.RevisionVector), typedWorkflowWire(t, calculation.OrganisationSnapshot), calculationWire, createdAt}
	insertTypedWorkflowRow(t, executor, "bas_calculations_v1", columns, values)
	owner.Version++
	owner.ContentHash = nil
	owner.CurrentCalculation = calculationReference(calculation)
	ownerRef = appendTypedWorkflowPreparation(t, executor, owner, inputRef)
	validation := proto.Clone(readyReviewInput().Validation).(*tammyv1.BasValidationSnapshot)
	validation.PreparationId, validation.PreparationHash, validation.CalculationHash = owner.PreparationId, ownerRef.ContentHash, calculationHash
	validation.InputRevision, validation.InputHash, validation.SourceSnapshotHash, validation.ContentHash = owner.InputRevision, owner.InputHash, owner.SourceSnapshotHash, nil
	validationWire, validationHash, err := sealValidation(validation, owner)
	if err != nil {
		t.Fatal(err)
	}
	validation.ContentHash = validationHash
	validationAt, _ := basTimestampSQLText(validation.CreatedAt)
	insertTypedWorkflowRow(t, executor, "bas_validations_v1", []string{"validation_id", "organisation_id", "family_id", "preparation_id", "preparation_version", "revision", "content_hash", "preparation_hash", "calculation_id", "calculation_version", "calculation_hash", "source_snapshot_hash", "input_revision", "input_hash", "input_snapshot_id", "input_content_hash", "validation_proto", "created_at"}, []any{validation.ValidationId, owner.OrganisationId, owner.Family.Id, owner.PreparationId, owner.Version, validation.Revision, validationHash, ownerRef.ContentHash, calculation.CalculationId, calculation.Version, calculationHash, owner.SourceSnapshotHash, owner.InputRevision, owner.InputHash, inputRef.Id, inputRef.ContentHash, validationWire, validationAt})
	owner.Version++
	owner.ContentHash = nil
	owner.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY
	owner.CurrentValidation = &tammyv1.SourceRef{Type: "bas_validation", Id: validation.ValidationId, Revision: validation.Revision, ContentHash: validationHash}
	ownerRef = appendTypedWorkflowPreparation(t, executor, owner, inputRef)
	factor, session := testUUID(9964), testUUID(9965)
	if _, err := executor.ExecContext(ctx, `INSERT INTO application_sessions(id,user_id,state,created_at,last_active_at,expires_at) VALUES(?,?,1,'2026-10-11T00:00:00Z','2026-10-11T00:00:00Z','2026-10-12T00:00:00Z')`, session, testUUID(36)); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO factor_assertions(id,user_id,session_id,purpose,asserted_at,consumed) VALUES(?,?,?,'bas_freeze_manual_handoff','2026-10-11T00:00:00Z',0)`, factor, testUUID(36), session); err != nil {
		t.Fatal(err)
	}
	frozen := &tammyv1.BasFrozenBas{FrozenId: testUUID(9966), OrganisationId: owner.OrganisationId, Preparation: ownerRef, Family: cloneRef(owner.Family), Period: cloneRef(owner.Period), Profile: cloneRef(owner.Profile), Calculation: cloneRef(owner.CurrentCalculation), Validation: cloneRef(owner.CurrentValidation), CalculationSnapshot: calculation, ApprovedSnapshotHash: testHash(86), ApprovalActorUserId: testUUID(36), FreshFactorAssertionId: factor, ApprovedAt: owner.UpdatedAt, ApprovalReason: "Retained exact approval for structural repository tests"}
	frozenWire, frozenHash, err := sealFrozen(frozen)
	if err != nil {
		t.Fatal(err)
	}
	approvedAt, _ := basTimestampSQLText(frozen.ApprovedAt)
	insertTypedWorkflowRow(t, executor, "bas_frozen_bas_v1", []string{"frozen_id", "organisation_id", "family_id", "preparation_id", "preparation_version", "preparation_hash", "profile_id", "profile_version", "profile_hash", "period_id", "period_version", "period_hash", "calculation_id", "calculation_version", "calculation_hash", "validation_id", "validation_revision", "validation_hash", "source_snapshot_hash", "approved_snapshot_hash", "content_hash", "calculation_snapshot_proto", "frozen_proto", "approval_actor_user_id", "fresh_factor_assertion_id", "approved_at", "approval_reason"}, []any{frozen.FrozenId, owner.OrganisationId, owner.Family.Id, owner.PreparationId, owner.Version, ownerRef.ContentHash, owner.Profile.Id, owner.Profile.Revision, owner.Profile.ContentHash, owner.Period.Id, owner.Period.Revision, owner.Period.ContentHash, calculation.CalculationId, calculation.Version, calculationHash, validation.ValidationId, validation.Revision, validationHash, owner.SourceSnapshotHash, frozen.ApprovedSnapshotHash, frozenHash, calculationWire, frozenWire, frozen.ApprovalActorUserId, factor, approvedAt, frozen.ApprovalReason})
	owner.Version++
	owner.ContentHash = nil
	owner.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF
	owner.FrozenBas = &tammyv1.SourceRef{Type: "bas_frozen", Id: frozen.FrozenId, Revision: 1, ContentHash: frozenHash}
	appendTypedWorkflowPreparation(t, executor, owner, inputRef)
	return owner, owner.FrozenBas
}

func appendTypedWorkflowPreparation(t *testing.T, executor app.CommandSQLExecutor, value *tammyv1.BasPreparation, inputs *tammyv1.SourceRef) *tammyv1.SourceRef {
	t.Helper()
	wire, hash, err := sealPreparation(value)
	if err != nil {
		t.Fatal(err)
	}
	createdAt, _ := basTimestampSQLText(value.CreatedAt)
	updatedAt, _ := basTimestampSQLText(value.UpdatedAt)
	calculationID, calculationVersion, calculationHash := sqlSourceValues(value.CurrentCalculation)
	validationID, validationVersion, validationHash := sqlSourceValues(value.CurrentValidation)
	var frozenID, frozenHash any
	if value.FrozenBas != nil {
		frozenID, frozenHash = value.FrozenBas.Id, value.FrozenBas.ContentHash
	}
	insertTypedWorkflowRow(t, executor, "bas_preparation_versions_v1", []string{"preparation_id", "organisation_id", "family_id", "version", "profile_id", "profile_version", "profile_hash", "period_id", "period_version", "period_hash", "input_snapshot_id", "input_revision", "input_hash", "input_content_hash", "calculation_id", "calculation_version", "calculation_hash", "validation_id", "validation_revision", "validation_hash", "dispute_overlay", "source_snapshot_hash", "state", "freeze_id", "freeze_hash", "content_hash", "preparation_proto", "created_at", "updated_at"}, []any{value.PreparationId, value.OrganisationId, value.Family.Id, value.Version, value.Profile.Id, value.Profile.Revision, value.Profile.ContentHash, value.Period.Id, value.Period.Revision, value.Period.ContentHash, inputs.Id, value.InputRevision, value.InputHash, inputs.ContentHash, calculationID, calculationVersion, calculationHash, validationID, validationVersion, validationHash, "NONE", value.SourceSnapshotHash, enumSQL(value.State.String(), "BAS_PREPARATION_STATE_"), frozenID, frozenHash, hash, wire, createdAt, updatedAt})
	if _, err := executor.ExecContext(context.Background(), `UPDATE bas_preparation_heads_v1 SET current_version=?,current_hash=?,cas_version=cas_version+1,updated_at=? WHERE preparation_id=?`, value.Version, hash, updatedAt, value.PreparationId); err != nil {
		t.Fatal(err)
	}
	value.ContentHash = hash
	return preparationReference(value)
}

func seedTypedWorkflowResolution(t *testing.T, executor app.CommandSQLExecutor, family *tammyv1.SourceRef, frozenPreparation *tammyv1.BasPreparation, affected ...*tammyv1.SourceRef) (*tammyv1.SourceRef, uint64) {
	t.Helper()
	identities := newFamilyIdentityRepositoryForTest(t, executor)
	// The fixture's organisation is fixed by the independently retained owner.
	org := testUUID(9101)
	identity, err := identities.GetExact(context.Background(), org, family)
	if err != nil {
		t.Fatal(err)
	}
	observation := retainedObservation(AttemptSnapshot{Preparation: frozenPreparation, FrozenReference: frozenPreparation.FrozenBas}, 9968, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	observation.ActorUserId, observation.ContentHash = testUUID(36), nil
	observationWire, observationHash, err := sealObservation(observation)
	if err != nil {
		t.Fatal(err)
	}
	claimedAt, _ := basTimestampSQLText(observation.GetObservation().ClaimedEventAt)
	recordedAt, _ := basTimestampSQLText(observation.RecordedAt)
	insertTypedWorkflowRow(t, executor, "bas_family_events_v1", []string{"event_id", "organisation_id", "family_id", "event_kind", "operation_key", "claimed_at", "event_proto", "body_hash", "created_at"}, []any{testUUID(9970), org, family.Id, "OBSERVATION", "retained-observation-test", claimedAt, observationWire, observationHash, recordedAt})
	insertTypedWorkflowRow(t, executor, "bas_external_observations_v1", []string{"observation_id", "event_id", "organisation_id", "family_id", "frozen_id", "frozen_hash", "event_kind", "channel", "claimed_event_at", "external_reference", "reason", "trust", "actor_user_id", "recorded_at", "content_hash", "observation_proto"}, []any{observation.ObservationId, testUUID(9970), org, family.Id, frozenPreparation.FrozenBas.Id, frozenPreparation.FrozenBas.ContentHash, "REJECTION", "ONLINE_PORTAL", claimedAt, "", observation.GetObservation().Reason, "USER_RECORDED_UNVERIFIED", observation.ActorUserId, recordedAt, observationHash, observationWire})
	observationRef := &tammyv1.SourceRef{Type: "bas_external_observation", Id: observation.ObservationId, Revision: 1, ContentHash: observationHash}
	attempt := AttemptSnapshot{Preparation: frozenPreparation, FrozenReference: frozenPreparation.FrozenBas}
	firstCorrection := retainedCorrection(attempt, 9971, observationRef, tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION, nil, observation.RecordedAt)
	firstCorrection.ActorUserId, firstCorrection.ContentHash = testUUID(36), nil
	firstCorrectionRef := seedTypedWorkflowCorrection(t, executor, org, family, firstCorrection, "OBSERVATION", 9973)
	correctedKind := tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION
	secondCorrection := retainedCorrection(attempt, 9972, firstCorrectionRef, tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION, &correctedKind, nil)
	secondCorrection.ActorUserId, secondCorrection.ContentHash = testUUID(36), nil
	secondCorrectionRef := seedTypedWorkflowCorrection(t, executor, org, family, secondCorrection, "CORRECTION", 9974)
	value := &tammyv1.BasExternalDisputeResolution{ResolutionId: testUUID(9967), Family: cloneRef(family), ActorUserId: testUUID(36), ResolvedAt: readyReviewInput().Preparation.UpdatedAt,
		Resolution: &tammyv1.BasExternalDisputeInput{Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION, DisputedObservations: []*tammyv1.SourceRef{observationRef, firstCorrectionRef, secondCorrectionRef}, AffectedConfirmedChain: affected, DependentChainManifestHash: dependencyRefsManifest(affected), Reason: "Retained corrective resolution for structural repository tests"}}
	wire, hash, err := sealResolution(value, identity)
	if err != nil {
		t.Fatal(err)
	}
	resolvedAt, _ := basTimestampSQLText(value.ResolvedAt)
	result, err := executor.ExecContext(context.Background(), `INSERT INTO bas_family_events_v1(event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at) VALUES(?,?,?,'RESOLUTION',?,?,?,?,?)`, testUUID(9969), org, family.Id, "retained-resolution-test", resolvedAt, wire, hash, resolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	ordinal, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	insertTypedWorkflowRow(t, executor, "bas_dispute_resolutions_v1", []string{"resolution_id", "event_id", "organisation_id", "family_id", "outcome", "dependent_chain_manifest_hash", "reason", "actor_user_id", "resolved_at", "content_hash", "resolution_proto"}, []any{value.ResolutionId, testUUID(9969), org, family.Id, "REQUIRE_CORRECTIVE_REVISION", value.Resolution.DependentChainManifestHash, value.Resolution.Reason, value.ActorUserId, resolvedAt, hash, wire})
	insertTypedWorkflowRow(t, executor, "bas_dispute_resolution_observations_v1", []string{"resolution_id", "organisation_id", "family_id", "subject_kind", "subject_id", "subject_hash", "observation_id", "observation_hash"}, []any{value.ResolutionId, org, family.Id, "OBSERVATION", observation.ObservationId, observationHash, observation.ObservationId, observationHash})
	for _, ref := range []*tammyv1.SourceRef{firstCorrectionRef, secondCorrectionRef} {
		insertTypedWorkflowRow(t, executor, "bas_dispute_resolution_observations_v1", []string{"resolution_id", "organisation_id", "family_id", "subject_kind", "subject_id", "subject_hash", "correction_id", "correction_hash"}, []any{value.ResolutionId, org, family.Id, "CORRECTION", ref.Id, ref.ContentHash, ref.Id, ref.ContentHash})
	}
	for _, ref := range affected {
		insertTypedWorkflowRow(t, executor, "bas_dispute_resolution_preparations_v1", []string{"resolution_id", "organisation_id", "family_id", "preparation_id", "preparation_version", "preparation_hash"}, []any{value.ResolutionId, org, family.Id, ref.Id, ref.Revision, ref.ContentHash})
	}
	return &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: value.ResolutionId, Revision: 1, ContentHash: hash}, uint64(ordinal)
}

func seedTypedWorkflowCorrection(t *testing.T, executor app.CommandSQLExecutor, org string, family *tammyv1.SourceRef, value *tammyv1.BasExternalObservation, subjectKind string, eventID uint64) *tammyv1.SourceRef {
	t.Helper()
	wire, hash, err := sealObservation(value)
	if err != nil {
		t.Fatal(err)
	}
	recordedAt, _ := basTimestampSQLText(value.RecordedAt)
	insertTypedWorkflowRow(t, executor, "bas_family_events_v1", []string{"event_id", "organisation_id", "family_id", "event_kind", "operation_key", "claimed_at", "event_proto", "body_hash", "created_at"}, []any{testUUID(eventID), org, family.Id, "CORRECTION", value.ObservationId, recordedAt, wire, hash, recordedAt})
	var observationID, observationHash, correctionID, correctionHash, correctedKind, correctedAt any
	if subjectKind == "OBSERVATION" {
		observationID, observationHash = value.CorrectsObservation.Id, value.CorrectsObservation.ContentHash
	} else {
		correctionID, correctionHash = value.CorrectsObservation.Id, value.CorrectsObservation.ContentHash
	}
	input := value.GetCorrection()
	if input.CorrectedEventKind != nil {
		correctedKind = enumSQL(input.GetCorrectedEventKind().String(), "BAS_EXTERNAL_EVENT_KIND_")
	}
	if input.CorrectedClaimedEventAt != nil {
		correctedAt, _ = basTimestampSQLText(input.CorrectedClaimedEventAt)
	}
	insertTypedWorkflowRow(t, executor, "bas_external_observation_corrections_v1", []string{"correction_id", "event_id", "organisation_id", "family_id", "frozen_id", "frozen_hash", "disputed_observation_id", "disputed_observation_hash", "disputed_correction_id", "disputed_correction_hash", "correction_kind", "corrected_event_kind", "corrected_claimed_event_at", "reason", "trust", "actor_user_id", "recorded_at", "content_hash", "correction_proto"}, []any{value.ObservationId, testUUID(eventID), org, family.Id, value.FrozenBas.Id, value.FrozenBas.ContentHash, observationID, observationHash, correctionID, correctionHash, enumSQL(input.CorrectionKind.String(), "BAS_CORRECTION_KIND_"), correctedKind, correctedAt, input.Reason, "USER_RECORDED_UNVERIFIED", value.ActorUserId, recordedAt, hash, wire})
	return &tammyv1.SourceRef{Type: "bas_external_observation", Id: value.ObservationId, Revision: 1, ContentHash: hash}
}

func typedWorkflowWire(t *testing.T, value proto.Message) []byte {
	t.Helper()
	wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
func insertTypedWorkflowRow(t *testing.T, executor app.CommandSQLExecutor, table string, columns []string, values []any) {
	t.Helper()
	if len(columns) != len(values) {
		t.Fatal("fixture column/value mismatch")
	}
	query := "INSERT INTO " + table + "(" + strings.Join(columns, ",") + ") VALUES(" + strings.TrimSuffix(strings.Repeat("?,", len(values)), ",") + ")"
	if _, err := executor.ExecContext(context.Background(), query, values...); err != nil {
		t.Fatalf("insert typed %s: %v", table, err)
	}
}
