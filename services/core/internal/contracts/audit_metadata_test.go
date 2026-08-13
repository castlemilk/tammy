package contracts_test

import (
	"testing"

	_ "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

func TestAuditEventDescriptorRetainsNormativeCommandMetadataWithoutSecrets(t *testing.T) {
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.AuditEvent")
	if err != nil {
		t.Fatalf("find AuditEvent descriptor: %v", err)
	}
	event, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatal("AuditEvent descriptor is not a message")
	}
	want := map[protoreflect.Name]protoreflect.Kind{
		"organisation_id":      protoreflect.StringKind,
		"command_id":           protoreflect.StringKind,
		"command_type":         protoreflect.StringKind,
		"idempotency_key":      protoreflect.StringKind,
		"affected_resources":   protoreflect.MessageKind,
		"before_semantic_hash": protoreflect.BytesKind,
		"after_semantic_hash":  protoreflect.BytesKind,
		"result":               protoreflect.MessageKind,
	}
	for name, kind := range want {
		field := event.Fields().ByName(name)
		if field == nil {
			t.Errorf("AuditEvent.%s descriptor not found", name)
			continue
		}
		if field.Kind() != kind {
			t.Errorf("AuditEvent.%s kind = %s, want %s", name, field.Kind(), kind)
		}
	}
	if field := event.Fields().ByName("affected_resources"); field != nil && !field.IsList() {
		t.Error("AuditEvent.affected_resources must be repeated")
	}
	if field := event.Fields().ByName("result"); field != nil && field.Message().FullName() != "tammy.v1.AuditResultMetadata" {
		t.Errorf("AuditEvent.result type = %s, want tammy.v1.AuditResultMetadata", field.Message().FullName())
	}
	for _, prohibited := range []protoreflect.Name{
		"password", "passphrase", "secret", "private_key", "totp_code", "payload_plaintext",
	} {
		if event.Fields().ByName(prohibited) != nil {
			t.Errorf("AuditEvent must not retain secret field %q", prohibited)
		}
	}

	resultDescriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.AuditResultMetadata")
	if err != nil {
		t.Fatalf("find AuditResultMetadata descriptor: %v", err)
	}
	result, ok := resultDescriptor.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatal("AuditResultMetadata descriptor is not a message")
	}
	for name, kind := range map[protoreflect.Name]protoreflect.Kind{
		"type_name":            protoreflect.StringKind,
		"deterministic_sha256": protoreflect.BytesKind,
		"outcome_code":         protoreflect.StringKind,
	} {
		field := result.Fields().ByName(name)
		if field == nil || field.Kind() != kind {
			t.Errorf("AuditResultMetadata.%s descriptor missing or wrong kind", name)
		}
	}
}

func TestAuditEventDescriptorCarriesIndependentCommitmentOpenings(t *testing.T) {
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.AuditCommitmentOpenings")
	if err != nil {
		t.Fatalf("find AuditCommitmentOpenings descriptor: %v", err)
	}
	openings, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatal("AuditCommitmentOpenings descriptor is not a message")
	}
	for index, name := range []protoreflect.Name{
		"hidden_metadata_blinding",
		"payload_identity_blinding",
		"event_type_blinding",
		"occurred_at_blinding",
		"actor_user_id_blinding",
	} {
		field := openings.Fields().ByName(name)
		if field == nil || field.Kind() != protoreflect.BytesKind || field.Number() != protoreflect.FieldNumber(index+1) {
			t.Errorf("AuditCommitmentOpenings.%s descriptor missing or wrong kind/number", name)
		}
	}
	eventDescriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.AuditEvent")
	if err != nil {
		t.Fatalf("find AuditEvent descriptor: %v", err)
	}
	event := eventDescriptor.(protoreflect.MessageDescriptor)
	field := event.Fields().ByName("commitment_openings")
	if field == nil || field.Number() != 21 || field.Kind() != protoreflect.MessageKind ||
		field.Message().FullName() != "tammy.v1.AuditCommitmentOpenings" {
		t.Fatalf("AuditEvent.commitment_openings descriptor missing or wrong: %v", field)
	}
}

func TestAuditDescriptorsExposeSigningKeyRotationContinuity(t *testing.T) {
	for messageName, fields := range map[protoreflect.FullName]map[protoreflect.Name]protoreflect.Kind{
		"tammy.v1.AuditSigningPublicKey": {
			"workspace_id": protoreflect.StringKind, "generation": protoreflect.Uint64Kind,
			"epoch": protoreflect.Uint64Kind, "key_id": protoreflect.StringKind,
			"public_key": protoreflect.BytesKind, "created_at": protoreflect.MessageKind,
			"retired_at": protoreflect.MessageKind,
		},
		"tammy.v1.AuditSigningKeyRotationLink": {
			"version": protoreflect.StringKind, "workspace_id": protoreflect.StringKind,
			"generation": protoreflect.Uint64Kind, "prior_sequence": protoreflect.Uint64Kind,
			"prior_head": protoreflect.BytesKind, "successor_epoch": protoreflect.Uint64Kind,
			"predecessor_key_id": protoreflect.StringKind, "predecessor_public_key": protoreflect.BytesKind,
			"successor_key_id": protoreflect.StringKind, "successor_public_key": protoreflect.BytesKind,
			"rotated_at": protoreflect.MessageKind, "predecessor_signature": protoreflect.BytesKind,
			"successor_possession_signature": protoreflect.BytesKind,
		},
		"tammy.v1.AuditSigningKeyChain": {
			"version": protoreflect.StringKind, "keys": protoreflect.MessageKind, "links": protoreflect.MessageKind,
			"event_proofs": protoreflect.MessageKind,
		},
		"tammy.v1.AuditSigningKeyRotationEventProof": {
			"successor_epoch": protoreflect.Uint64Kind, "schema_fingerprint": protoreflect.BytesKind,
			"payload_identity_blinding": protoreflect.BytesKind, "event_type_blinding": protoreflect.BytesKind,
			"occurred_at_blinding": protoreflect.BytesKind,
		},
		"tammy.v1.SigningKeyRotatedEvent": {
			"workspace_id": protoreflect.StringKind, "generation": protoreflect.Uint64Kind,
			"successor_epoch": protoreflect.Uint64Kind, "predecessor_key_id": protoreflect.StringKind,
			"successor_key_id": protoreflect.StringKind, "rotation_link_sha256": protoreflect.BytesKind,
		},
	} {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(messageName)
		if err != nil {
			t.Fatalf("find %s descriptor: %v", messageName, err)
		}
		message, ok := descriptor.(protoreflect.MessageDescriptor)
		if !ok {
			t.Fatalf("%s is not a message", messageName)
		}
		for name, kind := range fields {
			field := message.Fields().ByName(name)
			if field == nil || field.Kind() != kind {
				t.Errorf("%s.%s descriptor missing or wrong kind", messageName, name)
			}
		}
		wantNumbers := map[protoreflect.FullName]map[protoreflect.Name]protoreflect.FieldNumber{
			"tammy.v1.AuditSigningPublicKey": {
				"workspace_id": 1, "generation": 2, "epoch": 3, "key_id": 4,
				"public_key": 5, "created_at": 6, "retired_at": 7,
			},
			"tammy.v1.AuditSigningKeyRotationLink": {
				"version": 1, "workspace_id": 2, "generation": 3, "prior_sequence": 4,
				"prior_head": 5, "successor_epoch": 6, "predecessor_key_id": 7,
				"predecessor_public_key": 8, "successor_key_id": 9, "successor_public_key": 10,
				"rotated_at": 11, "predecessor_signature": 12, "successor_possession_signature": 13,
			},
			"tammy.v1.AuditSigningKeyChain": {"version": 1, "keys": 2, "links": 3, "event_proofs": 4},
			"tammy.v1.AuditSigningKeyRotationEventProof": {
				"successor_epoch": 1, "schema_fingerprint": 4, "payload_identity_blinding": 5,
				"event_type_blinding": 6, "occurred_at_blinding": 7,
			},
			"tammy.v1.SigningKeyRotatedEvent": {
				"workspace_id": 1, "generation": 2, "successor_epoch": 3,
				"predecessor_key_id": 4, "successor_key_id": 5, "rotation_link_sha256": 6,
			},
		}
		for name, number := range wantNumbers[messageName] {
			if field := message.Fields().ByName(name); field == nil || field.Number() != number {
				t.Errorf("%s.%s field number=%v, want %d", messageName, name, field, number)
			}
		}
	}
	manifestDescriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.AuditExportManifest")
	if err != nil {
		t.Fatal(err)
	}
	manifest := manifestDescriptor.(protoreflect.MessageDescriptor)
	if root := manifest.Fields().ByName("root_signing_key_id"); root == nil || root.Number() != 12 || root.Kind() != protoreflect.StringKind {
		t.Fatalf("AuditExportManifest.root_signing_key_id descriptor missing or wrong: %v", root)
	}
	if epoch := manifest.Fields().ByName("signing_key_epoch"); epoch == nil || epoch.Number() != 13 || epoch.Kind() != protoreflect.Uint64Kind {
		t.Fatalf("AuditExportManifest.signing_key_epoch descriptor missing or wrong: %v", epoch)
	}
	payloadDescriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.AuditEventPayload")
	if err != nil {
		t.Fatal(err)
	}
	payload := payloadDescriptor.(protoreflect.MessageDescriptor)
	rotation := payload.Fields().ByName("signing_key_rotated")
	if rotation == nil || rotation.Number() != 15 || rotation.Message().FullName() != "tammy.v1.SigningKeyRotatedEvent" {
		t.Fatalf("AuditEventPayload.signing_key_rotated descriptor missing or wrong: %v", rotation)
	}
	eventTypeDescriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.AuditEventType")
	if err != nil {
		t.Fatal(err)
	}
	eventType := eventTypeDescriptor.(protoreflect.EnumDescriptor)
	rotationEventType := eventType.Values().ByName("AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED")
	if rotationEventType == nil || rotationEventType.Number() != 15 {
		t.Fatalf("AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED value=%v, want 15", rotationEventType)
	}
}

func TestAuditMirrorBaselineDescriptorIsBoundedAndContainsNoCredentialProof(t *testing.T) {
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.AuditMirrorBaseline")
	if err != nil {
		t.Fatalf("find AuditMirrorBaseline descriptor: %v", err)
	}
	baseline, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatal("AuditMirrorBaseline descriptor is not a message")
	}
	for name, kind := range map[protoreflect.Name]protoreflect.Kind{
		"workspace_id": protoreflect.StringKind,
		"generation":   protoreflect.Uint64Kind,
		"sequence":     protoreflect.Uint64Kind,
		"head":         protoreflect.BytesKind,
	} {
		field := baseline.Fields().ByName(name)
		if field == nil || field.Kind() != kind {
			t.Errorf("AuditMirrorBaseline.%s descriptor missing or wrong kind", name)
		}
	}
	for _, prohibited := range []protoreflect.Name{"password", "passphrase", "totp", "recovery_proof", "private_key"} {
		if baseline.Fields().ByName(prohibited) != nil {
			t.Errorf("AuditMirrorBaseline must not retain %q", prohibited)
		}
	}
}

func TestAuditExportManifestDescriptorsCarryOnlyHashesAndPublicVerificationMaterial(t *testing.T) {
	for messageName, fields := range map[protoreflect.FullName]map[protoreflect.Name]protoreflect.Kind{
		"tammy.v1.AuditExportObject": {
			"path": protoreflect.StringKind, "sha256": protoreflect.BytesKind, "byte_length": protoreflect.Uint64Kind,
		},
		"tammy.v1.AuditExportManifest": {
			"format": protoreflect.StringKind, "workspace_id": protoreflect.StringKind,
			"generation": protoreflect.Uint64Kind, "start_sequence": protoreflect.Uint64Kind,
			"end_sequence": protoreflect.Uint64Kind, "chain_salt": protoreflect.BytesKind,
			"genesis_hash": protoreflect.BytesKind, "verified_head": protoreflect.BytesKind,
			"signing_key_id": protoreflect.StringKind, "created_at": protoreflect.MessageKind,
			"objects": protoreflect.MessageKind,
		},
	} {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(messageName)
		if err != nil {
			t.Fatalf("find %s descriptor: %v", messageName, err)
		}
		message, ok := descriptor.(protoreflect.MessageDescriptor)
		if !ok {
			t.Fatalf("%s is not a message", messageName)
		}
		for fieldName, kind := range fields {
			field := message.Fields().ByName(fieldName)
			if field == nil || field.Kind() != kind {
				t.Errorf("%s.%s descriptor missing or wrong kind", messageName, fieldName)
			}
		}
		for _, prohibited := range []protoreflect.Name{
			"private_key", "password", "passphrase", "secret", "totp_code", "destination_path",
		} {
			if message.Fields().ByName(prohibited) != nil {
				t.Errorf("%s must not retain %q", messageName, prohibited)
			}
		}
	}
}
