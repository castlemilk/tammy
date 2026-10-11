//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package artefacts_test

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
)

func TestRuleRepositoryGetForReportingRejectsChangedPayloadWithStaleSourceHash(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}

	// Trailing JSON whitespace remains parseable, so this specifically checks
	// that the reader verifies the exact retained bytes against the source hash.
	changed := bundle
	changed.RetainedBytes = append(append([]byte(nil), bundle.RetainedBytes...), ' ')
	insertRuleReportingBundleRows(t, fixture, changed, "AU_GST", bundle.Version,
		hex.EncodeToString(bundle.Source.ContentHash), bundle.EffectiveFrom, nil,
		fixture.now.Format(time.RFC3339Nano), ruleReportingCatalogueRows(bundle))

	record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
	if record != nil || !errors.Is(err, artefacts.ErrRuleRepository) {
		t.Fatalf("proof for changed retained bytes with stale source hash = %#v, %v; want nil and ErrRuleRepository",
			record, err)
	}
}

func TestRuleRepositoryGetForReportingRejectsIncompleteCatalogueEvenBelowRowCap(t *testing.T) {
	for _, mode := range []string{"missing row", "extra row", "missing row replaced by extra row"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newRuleReportingFixture(t)
			bundle, err := artefacts.LoadAUGSTV1()
			if err != nil {
				t.Fatal(err)
			}
			catalogue := ruleReportingCatalogueRows(bundle)
			extra := ruleReportingCatalogueSQLRow{
				ID: "018f0000-0000-7000-8000-000000009921", Code: "EXTRA_REPORTING_ONLY",
				Label: "Extra retained row", Treatment: "GST_FREE", RateMillionths: 0,
				EffectiveFrom: bundle.EffectiveFrom,
			}
			switch mode {
			case "missing row":
				catalogue = catalogue[:len(catalogue)-1]
			case "extra row":
				catalogue = append(catalogue, extra)
			case "missing row replaced by extra row":
				catalogue = append(catalogue[:len(catalogue)-1], extra)
			}
			if len(catalogue) > 200 {
				t.Fatalf("adversarial catalogue fixture unexpectedly exceeds row cap: %d", len(catalogue))
			}
			insertRuleReportingBundleRows(t, fixture, bundle, "AU_GST", bundle.Version,
				hex.EncodeToString(bundle.Source.ContentHash), bundle.EffectiveFrom, nil,
				fixture.now.Format(time.RFC3339Nano), catalogue)

			record, err := fixture.repository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, bundle.Source)
			if record != nil || !errors.Is(err, artefacts.ErrRuleRepository) {
				t.Fatalf("proof for %s = %#v, %v; want nil and ErrRuleRepository", mode, record, err)
			}
		})
	}
}

func TestRuleRepositoryGetForReportingChecksCallerBudgetBeforeHydratingLargeCatalogue(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	base := retainReportingBundle(t, fixture)
	probe := &ruleReportingQueryProbe{CommandSQLExecutor: fixture.tx}
	probeRepository, err := artefacts.NewRepository(probe)
	if err != nil {
		t.Fatal(err)
	}

	baseline, err := probeRepository.GetForReporting(fixture.ctx, ruleReportingOrganisationID, base.Source)
	if err != nil || baseline == nil || probe.hydrationQueries == 0 {
		t.Fatalf("successful probe read = %#v, %v with %d hydration queries; want a real successful hydration",
			baseline, err, probe.hydrationQueries)
	}
	probe.preflightQueries, probe.hydrationQueries = 0, 0

	large, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}
	large = cloneRuleReportingBundleWithNewIdentity(t, large,
		"018f0000-0000-7000-8000-000000009922", 1600)
	// Retain trusts the caller's parsed fields, so this label is intentionally
	// inconsistent with the source JSON while remaining valid SQL row data.
	// Three MiB of UTF-8 exceeds the two MiB caller budget despite only one
	// million Unicode code points.
	large.TaxCodes[0].Label = strings.Repeat("界", 1<<20)
	if err := fixture.repository.Retain(fixture.ctx, ruleReportingOrganisationID, large, fixture.now); err != nil {
		t.Fatalf("retain adversarial but validly-shaped catalogue row: %v", err)
	}

	limited, err := probeRepository.GetForReportingWithPayloadBudget(
		fixture.ctx, ruleReportingOrganisationID, large.Source, 2*1024*1024)
	if limited != nil || !errors.Is(err, artefacts.ErrRuleReportingBound) {
		t.Fatalf("under-budget large catalogue read = %#v, %v; want nil and ErrRuleReportingBound", limited, err)
	}
	if probe.preflightQueries == 0 || probe.hydrationQueries != 0 {
		t.Fatalf("under-budget read ran %d preflight and %d hydration queries; want rejection before hydration",
			probe.preflightQueries, probe.hydrationQueries)
	}
}

func TestRuleRepositoryGetForReportingPreservesCancellationFromFirstQuery(t *testing.T) {
	fixture := newRuleReportingFixture(t)
	bundle := retainReportingBundle(t, fixture)
	captureContext, cancel := context.WithCancel(fixture.ctx)
	t.Cleanup(cancel)
	executor := &cancelFirstRuleReportingQueryExecutor{CommandSQLExecutor: fixture.tx, cancel: cancel}
	repository, err := artefacts.NewRepository(executor)
	if err != nil {
		t.Fatal(err)
	}

	record, err := repository.GetForReporting(captureContext, ruleReportingOrganisationID, bundle.Source)
	if record != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, artefacts.ErrRuleRepository) {
		t.Fatalf("read after first-query cancellation = %#v, %v; want nil, context.Canceled and ErrRuleRepository",
			record, err)
	}
	if executor.queryCalls != 1 {
		t.Fatalf("query calls before cancellation = %d; want the first repository query", executor.queryCalls)
	}
}

type ruleReportingQueryProbe struct {
	app.CommandSQLExecutor
	preflightQueries int
	hydrationQueries int
}

func (executor *ruleReportingQueryProbe) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	normalized := strings.ToLower(query)
	if strings.Contains(normalized, "count(") && strings.Contains(normalized, "total(") {
		executor.preflightQueries++
	} else {
		executor.hydrationQueries++
	}
	return executor.CommandSQLExecutor.QueryContext(ctx, query, arguments...)
}

type cancelFirstRuleReportingQueryExecutor struct {
	app.CommandSQLExecutor
	cancel     context.CancelFunc
	queryCalls int
}

func (executor *cancelFirstRuleReportingQueryExecutor) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	executor.queryCalls++
	if executor.queryCalls == 1 {
		executor.cancel()
	}
	return executor.CommandSQLExecutor.QueryContext(ctx, query, arguments...)
}

var _ app.CommandSQLExecutor = (*ruleReportingQueryProbe)(nil)
var _ app.CommandSQLExecutor = (*cancelFirstRuleReportingQueryExecutor)(nil)
