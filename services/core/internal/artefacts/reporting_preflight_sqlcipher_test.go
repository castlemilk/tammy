//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package artefacts_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

const ruleReportingPreflightHardLimit = int64(8 * 1024 * 1024)

func TestRuleRepositoryPreflightForReportingWithPayloadBudgetMatchesCaptureWithoutHydration(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)
	full, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if err != nil || full == nil || full.PayloadBytes <= 0 {
		t.Fatalf("GetForReporting() = %#v, %v; want a full proof and positive payload size", full, err)
	}

	probe := &ruleReportingPreflightQueryRecorder{CommandSQLExecutor: fixture.tx}
	repository, err := artefacts.NewRepository(probe)
	if err != nil {
		t.Fatalf("NewRepository(probe) = %v", err)
	}
	before := ruleReportingPreflightState(t, fixture)

	preflight, err := repository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, ruleReportingOrganisationID, bundle.Source, full.PayloadBytes)
	if err != nil || preflight == nil {
		t.Fatalf("exact-budget preflight = %#v, %v; want size-only metadata", preflight, err)
	}
	wantParentBytes, wantCatalogueCount, wantCatalogueBytes := ruleReportingRetainedByteComponents(t, fixture, bundle.Source.Id)
	if preflight.BundleID != bundle.Source.Id || preflight.CatalogueCount != wantCatalogueCount ||
		preflight.CatalogueCount != len(full.Catalogue) ||
		preflight.ParentPayloadBytes != wantParentBytes || preflight.CataloguePayloadBytes != wantCatalogueBytes ||
		preflight.PayloadBytes != full.PayloadBytes ||
		preflight.ParentPayloadBytes+preflight.CataloguePayloadBytes != preflight.PayloadBytes {
		t.Fatalf("preflight metadata = %#v; want source identity, exact retained component bytes, and capture size %d",
			preflight, full.PayloadBytes)
	}
	assertNoRuleReportingBodyHydration(t, probe.queries)
	probe.queries = nil
	clamped, err := repository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, ruleReportingOrganisationID, bundle.Source, 16*1024*1024)
	if err != nil || clamped == nil || clamped.PayloadBytes != full.PayloadBytes {
		t.Fatalf("preflight with a caller budget above the shared cap = %#v, %v; want the complete in-cap size metadata", clamped, err)
	}
	assertNoRuleReportingBodyHydration(t, probe.queries)
	if !fixture.tx.Authenticated() {
		t.Fatal("size-only preflight ended the caller-owned encrypted transaction")
	}
	if after := ruleReportingPreflightState(t, fixture); before != after {
		t.Fatalf("exact-budget preflight changed retained data or revisions: before=%#v after=%#v", before, after)
	}

	probe.queries = nil
	limited, err := repository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, ruleReportingOrganisationID, bundle.Source, full.PayloadBytes-1)
	assertRuleReportingPreflightBound(t, limited, err, "capture bytes", full.PayloadBytes-1)
	assertNoRuleReportingBodyHydration(t, probe.queries)
	if after := ruleReportingPreflightState(t, fixture); before != after {
		t.Fatalf("over-budget preflight changed retained data or revisions: before=%#v after=%#v", before, after)
	}
}

func TestRuleRepositoryPreflightForReportingValidatesArgumentsAndOwnerBeforeHydration(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)
	probe := &ruleReportingPreflightQueryRecorder{CommandSQLExecutor: fixture.tx}
	repository, err := artefacts.NewRepository(probe)
	if err != nil {
		t.Fatalf("NewRepository(probe) = %v", err)
	}
	before := ruleReportingPreflightState(t, fixture)
	maximum := ruleReportingPreflightHardLimit

	canceled, cancel := context.WithCancel(fixture.ctx)
	cancel()
	result, err := repository.PreflightForReportingWithPayloadBudget(
		canceled, ruleReportingOrganisationID, bundle.Source, maximum)
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preflight = %#v, %v; want nil and context.Canceled", result, err)
	}
	if len(probe.queries) != 0 {
		t.Fatalf("canceled preflight issued %d database queries; want none", len(probe.queries))
	}

	result, err = repository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, ruleReportingOrganisationID, bundle.Source, -1)
	if result != nil || !errors.Is(err, artefacts.ErrRuleRepository) {
		t.Fatalf("negative-budget preflight = %#v, %v; want nil and ErrRuleRepository", result, err)
	}
	if len(probe.queries) != 0 {
		t.Fatalf("negative-budget preflight issued %d database queries; want none", len(probe.queries))
	}

	malformed := []struct {
		name   string
		source *tammyv1.SourceRef
	}{
		{name: "nil source"},
		{name: "wrong type", source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.Type = "other_rule" })},
		{name: "noncanonical ID", source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.Id = "not-a-v7-id" })},
		{name: "wrong revision", source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.Revision++ })},
		{name: "short hash", source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.ContentHash = source.ContentHash[:len(source.ContentHash)-1] })},
	}
	for _, test := range malformed {
		t.Run(test.name, func(t *testing.T) {
			result, err := repository.PreflightForReportingWithPayloadBudget(
				fixture.ctx, ruleReportingOrganisationID, test.source, maximum)
			if result != nil || !errors.Is(err, artefacts.ErrRuleRepository) {
				t.Fatalf("malformed-source preflight = %#v, %v; want nil and ErrRuleRepository", result, err)
			}
			if len(probe.queries) != 0 {
				t.Fatalf("malformed-source preflight issued %d database queries; want none", len(probe.queries))
			}
		})
	}

	for _, test := range []struct {
		name           string
		organisationID string
		source         *tammyv1.SourceRef
	}{
		{name: "foreign organisation", organisationID: "018f0000-0000-7000-8000-000000009999", source: bundle.Source},
		{name: "missing parent", organisationID: ruleReportingOrganisationID,
			source: mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) { source.Id = "018f0000-0000-7000-8000-000000009998" })},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe.queries = nil
			result, err := repository.PreflightForReportingWithPayloadBudget(
				fixture.ctx, test.organisationID, test.source, maximum)
			if result != nil || !errors.Is(err, artefacts.ErrRuleRepository) {
				t.Fatalf("unowned/missing preflight = %#v, %v; want nil and ErrRuleRepository", result, err)
			}
			assertNoRuleReportingBodyHydration(t, probe.queries)
			if len(probe.queries) != 1 || bytes.Contains(bytes.ToLower([]byte(probe.queries[0])), []byte("tax_code_catalogue")) {
				t.Fatalf("unowned/missing preflight queried catalogue rows before finding an owned parent: %v", probe.queries)
			}
		})
	}

	if !fixture.tx.Authenticated() {
		t.Fatal("argument-validation preflight ended the caller-owned encrypted transaction")
	}
	if after := ruleReportingPreflightState(t, fixture); before != after {
		t.Fatalf("argument/owner validation preflight changed retained data or revisions: before=%#v after=%#v", before, after)
	}
}

func TestRuleRepositoryPreflightForReportingPreservesCancellationFromFirstQuery(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)
	captureContext, cancel := context.WithCancel(fixture.ctx)
	t.Cleanup(cancel)
	executor := &cancelFirstRuleReportingQueryExecutor{CommandSQLExecutor: fixture.tx, cancel: cancel}
	repository, err := artefacts.NewRepository(executor)
	if err != nil {
		t.Fatal(err)
	}
	before := ruleReportingPreflightState(t, fixture)
	metadata, err := repository.PreflightForReportingWithPayloadBudget(
		captureContext, ruleReportingOrganisationID, bundle.Source, ruleReportingPreflightHardLimit)
	if metadata != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, artefacts.ErrRuleRepository) {
		t.Fatalf("first-query canceled preflight = %#v, %v; want nil and cancellation", metadata, err)
	}
	if executor.queryCalls != 1 {
		t.Fatalf("query calls=%d; want only the owned-parent preflight", executor.queryCalls)
	}
	if after := ruleReportingPreflightState(t, fixture); before != after || !fixture.tx.Authenticated() {
		t.Fatalf("canceled preflight changed state or ended transaction: before=%#v after=%#v", before, after)
	}
}

func TestRuleRepositoryPreflightForReportingDoesNotClaimToVerifyAValidShapeHash(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)
	wrongHash := mutateRuleReportingSource(bundle.Source, func(source *tammyv1.SourceRef) {
		source.ContentHash[0] ^= 0xff
	})
	probe := &ruleReportingPreflightQueryRecorder{CommandSQLExecutor: fixture.tx}
	repository, err := artefacts.NewRepository(probe)
	if err != nil {
		t.Fatalf("NewRepository(probe) = %v", err)
	}

	preflight, err := repository.PreflightForReportingWithPayloadBudget(
		fixture.ctx, ruleReportingOrganisationID, wrongHash, ruleReportingPreflightHardLimit)
	if err != nil || preflight == nil || preflight.PayloadBytes <= 0 {
		t.Fatalf("size-only preflight for a well-shaped but stale hash = %#v, %v; want metadata without semantic verification", preflight, err)
	}
	assertNoRuleReportingBodyHydration(t, probe.queries)

	probe.queries = nil
	capture, err := repository.GetForReportingWithPayloadBudget(
		fixture.ctx, ruleReportingOrganisationID, wrongHash, ruleReportingPreflightHardLimit)
	if capture != nil || !errors.Is(err, artefacts.ErrRuleRepository) {
		t.Fatalf("full capture with stale source hash = %#v, %v; want nil and ErrRuleRepository", capture, err)
	}
	if !ruleReportingHasBodyHydration(probe.queries) {
		t.Fatalf("full capture with stale source hash issued no parent/catalogue body query: %v", probe.queries)
	}
}

func TestRuleRepositoryPreflightForReportingPreservesTheCatalogueAndCaptureCaps(t *testing.T) {
	t.Run("two hundred catalogue rows are accepted; row 201 is rejected", func(t *testing.T) {
		fixture := newRuleReportingFixture(t)
		bundle := makeRuleReportingBundleWithCodeCount(t, 200,
			"018f0000-0000-7000-8000-000000009997", 1700)
		if err := fixture.repository.Retain(fixture.ctx, ruleReportingOrganisationID, bundle, fixture.now); err != nil {
			t.Fatalf("retain 200-code bundle: %v", err)
		}
		probe := &ruleReportingPreflightQueryRecorder{CommandSQLExecutor: fixture.tx}
		repository, err := artefacts.NewRepository(probe)
		if err != nil {
			t.Fatalf("NewRepository(probe) = %v", err)
		}
		stateBefore := ruleReportingPreflightState(t, fixture)
		accepted, err := repository.PreflightForReportingWithPayloadBudget(
			fixture.ctx, ruleReportingOrganisationID, bundle.Source, ruleReportingPreflightHardLimit)
		if err != nil || accepted == nil || accepted.CatalogueCount != 200 {
			t.Fatalf("200-row preflight = %#v, %v; want all rows within the catalogue cap", accepted, err)
		}
		assertNoRuleReportingBodyHydration(t, probe.queries)

		if _, err := fixture.tx.ExecContext(fixture.ctx, `INSERT INTO tax_code_catalogue(
			id,code,rule_bundle_id,label,rate_millionths,treatment,effective_from,effective_to)
			VALUES(?,?,?,?,?,?,?,NULL)`, "018f0000-0000-7000-8000-000000001999", "OVER_LIMIT",
			"018f0000-0000-7000-8000-000000009997", "Over limit fixture", 0, "GST_FREE", bundle.EffectiveFrom); err != nil {
			t.Fatalf("insert catalogue row 201: %v", err)
		}
		stateBefore = ruleReportingPreflightState(t, fixture)
		probe.queries = nil
		rejected, err := repository.PreflightForReportingWithPayloadBudget(
			fixture.ctx, ruleReportingOrganisationID, bundle.Source, ruleReportingPreflightHardLimit)
		assertRuleReportingPreflightBound(t, rejected, err, "catalogue rows", 200)
		assertNoRuleReportingBodyHydration(t, probe.queries)
		if !fixture.tx.Authenticated() {
			t.Fatal("row-cap preflight ended the caller-owned encrypted transaction")
		}
		if after := ruleReportingPreflightState(t, fixture); stateBefore != after {
			t.Fatalf("row-cap preflight changed retained data or revisions: before=%#v after=%#v", stateBefore, after)
		}
	})

	t.Run("eight MiB hard cap clamps a larger caller budget", func(t *testing.T) {
		fixture := newRuleReportingFixture(t)
		base, err := artefacts.LoadAUGSTV1()
		if err != nil {
			t.Fatal(err)
		}
		oversized := makeRuleReportingUTF8OversizedBundle(t, base)
		if err := fixture.repository.Retain(fixture.ctx, ruleReportingOrganisationID, oversized, fixture.now); err != nil {
			t.Fatalf("retain over-cap rule bundle: %v", err)
		}
		probe := &ruleReportingPreflightQueryRecorder{CommandSQLExecutor: fixture.tx}
		repository, err := artefacts.NewRepository(probe)
		if err != nil {
			t.Fatalf("NewRepository(probe) = %v", err)
		}
		stateBefore := ruleReportingPreflightState(t, fixture)
		rejected, err := repository.PreflightForReportingWithPayloadBudget(
			fixture.ctx, ruleReportingOrganisationID, oversized.Source, 16*1024*1024)
		assertRuleReportingPreflightBound(t, rejected, err, "capture bytes", ruleReportingPreflightHardLimit)
		assertNoRuleReportingBodyHydration(t, probe.queries)
		if !fixture.tx.Authenticated() {
			t.Fatal("hard-cap preflight ended the caller-owned encrypted transaction")
		}
		if after := ruleReportingPreflightState(t, fixture); stateBefore != after {
			t.Fatalf("hard-cap preflight changed retained data or revisions: before=%#v after=%#v", stateBefore, after)
		}
	})
}

func assertRuleReportingPreflightBound(
	t *testing.T,
	result *artefacts.RuleBundleReportingPreflight,
	err error,
	resource string,
	limit int64,
) {
	t.Helper()
	var bound *artefacts.RuleReportingBoundError
	if result != nil || !errors.Is(err, artefacts.ErrRuleReportingBound) || !errors.As(err, &bound) ||
		bound.Resource != resource || bound.Limit != limit {
		t.Fatalf("preflight result = %#v, %v; want typed %s bound at %d", result, err, resource, limit)
	}
}

type ruleReportingPreflightQueryRecorder struct {
	app.CommandSQLExecutor
	queries []string
}

func (executor *ruleReportingPreflightQueryRecorder) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	executor.queries = append(executor.queries, query)
	return executor.CommandSQLExecutor.QueryContext(ctx, query, arguments...)
}

func assertNoRuleReportingBodyHydration(t *testing.T, queries []string) {
	t.Helper()
	if ruleReportingHasBodyHydration(queries) {
		t.Fatalf("size-only preflight selected retained rule/catalogue body fields: %v", queries)
	}
}

func ruleReportingHasBodyHydration(queries []string) bool {
	for _, query := range queries {
		normalized := bytes.ToLower(bytes.Join(bytes.Fields([]byte(query)), []byte(" ")))
		// These are the complete row projections used by the capture loaders.
		// Scalar preflight projections can mention the same column names inside
		// length(CAST(...)) expressions; those must not count as body hydration.
		if bytes.Contains(normalized, []byte("select id, organisation_id, bundle_type, version, semantic_sha256, rules_proto, effective_from, effective_to, retained_at from rule_bundles")) ||
			bytes.Contains(normalized, []byte("select c.id, c.code, c.rule_bundle_id, c.label, c.treatment, c.rate_millionths, c.effective_from, c.effective_to from tax_code_catalogue")) {
			return true
		}
	}
	return false
}

func ruleReportingRetainedByteComponents(
	t *testing.T,
	fixture *ruleReportingFixture,
	bundleID string,
) (parentBytes int64, catalogueCount int, catalogueBytes int64) {
	t.Helper()
	const parentSize = `512 + coalesce(length(CAST(id AS BLOB)), 0) +
		coalesce(length(CAST(organisation_id AS BLOB)), 0) +
		coalesce(length(CAST(bundle_type AS BLOB)), 0) +
		coalesce(length(CAST(version AS BLOB)), 0) +
		coalesce(length(CAST(semantic_sha256 AS BLOB)), 0) +
		coalesce(length(CAST(rules_proto AS BLOB)), 0) +
		coalesce(length(CAST(effective_from AS BLOB)), 0) +
		coalesce(length(CAST(effective_to AS BLOB)), 0) +
		coalesce(length(CAST(retained_at AS BLOB)), 0)`
	const catalogueSize = `512 + coalesce(length(CAST(c.id AS BLOB)), 0) +
		coalesce(length(CAST(c.code AS BLOB)), 0) +
		coalesce(length(CAST(c.rule_bundle_id AS BLOB)), 0) +
		coalesce(length(CAST(c.label AS BLOB)), 0) +
		coalesce(length(CAST(c.rate_millionths AS BLOB)), 0) +
		coalesce(length(CAST(c.treatment AS BLOB)), 0) +
		coalesce(length(CAST(c.effective_from AS BLOB)), 0) +
		coalesce(length(CAST(c.effective_to AS BLOB)), 0)`
	query := `SELECT
		(SELECT coalesce(sum(` + parentSize + `), 0) FROM rule_bundles WHERE organisation_id=? AND id=?),
		(SELECT count(*) FROM tax_code_catalogue AS c JOIN rule_bundles AS b ON b.id=c.rule_bundle_id WHERE b.organisation_id=? AND b.id=?),
		(SELECT coalesce(sum(` + catalogueSize + `), 0) FROM tax_code_catalogue AS c
		 JOIN rule_bundles AS b ON b.id=c.rule_bundle_id WHERE b.organisation_id=? AND b.id=?)`
	if err := fixture.tx.QueryRowContext(fixture.ctx, query,
		ruleReportingOrganisationID, bundleID, ruleReportingOrganisationID, bundleID,
		ruleReportingOrganisationID, bundleID).Scan(&parentBytes, &catalogueCount, &catalogueBytes); err != nil {
		t.Fatalf("compute retained component bytes from SQL rows: %v", err)
	}
	return parentBytes, catalogueCount, catalogueBytes
}

type ruleReportingPreflightSnapshot struct {
	revisions   ruleReportingRevisionSnapshot
	totalChange int64
}

func ruleReportingPreflightState(t *testing.T, fixture *ruleReportingFixture) ruleReportingPreflightSnapshot {
	t.Helper()
	snapshot := ruleReportingPreflightSnapshot{revisions: ruleReportingRevisionState(t, fixture.ctx, fixture.tx)}
	if err := fixture.tx.QueryRowContext(fixture.ctx, `SELECT total_changes()`).Scan(&snapshot.totalChange); err != nil {
		t.Fatalf("read preflight total_changes: %v", err)
	}
	return snapshot
}
