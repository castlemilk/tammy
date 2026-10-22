//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestBASPreparationEventAppendsVerifiesAndUsesDescriptorRegistry(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	organisationID := "01890f60-4d6d-7c12-8f02-6c9129d5b002"
	actorID := "01890f60-4d6d-7c12-8f02-6c9129d5b006"
	sessionID := "01890f60-4d6d-7c12-8f02-6c9129d5b007"
	eventID := "01890f60-4d6d-7c12-8f02-6c9129d5b008"
	profileID := "01890f60-4d6d-7c12-8f02-6c9129d5b009"
	operationKey := "01890f60-4d6d-7c12-8f02-6c9129d5b010"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	descriptors := testAuditDescriptorSet(t)
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := PersistDescriptorSet(ctx, transaction, descriptors,
		time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, transaction, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 10, 22, 0, 0, 1, 0, time.UTC)}); err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	source := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: profileID,
		Revision: 1, ContentHash: bytes.Repeat([]byte{0x31}, sha256.Size)}
	payload := &tammyv1.BasPreparationChangedEvent{
		Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PROFILE_SET,
		Source: source,
	}
	payloadBytes, err := (proto.MarshalOptions{Deterministic: true}).Marshal(payload)
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	commandID := operationKey
	event := &tammyv1.AuditEvent{
		Id: eventID, WorkspaceId: workspaceID, OrganisationId: &organisationID,
		CommandId: &commandID, IdempotencyKey: &operationKey,
		Actor:                    &tammyv1.AuthenticationContext{ActorUserId: actorID, SessionId: sessionID},
		CommandType:              "tammy.v1.BasPreparationService.SetBasObligationsProfile",
		Type:                     tammyv1.AuditEventType_AUDIT_EVENT_TYPE_BAS_PREPARATION_CHANGED,
		OccurredAt:               timestamppb.New(time.Date(2026, 10, 22, 0, 0, 2, 0, time.UTC)),
		Source:                   proto.Clone(source).(*tammyv1.SourceRef),
		AffectedResources:        []*tammyv1.SourceRef{proto.Clone(source).(*tammyv1.SourceRef)},
		PayloadSchemaFingerprint: fingerprint[:],
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_BasPreparationChanged{
			BasPreparationChanged: proto.Clone(payload).(*tammyv1.BasPreparationChangedEvent),
		}},
		AfterSemanticHash: bytes.Repeat([]byte{0x31}, sha256.Size),
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SetBasObligationsProfileResponse",
			DeterministicSha256: bytes.Repeat([]byte{0x55}, sha256.Size), OutcomeCode: "SUCCESS"},
	}
	stored, err := appendStoredEventForTest(ctx, transaction, event, payloadBytes)
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if stored.Event.Sequence != 1 || !bytes.Equal(stored.Event.PayloadSchemaFingerprint, fingerprint[:]) {
		_ = transaction.Rollback()
		t.Fatalf("stored BAS audit position/fingerprint = %d/%x", stored.Event.Sequence,
			stored.Event.PayloadSchemaFingerprint)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	header, err := LoadChainHeader(ctx, database, workspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	events, err := LoadStoredEvents(ctx, database, workspaceID, 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].PayloadType != "tammy.v1.BasPreparationChangedEvent" ||
		!bytes.Equal(events[0].PayloadProto, payloadBytes) {
		t.Fatalf("loaded BAS audit payload = %#v", events)
	}
	verified := VerifyStoredChain(header, events)
	if verified.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		verified.VerifiedThroughSequence != 1 {
		t.Fatalf("BAS event chain verification = %#v", verified)
	}
	loadedDescriptors, err := LoadDescriptorSet(ctx, database, fingerprint)
	if err != nil || !bytes.Equal(loadedDescriptors, descriptors) || sha256.Sum256(loadedDescriptors) != fingerprint {
		t.Fatalf("normal descriptor registry round-trip = %v", err)
	}
}
