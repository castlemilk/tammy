//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestPreparationPayloadRequiresCompleteFrozenParentsForSupersededTerminals(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	base := typedPreparationForAppend(t, tx, org)

	for _, state := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION,
	} {
		t.Run(state.String(), func(t *testing.T) {
			for _, kind := range []string{"missing_freeze", "missing_calculation", "missing_validation", "complete"} {
				t.Run(kind, func(t *testing.T) {
					candidate := clonePreparationRetainedSnapshot(base)
					candidate.Preparation.State = state
					candidate.Preparation.CurrentCalculation = testPreparationTerminalRef("bas_calculation", 9982)
					candidate.Preparation.CurrentValidation = testPreparationTerminalRef("bas_validation", 9983)
					candidate.SavedPaygInputs = testPreparationTerminalRef("bas_saved_payg_inputs", 9984)
					candidate.Preparation.FrozenBas = testPreparationTerminalRef("bas_frozen", 9985)
					switch kind {
					case "missing_freeze":
						candidate.Preparation.FrozenBas = nil
					case "missing_calculation":
						candidate.Preparation.CurrentCalculation = nil
					case "missing_validation":
						candidate.Preparation.CurrentValidation = nil
					}
					got := validPreparationPayload(candidate)
					if kind == "complete" && !got {
						t.Fatalf("complete frozen %s structural payload was rejected", state)
					}
					if kind != "complete" && got {
						t.Fatalf("%s %s structural payload passed validation", kind, state)
					}
				})
			}
		})
	}
}

func TestPreparationRepositoryRejectsUnfrozenSupersededTerminals(t *testing.T) {
	for _, state := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION,
	} {
		t.Run(state.String(), func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			repository, _ := NewPreparationRepository(tx, codec)
			initial := typedPreparationForAppend(t, tx, org)
			initialRef, err := repository.Append(ctx, initial, nil)
			if err != nil {
				t.Fatal(err)
			}
			current, err := repository.GetExact(ctx, org, initialRef)
			if err != nil {
				t.Fatal(err)
			}

			var confirmedSuccessor *tammyv1.SourceRef
			if state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION {
				confirmedSuccessor = appendTypedConfirmedSuccessor(t, tx, repository, ctx, current, initialRef)
			}

			next := clonePreparationRetainedSnapshot(current)
			next.Preparation.Version++
			next.Preparation.ContentHash = nil
			next.Preparation.State = state
			next.Preparation.SupersededBy = confirmedSuccessor
			ref, err := repository.Append(ctx, next, initialRef)
			if ref != nil || !errors.Is(err, ErrPreparationRepository) {
				t.Fatalf("unfrozen %s successor advanced preparation: ref=%v err=%v", state, ref, err)
			}
			stillCurrent, err := repository.GetCurrent(ctx, org, current.Preparation.PreparationId)
			if err != nil || !sameBASSourceRef(preparationReference(stillCurrent.Preparation), initialRef) {
				t.Fatalf("rejected %s write changed the current preparation: current=%v err=%v", state, stillCurrent, err)
			}
		})
	}
}

func TestPreparationSchemaRejectsUnfrozenSupersededTerminals(t *testing.T) {
	for _, state := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION,
	} {
		t.Run(state.String(), func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			repository, _ := NewPreparationRepository(tx, codec)
			initial := typedPreparationForAppend(t, tx, org)
			if _, err := repository.Append(ctx, initial, nil); err != nil {
				t.Fatal(err)
			}
			next := clonePreparationRetainedSnapshot(initial)
			next.Preparation.Version = 2
			next.Preparation.ContentHash = nil
			next.Preparation.State = state
			if _, err := insertRawPreparationVersion(tx, next, false); err == nil {
				t.Fatalf("direct SQL insert accepted unfrozen terminal state %s", state)
			}
		})
	}
}

func TestPreparationReaderRejectsWellSealedUnfrozenSupersededTerminals(t *testing.T) {
	for _, state := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION,
	} {
		t.Run(state.String(), func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			repository, _ := NewPreparationRepository(tx, codec)
			initial := typedPreparationForAppend(t, tx, org)
			if _, err := repository.Append(ctx, initial, nil); err != nil {
				t.Fatal(err)
			}
			invalid := clonePreparationRetainedSnapshot(initial)
			invalid.Preparation.Version = 2
			invalid.Preparation.ContentHash = nil
			invalid.Preparation.State = state
			ref, err := insertRawPreparationVersion(tx, invalid, true)
			if err != nil {
				t.Fatalf("seed a well-sealed invalid retained row: %v", err)
			}
			reader, err := NewPreparationReader(tx, codec)
			if err != nil {
				t.Fatal(err)
			}
			retained, err := reader.GetExact(ctx, org, ref)
			if retained != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("exact read accepted well-sealed unfrozen terminal body: retained=%v err=%v", retained, err)
			}
		})
	}
}

func testPreparationTerminalRef(kind string, id uint64) *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: kind, Id: testUUID(id), Revision: 1, ContentHash: testHash(byte(id))}
}

func appendTypedConfirmedSuccessor(t *testing.T, executor app.CommandSQLExecutor, repository *PreparationRepository, ctx context.Context, root *PreparationRetainedSnapshot, rootRef *tammyv1.SourceRef) *tammyv1.SourceRef {
	t.Helper()
	codec := newBASRepositoryCursorCodec(t)
	families, _ := NewFamilyStateRepository(executor, codec)
	if _, err := families.Append(ctx, root.Preparation.OrganisationId, initialTypedFamilyState(root.Preparation, rootRef), nil); err != nil {
		t.Fatal(err)
	}
	identities, _ := NewBranchIdentityRepository(executor, codec)
	branch, err := identities.Append(ctx, typedReplacementIdentity(root.Preparation, rootRef, 9981))
	if err != nil {
		t.Fatal(err)
	}
	frozen, _ := seedTypedFrozenWorkflowAttempt(t, executor, root.Preparation, branch.Id)
	current, err := repository.GetCurrent(ctx, root.Preparation.OrganisationId, frozen.PreparationId)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := clonePreparationRetainedSnapshot(current)
	confirmed.Preparation.Version++
	confirmed.Preparation.ContentHash = nil
	confirmed.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	ref, err := repository.Append(ctx, confirmed, preparationReference(current.Preparation))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func insertRawPreparationVersion(executor app.CommandSQLExecutor, retained *PreparationRetainedSnapshot, ignoreChecks bool) (*tammyv1.SourceRef, error) {
	value := retained.Preparation
	wire, hash, err := sealPreparation(value)
	if err != nil {
		return nil, err
	}
	createdAt, _ := basTimestampSQLText(value.CreatedAt)
	updatedAt, _ := basTimestampSQLText(value.UpdatedAt)
	inputID, _, inputContentHash := sqlSourceValues(retained.SavedPaygInputs)
	calculationID, calculationVersion, calculationHash := sqlSourceValues(value.CurrentCalculation)
	validationID, validationRevision, validationHash := sqlSourceValues(value.CurrentValidation)
	predecessorID, predecessorVersion, predecessorHash := sqlSourceValues(value.Predecessor)
	successorID, successorVersion, successorHash := sqlSourceValues(value.SupersededBy)
	freezeID, _, freezeHash := sqlSourceValues(value.FrozenBas)
	columns := []string{
		"preparation_id", "organisation_id", "family_id", "version", "profile_id", "profile_version", "profile_hash",
		"period_id", "period_version", "period_hash", "input_snapshot_id", "input_revision", "input_hash", "input_content_hash",
		"calculation_id", "calculation_version", "calculation_hash", "validation_id", "validation_revision", "validation_hash",
		"predecessor_id", "predecessor_version", "predecessor_hash", "superseded_by_preparation_id", "superseded_by_version", "superseded_by_hash",
		"dispute_overlay", "source_snapshot_hash", "state", "freeze_id", "freeze_hash", "content_hash", "preparation_proto", "created_at", "updated_at",
	}
	values := []any{
		value.PreparationId, value.OrganisationId, value.Family.Id, int64(value.Version), value.Profile.Id, int64(value.Profile.Revision), value.Profile.ContentHash,
		value.Period.Id, int64(value.Period.Revision), value.Period.ContentHash, inputID, int64(value.InputRevision), value.InputHash, inputContentHash,
		calculationID, calculationVersion, calculationHash, validationID, validationRevision, validationHash,
		predecessorID, predecessorVersion, predecessorHash, successorID, successorVersion, successorHash,
		enumSQL(value.DisputeOverlay.String(), "BAS_DISPUTE_OVERLAY_STATE_"), value.SourceSnapshotHash,
		enumSQL(value.State.String(), "BAS_PREPARATION_STATE_"), freezeID, freezeHash, hash, wire, createdAt, updatedAt,
	}
	query := "INSERT INTO bas_preparation_versions_v1(" + strings.Join(columns, ",") + ") VALUES(" + strings.TrimSuffix(strings.Repeat("?,", len(values)), ",") + ")"
	if ignoreChecks {
		if _, err := executor.ExecContext(context.Background(), `PRAGMA ignore_check_constraints=ON`); err != nil {
			return nil, err
		}
	}
	_, err = executor.ExecContext(context.Background(), query, values...)
	if ignoreChecks {
		_, resetErr := executor.ExecContext(context.Background(), `PRAGMA ignore_check_constraints=OFF`)
		err = errors.Join(err, resetErr)
	}
	if err != nil {
		return nil, err
	}
	value.ContentHash = hash
	return preparationReference(value), nil
}
