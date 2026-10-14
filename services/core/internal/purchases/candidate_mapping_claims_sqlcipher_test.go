//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

// These writer/full-reader oracles join the existing physical scalar census to
// complete claim history and ordinary BAS output. They pin schema identities,
// source associations, and money conservation separately. They deliberately do
// not decide which historical decisions spend the future semantic candidate cap.
type basClaimOracleDeferralIdentity struct {
	id       string
	sequence uint64
}

type basClaimOracleSourceIdentity struct {
	kind, organisationID, postingID, paymentID string
	sequence                                   uint64
}

func basClaimOracleNextPeriod() *tammyv1.PurchaseClaimPeriod {
	return &tammyv1.PurchaseClaimPeriod{
		PeriodStart: &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 1},
		PeriodEnd:   &tammyv1.CivilDate{Year: 2027, Month: 3, Day: 31},
	}
}

func basClaimOracleLaterReview() *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 7}
}

func basClaimOraclePaymentPortion(t *testing.T, template *tammyv1.PurchaseGstSourcePortion, paid *tammyv1.RecordPurchasePaymentResponse) *tammyv1.PurchaseGstSourcePortion {
	t.Helper()
	if template == nil || paid == nil || paid.Payment == nil || paid.Accounting == nil || paid.Accounting.Posting == nil {
		t.Fatal("payment source fixture is incomplete")
	}
	portion := proto.Clone(template).(*tammyv1.PurchaseGstSourcePortion)
	portion.Source = &tammyv1.PurchaseGstSourcePortion_Payment{Payment: &tammyv1.PurchasePaymentPortionRef{
		PostingId: paid.Payment.PostingId, PostingHash: append([]byte(nil), paid.Accounting.Posting.SourceHash...),
		PaymentId: paid.Payment.Id, PaymentHash: append([]byte(nil), paid.Payment.SourceHash...), AllocationSequence: 1,
	}}
	portion.NormalEventDate = proto.Clone(paid.Payment.ActualPaymentDate).(*tammyv1.CivilDate)
	return portion
}

func basClaimOracleAssertReadOnly(t *testing.T, tx *sqlcipher.Transaction, before map[string]any, stage string) {
	t.Helper()
	if !tx.Authenticated() {
		t.Fatalf("%s ended or deauthenticated the caller-owned transaction", stage)
	}
	if after := purchaseBASScalarState(t, tx); !reflect.DeepEqual(after, before) {
		t.Fatalf("%s mutated caller-owned state: before=%v after=%v", stage, before, after)
	}
}

func basClaimOracleSource(t *testing.T, portion *tammyv1.PurchaseGstSourcePortion) basClaimOracleSourceIdentity {
	t.Helper()
	if portion == nil {
		t.Fatal("nil claim source portion")
	}
	if ref := portion.GetPayment(); ref != nil {
		return basClaimOracleSourceIdentity{kind: "PAYMENT", organisationID: postingOrg, postingID: ref.PostingId, paymentID: ref.PaymentId, sequence: uint64(ref.AllocationSequence)}
	}
	if ref := portion.GetPosting(); ref != nil {
		return basClaimOracleSourceIdentity{kind: "POSTING", organisationID: postingOrg, postingID: ref.PostingId, sequence: uint64(ref.AllocationSequence)}
	}
	t.Fatal("claim portion has no owned financial source")
	return basClaimOracleSourceIdentity{}
}

func basClaimOracleScalarSource(t *testing.T, key purchases.BASScalarKey) basClaimOracleSourceIdentity {
	t.Helper()
	if key.SourceKind == nil || key.PostingID == nil || key.AllocationSequence == nil {
		t.Fatalf("scalar claim source omitted required identity: %#v", key)
	}
	identity := basClaimOracleSourceIdentity{kind: *key.SourceKind, organisationID: key.OrganisationID, postingID: *key.PostingID, sequence: *key.AllocationSequence}
	if key.PaymentID != nil {
		identity.paymentID = *key.PaymentID
	}
	if identity.kind == "PAYMENT" && identity.paymentID == "" {
		t.Fatalf("cash scalar source lost payment identity: %#v", key)
	}
	if identity.kind == "POSTING" && identity.paymentID != "" {
		t.Fatalf("non-cash scalar source unexpectedly has a payment: %#v", key)
	}
	return identity
}

func basClaimOracleAssertLedger(t *testing.T, tx app.CommandSQLExecutor, state *tammyv1.PurchaseAccounting, deferred, suspense, attributed int64) {
	t.Helper()
	if state == nil || state.Posting == nil || state.Posting.EligibleGst == nil || state.EligibleGst == nil || state.DeferredGst == nil || state.SuspenseGst == nil || state.AttributedGst == nil {
		t.Fatalf("incomplete replayed accounting state: %v", state)
	}
	eligible := state.Posting.EligibleGst.MinorUnits
	if state.EligibleGst.MinorUnits != eligible || state.DeferredGst.MinorUnits != deferred || state.SuspenseGst.MinorUnits != suspense || state.AttributedGst.MinorUnits != attributed {
		t.Fatalf("replayed ledger deferred/suspense/attributed=%d/%d/%d, eligible=%d; want %d/%d/%d/%d", state.DeferredGst.MinorUnits, state.SuspenseGst.MinorUnits, state.AttributedGst.MinorUnits, eligible, deferred, suspense, attributed, eligible)
	}
	if state.DeferredGst.MinorUnits+state.SuspenseGst.MinorUnits+state.AttributedGst.MinorUnits != eligible {
		t.Fatalf("purchase GST ledger does not conserve posting eligible GST: %v", state)
	}
	for _, test := range []struct {
		code string
		want int64
	}{{"1160", deferred}, {"1170", suspense}, {"1150", attributed}} {
		if got := paymentBalance(t, tx, test.code); got != test.want {
			t.Fatalf("journal balance %s=%d, replay state wants %d", test.code, got, test.want)
		}
	}
}

func basClaimOracleReadScalarKeys(t *testing.T, ctx context.Context, repo *purchases.Repository) []purchases.BASScalarKey {
	t.Helper()
	summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("purchase scalar summary=%#v, err=%v", summary, err)
	}
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
		t.Fatalf("purchase scalar keys=%d, err=%v, reservation=%#v", len(keys), err, summary)
	}
	return keys
}

func basClaimOracleAssertScalarHistory(
	t *testing.T,
	keys []purchases.BASScalarKey,
	deferrals []*tammyv1.PurchaseGstDeferral,
	attributions []*tammyv1.PurchaseGstAttribution,
	wantPortions map[basClaimOracleDeferralIdentity]*tammyv1.PurchaseGstSourcePortion,
	deferralOperationKeys map[string]string,
	attributionOperationKeys map[string]string,
	capture *purchases.PurchaseCoverageReportingCapture,
) {
	t.Helper()
	headerIDs := make(map[string]bool)
	portionKeys := make(map[basClaimOracleDeferralIdentity]purchases.BASScalarKey)
	attributionKeys := make(map[string]purchases.BASScalarKey)
	for _, key := range keys {
		switch key.Kind {
		case purchases.BASScalarDeferral:
			if headerIDs[key.ID] {
				t.Fatalf("duplicate physical deferral scalar identity %q", key.ID)
			}
			headerIDs[key.ID] = true
		case purchases.BASScalarDeferralPortion:
			if key.DeferralID == nil || key.Sequence == nil {
				t.Fatalf("deferral portion lost exact (deferral_id, sequence): %#v", key)
			}
			identity := basClaimOracleDeferralIdentity{id: *key.DeferralID, sequence: *key.Sequence}
			if _, exists := portionKeys[identity]; exists {
				t.Fatalf("duplicate physical deferral portion identity %+v", identity)
			}
			portionKeys[identity] = key
		case purchases.BASScalarAttribution:
			if _, exists := attributionKeys[key.ID]; exists {
				t.Fatalf("duplicate physical attribution identity %q", key.ID)
			}
			attributionKeys[key.ID] = key
		}
	}
	if len(headerIDs) != len(deferrals) || len(portionKeys) != len(wantPortions) || len(attributionKeys) != len(attributions) {
		t.Fatalf("scalar physical identities deferrals/portions/attributions=%d/%d/%d; full reader has %d deferrals, %d expected portions, %d attributions", len(headerIDs), len(portionKeys), len(attributionKeys), len(deferrals), len(wantPortions), len(attributions))
	}
	for _, d := range deferrals {
		if d == nil || !headerIDs[d.Id] {
			t.Fatalf("full reader deferral %v has no exact scalar header key", d)
		}
		key := findBASClaimOracleKey(t, keys, purchases.BASScalarDeferral, d.Id, 0)
		wantOperationKey := deferralOperationKeys[d.Id]
		if wantOperationKey == "" || key.OperationKey == nil || *key.OperationKey != wantOperationKey || key.PostingID == nil || *key.PostingID != d.PostingId || key.DocumentID == nil || *key.DocumentID != d.DocumentId || key.OrganisationID != d.OrganisationId {
			t.Fatalf("deferral %s scalar key lost its writer operation association: key=%#v want operation %q", d.Id, key, wantOperationKey)
		}
		if key.SourceHash != nil || len(d.SourceHash) != 32 || !bytes.Equal(key.RecordHash, d.SourceHash) {
			t.Fatalf("deferral %s scalar record proof was merged with source provenance: %#v body source hash=%x", d.Id, key, d.SourceHash)
		}
		row := retainedRowForText(t, coverageRawRows(capture), "purchase_gst_deferrals_v1", "id", d.Id)
		assertBASClaimOracleRawText(t, row, "organisation_id", d.OrganisationId, "deferral", d.Id)
		assertBASClaimOracleRawText(t, row, "posting_id", d.PostingId, "deferral", d.Id)
		assertBASClaimOracleRawText(t, row, "document_id", d.DocumentId, "deferral", d.Id)
		assertBASClaimOracleRetainedHash(t, row, "source_hash", key.RecordHash, "deferral", d.Id)
		assertBASClaimOracleDeferralEvent(t, coverageRawRows(capture), d, wantOperationKey)
	}
	for identity, portion := range wantPortions {
		key, ok := portionKeys[identity]
		if !ok {
			t.Fatalf("missing scalar physical deferral portion %+v", identity)
		}
		if key.ID != identity.id || key.OrganisationID != postingOrg || basClaimOracleScalarSource(t, key) != basClaimOracleSource(t, portion) {
			t.Fatalf("scalar physical decision %+v was confused with its underlying source: key=%#v portion=%v", identity, key, portion)
		}
		if key.SourceHash != nil {
			t.Fatalf("deferral portion %s/%d merged row proof with source provenance: %#v", identity.id, identity.sequence, key)
		}
		assertBASClaimOracleScalarPortionProvenance(t, key, portion, "deferral portion", identity.id)
		row := retainedBASClaimOraclePortionRow(t, capture, identity)
		assertBASClaimOracleRawText(t, row, "organisation_id", postingOrg, "deferral portion", identity.id)
		assertBASClaimOracleRawText(t, row, "posting_id", basClaimOracleSource(t, portion).postingID, "deferral portion", identity.id)
		assertBASClaimOracleRawInteger(t, row, "sequence", int64(identity.sequence), "deferral portion", identity.id)
		assertBASClaimOracleRetainedHash(t, row, "source_hash", key.RecordHash, "deferral portion", identity.id)
	}
	for _, attribution := range attributions {
		key, ok := attributionKeys[attribution.Id]
		if !ok {
			t.Fatalf("full reader attribution %q has no exact scalar identity", attribution.Id)
		}
		wantOperationKey := attributionOperationKeys[attribution.Id]
		if wantOperationKey == "" || key.OperationKey == nil || *key.OperationKey != wantOperationKey || key.JournalID == nil || *key.JournalID != attribution.JournalId || key.RecordHash == nil || len(key.RecordHash) != 32 || len(attribution.SourceHash) != 32 || !bytes.Equal(key.RecordHash, attribution.SourceHash) || key.SourceHash != nil || key.OrganisationID != attribution.OrganisationId || key.PostingID == nil || *key.PostingID != attribution.PostingId || key.DocumentID == nil || *key.DocumentID != attribution.DocumentId {
			t.Fatalf("attribution physical id, operation owner, journal id, or independent record proof was merged: key=%#v want operation %q journal %q", key, wantOperationKey, attribution.JournalId)
		}
		if basClaimOracleScalarSource(t, key) != basClaimOracleSource(t, attribution.Portion) {
			t.Fatalf("attribution scalar source differs from full verified source: %#v / %v", key, attribution.Portion)
		}
		assertBASClaimOracleScalarPortionProvenance(t, key, attribution.Portion, "attribution", attribution.Id)
		if attribution.GetClosesDeferralId() == "" {
			if key.DeferralID != nil {
				t.Fatalf("attribution %s scalar invented a physical closing decision %q", attribution.Id, *key.DeferralID)
			}
		} else if key.DeferralID == nil || *key.DeferralID != attribution.GetClosesDeferralId() {
			t.Fatalf("attribution %s scalar lost the exact named closing deferral: %#v body closes %q", attribution.Id, key, attribution.GetClosesDeferralId())
		}
		if key.SourceKind == nil || (*key.SourceKind != "PAYMENT" && *key.SourceKind != "POSTING") || key.JournalSourceKind == nil || *key.JournalSourceKind != 8 || key.SourceType == nil || *key.SourceType != "TAX" {
			t.Fatalf("attribution scalar lost the release journal's independent TAX/8 proof: %#v", key)
		}
		row := retainedRowForText(t, coverageRawRows(capture), "purchase_gst_attributions_v1", "id", attribution.Id)
		assertBASClaimOracleRetainedHash(t, row, "source_hash", key.RecordHash, "attribution", attribution.Id)
		assertBASClaimOracleAttributionRawAssociation(t, row, attribution, wantOperationKey)
		assertBASClaimOracleAttributionEvent(t, coverageRawRows(capture), attribution, wantOperationKey)
	}
}

func assertBASClaimOracleScalarPortionProvenance(t *testing.T, key purchases.BASScalarKey, portion *tammyv1.PurchaseGstSourcePortion, label, id string) {
	t.Helper()
	if portion == nil || portion.DueDateEvidence == nil || key.DueDateSourceID == nil || *key.DueDateSourceID != portion.DueDateEvidence.SourceId || key.DueDateSourceVersion == nil || *key.DueDateSourceVersion != portion.DueDateEvidence.SourceVersion || !bytes.Equal(key.DueDateSourceHash, portion.DueDateEvidence.SourceHash) || key.EvidenceID == nil || *key.EvidenceID != portion.EvidenceId {
		t.Fatalf("%s %s scalar key merged or lost due-date/evidence source provenance: key=%#v portion=%v", label, id, key, portion)
	}
}

func findBASClaimOracleKey(t *testing.T, keys []purchases.BASScalarKey, kind purchases.BASScalarKeyKind, id string, sequence uint64) purchases.BASScalarKey {
	t.Helper()
	for _, key := range keys {
		if key.Kind != kind || key.ID != id {
			continue
		}
		if sequence == 0 || key.Sequence != nil && *key.Sequence == sequence {
			return key
		}
	}
	t.Fatalf("missing scalar key kind=%d id=%q sequence=%d", kind, id, sequence)
	return purchases.BASScalarKey{}
}

func assertBASClaimOracleRetainedHash(t *testing.T, row purchases.PurchaseReportingRetainedRow, column string, want []byte, label, id string) {
	t.Helper()
	value, ok := retainedValue(row, column)
	if !ok || value.Kind != "BLOB" || len(want) != 32 || !bytes.Equal(value.Bytes, want) {
		t.Fatalf("%s %s scalar record proof differs from retained %s: got=%#v want=%x", label, id, column, value, want)
	}
}

func assertBASClaimOracleRawText(t *testing.T, row purchases.PurchaseReportingRetainedRow, column, want, label, id string) {
	t.Helper()
	value, ok := retainedValue(row, column)
	if !ok || value.Kind != "TEXT" || value.Text != want {
		t.Fatalf("%s %s raw %s=%#v, want %q", label, id, column, value, want)
	}
}

func assertBASClaimOracleRawInteger(t *testing.T, row purchases.PurchaseReportingRetainedRow, column string, want int64, label, id string) {
	t.Helper()
	value, ok := retainedValue(row, column)
	if !ok || value.Kind != "INTEGER" || value.Integer != want {
		t.Fatalf("%s %s raw %s=%#v, want %d", label, id, column, value, want)
	}
}

func retainedBASClaimOraclePortionRow(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, identity basClaimOracleDeferralIdentity) purchases.PurchaseReportingRetainedRow {
	t.Helper()
	for _, row := range capture.Rows {
		if row.Table != "purchase_gst_deferral_portions_v1" {
			continue
		}
		deferralID, hasDeferral := retainedValue(row, "deferral_id")
		sequence, hasSequence := retainedValue(row, "sequence")
		if hasDeferral && deferralID.Kind == "TEXT" && deferralID.Text == identity.id && hasSequence && sequence.Kind == "INTEGER" && sequence.Integer == int64(identity.sequence) {
			return row
		}
	}
	t.Fatalf("capture omitted exact deferral portion row (%s, %d)", identity.id, identity.sequence)
	return purchases.PurchaseReportingRetainedRow{}
}

func assertBASClaimOracleAttributionRawAssociation(t *testing.T, row purchases.PurchaseReportingRetainedRow, attribution *tammyv1.PurchaseGstAttribution, operationKey string) {
	t.Helper()
	assertBASClaimOracleRawText(t, row, "organisation_id", attribution.OrganisationId, "attribution", attribution.Id)
	for column, want := range map[string]string{
		"operation_key": operationKey,
		"posting_id":    attribution.PostingId,
		"document_id":   attribution.DocumentId,
		"journal_id":    attribution.JournalId,
	} {
		value, ok := retainedValue(row, column)
		if !ok || value.Kind != "TEXT" || value.Text != want {
			t.Fatalf("attribution %s raw %s=%#v, want %q", attribution.Id, column, value, want)
		}
	}
	closing, closingOK := retainedValue(row, "closes_deferral_id")
	if !closingOK {
		t.Fatalf("attribution %s capture omitted closes_deferral_id", attribution.Id)
	}
	if attribution.GetClosesDeferralId() == "" {
		if closing.Kind != "NULL" {
			t.Fatalf("attribution %s raw closing deferral=%#v, want NULL", attribution.Id, closing)
		}
	} else if closing.Kind != "TEXT" || closing.Text != attribution.GetClosesDeferralId() {
		t.Fatalf("attribution %s raw closing deferral=%#v, want %q", attribution.Id, closing, attribution.GetClosesDeferralId())
	}
	kind, kindOK := retainedValue(row, "source_kind")
	allocation, allocationOK := retainedValue(row, "allocation_sequence")
	payment, paymentOK := retainedValue(row, "payment_id")
	wantSource := basClaimOracleSource(t, attribution.Portion)
	if !kindOK || kind.Kind != "TEXT" || kind.Text != wantSource.kind || !allocationOK || allocation.Kind != "INTEGER" || allocation.Integer != int64(wantSource.sequence) || !paymentOK {
		t.Fatalf("attribution %s raw financial-source key differs from its complete body: kind=%#v allocation=%#v payment=%#v body=%+v", attribution.Id, kind, allocation, payment, wantSource)
	}
	if wantSource.paymentID == "" {
		if payment.Kind != "NULL" {
			t.Fatalf("non-cash attribution %s acquired raw payment id %#v", attribution.Id, payment)
		}
	} else if payment.Kind != "TEXT" || payment.Text != wantSource.paymentID {
		t.Fatalf("cash attribution %s raw payment id=%#v, want %q", attribution.Id, payment, wantSource.paymentID)
	}
}

func assertBASClaimOracleDeferralEvent(t *testing.T, rows *purchases.PostingReportingCapture, deferral *tammyv1.PurchaseGstDeferral, operationKey string) {
	t.Helper()
	if countCoverageRows(rows, "purchase_accounting_events_v1", "operation_key", operationKey) != 1 {
		t.Fatalf("deferral %s writer operation %s does not resolve to one retained accounting event", deferral.Id, operationKey)
	}
	row := retainedRowForText(t, rows, "purchase_accounting_events_v1", "operation_key", operationKey)
	for column, want := range map[string]string{
		"scope_kind":   "POSTING",
		"scope_id":     deferral.PostingId,
		"command_type": "tammy.v1.PurchaseService.DeferPurchaseGstClaim",
	} {
		value, ok := retainedValue(row, column)
		if !ok || value.Kind != "TEXT" || value.Text != want {
			t.Fatalf("deferral %s event operation %s has wrong raw %s: %#v, want %q", deferral.Id, operationKey, column, value, want)
		}
	}
	version, ok := retainedValue(row, "accounting_version")
	if !ok || version.Kind != "INTEGER" || version.Integer != int64(deferral.AccountingVersion) {
		t.Fatalf("deferral %s event operation %s has wrong raw accounting version: %#v", deferral.Id, operationKey, version)
	}
	journalID, ok := retainedValue(row, "journal_id")
	if !ok || journalID.Kind != "NULL" {
		t.Fatalf("deferral %s event acquired a journal release owner: %#v", deferral.Id, journalID)
	}
}

func assertBASClaimOracleAttributionEvent(t *testing.T, rows *purchases.PostingReportingCapture, attribution *tammyv1.PurchaseGstAttribution, operationKey string) {
	t.Helper()
	if countCoverageRows(rows, "purchase_accounting_events_v1", "operation_key", operationKey) != 1 {
		t.Fatalf("attribution %s operation %s does not resolve to one retained accounting event", attribution.Id, operationKey)
	}
	row := retainedRowForText(t, rows, "purchase_accounting_events_v1", "operation_key", operationKey)
	for column, want := range map[string]string{
		"scope_kind":   "POSTING",
		"scope_id":     attribution.PostingId,
		"command_type": "tammy.v1.PurchaseService.AttributePurchaseGst",
		"journal_id":   attribution.JournalId,
	} {
		value, ok := retainedValue(row, column)
		if !ok || value.Kind != "TEXT" || value.Text != want {
			t.Fatalf("attribution %s event operation %s has wrong raw %s: %#v, want %q", attribution.Id, operationKey, column, value, want)
		}
	}
	version, ok := retainedValue(row, "accounting_version")
	if !ok || version.Kind != "INTEGER" || version.Integer != int64(attribution.AccountingVersion) {
		t.Fatalf("attribution %s event operation %s has wrong raw accounting version: %#v", attribution.Id, operationKey, version)
	}
}

func basClaimOracleAssertCapturedClaims(
	t *testing.T,
	capture *purchases.PurchaseCoverageReportingCapture,
	postingID string,
	wantDeferrals []*tammyv1.PurchaseGstDeferral,
	wantAttributions []*tammyv1.PurchaseGstAttribution,
	wantPortions map[basClaimOracleDeferralIdentity]struct {
		portion   *tammyv1.PurchaseGstSourcePortion
		allocated int64
		remaining int64
	},
) {
	t.Helper()
	actual := coveragePosting(t, capture, postingID).Claims
	if len(actual.Deferrals) != len(wantDeferrals) || len(actual.Attributions) != len(wantAttributions) {
		t.Fatalf("complete coverage changed physical deferral/attribution counts: got=%d/%d want=%d/%d", len(actual.Deferrals), len(actual.Attributions), len(wantDeferrals), len(wantAttributions))
	}
	byDeferral := make(map[string]*tammyv1.PurchaseGstDeferral, len(actual.Deferrals))
	for _, deferral := range actual.Deferrals {
		if deferral != nil {
			byDeferral[deferral.Id] = deferral
		}
	}
	for _, want := range wantDeferrals {
		if want == nil || !proto.Equal(byDeferral[want.Id], want) {
			t.Fatalf("complete coverage changed or omitted exact deferral body %v: %#v", want, byDeferral[want.GetId()])
		}
	}
	byAttribution := make(map[string]*tammyv1.PurchaseGstAttribution, len(actual.Attributions))
	for _, attribution := range actual.Attributions {
		if attribution != nil {
			byAttribution[attribution.Id] = attribution
		}
	}
	for _, want := range wantAttributions {
		if want == nil || !proto.Equal(byAttribution[want.Id], want) {
			t.Fatalf("complete coverage changed or omitted exact attribution row %v: %#v", want, byAttribution[want.GetId()])
		}
		row := retainedRowForText(t, coverageRawRows(capture), "purchase_gst_attributions_v1", "id", want.Id)
		amount, ok := retainedValue(row, "claimed_gst_minor")
		if !ok || amount.Kind != "INTEGER" || want.ClaimedGst == nil || amount.Integer != want.ClaimedGst.MinorUnits {
			t.Fatalf("complete coverage attribution %s raw claimed amount=%#v, body=%v", want.Id, amount, want.ClaimedGst)
		}
	}
	for identity, expected := range wantPortions {
		row := retainedBASClaimOraclePortionRow(t, capture, identity)
		amount, ok := retainedValue(row, "deferred_gst_minor")
		if !ok || amount.Kind != "INTEGER" || amount.Integer != expected.allocated {
			t.Fatalf("complete coverage deferral %s/%d raw allocated amount=%#v, want %d", identity.id, identity.sequence, amount, expected.allocated)
		}
	}
	basClaimOracleVerifiedPortions(t, actual, wantPortions)
}

func basClaimOracleAssertOwnedReleaseJournal(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, operationKey string, journal *tammyv1.Journal) {
	t.Helper()
	if journal == nil || journal.Id == "" || journal.Id == operationKey {
		t.Fatalf("release journal identity was not kept distinct from its operation key: journal=%v operation=%q", journal, operationKey)
	}
	owned := 0
	for _, candidate := range capture.Journals {
		if candidate == nil || candidate.Journal == nil || candidate.SourceType != "TAX" || candidate.SourceKind != 8 || candidate.SourceID != operationKey || candidate.SourceRevision != 1 {
			continue
		}
		owned++
		if candidate.Journal.Id != journal.Id || !proto.Equal(candidate.Journal, journal) {
			t.Fatalf("capture retained a different journal for TAX/8 operation %s: %#v", operationKey, candidate)
		}
	}
	if owned != 1 || countCoverageJournals(capture, journal.Id) != 1 {
		t.Fatalf("capture retained %d journals owned by TAX/8 operation %s and %d rows for exact journal id %s; want exactly one", owned, operationKey, countCoverageJournals(capture, journal.Id), journal.Id)
	}
}

func basClaimOracleVerifiedPortions(t *testing.T, history purchases.VerifiedClaimHistory, want map[basClaimOracleDeferralIdentity]struct {
	portion   *tammyv1.PurchaseGstSourcePortion
	allocated int64
	remaining int64
}) map[basClaimOracleSourceIdentity]int64 {
	t.Helper()
	if len(history.DeferralPortions) != len(want) {
		t.Fatalf("verified physical deferral portions=%d, want %d", len(history.DeferralPortions), len(want))
	}
	seen := make(map[basClaimOracleDeferralIdentity]bool)
	uniqueOutstanding := make(map[basClaimOracleSourceIdentity]int64)
	for _, verified := range history.DeferralPortions {
		identity := basClaimOracleDeferralIdentity{id: verified.DeferralID, sequence: uint64(verified.Sequence)}
		expected, ok := want[identity]
		if !ok || seen[identity] || verified.Portion == nil || !proto.Equal(verified.Portion, expected.portion) || verified.DeferredGST == nil || verified.DeferredGST.MinorUnits != expected.allocated || verified.UnclaimedGST == nil || verified.UnclaimedGST.MinorUnits != expected.remaining {
			t.Fatalf("verified physical deferral decision %+v differs from writer expectation: %#v", identity, verified)
		}
		seen[identity] = true
		source := basClaimOracleSource(t, verified.Portion)
		if old, exists := uniqueOutstanding[source]; exists {
			if old != expected.remaining {
				t.Fatalf("repeated deferral decisions disagree on one source remainder: %+v had %d and %d", source, old, expected.remaining)
			}
		} else {
			uniqueOutstanding[source] = expected.remaining
		}
	}
	return uniqueOutstanding
}

func basClaimOracleAssertReview(t *testing.T, snapshot *purchases.BASSnapshot, deferralID string, portion *tammyv1.PurchaseGstSourcePortion, allocated, unclaimed int64) {
	t.Helper()
	if snapshot == nil {
		t.Fatalf("BAS omitted the snapshot for deferral decision %s", deferralID)
	}
	var found *tammyv1.BasPurchaseReview
	for _, row := range snapshot.Reviews {
		if row == nil || row.DeferralId == nil || *row.DeferralId != deferralID || row.FutureClaimObligation == nil || row.FutureClaimObligation.Portion == nil {
			continue
		}
		if !proto.Equal(row.FutureClaimObligation.Portion, portion) {
			continue
		}
		if found != nil {
			t.Fatalf("BAS duplicated deferral decision %s for source %v", deferralID, portion)
		}
		found = row
	}
	if found == nil || found.Kind != tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_DEFERRED_FUTURE_CLAIM || found.UnclaimedGst == nil || found.UnclaimedGst.MinorUnits != unclaimed || found.FutureClaimObligation.AllocatedDeferredGst == nil || found.FutureClaimObligation.AllocatedDeferredGst.MinorUnits != allocated {
		t.Fatalf("BAS did not retain exact decision/remainder %s for %v: %#v", deferralID, portion, found)
	}
}

func basClaimOracleDeferralReviewCount(snapshot *purchases.BASSnapshot) int {
	count := 0
	for _, row := range snapshot.Reviews {
		if row != nil && row.Kind == tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_DEFERRED_FUTURE_CLAIM && row.FutureClaimObligation != nil {
			count++
		}
	}
	return count
}

// This is the central cross-projection oracle: three period-specific physical
// decisions refer to two exact cash sources. Claiming the first source in full
// names the later deferral, while the earlier decision for that same source is
// also consumed; the sibling and still-unpaid cash remain in separate balances.
// Period-status and reason resolution remain pinned by the existing
// TestPurchaseBASDeferralResolvesOnlyItsAffectedPeriod and
// TestPurchaseBASLaterPeriodDeferralClearsItsOwnDecisionWithoutDoubleFutureCredit
// suites; this oracle compares money and physical/source identities.
func TestBASCandidateClaimDecisionScalarHistoryAndConservationOracle(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f, repo, state, first := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	secondPayment, err := repo.RecordPayment(ctx, paymentRequest(t, f, state.AccountingVersion, 2750, paymentDate(9, 29)))
	if err != nil {
		t.Fatal(err)
	}
	if secondPayment.Payment == nil || secondPayment.Accounting == nil || len(secondPayment.Payment.Allocations) != 1 || secondPayment.Payment.Allocations[0].EligibleGst.MinorUnits != 250 {
		t.Fatalf("second partial cash payment eligible extent=%v, want one 250-cent portion", secondPayment.Payment.Allocations)
	}
	second := basClaimOraclePaymentPortion(t, first, secondPayment)
	preDeferral, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	basClaimOracleAssertLedger(t, tx, preDeferral, 250, 750, 0)

	firstRequest := deferRequest(t, f, preDeferral, first)
	firstRequest.Portions = append(firstRequest.Portions, proto.Clone(second).(*tammyv1.PurchaseGstSourcePortion))
	firstDeferral, err := deferCommand(t, repo, firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	if firstDeferral.Deferral.DeferredGst.MinorUnits != 750 {
		t.Fatalf("first deferral extent=%v, want both distinct source portions totaling 750", firstDeferral.Deferral.DeferredGst)
	}
	basClaimOracleAssertLedger(t, tx, firstDeferral.Accounting, 250, 750, 0)

	f.now = time.Date(2027, 1, 7, 12, 0, 0, 0, time.UTC)
	nextPeriod := basClaimOracleNextPeriod()
	secondRequest := deferRequest(t, f, firstDeferral.Accounting, first)
	secondRequest.AffectedPeriod = nextPeriod
	secondRequest.TimingReview.ReviewedAsOf = basClaimOracleLaterReview()
	secondDeferral, err := deferCommand(t, repo, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if secondDeferral.Deferral.DeferredGst.MinorUnits != 500 || secondDeferral.Deferral.Id == firstDeferral.Deferral.Id {
		t.Fatalf("second period did not retain a separate 500-cent physical decision: %#v", secondDeferral.Deferral)
	}
	basClaimOracleAssertLedger(t, tx, secondDeferral.Accounting, 250, 750, 0)

	claim := claimRequest(t, f, secondDeferral.Accounting, first)
	claim.ClaimPeriod = proto.Clone(nextPeriod).(*tammyv1.PurchaseClaimPeriod)
	claim.ReleasePostingDate = basClaimOracleLaterReview()
	claim.TimingReview.ReviewedAsOf = basClaimOracleLaterReview()
	claim.DeferralId = &secondDeferral.Deferral.Id
	claimed, err := attributeCommand(t, repo, claim)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed.Attributions) != 1 || claimed.Attributions[0].ClaimedGst.MinorUnits != 500 || !proto.Equal(claimed.Attributions[0].Portion, first) || claimed.Attributions[0].GetClosesDeferralId() != secondDeferral.Deferral.Id {
		t.Fatalf("API did not attribute one full selected source while naming the later decision: %#v", claimed.Attributions)
	}
	if claimed.Journal == nil || claimed.Journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_GST_ATTRIBUTION || postScalar(t, tx, `SELECT count(*) FROM journals WHERE id=? AND organisation_id=? AND source_type='TAX' AND source_kind=8 AND source_id=? AND source_revision=1`, claimed.Journal.Id, postingOrg, claim.CommandContext.IdempotencyKey) != 1 {
		t.Fatal("aggregate release proof is not owned by the operation key as TAX kind 8 revision 1")
	}
	if postScalar(t, tx, `SELECT count(*) FROM journals WHERE id=? AND organisation_id=?`, claimed.Attributions[0].Id, postingOrg) != 0 {
		t.Fatal("physical attribution id was incorrectly treated as its aggregate journal proof owner")
	}

	afterWriters := purchaseBASScalarState(t, tx)
	current, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "post-claim full accounting replay")
	basClaimOracleAssertLedger(t, tx, current, 250, 250, 500)
	history, err := repo.VerifiedClaims(ctx, postingOrg, f.document.Id)
	if err != nil || len(history.Deferrals) != 2 || len(history.Attributions) != 1 {
		t.Fatalf("complete verified claim history=%#v, err=%v", history, err)
	}
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "VerifiedClaims full reader")
	writerDeferrals := []*tammyv1.PurchaseGstDeferral{firstDeferral.Deferral, secondDeferral.Deferral}
	if !proto.Equal(history.Deferrals[0], writerDeferrals[0]) || !proto.Equal(history.Deferrals[1], writerDeferrals[1]) || !proto.Equal(history.Attributions[0], claimed.Attributions[0]) {
		t.Fatalf("verified history differs from exact immutable writer responses: %#v", history)
	}
	if first.GetPayment() == nil || second.GetPayment() == nil || first.GetPayment().PaymentId == second.GetPayment().PaymentId || !proto.Equal(first.GetPayment(), history.Deferrals[1].Portions[0].GetPayment()) {
		t.Fatal("same-source repeat and sibling payment source identities were conflated")
	}

	wantDecisionPortions := map[basClaimOracleDeferralIdentity]struct {
		portion   *tammyv1.PurchaseGstSourcePortion
		allocated int64
		remaining int64
	}{
		{firstDeferral.Deferral.Id, 1}:  {first, 500, 0},
		{firstDeferral.Deferral.Id, 2}:  {second, 250, 250},
		{secondDeferral.Deferral.Id, 1}: {first, 500, 0},
	}
	uniqueOutstanding := basClaimOracleVerifiedPortions(t, history, wantDecisionPortions)
	if len(uniqueOutstanding) != 2 || uniqueOutstanding[basClaimOracleSource(t, first)] != 0 || uniqueOutstanding[basClaimOracleSource(t, second)] != 250 {
		t.Fatalf("physical deferral history did not collapse to one outstanding balance per exact source: %#v", uniqueOutstanding)
	}
	var uniqueUnclaimed int64
	for _, amount := range uniqueOutstanding {
		uniqueUnclaimed += amount
	}
	if uniqueUnclaimed != 250 {
		t.Fatalf("repeated period decisions multiplied outstanding credit: %d, want sibling-only 250", uniqueUnclaimed)
	}

	keys := basClaimOracleReadScalarKeys(t, ctx, repo)
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "BAS scalar summary and key reader")
	priorSnapshot, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, f.now)
	if err != nil || priorSnapshot == nil {
		t.Fatalf("capture prior-period BAS workpaper: snapshot=%v err=%v", priorSnapshot, err)
	}
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "prior-period BAS workpaper")
	selectedSnapshot, err := repo.CaptureBAS(ctx, postingOrg, nextPeriod, nextPeriod.PeriodEnd, f.now)
	if err != nil || selectedSnapshot == nil {
		t.Fatalf("capture selected-period BAS workpaper: snapshot=%v err=%v", selectedSnapshot, err)
	}
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "selected-period BAS workpaper")
	coverage := captureCoverage(t, repo, ctx)
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "complete CaptureCoverageForBAS source capture")
	basClaimOracleAssertCapturedClaims(t, coverage, state.Posting.Id, writerDeferrals, claimed.Attributions, wantDecisionPortions)
	basClaimOracleAssertScalarHistory(t, keys, writerDeferrals, claimed.Attributions, map[basClaimOracleDeferralIdentity]*tammyv1.PurchaseGstSourcePortion{
		{firstDeferral.Deferral.Id, 1}:  first,
		{firstDeferral.Deferral.Id, 2}:  second,
		{secondDeferral.Deferral.Id, 1}: first,
	}, map[string]string{
		firstDeferral.Deferral.Id:  firstRequest.CommandContext.IdempotencyKey,
		secondDeferral.Deferral.Id: secondRequest.CommandContext.IdempotencyKey,
	}, map[string]string{
		claimed.Attributions[0].Id: claim.CommandContext.IdempotencyKey,
	}, coverage)
	basClaimOracleAssertOwnedReleaseJournal(t, coverage, claim.CommandContext.IdempotencyKey, claimed.Journal)

	for _, snapshot := range []*purchases.BASSnapshot{priorSnapshot, selectedSnapshot} {
		if basClaimOracleDeferralReviewCount(snapshot) != 3 {
			t.Fatalf("BAS did not retain all three physical deferral decisions: %#v", snapshot.Reviews)
		}
		basClaimOracleAssertReview(t, snapshot, firstDeferral.Deferral.Id, first, 500, 0)
		basClaimOracleAssertReview(t, snapshot, firstDeferral.Deferral.Id, second, 250, 250)
		basClaimOracleAssertReview(t, snapshot, secondDeferral.Deferral.Id, first, 500, 0)
	}
	if len(priorSnapshot.Sources) != 0 || len(selectedSnapshot.Sources) != 1 {
		t.Fatalf("BAS period reconciliation sources prior/current=%d/%d, want no prior and one selected attribution", len(priorSnapshot.Sources), len(selectedSnapshot.Sources))
	}
	line := selectedSnapshot.Sources[0]
	if line.AttributionId == nil || *line.AttributionId != claimed.Attributions[0].Id || line.PaymentId == nil || *line.PaymentId != first.GetPayment().PaymentId || line.ClaimedGst.MinorUnits != 500 || !bytes.Equal(line.AttributionHash, claimed.Attributions[0].SourceHash) {
		t.Fatalf("BAS amount/source does not match the exact fully selected attribution: %#v", line)
	}
}

// Non-cash claims use the posting source family and must conserve the same
// three accounting states. This small cross-projection case also proves the
// physical deferral and attribution ids survive scalar, verified, and BAS reads.
func TestBASCandidateNonCashClaimDecisionScalarHistoryAndConservationOracle(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	basClaimOracleAssertLedger(t, tx, state, 0, 1000, 0)
	beforeJournals := postScalar(t, tx, `SELECT count(*) FROM journals`)
	deferInput := deferRequest(t, f, state, portion)
	deferred, err := deferCommand(t, repo, deferInput)
	if err != nil {
		t.Fatal(err)
	}
	if postScalar(t, tx, `SELECT count(*) FROM journals`) != beforeJournals {
		t.Fatal("non-cash deferral invented a release journal")
	}
	basClaimOracleAssertLedger(t, tx, deferred.Accounting, 0, 1000, 0)
	claim := claimRequest(t, f, deferred.Accounting, portion)
	claim.DeferralId = &deferred.Deferral.Id
	claimed, err := attributeCommand(t, repo, claim)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed.Attributions) != 1 || claimed.Attributions[0].ClaimedGst.MinorUnits != 1000 || !proto.Equal(claimed.Attributions[0].Portion, portion) {
		t.Fatalf("non-cash source was not attributed as one full selected portion: %#v", claimed.Attributions)
	}
	afterWriters := purchaseBASScalarState(t, tx)
	current, err := repo.Get(ctx, postingOrg, f.document.Id)
	if err != nil {
		t.Fatal(err)
	}
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "non-cash post-claim full accounting replay")
	basClaimOracleAssertLedger(t, tx, current, 0, 0, 1000)
	history, err := repo.VerifiedClaims(ctx, postingOrg, f.document.Id)
	if err != nil || len(history.Deferrals) != 1 || len(history.Attributions) != 1 || len(history.DeferralPortions) != 1 || history.DeferralPortions[0].UnclaimedGST.MinorUnits != 0 {
		t.Fatalf("non-cash complete history=%#v, err=%v", history, err)
	}
	writerDeferrals := []*tammyv1.PurchaseGstDeferral{deferred.Deferral}
	if !proto.Equal(history.Deferrals[0], writerDeferrals[0]) || !proto.Equal(history.Attributions[0], claimed.Attributions[0]) {
		t.Fatalf("non-cash verified history differs from exact immutable writer responses: %#v", history)
	}
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "non-cash VerifiedClaims full reader")
	keys := basClaimOracleReadScalarKeys(t, ctx, repo)
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "non-cash BAS scalar summary and key reader")
	wantPortions := map[basClaimOracleDeferralIdentity]struct {
		portion   *tammyv1.PurchaseGstSourcePortion
		allocated int64
		remaining int64
	}{
		{deferred.Deferral.Id, 1}: {portion, 1000, 0},
	}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, f.now)
	if err != nil || snapshot == nil {
		t.Fatalf("capture non-cash BAS workpaper: snapshot=%v err=%v", snapshot, err)
	}
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "non-cash BAS workpaper")
	coverage := captureCoverage(t, repo, ctx)
	basClaimOracleAssertReadOnly(t, tx, afterWriters, "non-cash complete CaptureCoverageForBAS source capture")
	basClaimOracleAssertCapturedClaims(t, coverage, state.Posting.Id, writerDeferrals, claimed.Attributions, wantPortions)
	basClaimOracleAssertScalarHistory(t, keys, writerDeferrals, claimed.Attributions, map[basClaimOracleDeferralIdentity]*tammyv1.PurchaseGstSourcePortion{
		{deferred.Deferral.Id, 1}: portion,
	}, map[string]string{
		deferred.Deferral.Id: deferInput.CommandContext.IdempotencyKey,
	}, map[string]string{
		claimed.Attributions[0].Id: claim.CommandContext.IdempotencyKey,
	}, coverage)
	basClaimOracleAssertOwnedReleaseJournal(t, coverage, claim.CommandContext.IdempotencyKey, claimed.Journal)
	basClaimOracleAssertReview(t, snapshot, deferred.Deferral.Id, portion, 1000, 0)
	if len(snapshot.Sources) != 1 || snapshot.Sources[0].AttributionId == nil || *snapshot.Sources[0].AttributionId != claimed.Attributions[0].Id || snapshot.Sources[0].ClaimedGst.MinorUnits != 1000 {
		t.Fatalf("non-cash BAS selected attribution amount=%#v", snapshot.Sources)
	}
}
