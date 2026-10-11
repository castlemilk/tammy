//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestProfileRepositoryReadsExactHistoryIndependentlyFromCurrentHead(t *testing.T) {
	workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)
	profileID := testUUID(9201)

	tx := beginBASRepositoryTx(t, workspace)
	repository := newProfileRepositoryForTest(t, tx, codec)
	first := validBASProfile(organisationID, profileID, 1)
	firstRef := appendBASProfile(t, repository, first, nil)
	commitBASRepositoryTx(t, tx)

	second := nextBASProfile(first, firstRef, 2)
	tx = beginBASRepositoryTx(t, workspace)
	repository = newProfileRepositoryForTest(t, tx, codec)
	secondRef := appendBASProfile(t, repository, second, firstRef)
	commitBASRepositoryTx(t, tx)

	tx = beginBASRepositoryTx(t, workspace)
	repository = newProfileRepositoryForTest(t, tx, codec)
	old, err := repository.GetExact(ctx, organisationID, firstRef)
	if err != nil || old == nil || old.Revision != 1 || old.Profile.Reason != first.Profile.Reason {
		t.Fatalf("exact profile revision 1 = %#v, %v", old, err)
	}
	current, err := repository.GetCurrent(ctx, organisationID, profileID)
	if err != nil || current == nil || current.Revision != 2 || !bytes.Equal(current.ContentHash, secondRef.ContentHash) {
		t.Fatalf("current profile = %#v, %v", current, err)
	}
	commitBASRepositoryTx(t, tx)

	for _, testCase := range []struct {
		name string
		org  string
		edit func(*tammyv1.SourceRef)
	}{
		{name: "other organisation", org: otherOrganisationID},
		{name: "other revision", edit: func(ref *tammyv1.SourceRef) { ref.Revision++ }},
		{name: "other hash", edit: func(ref *tammyv1.SourceRef) { ref.ContentHash[0] ^= 0xff }},
		{name: "other resource", edit: func(ref *tammyv1.SourceRef) { ref.Type = "bas_period" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ref := proto.Clone(firstRef).(*tammyv1.SourceRef)
			if testCase.edit != nil {
				testCase.edit(ref)
			}
			org := organisationID
			if testCase.org != "" {
				org = testCase.org
			}
			readTx := beginBASRepositoryTx(t, workspace)
			readRepository := newProfileRepositoryForTest(t, readTx, codec)
			value, err := readRepository.GetExact(ctx, org, ref)
			if err == nil || value != nil {
				t.Fatalf("mismatched exact profile reference returned value=%#v err=%v", value, err)
			}
			commitBASRepositoryTx(t, readTx)
		})
	}
}

func TestProfileRepositoryRejectsMalformedAndNoncanonicalRetainedBytes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "malformed protobuf", mutate: func([]byte) []byte { return []byte{0xff} }},
		{name: "duplicate scalar encoding", mutate: func(wire []byte) []byte {
			wire = append(wire, protowire.AppendTag(nil, 3, protowire.VarintType)...)
			return protowire.AppendVarint(wire, 1)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			workspace, organisationID, _ := newBASRepositoryWorkspace(t)
			ctx := context.Background()
			codec := newBASRepositoryCursorCodec(t)
			profile := validBASProfile(organisationID, testUUID(9210), 1)
			tx := beginBASRepositoryTx(t, workspace)
			repository := newProfileRepositoryForTest(t, tx, codec)
			ref := appendBASProfile(t, repository, profile, nil)
			commitBASRepositoryTx(t, tx)

			wire, _, err := sealProfile(profile)
			if err != nil {
				t.Fatal(err)
			}
			tampered := testCase.mutate(bytes.Clone(wire))
			corruptTx := beginBASRepositoryTx(t, workspace)
			if _, err := corruptTx.ExecContext(ctx, `DROP TRIGGER bas_profile_versions_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := corruptTx.ExecContext(ctx, `UPDATE bas_profile_versions_v1 SET profile_proto=? WHERE profile_id=? AND version=1`,
				tampered, profile.ProfileId); err != nil {
				t.Fatal(err)
			}
			commitBASRepositoryTx(t, corruptTx)

			readTx := beginBASRepositoryTx(t, workspace)
			readRepository := newProfileRepositoryForTest(t, readTx, codec)
			value, err := readRepository.GetExact(ctx, organisationID, ref)
			if err == nil || value != nil {
				t.Fatalf("corrupt immutable history returned value=%#v err=%v", value, err)
			}
			commitBASRepositoryTx(t, readTx)
		})
	}
}

func TestProfileRepositoryRejectsDuplicateAndStaleAppendsWithoutChangingHead(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)
	profileID := testUUID(9220)

	tx := beginBASRepositoryTx(t, workspace)
	repository := newProfileRepositoryForTest(t, tx, codec)
	first := validBASProfile(organisationID, profileID, 1)
	firstRef := appendBASProfile(t, repository, first, nil)
	commitBASRepositoryTx(t, tx)

	second := nextBASProfile(first, firstRef, 2)
	tx = beginBASRepositoryTx(t, workspace)
	repository = newProfileRepositoryForTest(t, tx, codec)
	secondRef := appendBASProfile(t, repository, second, firstRef)
	commitBASRepositoryTx(t, tx)

	for _, testCase := range []struct {
		name     string
		profile  *tammyv1.BasObligationsProfile
		expected *tammyv1.SourceRef
	}{
		{name: "duplicate first revision", profile: validBASProfile(organisationID, profileID, 1)},
		{name: "stale expected head", profile: nextBASProfile(first, firstRef, 3), expected: firstRef},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			appendTx := beginBASRepositoryTx(t, workspace)
			appendRepository := newProfileRepositoryForTest(t, appendTx, codec)
			if ref, err := appendRepository.Append(ctx, testCase.profile, testCase.expected); err == nil || ref != nil {
				t.Fatalf("conflicting append returned ref=%#v err=%v", ref, err)
			}
			// The repository never owns the transaction. Its caller rolls back the failed command.
			if err := appendTx.Rollback(); err != nil {
				t.Fatal(err)
			}

			readTx := beginBASRepositoryTx(t, workspace)
			readRepository := newProfileRepositoryForTest(t, readTx, codec)
			current, err := readRepository.GetCurrent(ctx, organisationID, profileID)
			if err != nil || current == nil || current.Revision != 2 || !bytes.Equal(current.ContentHash, secondRef.ContentHash) {
				t.Fatalf("head after caller rollback = %#v, %v", current, err)
			}
			commitBASRepositoryTx(t, readTx)
		})
	}
}

func TestProfileRepositoryHistoryPagesAreStableSignedAndCountExactRows(t *testing.T) {
	workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)

	writeTx := beginBASRepositoryTx(t, workspace)
	writeRepository := newProfileRepositoryForTest(t, writeTx, codec)
	first := validBASProfile(organisationID, testUUID(9231), 1)
	firstRef := appendBASProfile(t, writeRepository, first, nil)
	appendBASProfile(t, writeRepository, nextBASProfile(first, firstRef, 2), firstRef)
	appendBASProfile(t, writeRepository, validBASProfile(organisationID, testUUID(9232), 1), nil)
	appendBASProfile(t, writeRepository, validBASProfile(organisationID, testUUID(9233), 1), nil)
	commitBASRepositoryTx(t, writeTx)

	readTx := beginBASRepositoryTx(t, workspace)
	readRepository := newProfileRepositoryForTest(t, readTx, codec)
	firstPage, err := readRepository.List(ctx, ProfileListRequest{OrganisationID: organisationID, PageSize: 2})
	if err != nil || firstPage == nil || firstPage.Page == nil {
		t.Fatalf("first profile history page = %#v, %v", firstPage, err)
	}
	if got := firstPage.Page.ReturnedCount; got != uint32(len(firstPage.Profiles)) || got != 2 {
		t.Fatalf("first page returned_count=%d rows=%d", got, len(firstPage.Profiles))
	}
	if firstPage.Page.NextCursor == nil || firstPage.Profiles[0].Profile.Revision != 1 || firstPage.Profiles[1].Profile.Revision != 2 {
		t.Fatalf("profile history first page should retain both ordered revisions: %#v", firstPage)
	}
	cursor := *firstPage.Page.NextCursor
	commitBASRepositoryTx(t, readTx)

	// Append after page one; the signed watermark must keep this new history out of page two.
	writeTx = beginBASRepositoryTx(t, workspace)
	writeRepository = newProfileRepositoryForTest(t, writeTx, codec)
	appendBASProfile(t, writeRepository, validBASProfile(organisationID, testUUID(9234), 1), nil)
	commitBASRepositoryTx(t, writeTx)

	newCodec := newBASRepositoryCursorCodec(t) // same durable signing key after a process/repository restart
	pageTwoTx := beginBASRepositoryTx(t, workspace)
	pageTwoRepository := newProfileRepositoryForTest(t, pageTwoTx, newCodec)
	pageTwo, err := pageTwoRepository.List(ctx, ProfileListRequest{OrganisationID: organisationID, PageSize: 2, Cursor: &cursor})
	if err != nil || pageTwo == nil || pageTwo.Page == nil {
		t.Fatalf("second profile history page after restart = %#v, %v", pageTwo, err)
	}
	if got := pageTwo.Page.ReturnedCount; got != uint32(len(pageTwo.Profiles)) || got != 2 {
		t.Fatalf("second page returned_count=%d rows=%d", got, len(pageTwo.Profiles))
	}
	if pageTwo.Page.NextCursor != nil || pageTwo.Profiles[0].Profile.Id != testUUID(9232) || pageTwo.Profiles[1].Profile.Id != testUUID(9233) {
		t.Fatalf("page two must resume the fixed append snapshot: %#v", pageTwo)
	}
	commitBASRepositoryTx(t, pageTwoTx)

	for _, testCase := range []struct {
		name string
		org  string
		edit func(*string)
	}{
		{name: "other organisation", org: otherOrganisationID},
		{name: "tampered HMAC token", org: organisationID, edit: func(token *string) {
			position := len(*token) / 2
			changed := []byte(*token)
			if changed[position] == 'A' {
				changed[position] = 'B'
			} else {
				changed[position] = 'A'
			}
			*token = string(changed)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			badCursor := cursor
			if testCase.edit != nil {
				testCase.edit(&badCursor)
			}
			badTx := beginBASRepositoryTx(t, workspace)
			badRepository := newProfileRepositoryForTest(t, badTx, newCodec)
			page, err := badRepository.List(ctx, ProfileListRequest{OrganisationID: testCase.org, PageSize: 2, Cursor: &badCursor})
			if err == nil || page != nil {
				t.Fatalf("invalid page scope/token returned %#v, %v", page, err)
			}
			commitBASRepositoryTx(t, badTx)
		})
	}
}

func newBASRepositoryWorkspace(t *testing.T) (*testkit.EncryptedWorkspace, string, string) {
	t.Helper()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	organisationID, otherOrganisationID := testUUID(9101), testUUID(9102)
	seedBASRepositoryOrganisation(t, workspace, organisationID)
	if _, err := workspace.Database.ExecContext(context.Background(), `DROP TRIGGER organisations_singleton_insert`); err != nil {
		t.Fatal(err)
	}
	seedBASRepositoryOrganisation(t, workspace, otherOrganisationID)
	if _, err := workspace.Database.ExecContext(context.Background(), `CREATE TRIGGER organisations_singleton_insert BEFORE INSERT ON organisations WHEN EXISTS(SELECT 1 FROM organisations) BEGIN SELECT RAISE(ABORT,'workspace already has an organisation'); END`); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{testUUID(36), testUUID(72)} {
		if _, err := workspace.Database.ExecContext(context.Background(), `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES(?,?,?,'ACTIVE',?,?)`,
			actor, actor+"@example.invalid", "Retained test actor", "2026-10-11T00:00:00Z", "2026-10-11T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	return workspace, organisationID, otherOrganisationID
}

func seedBASRepositoryOrganisation(t *testing.T, workspace *testkit.EncryptedWorkspace, organisationID string) {
	t.Helper()
	_, err := workspace.Database.ExecContext(context.Background(),
		`INSERT INTO organisations(id,legal_name,status,created_at) VALUES(?,?,?,?)`,
		organisationID, "BAS Repository Test "+organisationID, "ACTIVE", "2026-10-11T00:00:00Z")
	if err != nil {
		t.Fatalf("seed BAS repository organisation: %v", err)
	}
}

func beginBASRepositoryTx(t *testing.T, workspace *testkit.EncryptedWorkspace) *sqlcipher.Transaction {
	t.Helper()
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin caller-owned BAS repository transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func commitBASRepositoryTx(t *testing.T, tx *sqlcipher.Transaction) {
	t.Helper()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit caller-owned BAS repository transaction: %v", err)
	}
}

func newBASRepositoryCursorCodec(t *testing.T) *paging.Codec {
	t.Helper()
	codec, err := paging.NewCodec(bytes.Repeat([]byte{0x71}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func newProfileRepositoryForTest(t *testing.T, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, codec *paging.Codec) *ProfileRepository {
	t.Helper()
	repository, err := NewProfileRepository(executor, codec)
	if err != nil {
		t.Fatalf("construct profile repository: %v", err)
	}
	return repository
}

func validBASProfile(organisationID, profileID string, revision uint64) *tammyv1.BasObligationsProfile {
	profile := obligationsProfile(organisationID)
	profile.ProfileId = profileID
	profile.Revision = revision
	profile.ContentHash = nil
	profile.PreviousProfile = nil
	return profile
}

func nextBASProfile(previous *tammyv1.BasObligationsProfile, previousRef *tammyv1.SourceRef, revision uint64) *tammyv1.BasObligationsProfile {
	profile := proto.Clone(previous).(*tammyv1.BasObligationsProfile)
	profile.Revision = revision
	profile.ContentHash = nil
	profile.PreviousProfile = proto.Clone(previousRef).(*tammyv1.SourceRef)
	profile.Profile.Reason = fmt.Sprintf("complete profile revision %d", revision)
	return profile
}

func appendBASProfile(t *testing.T, repository *ProfileRepository, profile *tammyv1.BasObligationsProfile, expected *tammyv1.SourceRef) *tammyv1.SourceRef {
	t.Helper()
	ref, err := repository.Append(context.Background(), profile, expected)
	if err != nil {
		t.Fatalf("append BAS profile revision %d: %v", profile.Revision, err)
	}
	if ref == nil || ref.Type != "bas_obligations_profile" || ref.Id != profile.ProfileId || ref.Revision != profile.Revision || len(ref.ContentHash) != 32 {
		t.Fatalf("profile append reference = %#v", ref)
	}
	return ref
}
