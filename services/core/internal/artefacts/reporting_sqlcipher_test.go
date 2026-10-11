//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package artefacts_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

const (
	ruleReportingOrganisationID = "018f0000-0000-7000-8000-000000000901"
	ruleReportingOwnerID        = "018f0000-0000-7000-8000-000000000902"
	ruleReportingPayloadLimit   = 8 * 1024 * 1024
)

func TestRuleRepositoryGetForReportingReturnsCompleteDeterministicProof(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)

	record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, proto.Clone(bundle.Source).(*tammyv1.SourceRef))
	if err != nil {
		t.Fatalf("GetForReporting() = %v", err)
	}
	assertCompleteRuleReportingRecord(t, record, bundle, "AU_GST", fixture.now.Format(time.RFC3339Nano), nil)

	semanticDigest := sha256.Sum256(bundle.RetainedBytes)
	if !bytes.Equal(record.Bundle.Source.ContentHash, semanticDigest[:]) ||
		record.SemanticSHA256 != hex.EncodeToString(semanticDigest[:]) {
		t.Fatalf("original retained bytes hash = source:%x semantic:%q; want %x", record.Bundle.Source.ContentHash,
			record.SemanticSHA256, semanticDigest)
	}
	if bytes.Equal(record.ContentHash, semanticDigest[:]) {
		t.Fatalf("computed complete proof hash reused the source's original bytes hash: %x", record.ContentHash)
	}
	for _, code := range record.Catalogue {
		if code.EffectiveFrom != bundle.EffectiveFrom || code.EffectiveTo != nil {
			t.Fatalf("unfiltered raw catalogue window for %q = (%q,%v); want (%q,NULL)",
				code.Code, code.EffectiveFrom, code.EffectiveTo, bundle.EffectiveFrom)
		}
	}

	repeated, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, proto.Clone(bundle.Source).(*tammyv1.SourceRef))
	if err != nil || repeated == nil {
		t.Fatalf("repeated GetForReporting() = %#v, %v", repeated, err)
	}
	if !bytes.Equal(record.ContentHash, repeated.ContentHash) || record.PayloadBytes != repeated.PayloadBytes {
		t.Fatalf("repeated proof changed hash or payload size: first=(%x,%d) repeated=(%x,%d)",
			record.ContentHash, record.PayloadBytes, repeated.ContentHash, repeated.PayloadBytes)
	}
	if len(record.Catalogue) != len(repeated.Catalogue) {
		t.Fatalf("repeated proof changed catalogue row count: first=%d repeated=%d", len(record.Catalogue), len(repeated.Catalogue))
	}
	for index := range record.Catalogue {
		if record.Catalogue[index].ID != repeated.Catalogue[index].ID ||
			record.Catalogue[index].Code != repeated.Catalogue[index].Code {
			t.Fatalf("repeated proof changed catalogue order at %d: first=%#v repeated=%#v",
				index, record.Catalogue[index], repeated.Catalogue[index])
		}
	}
}

func TestRuleRepositoryGetForReportingBindsOrganisationAndExactSourceRef(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)

	tests := []struct {
		name           string
		organisationID string
		source         *tammyv1.SourceRef
	}{
		{name: "wrong organisation", organisationID: "018f0000-0000-7000-8000-000000000902", source: proto.Clone(bundle.Source).(*tammyv1.SourceRef)},
		{name: "wrong source type", organisationID: ruleReportingOrganisationID, source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.Type = "other_rule" })},
		{name: "wrong source id", organisationID: ruleReportingOrganisationID, source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.Id = "018f0000-0000-7000-8000-000000000999" })},
		{name: "wrong source revision", organisationID: ruleReportingOrganisationID, source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.Revision++ })},
		{name: "wrong source bytes hash", organisationID: ruleReportingOrganisationID, source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.ContentHash[0] ^= 0xff })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, err := fixture.repository.GetForReporting(fixture.ctx, test.organisationID, test.source)
			if err == nil || record != nil {
				t.Fatalf("GetForReporting() = %#v, %v; want error and nil record", record, err)
			}
		})
	}
}

func TestRuleRepositoryGetForReportingPreservesNullableExpiry(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}
	parentExpiry := "2025-12-31"
	const catalogueStart = "2022-01-01"
	const expiredCatalogueEnd = "2024-06-30"
	catalogue := ruleReportingCatalogueRows(bundle)
	catalogue[0].EffectiveFrom = catalogueStart
	catalogue[0].EffectiveTo = expiredCatalogueEnd
	insertRuleReportingBundleRows(t, fixture, bundle, "AU_GST", bundle.Version,
		hex.EncodeToString(bundle.Source.ContentHash), bundle.EffectiveFrom, parentExpiry,
		fixture.now.Format(time.RFC3339Nano), catalogue)

	record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, proto.Clone(bundle.Source).(*tammyv1.SourceRef))
	if err != nil {
		t.Fatalf("GetForReporting() = %v", err)
	}
	assertCompleteRuleReportingRecord(t, record, bundle, "AU_GST", fixture.now.Format(time.RFC3339Nano), &parentExpiry)
	for _, code := range record.Catalogue {
		if code.Code == bundle.TaxCodes[0].Code {
			if code.EffectiveFrom != catalogueStart || code.EffectiveTo == nil || *code.EffectiveTo != expiredCatalogueEnd {
				t.Fatalf("independent expired catalogue window for %q = (%q,%v); want (%q,%q)",
					code.Code, code.EffectiveFrom, code.EffectiveTo, catalogueStart, expiredCatalogueEnd)
			}
		} else if code.EffectiveFrom != bundle.EffectiveFrom || code.EffectiveTo != nil {
			t.Fatalf("independent catalogue window for %q = (%q,%v); want (%q,NULL)",
				code.Code, code.EffectiveFrom, code.EffectiveTo, bundle.EffectiveFrom)
		}
	}

	baseFixture := newRuleReportingFixture(t)
	baseBundle := retainReportingBundle(t, baseFixture)
	baseRecord, err := baseFixture.repository.GetForReporting(baseFixture.ctx,
		ruleReportingOrganisationID, baseBundle.Source)
	if err != nil || baseRecord == nil {
		t.Fatalf("GetForReporting() for NULL timing metadata = %#v, %v", baseRecord, err)
	}
	if !bytes.Equal(baseRecord.Bundle.Source.ContentHash, record.Bundle.Source.ContentHash) {
		t.Fatalf("comparison fixtures have different original source hashes: null=%x present=%x",
			baseRecord.Bundle.Source.ContentHash, record.Bundle.Source.ContentHash)
	}
	if bytes.Equal(baseRecord.ContentHash, record.ContentHash) {
		t.Fatalf("changing only raw nullable expiry windows left proof hash unchanged: %x", record.ContentHash)
	}
}

func TestRuleRepositoryGetForReportingAcceptsTwoHundredAndBoundsTwoHundredOneCatalogueRows(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := makeRuleReportingBundleWithCodeCount(t, 200,
		"018f0000-0000-7000-8000-000000000912", 1000)
	if err := fixture.repository.Retain(fixture.ctx, ruleReportingOrganisationID, bundle, fixture.now); err != nil {
		t.Fatalf("retain 200-code rule bundle: %v", err)
	}
	full, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if err != nil || full == nil || len(full.Catalogue) != 200 {
		t.Fatalf("200-row reporting proof = %#v, %v; want all 200 catalogue rows", full, err)
	}

	if _, err := fixture.tx.ExecContext(fixture.ctx, `INSERT INTO tax_code_catalogue(
		id,code,rule_bundle_id,label,rate_millionths,treatment,effective_from,effective_to)
		VALUES(?,?,?,?,?,?,?,NULL)`, "018f0000-0000-7000-8000-000000001250", "OVER_LIMIT",
		bundle.Source.Id, "Over limit fixture", 0, "GST_FREE", bundle.EffectiveFrom); err != nil {
		t.Fatalf("insert cap+1 immutable catalogue row: %v", err)
	}
	tooMany, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	assertRuleReportingBound(t, tooMany, err, 200)
}

func TestRuleRepositoryGetForReportingRejectsParentMetadataMismatch(t *testing.T) {
	for _, test := range []struct {
		name       string
		bundleType string
		version    string
		semantic   string
		effective  string
		expiry     any
	}{
		{name: "bundle type", bundleType: "OTHER", version: "au_gst_v1", effective: "2000-07-01"},
		{name: "version", bundleType: "AU_GST", version: "au_gst_v2", effective: "2000-07-01"},
		{name: "semantic hash", bundleType: "AU_GST", version: "au_gst_v1", semantic: strings.Repeat("0", 64), effective: "2000-07-01"},
		{name: "effective start", bundleType: "AU_GST", version: "au_gst_v1", effective: "2000-07-02"},
		{name: "expiry before effective start", bundleType: "AU_GST", version: "au_gst_v1", effective: "2000-07-01", expiry: "1999-12-31"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuleReportingFixture(t)
			embedded, err := artefacts.LoadAUGSTV1()
			if err != nil {
				t.Fatal(err)
			}
			bundle := cloneRuleReportingBundleWithNewIdentity(t, embedded,
				"018f0000-0000-7000-8000-000000000913", 1100)
			semantic := test.semantic
			if semantic == "" {
				semantic = hex.EncodeToString(bundle.Source.ContentHash)
			}
			insertRuleReportingBundleRows(t, fixture, bundle, test.bundleType, test.version,
				semantic, test.effective, test.expiry, fixture.now.Format(time.RFC3339Nano), ruleReportingCatalogueRows(bundle))

			record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
			if err == nil || record != nil {
				t.Fatalf("GetForReporting() = %#v, %v; want fail-closed parent mismatch", record, err)
			}
		})
	}
}

func TestRuleRepositoryGetForReportingRejectsEachCatalogueSourceMismatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*ruleReportingCatalogueSQLRow)
	}{
		{name: "id", mutate: func(row *ruleReportingCatalogueSQLRow) { row.ID = "018f0000-0000-7000-8000-000000001299" }},
		{name: "code", mutate: func(row *ruleReportingCatalogueSQLRow) { row.Code = "ALTERED" }},
		{name: "label", mutate: func(row *ruleReportingCatalogueSQLRow) { row.Label = "Changed label" }},
		{name: "treatment", mutate: func(row *ruleReportingCatalogueSQLRow) { row.Treatment = "GST_FREE" }},
		{name: "rate", mutate: func(row *ruleReportingCatalogueSQLRow) { row.RateMillionths = 99999 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuleReportingFixture(t)
			embedded, err := artefacts.LoadAUGSTV1()
			if err != nil {
				t.Fatal(err)
			}
			bundle := cloneRuleReportingBundleWithNewIdentity(t, embedded,
				"018f0000-0000-7000-8000-000000000914", 1200)
			catalogue := ruleReportingCatalogueRows(bundle)
			test.mutate(&catalogue[0])
			insertRuleReportingBundleRows(t, fixture, bundle, "AU_GST", bundle.Version,
				hex.EncodeToString(bundle.Source.ContentHash), bundle.EffectiveFrom, nil,
				fixture.now.Format(time.RFC3339Nano), catalogue)

			record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
			if err == nil || record != nil {
				t.Fatalf("GetForReporting() = %#v, %v; want fail-closed catalogue mismatch", record, err)
			}
		})
	}
}

func TestRuleRepositoryGetForReportingRejectsInvalidCatalogueDateWindow(t *testing.T) {
	for _, test := range []struct {
		name       string
		start, end string
	}{
		{name: "malformed start", start: "not-a-civil-date"},
		{name: "start before parent", start: "1999-12-31"},
		{name: "end before start", start: "2022-01-01", end: "2021-12-31"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuleReportingFixture(t)
			embedded, err := artefacts.LoadAUGSTV1()
			if err != nil {
				t.Fatal(err)
			}
			bundle := cloneRuleReportingBundleWithNewIdentity(t, embedded,
				"018f0000-0000-7000-8000-000000000915", 1300)
			catalogue := ruleReportingCatalogueRows(bundle)
			catalogue[0].EffectiveFrom = test.start
			if test.end != "" {
				catalogue[0].EffectiveTo = test.end
			}
			insertRuleReportingBundleRows(t, fixture, bundle, "AU_GST", bundle.Version,
				hex.EncodeToString(bundle.Source.ContentHash), bundle.EffectiveFrom, nil,
				fixture.now.Format(time.RFC3339Nano), catalogue)

			record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
			if err == nil || record != nil {
				t.Fatalf("invalid catalogue date read = %#v, %v; want a closed error", record, err)
			}
		})
	}
}

func TestRuleRepositoryGetForReportingEnforcesPayloadBudgets(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)

	full, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if err != nil || full == nil || full.PayloadBytes <= 0 || full.PayloadBytes >= ruleReportingPayloadLimit {
		t.Fatalf("GetForReporting() = %#v, %v; want a positive payload below the shared cap", full, err)
	}

	exact, err := fixture.repository.GetForReportingWithPayloadBudget(fixture.ctx,
		ruleReportingOrganisationID, bundle.Source, full.PayloadBytes)
	if err != nil || exact == nil || exact.PayloadBytes != full.PayloadBytes || !bytes.Equal(exact.ContentHash, full.ContentHash) {
		t.Fatalf("exact-budget read = %#v, %v; want the complete stable proof", exact, err)
	}

	below, err := fixture.repository.GetForReportingWithPayloadBudget(fixture.ctx,
		ruleReportingOrganisationID, bundle.Source, full.PayloadBytes-1)
	assertRuleReportingBound(t, below, err, full.PayloadBytes-1)

	zero, err := fixture.repository.GetForReportingWithPayloadBudget(fixture.ctx,
		ruleReportingOrganisationID, bundle.Source, 0)
	assertRuleReportingBound(t, zero, err, 0)

	negative, err := fixture.repository.GetForReportingWithPayloadBudget(fixture.ctx,
		ruleReportingOrganisationID, bundle.Source, -1)
	if err == nil || negative != nil {
		t.Fatalf("negative-budget read = %#v, %v; want an invalid-argument error and nil proof", negative, err)
	}
}

func TestRuleRepositoryGetForReportingBoundsUTF8OversizedRetainedBytes(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	embedded, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}
	bundle := makeRuleReportingUTF8OversizedBundle(t, embedded)
	if err := fixture.repository.Retain(fixture.ctx, ruleReportingOrganisationID, bundle, fixture.now); err != nil {
		t.Fatalf("retain legal padded UTF-8 bundle: %v", err)
	}

	record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	assertRuleReportingBound(t, record, err, ruleReportingPayloadLimit)

	limited, err := fixture.repository.GetForReportingWithPayloadBudget(fixture.ctx,
		ruleReportingOrganisationID, bundle.Source, 1024)
	assertRuleReportingBound(t, limited, err, 1024)
}

func TestRuleRepositoryGetForReportingHonorsCanceledContext(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)
	canceled, cancel := context.WithCancel(fixture.ctx)
	cancel()

	record, err := fixture.repository.GetForReporting(canceled, ruleReportingOrganisationID, bundle.Source)
	if !errors.Is(err, context.Canceled) || record != nil {
		t.Fatalf("canceled GetForReporting() = %#v, %v; want context.Canceled and nil record", record, err)
	}
}

func TestRuleRepositoryGetForReportingLeavesCallerTransactionOpenAndRollbackEffective(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)

	record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if err != nil || record == nil {
		t.Fatalf("read inside caller transaction = %#v, %v", record, err)
	}
	if !fixture.tx.Authenticated() {
		t.Fatal("GetForReporting ended the caller-owned encrypted transaction")
	}
	if err := fixture.tx.Rollback(); err != nil {
		t.Fatalf("rollback caller transaction: %v", err)
	}
	fixture.tx = nil
	if err := fixture.workspace.Database.Close(); err != nil {
		t.Fatalf("close workspace after caller rollback: %v", err)
	}

	reopened, err := sqlcipher.Open(fixture.ctx, fixture.workspace.Path, fixture.workspace.Key)
	if err != nil {
		t.Fatalf("reopen encrypted workspace: %v", err)
	}
	defer reopened.Close()
	readTx, err := reopened.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin post-rollback read transaction: %v", err)
	}
	defer readTx.Rollback()
	readRepository, err := artefacts.NewRepository(readTx)
	if err != nil {
		t.Fatal(err)
	}
	afterRollback, err := readRepository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if err == nil || afterRollback != nil {
		t.Fatalf("proof after caller rollback = %#v, %v; want absent row and nil proof", afterRollback, err)
	}
}

func TestRuleRepositoryGetForReportingStableAcrossEncryptedReopenWithoutRevisionWrites(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)
	beforeRevision := ruleReportingRevisionState(t, fixture.ctx, fixture.tx)

	first, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if err != nil || first == nil {
		t.Fatalf("initial GetForReporting() = %#v, %v", first, err)
	}
	second, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if err != nil || second == nil || !bytes.Equal(first.ContentHash, second.ContentHash) {
		t.Fatalf("repeated GetForReporting() = %#v, %v; want stable proof %x", second, err, first.ContentHash)
	}
	afterRevision := ruleReportingRevisionState(t, fixture.ctx, fixture.tx)
	if beforeRevision != afterRevision {
		t.Fatalf("read projection changed financial revisions: before=%#v after=%#v", beforeRevision, afterRevision)
	}
	if err := fixture.tx.Commit(); err != nil {
		t.Fatalf("commit caller-owned retained rule transaction: %v", err)
	}
	fixture.tx = nil
	if err := fixture.workspace.Database.Close(); err != nil {
		t.Fatalf("close encrypted workspace: %v", err)
	}

	reopened, err := sqlcipher.Open(fixture.ctx, fixture.workspace.Path, fixture.workspace.Key)
	if err != nil {
		t.Fatalf("reopen encrypted workspace: %v", err)
	}
	defer reopened.Close()
	readTx, err := reopened.BeginEncryptedTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin reopened read transaction: %v", err)
	}
	defer readTx.Rollback()
	readRepository, err := artefacts.NewRepository(readTx)
	if err != nil {
		t.Fatal(err)
	}
	afterReopen, err := readRepository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if err != nil || afterReopen == nil || !bytes.Equal(first.ContentHash, afterReopen.ContentHash) {
		t.Fatalf("GetForReporting() after encrypted reopen = %#v, %v; want stable proof %x", afterReopen, err, first.ContentHash)
	}
	if reopenedRevision := ruleReportingRevisionState(t, fixture.ctx, readTx); beforeRevision != reopenedRevision {
		t.Fatalf("reopened read changed financial revisions: before=%#v after=%#v", beforeRevision, reopenedRevision)
	}
}

type ruleReportingFixture struct {
	ctx        context.Context
	workspace  *testkit.EncryptedWorkspace
	tx         *sqlcipher.Transaction
	repository *artefacts.Repository
	now        time.Time
}

func newRuleReportingFixture(t *testing.T) *ruleReportingFixture {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin schema-13 encrypted transaction: %v", err)
	}
	fixture := &ruleReportingFixture{ctx: ctx, workspace: workspace, tx: tx,
		now: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
	t.Cleanup(func() {
		if fixture.tx != nil {
			_ = fixture.tx.Rollback()
		}
	})
	createdAt := fixture.now.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(
		id,email,display_name,status,created_at,updated_at) VALUES(?,?,'Rule Owner','ACTIVE',?,?)`,
		ruleReportingOwnerID, "rule-owner@example.test", createdAt, createdAt); err != nil {
		t.Fatalf("insert actual organisation owner: %v", err)
	}
	activeRule, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatalf("load active AU GST rule: %v", err)
	}

	organisationRepository, err := organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	profile := &tammyv1.Organisation{
		Id:                    ruleReportingOrganisationID,
		Version:               1,
		Abn:                   "51824753556",
		LegalName:             "Tammy Business Pty Ltd",
		DisplayName:           "Tammy",
		EntityType:            "AU_PRIVATE_COMPANY",
		GstBasis:              tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6,
		OwnerUserId:           ruleReportingOwnerID,
		VerificationState:     tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		ActiveTaxRuleBundle:   proto.Clone(activeRule.Source).(*tammyv1.SourceRef),
	}
	if err := organisationRepository.Create(ctx, profile, fixture.now); err != nil {
		t.Fatalf("create actual organisation: %v", err)
	}
	fixture.repository, err = artefacts.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func retainReportingBundle(t *testing.T, fixture *ruleReportingFixture) artefacts.RuleBundle {
	t.Helper()
	bundle, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.Retain(fixture.ctx, ruleReportingOrganisationID, bundle, fixture.now); err != nil {
		t.Fatalf("retain actual AU GST rule bundle: %v", err)
	}
	return bundle
}

func assertCompleteRuleReportingRecord(
	t *testing.T,
	record *artefacts.RuleBundleReportingRecord,
	wantBundle artefacts.RuleBundle,
	wantType, wantRetainedAt string,
	wantExpiry *string,
) {
	t.Helper()
	if record == nil || record.Bundle.Source == nil || len(record.ContentHash) != sha256.Size {
		t.Fatalf("rule reporting record = %#v; want complete record and SHA-256 proof", record)
	}
	if record.OrganisationID != ruleReportingOrganisationID || record.BundleType != wantType ||
		record.SemanticSHA256 != hex.EncodeToString(wantBundle.Source.ContentHash) ||
		record.RetainedAt != wantRetainedAt || !sameOptionalRuleDate(record.EffectiveTo, wantExpiry) {
		t.Fatalf("raw rule parent metadata = %#v", record)
	}
	if record.Bundle.Version != wantBundle.Version || record.Bundle.EffectiveFrom != wantBundle.EffectiveFrom ||
		record.Bundle.Source.Type != wantBundle.Source.Type || record.Bundle.Source.Id != wantBundle.Source.Id ||
		record.Bundle.Source.Revision != wantBundle.Source.Revision ||
		!bytes.Equal(record.Bundle.Source.ContentHash, wantBundle.Source.ContentHash) ||
		!bytes.Equal(record.Bundle.RetainedBytes, wantBundle.RetainedBytes) ||
		len(record.Bundle.TaxCodes) != len(wantBundle.TaxCodes) {
		t.Fatalf("parsed bundle projection = %#v; want exact retained parsed bundle %#v", record.Bundle, wantBundle)
	}
	if len(record.Catalogue) != len(wantBundle.TaxCodes) || record.PayloadBytes <= 0 || record.PayloadBytes > ruleReportingPayloadLimit {
		t.Fatalf("catalogue count or payload bound = count:%d bytes:%d; want %d catalogue rows within cap", len(record.Catalogue),
			record.PayloadBytes, len(wantBundle.TaxCodes))
	}

	expected := make(map[string]artefacts.TaxCodeRule, len(wantBundle.TaxCodes))
	for _, code := range wantBundle.TaxCodes {
		expected[code.Code] = code
	}
	seen := make(map[string]struct{}, len(record.Catalogue))
	for _, actual := range record.Catalogue {
		want, ok := expected[actual.Code]
		if !ok {
			t.Fatalf("unexpected retained catalogue row %#v", actual)
		}
		if _, duplicate := seen[actual.Code]; duplicate {
			t.Fatalf("duplicate catalogue code %q", actual.Code)
		}
		seen[actual.Code] = struct{}{}
		if actual.ID != want.ID || actual.RuleBundleID != wantBundle.Source.Id || actual.Label != want.Label ||
			actual.Treatment != ruleReportingTreatmentName(want.Treatment) || actual.RateMillionths != want.RateMillionths ||
			actual.EffectiveFrom == "" {
			t.Fatalf("raw catalogue row for %q = %#v; want parsed source fields and valid source timing metadata", actual.Code, actual)
		}
	}
}

func assertRuleReportingBound(t *testing.T, record *artefacts.RuleBundleReportingRecord, err error, limit int64) {
	t.Helper()
	if !errors.Is(err, artefacts.ErrRuleReportingBound) || record != nil {
		t.Fatalf("bounded GetForReporting() = %#v, %v; want ErrRuleReportingBound and no partial record at limit %d",
			record, err, limit)
	}
}

func mutateRuleReportingSource(source *tammyv1.SourceRef, mutate func(*tammyv1.SourceRef)) *tammyv1.SourceRef {
	copy := proto.Clone(source).(*tammyv1.SourceRef)
	mutate(copy)
	return copy
}

func sameOptionalRuleDate(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func ruleReportingTreatmentName(treatment tammyv1.TaxTreatment) string {
	switch treatment {
	case tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE:
		return "TAXABLE"
	case tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE:
		return "GST_FREE"
	case tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED:
		return "INPUT_TAXED"
	case tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE:
		return "OUT_OF_SCOPE"
	default:
		return "UNSPECIFIED"
	}
}

func cloneRuleReportingBundleWithNewIdentity(
	t *testing.T,
	bundle artefacts.RuleBundle,
	newBundleID string,
	firstCodeID int,
) artefacts.RuleBundle {
	t.Helper()
	var document struct {
		BundleID      string `json:"bundle_id"`
		Version       string `json:"version"`
		Revision      uint64 `json:"revision"`
		EffectiveFrom string `json:"effective_from"`
		TaxCodes      []struct {
			ID             string `json:"id"`
			Code           string `json:"code"`
			Label          string `json:"label"`
			Treatment      string `json:"treatment"`
			RateMillionths int64  `json:"rate_millionths"`
		} `json:"tax_codes"`
	}
	if err := json.Unmarshal(bundle.RetainedBytes, &document); err != nil {
		t.Fatalf("decode embedded bundle identity fixture: %v", err)
	}
	document.BundleID = newBundleID
	for index := range document.TaxCodes {
		document.TaxCodes[index].ID = fmt.Sprintf("018f0000-0000-7000-8000-%012d", firstCodeID+index)
	}
	retained, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode distinct retained bundle fixture: %v", err)
	}
	digest := sha256.Sum256(retained)
	cloned, err := artefacts.ParseRuleBundle(retained, digest[:])
	if err != nil {
		t.Fatalf("parse distinct retained bundle fixture: %v", err)
	}
	return cloned
}

type ruleReportingCatalogueSQLRow struct {
	ID, Code, Label, Treatment, EffectiveFrom string
	RateMillionths                            int64
	EffectiveTo                               any
}

func ruleReportingCatalogueRows(bundle artefacts.RuleBundle) []ruleReportingCatalogueSQLRow {
	rows := make([]ruleReportingCatalogueSQLRow, 0, len(bundle.TaxCodes))
	for _, code := range bundle.TaxCodes {
		rows = append(rows, ruleReportingCatalogueSQLRow{ID: code.ID, Code: code.Code, Label: code.Label,
			Treatment: ruleReportingTreatmentName(code.Treatment), RateMillionths: code.RateMillionths,
			EffectiveFrom: bundle.EffectiveFrom})
	}
	return rows
}

func insertRuleReportingBundleRows(
	t *testing.T,
	fixture *ruleReportingFixture,
	bundle artefacts.RuleBundle,
	bundleType, version, semantic, effectiveFrom string,
	effectiveTo any,
	retainedAt string,
	catalogue []ruleReportingCatalogueSQLRow,
) {
	t.Helper()
	if _, err := fixture.tx.ExecContext(fixture.ctx, `INSERT INTO rule_bundles(
		id,organisation_id,bundle_type,version,semantic_sha256,rules_proto,effective_from,effective_to,retained_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, bundle.Source.Id, ruleReportingOrganisationID, bundleType, version,
		semantic, bundle.RetainedBytes, effectiveFrom, effectiveTo, retainedAt); err != nil {
		t.Fatalf("insert immutable rule bundle fixture with expiry: %v", err)
	}
	for _, code := range catalogue {
		if _, err := fixture.tx.ExecContext(fixture.ctx, `INSERT INTO tax_code_catalogue(
			id,code,rule_bundle_id,label,rate_millionths,treatment,effective_from,effective_to)
			VALUES(?,?,?,?,?,?,?,?)`, code.ID, code.Code, bundle.Source.Id, code.Label, code.RateMillionths,
			code.Treatment, code.EffectiveFrom, code.EffectiveTo); err != nil {
			t.Fatalf("insert immutable catalogue fixture %q: %v", code.Code, err)
		}
	}
}

func makeRuleReportingBundleWithCodeCount(
	t *testing.T,
	count int,
	bundleID string,
	firstCodeID int,
) artefacts.RuleBundle {
	t.Helper()
	document := struct {
		BundleID      string `json:"bundle_id"`
		Version       string `json:"version"`
		Revision      uint64 `json:"revision"`
		EffectiveFrom string `json:"effective_from"`
		TaxCodes      []struct {
			ID             string `json:"id"`
			Code           string `json:"code"`
			Label          string `json:"label"`
			Treatment      string `json:"treatment"`
			RateMillionths int64  `json:"rate_millionths"`
		} `json:"tax_codes"`
	}{BundleID: bundleID, Version: "au_gst_v1", Revision: 1, EffectiveFrom: "2000-07-01"}
	document.TaxCodes = make([]struct {
		ID             string `json:"id"`
		Code           string `json:"code"`
		Label          string `json:"label"`
		Treatment      string `json:"treatment"`
		RateMillionths int64  `json:"rate_millionths"`
	}, count)
	for index := range document.TaxCodes {
		document.TaxCodes[index].ID = fmt.Sprintf("018f0000-0000-7000-8000-%012d", firstCodeID+index)
		document.TaxCodes[index].Code = fmt.Sprintf("G%03d", index)
		document.TaxCodes[index].Label = fmt.Sprintf("GST-free category %03d", index)
		document.TaxCodes[index].Treatment = "GST_FREE"
	}
	retained, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode %d-code rule bundle: %v", count, err)
	}
	digest := sha256.Sum256(retained)
	bundle, err := artefacts.ParseRuleBundle(retained, digest[:])
	if err != nil {
		t.Fatalf("parse %d-code rule bundle: %v", count, err)
	}
	return bundle
}

func makeRuleReportingUTF8OversizedBundle(t *testing.T, bundle artefacts.RuleBundle) artefacts.RuleBundle {
	t.Helper()
	var document struct {
		BundleID      string `json:"bundle_id"`
		Version       string `json:"version"`
		Revision      uint64 `json:"revision"`
		EffectiveFrom string `json:"effective_from"`
		TaxCodes      []struct {
			ID             string `json:"id"`
			Code           string `json:"code"`
			Label          string `json:"label"`
			Treatment      string `json:"treatment"`
			RateMillionths int64  `json:"rate_millionths"`
		} `json:"tax_codes"`
	}
	if err := json.Unmarshal(bundle.RetainedBytes, &document); err != nil || len(document.TaxCodes) == 0 {
		t.Fatalf("decode oversized retained bundle fixture: %v", err)
	}
	document.TaxCodes[0].Label = strings.Repeat("界", 53)
	retained, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode UTF-8 retained bundle fixture: %v", err)
	}
	if len(retained) >= ruleReportingPayloadLimit {
		t.Fatalf("UTF-8 bundle fixture unexpectedly reached cap before legal JSON padding: %d", len(retained))
	}
	retained = append(retained, bytes.Repeat([]byte(" "), ruleReportingPayloadLimit+1024-len(retained))...)
	digest := sha256.Sum256(retained)
	oversized, err := artefacts.ParseRuleBundle(retained, digest[:])
	if err != nil {
		t.Fatalf("parse legal UTF-8 bundle with trailing JSON whitespace: %v", err)
	}
	if len(oversized.RetainedBytes) <= ruleReportingPayloadLimit || !strings.Contains(oversized.TaxCodes[0].Label, "界") {
		t.Fatalf("UTF-8 oversized bundle fixture has unexpected retained data: bytes=%d first label=%q",
			len(oversized.RetainedBytes), oversized.TaxCodes[0].Label)
	}
	return oversized
}

type ruleReportingRevisionSnapshot struct {
	financial, ledger, settlement, banking, taxSource, profile, ruleBundle int64
	updatedAt                                                              string
}

func ruleReportingRevisionState(
	t *testing.T,
	ctx context.Context,
	executor interface {
		QueryRowContext(context.Context, string, ...any) *sqlcipher.Row
	},
) ruleReportingRevisionSnapshot {
	t.Helper()
	var snapshot ruleReportingRevisionSnapshot
	err := executor.QueryRowContext(ctx, `SELECT financial_revision,ledger_revision,settlement_revision,
		banking_revision,tax_source_revision,organisation_profile_revision,rule_bundle_revision,updated_at
		FROM financial_revisions WHERE id=1`).Scan(&snapshot.financial, &snapshot.ledger, &snapshot.settlement,
		&snapshot.banking, &snapshot.taxSource, &snapshot.profile, &snapshot.ruleBundle, &snapshot.updatedAt)
	if err != nil {
		t.Fatalf("read financial revision snapshot: %v", err)
	}
	return snapshot
}
