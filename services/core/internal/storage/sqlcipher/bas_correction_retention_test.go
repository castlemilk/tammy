//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"database/sql"
	"testing"
)

func TestBasCorrectionsRetainNormalizedFrozenTarget(t *testing.T) {
	db := newBasLifecycleConstraintFixture(t)
	rows, err := db.QueryContext(context.Background(), "PRAGMA table_info(bas_external_observation_corrections_v1)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var targetColumns int
	for rows.Next() {
		var ordinal, notNull, primaryKey int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&ordinal, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "frozen_id" || name == "frozen_hash" {
			targetColumns++
			if notNull != 1 {
				t.Errorf("correction %s must retain an exact non-null frozen target", name)
			}
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if targetColumns != 2 {
		t.Fatalf("corrections retain %d frozen target columns, want 2 for exact frozen history", targetColumns)
	}
}

func TestBasCorrectionCannotChangeFrozenTargetWithinSameFamily(t *testing.T) {
	for _, subject := range []string{"observation", "correction"} {
		t.Run(subject, func(t *testing.T) {
			db := newBasLifecycleConstraintFixture(t)
			prior := basFixtureRow(t, db, "bas_external_observation_corrections_v1", "1=1")
			other := basFixtureRow(t, db, "bas_frozen_bas_v1", "frozen_id NOT IN (SELECT frozen_id FROM bas_external_observations_v1)")
			newID := "0199c110-0000-7000-8000-000000000021"
			insertBasFamilyEvent(t, db, newID, prior["family_id"].(string), "CORRECTION", "changed-frozen-correction", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", 0x76)
			next := purchaseChanged(prior, map[string]any{"correction_id": newID, "event_id": newID, "frozen_id": other["frozen_id"], "frozen_hash": other["content_hash"],
				"content_hash": bytes.Repeat([]byte{0x76}, 32), "correction_proto": []byte("incorrect-frozen-target")})
			if subject == "correction" {
				next["disputed_observation_id"], next["disputed_observation_hash"] = nil, nil
				next["disputed_correction_id"], next["disputed_correction_hash"] = prior["correction_id"], prior["content_hash"]
			}
			purchaseInsert(t, db, "bas_external_observation_corrections_v1", next, false)
		})
	}
}

func TestBasCorrectionRetentionAcceptsPartialAndChainedReplacementAssertions(t *testing.T) {
	for _, kind := range []string{"event-kind-only", "event-time-only", "prior-correction"} {
		t.Run(kind, func(t *testing.T) {
			db := newBasLifecycleConstraintFixture(t)
			prior := basFixtureRow(t, db, "bas_external_observation_corrections_v1", "1=1")
			newID := "0199c110-0000-7000-8000-000000000001"
			insertBasFamilyEvent(t, db, newID, prior["family_id"].(string), "CORRECTION", "next-correction", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", 0x74)
			next := purchaseChanged(prior, map[string]any{"correction_id": newID, "event_id": newID, "content_hash": bytes.Repeat([]byte{0x74}, 32), "correction_proto": []byte("next-full-correction")})
			switch kind {
			case "event-kind-only":
				next["corrected_claimed_event_at"] = nil
			case "event-time-only":
				next["corrected_event_kind"] = nil
			case "prior-correction":
				next["disputed_observation_id"] = nil
				next["disputed_observation_hash"] = nil
				next["disputed_correction_id"] = prior["correction_id"]
				next["disputed_correction_hash"] = prior["content_hash"]
			}
			purchaseInsert(t, db, "bas_external_observation_corrections_v1", next, true)
			if err := migrationRelationalIntegrityCheck(context.Background(), db); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBasResolutionRetainsSeveralExactCorrectionSubjectsForOneOriginal(t *testing.T) {
	db := newBasLifecycleConstraintFixture(t)
	prior := basFixtureRow(t, db, "bas_external_observation_corrections_v1", "1=1")
	newID := "0199c110-0000-7000-8000-000000000011"
	insertBasFamilyEvent(t, db, newID, prior["family_id"].(string), "CORRECTION", "next-retraction", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", 0x74)
	next := purchaseChanged(prior, map[string]any{"correction_id": newID, "event_id": newID, "correction_kind": "RETRACT_AS_INCORRECT", "corrected_event_kind": nil, "corrected_claimed_event_at": nil,
		"content_hash": bytes.Repeat([]byte{0x74}, 32), "correction_proto": []byte("next-retraction")})
	purchaseInsert(t, db, "bas_external_observation_corrections_v1", next, true)
	resolution := basFixtureRow(t, db, "bas_dispute_resolutions_v1", "1=1")
	newResolutionID := "0199c110-0000-7000-8000-000000000012"
	insertBasFamilyEvent(t, db, newResolutionID, prior["family_id"].(string), "RESOLUTION", "next-resolution", "2026-10-01T00:00:00Z", "2026-10-09T00:00:00Z", 0x75)
	resolution = purchaseChanged(resolution, map[string]any{"resolution_id": newResolutionID, "event_id": newResolutionID, "content_hash": bytes.Repeat([]byte{0x75}, 32), "resolution_proto": []byte("several-correction-subjects")})
	purchaseInsert(t, db, "bas_dispute_resolutions_v1", resolution, true)
	for _, correction := range []map[string]any{prior, next} {
		purchaseInsert(t, db, "bas_dispute_resolution_observations_v1", map[string]any{"resolution_id": newResolutionID, "organisation_id": "org", "family_id": prior["family_id"],
			"subject_kind": "CORRECTION", "subject_id": correction["correction_id"], "subject_hash": correction["content_hash"],
			"correction_id": correction["correction_id"], "correction_hash": correction["content_hash"]}, true)
	}
	if err := migrationRelationalIntegrityCheck(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}
