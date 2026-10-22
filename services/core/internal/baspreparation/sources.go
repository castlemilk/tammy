package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"math"
	"sort"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

const maximumBASRecordCandidates = 1000

// These ports intentionally expose only existing module-owned reservations,
// scalar selections, and bounded captures. Production construction must bind
// every concrete repository to the same caller-owned serializable SQLCipher
// transaction. No method here starts, commits, or rolls back a transaction.
type BASInvoiceRecordPort interface {
	SummarizeBASKeys(context.Context, string) (*invoices.BASScalarSummary, error)
	ReadBASKeys(context.Context, string, invoices.BASScalarSummary, int64, int64) ([]invoices.BASScalarKey, error)
	PreflightBASWithPayloadBudget(context.Context, string, *tammyv1.BasCivilDateRange, int64) (*invoices.BASReportingPreflight, error)
	CaptureBASWithPayloadBudget(context.Context, string, *tammyv1.BasCivilDateRange, int64) (*invoices.BASReportingCapture, error)
}

type BASPurchaseRecordPort interface {
	SummarizeBASKeys(context.Context, string) (*purchases.BASScalarSummary, error)
	ReadModernBASCandidates(context.Context, string, purchases.BASScalarSummary, int64, int64, int64) (*purchases.BASModernCandidateDiscovery, error)
	PreflightCoverageForBASWithPayloadBudget(context.Context, string, int64) (*purchases.PurchaseCoverageReportingPreflight, error)
	CaptureCoverageForBASWithPayloadBudget(context.Context, string, int64) (*purchases.PurchaseCoverageReportingCapture, error)
	SummarizeBASLegacyDecisions(context.Context, string) (*purchases.BASLegacyDecisionSummary, error)
	ReadBASLegacyDecisions(context.Context, string, purchases.BASLegacyDecisionSummary, int64, int64) (*purchases.BASLegacyDecisionRead, error)
	SummarizeBASDocumentStates(context.Context, string) (*purchases.BASDocumentStateSummary, error)
	ReadBASDocumentStates(context.Context, string, purchases.BASDocumentStateSummary, int64, int64) (*purchases.BASDocumentStateRead, error)
}

type BASAccountingRecordPort interface {
	SummarizeTaxFactBASKeys(context.Context, string) (*accounting.TaxFactBASScalarSummary, error)
	ReadTaxFactBASKeys(context.Context, string, accounting.TaxFactBASScalarSummary, int64, int64) ([]accounting.TaxFactBASScalarKey, error)
	PreflightTaxFactsForReportingWithPayloadBudget(context.Context, string, int64) (*accounting.TaxFactReportingPreflight, error)
	CaptureTaxFactsForReportingWithPayloadBudget(context.Context, string, int64) (*accounting.TaxFactReportingCapture, error)
	SummarizeBASLegacyCash(context.Context, accounting.BASLegacyCashQuery) (*accounting.BASLegacyCashSummary, error)
	ReadBASLegacyCash(context.Context, accounting.BASLegacyCashQuery, accounting.BASLegacyCashSummary, int64, int64) (*accounting.BASLegacyCashRead, error)
}

type BASRuleRecordPort interface {
	PreflightForReportingWithPayloadBudget(context.Context, string, *tammyv1.SourceRef, int64) (*artefacts.RuleBundleReportingPreflight, error)
	GetForReportingWithPayloadBudget(context.Context, string, *tammyv1.SourceRef, int64) (*artefacts.RuleBundleReportingRecord, error)
}

type BASBoundedSourcePorts struct {
	Invoices   BASInvoiceRecordPort
	Purchases  BASPurchaseRecordPort
	Accounting BASAccountingRecordPort
	Rules      BASRuleRecordPort
}

type BASBoundedSourceRequest struct {
	OrganisationID       string
	ConfirmedPeriod      *tammyv1.BasConfirmedPeriod
	ActiveRuleBundle     *tammyv1.SourceRef
	MaximumPayloadBytes  int64
	MaximumCandidateRows int64
}

// BASPurchaseSourceRecordView is a post-proof retained representation. Rows
// excludes exactly the selected raw tax_facts bodies after verifyBASSharedTaxFacts
// has matched every retained raw cell (including NULL shape) to Accounting.Facts.
// OriginalPayloadBytes, OriginalTaxFactPayloadBytes, Fingerprint, module counts,
// and journal charge maps describe the complete untouched module capture used
// for source fingerprinting and reservation, not the smaller returned Rows.
// Every tax fact still exists independently in the canonical accounting source
// records. TaxFactBodiesReplaced must remain true so downstream code cannot
// mistake this for an unmodified purchases capture.
type BASPurchaseSourceRecordView struct {
	Capture                     *purchases.PurchaseCoverageReportingCapture
	OriginalFingerprint         []byte
	OriginalPayloadBytes        int64
	OriginalTaxFactPayloadBytes int64
	TaxFactBodiesReplaced       bool
}

// BASBoundedSourceRecordCapture is a source-record layer, not BasSourceCoverage,
// a tax calculation, or a source-currentness hash. It preserves independent
// candidate families and module fingerprints for the later reviewed mapper.
type BASBoundedSourceRecordCapture struct {
	OrganisationID   string
	ConfirmedPeriod  *tammyv1.BasConfirmedPeriod
	ActiveRuleBundle *tammyv1.SourceRef

	ScalarReservation   *BASSourceScalarReservation
	InvoicePreflight    *invoices.BASReportingPreflight
	PurchasePreflight   *purchases.PurchaseCoverageReportingPreflight
	AccountingPreflight *accounting.TaxFactReportingPreflight
	RulePreflight       *artefacts.RuleBundleReportingPreflight
	DecisionSummary     *purchases.BASLegacyDecisionSummary
	DocumentSummary     *purchases.BASDocumentStateSummary
	CashQuery           accounting.BASLegacyCashQuery
	CashSummary         *accounting.BASLegacyCashSummary
	PayloadReservation  *BASPayloadReservation

	InvoiceKeys              []invoices.BASScalarKey
	InvoiceCandidates        []invoices.BASCandidateKey
	PurchaseKeys             []purchases.BASScalarKey
	ModernPurchaseCandidates *purchases.BASModernCandidateDiscovery
	LegacyPurchaseCandidates *purchases.BASLegacyCandidateDiscovery
	AccountingFactKeys       []accounting.TaxFactBASScalarKey
	DocumentStates           *purchases.BASDocumentStateRead
	LegacyDecisions          *purchases.BASLegacyDecisionRead
	LegacyCash               *accounting.BASLegacyCashRead

	Invoices        *invoices.BASReportingCapture
	Purchases       *BASPurchaseSourceRecordView
	Accounting      *accounting.TaxFactReportingCapture
	RuleBundle      *artefacts.RuleBundleReportingRecord
	SharedFactProof *BASSharedFactProof
	// Financial is populated only by CaptureBASFinancialSourceRecords, while
	// the complete purchase raw projection is still available for reconciliation.
	Financial *BASSourceFinancialMapping
}

// CaptureBASBoundedSourceRecords executes summary -> complete preflight -> typed
// keys -> cash summary -> one shared reservation -> bounded selector reads and
// pure nomination -> full source bodies. It returns no partial result and leaves
// all transaction lifecycle to its caller. Period dates are passed to the
// readers/legacy candidate selector; they never narrow the organisation census.
func CaptureBASBoundedSourceRecords(
	ctx context.Context,
	request BASBoundedSourceRequest,
	ports BASBoundedSourcePorts,
) (*BASBoundedSourceRecordCapture, error) {
	return captureBASBoundedSourceRecords(ctx, request, ports, nil)
}

func captureBASBoundedSourceRecords(ctx context.Context, request BASBoundedSourceRequest,
	ports BASBoundedSourcePorts, financialRequest *BASFinancialSourceRequest,
) (*BASBoundedSourceRecordCapture, error) {
	fail := func(err error) (*BASBoundedSourceRecordCapture, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return nil, errors.Join(ErrSourceReservation, err)
	}
	if ctx == nil || request.OrganisationID == "" || request.ConfirmedPeriod == nil ||
		request.ConfirmedPeriod.OrganisationId != request.OrganisationID ||
		request.ConfirmedPeriod.Confirmation == nil || request.ConfirmedPeriod.Confirmation.Period == nil ||
		request.ActiveRuleBundle == nil || request.MaximumPayloadBytes < 0 || request.MaximumCandidateRows < 0 ||
		ports.Invoices == nil || ports.Purchases == nil || ports.Accounting == nil || ports.Rules == nil {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if _, err := FamilyScopeFromPeriod(request.ConfirmedPeriod); err != nil {
		return fail(err)
	}
	periodRange := request.ConfirmedPeriod.Confirmation.Period
	claimPeriod := &tammyv1.PurchaseClaimPeriod{
		PeriodStart: proto.Clone(periodRange.StartDate).(*tammyv1.CivilDate),
		PeriodEnd:   proto.Clone(periodRange.EndDate).(*tammyv1.CivilDate),
	}
	balanceDate := cloneCalendarDate(periodRange.EndDate)
	maximumPayload := request.MaximumPayloadBytes
	if maximumPayload > maximumBASSourcePayloadBytes {
		maximumPayload = maximumBASSourcePayloadBytes
	}
	maximumCandidates := request.MaximumCandidateRows
	if maximumCandidates > maximumBASRecordCandidates {
		maximumCandidates = maximumBASRecordCandidates
	}

	// Stage 1: only the existing three aggregate summaries. No parent IDs or
	// bodies can be read before shared root, receipt, key-count, and byte caps.
	scalar, err := ReserveBASSourceScalars(ctx, request.OrganisationID, BASSourceScalarPorts{
		Invoices: ports.Invoices, Purchases: ports.Purchases, Accounting: ports.Accounting,
	}, maximumBASSourceScalarBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}

	// Stage 2: all full-payload size reservations before any typed-key reads.
	// Purchase's exact tax-fact reuse proof is run only after the complete
	// accounting preflight above, on these same caller-owned transaction ports.
	invoicePreflight, err := ports.Invoices.PreflightBASWithPayloadBudget(ctx,
		request.OrganisationID, periodRange, maximumBASSourcePayloadBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	accountingPreflight, err := ports.Accounting.PreflightTaxFactsForReportingWithPayloadBudget(ctx,
		request.OrganisationID, maximumBASSourcePayloadBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	purchasePreflight, err := ports.Purchases.PreflightCoverageForBASWithPayloadBudget(ctx,
		request.OrganisationID, maximumBASSourcePayloadBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	rulePreflight, err := ports.Rules.PreflightForReportingWithPayloadBudget(ctx,
		request.OrganisationID, request.ActiveRuleBundle, maximumBASSourcePayloadBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	decisionSummary, err := ports.Purchases.SummarizeBASLegacyDecisions(ctx, request.OrganisationID)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	documentSummary, err := ports.Purchases.SummarizeBASDocumentStates(ctx, request.OrganisationID)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if err := validateBASSourcePreflightCounts(scalar, invoicePreflight, purchasePreflight, accountingPreflight,
		rulePreflight, decisionSummary, documentSummary); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	// Reject an already oversized known payload before copying any typed keys.
	// Cash is a dependent nonnegative category; its exact reservation follows
	// discovery of the selected root IDs and repeats this final byte check.
	if _, err := reserveBASPayloadBytes(request.OrganisationID, *scalar,
		moduleSizesFromInvoicePreflight(invoicePreflight), moduleSizesFromPurchasePreflight(purchasePreflight),
		moduleSizesFromAccountingPreflight(accountingPreflight), BASPayloadExtraSizes{
			RuleBytes: rulePreflight.PayloadBytes, DecisionBytes: decisionSummary.DecisionBytes,
			DocumentStateBytes: documentSummary.StateBytes, PurchaseTaxFactBytes: purchasePreflight.TaxFactPayloadBytes,
		}, purchasePreflight.TaxFactReuseProof, purchasePreflight.TaxFactCount, maximumPayload); err != nil {
		return fail(err)
	}

	// Stage 3: only bounded typed identities. Modern purchase discovery reads
	// its family set once and returns the exact physical keys for the later
	// legacy nomination/full-proof comparison. Accounting fact keys remain an
	// independent candidate stream even when purchase facts prove byte equality.
	invoiceKeys, err := ports.Invoices.ReadBASKeys(ctx, request.OrganisationID, scalar.Invoices,
		scalar.Invoices.ScalarKeyCount, scalar.Invoices.ScalarBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	invoiceCandidates, err := invoices.NominateBASCandidates(ctx, scalar.Invoices, invoiceKeys, maximumCandidates)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	modernPurchase, err := ports.Purchases.ReadModernBASCandidates(ctx, request.OrganisationID, scalar.Purchases,
		scalar.Purchases.ScalarKeyCount, scalar.Purchases.ScalarBytes, maximumCandidates)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if modernPurchase == nil {
		return fail(nil)
	}
	purchaseKeys := modernPurchase.PhysicalKeys
	accountingKeys, err := ports.Accounting.ReadTaxFactBASKeys(ctx, request.OrganisationID, scalar.Accounting,
		scalar.Accounting.ScalarKeyCount, scalar.Accounting.ScalarBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if int64(len(invoiceKeys)) != scalar.Invoices.ScalarKeyCount ||
		int64(len(purchaseKeys)) != scalar.Purchases.ScalarKeyCount ||
		int64(len(accountingKeys)) != scalar.Accounting.ScalarKeyCount {
		return fail(nil)
	}

	// Cash is a dependent selector: its candidate root IDs are available only
	// after guarded purchase keys. It is summary-only until the combined cap.
	cashSources, err := basLegacyCashRootIDs(request.OrganisationID, purchaseKeys)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	cashQuery := accounting.BASLegacyCashQuery{OrganisationID: request.OrganisationID, SourceIDs: cashSources}
	cashSummary, err := ports.Accounting.SummarizeBASLegacyCash(ctx, cashQuery)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if !validBASLegacyCashSummary(request.OrganisationID, cashSummary) {
		return fail(nil)
	}

	// Stage 4: a single global ceiling, including all scalar rows, all selector
	// projections, each module base and each complete journal once. The physical
	// purchase-fact discount changes only byte accounting, never candidate sets.
	payloadReservation, err := reserveBASPayloadBytes(
		request.OrganisationID, *scalar,
		moduleSizesFromInvoicePreflight(invoicePreflight),
		moduleSizesFromPurchasePreflight(purchasePreflight),
		moduleSizesFromAccountingPreflight(accountingPreflight),
		BASPayloadExtraSizes{
			RuleBytes: rulePreflight.PayloadBytes, DecisionBytes: decisionSummary.DecisionBytes,
			DocumentStateBytes: documentSummary.StateBytes, CashProjectionBytes: cashSummary.Bytes,
			PurchaseTaxFactBytes: purchasePreflight.TaxFactPayloadBytes,
		},
		purchasePreflight.TaxFactReuseProof, purchasePreflight.TaxFactCount, maximumPayload,
	)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	// Stage 5: bounded selector bodies and a frozen provisional legacy choice;
	// no complete invoice/purchase/accounting/rule bodies are loaded until after
	// this pure selection has completed.
	documentStates, err := ports.Purchases.ReadBASDocumentStates(ctx, request.OrganisationID,
		*documentSummary, documentSummary.DocumentCount, documentSummary.StateBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	decisions, err := ports.Purchases.ReadBASLegacyDecisions(ctx, request.OrganisationID,
		*decisionSummary, decisionSummary.RecordCount, decisionSummary.DecisionBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	cashRead, err := ports.Accounting.ReadBASLegacyCash(ctx, cashQuery, *cashSummary,
		cashSummary.RowCount, cashSummary.Bytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if documentStates == nil || documentStates.Summary != *documentSummary || decisions == nil ||
		decisions.Summary != *decisionSummary || cashRead == nil || cashRead.Summary != *cashSummary {
		return fail(nil)
	}
	legacyCandidates, err := purchases.NominateBASLegacyCandidates(ctx, request.OrganisationID,
		claimPeriod, balanceDate, documentStates.Documents, purchaseKeys, decisions.Reviews,
		*cashRead, maximumCandidates)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	count, valid := sumBASSourceScalars(int64(len(invoiceCandidates)), int64(len(modernPurchase.Candidates)), int64(len(legacyCandidates.Candidates)), int64(len(accountingKeys)))
	if !valid || count > maximumCandidates {
		return fail(&SourceBoundError{Resource: "coverage candidates", Limit: maximumCandidates})
	}

	// Stage 6: only now hydrate the complete bounded module bodies. Each reader
	// repeats and checks its own reservation on the same transaction. The global
	// reservation above is the prerequisite to the first body query.
	invoiceCapture, err := ports.Invoices.CaptureBASWithPayloadBudget(ctx,
		request.OrganisationID, periodRange, maximumBASSourcePayloadBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if err := validateBASInvoiceCapture(invoicePreflight, invoiceCapture); err != nil {
		return fail(err)
	}
	purchaseCapture, err := ports.Purchases.CaptureCoverageForBASWithPayloadBudget(ctx,
		request.OrganisationID, maximumBASSourcePayloadBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if err := validateBASPurchaseCapture(request.OrganisationID, purchasePreflight, purchaseCapture); err != nil {
		return fail(err)
	}
	accountingCapture, err := ports.Accounting.CaptureTaxFactsForReportingWithPayloadBudget(ctx,
		request.OrganisationID, maximumBASSourcePayloadBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if err := validateBASAccountingCapture(accountingPreflight, accountingCapture); err != nil {
		return fail(err)
	}
	ruleBundle, err := ports.Rules.GetForReportingWithPayloadBudget(ctx,
		request.OrganisationID, request.ActiveRuleBundle, rulePreflight.PayloadBytes)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if err := validateBASSourceCaptures(request.OrganisationID, invoicePreflight, purchasePreflight, accountingPreflight,
		rulePreflight, invoiceCapture, purchaseCapture, accountingCapture, ruleBundle); err != nil {
		return fail(err)
	}

	if err := reconcileBASInvoicePhysicalKeys(ctx, request.OrganisationID, invoiceKeys, invoiceCapture); err != nil {
		return fail(err)
	}
	if err := reconcileBASTaxFactPhysicalKeys(ctx, request.OrganisationID, accountingKeys, accountingCapture); err != nil {
		return fail(err)
	}

	if err := purchases.ReconcileBASPhysicalKeys(ctx, request.OrganisationID, purchaseKeys, purchaseCapture); err != nil {
		return fail(err)
	}

	if !sameBASSourceRef(ruleBundle.Bundle.Source, request.ActiveRuleBundle) {
		return fail(nil)
	}

	// Complete raw purchase/accounting cell parity is required after the
	// pre-body proof; the early discount alone is not a semantic fact proof.
	sharedFactProof, err := verifyBASSharedTaxFacts(ctx, request.OrganisationID, purchaseCapture, accountingCapture)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if sharedFactProof.Count != int(purchasePreflight.TaxFactCount) ||
		sharedFactProof.PurchaseBytes != purchasePreflight.TaxFactPayloadBytes {
		return fail(nil)
	}
	if err := verifyBASSharedJournalCaptureProofs(ctx, request.OrganisationID,
		invoiceCapture, purchaseCapture, accountingCapture); err != nil {
		return fail(err)
	}
	if err := purchases.ReconcileBASLegacyCandidates(ctx, request.OrganisationID,
		claimPeriod, balanceDate, documentStates.Documents, purchaseKeys, decisions.Reviews,
		*cashRead, legacyCandidates, purchaseCapture); err != nil {
		return fail(err)
	}
	if err := basRebindSharedJournalBodies(invoiceCapture, purchaseCapture, accountingCapture); err != nil {
		return fail(err)
	}
	var financial *BASSourceFinancialMapping
	if financialRequest != nil {
		mappedInvoices, err := MapBASInvoiceFinancialEffects(ctx, invoiceCapture, invoiceCandidates,
			financialRequest.GstBasis, periodRange)
		if err != nil {
			return fail(err)
		}
		mappedPurchases, err := purchases.MapBASPurchaseReporting(ctx, purchaseCapture, modernPurchase,
			legacyCandidates, financialRequest.GstBasis, claimPeriod, balanceDate, financialRequest.AsOf)
		if err != nil {
			return fail(err)
		}
		financial = &BASSourceFinancialMapping{Invoices: mappedInvoices, Purchases: mappedPurchases}
	}

	// The reuse proof authorizes a retained-memory representation change only
	// after both the canonical all-cell comparison and full legacy replay. The
	// original module fingerprint and complete charge metadata remain untouched.
	purchaseView, err := basDropProvedPurchaseTaxFactBodies(purchaseCapture,
		purchasePreflight.TaxFactPayloadBytes, sharedFactProof)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return &BASBoundedSourceRecordCapture{
		OrganisationID:    request.OrganisationID,
		ConfirmedPeriod:   proto.Clone(request.ConfirmedPeriod).(*tammyv1.BasConfirmedPeriod),
		ActiveRuleBundle:  proto.Clone(request.ActiveRuleBundle).(*tammyv1.SourceRef),
		ScalarReservation: scalar, InvoicePreflight: invoicePreflight,
		PurchasePreflight: purchasePreflight, AccountingPreflight: accountingPreflight,
		RulePreflight: rulePreflight, DecisionSummary: decisionSummary,
		DocumentSummary: documentSummary, CashQuery: cashQuery, CashSummary: cashSummary,
		PayloadReservation: payloadReservation, InvoiceKeys: invoiceKeys,
		InvoiceCandidates: invoiceCandidates, PurchaseKeys: purchaseKeys,
		ModernPurchaseCandidates: modernPurchase, LegacyPurchaseCandidates: legacyCandidates,
		AccountingFactKeys: accountingKeys, DocumentStates: documentStates,
		LegacyDecisions: decisions, LegacyCash: cashRead, Invoices: invoiceCapture,
		Purchases: purchaseView, Accounting: accountingCapture, RuleBundle: ruleBundle,
		SharedFactProof: sharedFactProof,
		Financial:       financial,
	}, nil
}

func basLegacyCashRootIDs(organisationID string, keys []purchases.BASScalarKey) ([]string, error) {
	set := make(map[string]struct{})
	for _, key := range keys {
		if key.OrganisationID != organisationID {
			return nil, ErrSourceReservation
		}
		if !key.IsRoot || (key.Kind != purchases.BASScalarDocument && key.Kind != purchases.BASScalarOpeningPayable) {
			continue
		}
		if key.ID == "" {
			return nil, ErrSourceReservation
		}
		set[key.ID] = struct{}{}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func moduleSizesFromInvoicePreflight(p *invoices.BASReportingPreflight) BASPayloadModuleSizes {
	if p == nil {
		return BASPayloadModuleSizes{}
	}
	return BASPayloadModuleSizes{BasePayloadBytes: p.BasePayloadBytes, PayloadBytes: p.PayloadBytes,
		JournalIDs: append([]string(nil), p.JournalIDs...), JournalBytes: cloneBASByteMap(p.JournalPayloadBytes)}
}
func moduleSizesFromPurchasePreflight(p *purchases.PurchaseCoverageReportingPreflight) BASPayloadModuleSizes {
	if p == nil {
		return BASPayloadModuleSizes{}
	}
	return BASPayloadModuleSizes{BasePayloadBytes: p.BasePayloadBytes, PayloadBytes: p.PayloadBytes,
		JournalIDs: append([]string(nil), p.JournalIDs...), JournalBytes: cloneBASByteMap(p.JournalPayloadBytes)}
}
func moduleSizesFromAccountingPreflight(p *accounting.TaxFactReportingPreflight) BASPayloadModuleSizes {
	if p == nil {
		return BASPayloadModuleSizes{}
	}
	return BASPayloadModuleSizes{BasePayloadBytes: p.BasePayloadBytes, PayloadBytes: p.PayloadBytes,
		JournalIDs: append([]string(nil), p.JournalIDs...), JournalBytes: cloneBASByteMap(p.JournalPayloadBytes)}
}
func cloneBASByteMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func validateBASSourcePreflightCounts(
	s *BASSourceScalarReservation,
	i *invoices.BASReportingPreflight,
	p *purchases.PurchaseCoverageReportingPreflight,
	a *accounting.TaxFactReportingPreflight,
	r *artefacts.RuleBundleReportingPreflight,
	d *purchases.BASLegacyDecisionSummary,
	sd *purchases.BASDocumentStateSummary,
) error {
	if s == nil || i == nil || p == nil || a == nil || r == nil || d == nil || sd == nil ||
		i.CandidateCount != int(s.Invoices.RootCount) || i.ReceiptCount != int(s.Invoices.ReceiptCount) ||
		p.RootCount != int(s.Purchases.RootCount) || p.ReceiptCount != int(s.Purchases.ReceiptCount) ||
		a.FactCount != int(s.Accounting.ScalarKeyCount) || p.TaxFactCount != p.TaxFactReuseProof.FactCount ||
		p.TaxFactReuseProof.MatchedFactCount != p.TaxFactCount || p.TaxFactReuseProof.MismatchCount != 0 ||
		p.TaxFactReuseProof.PayloadBytes != p.TaxFactPayloadBytes ||
		r.CatalogueCount < 0 || r.ParentPayloadBytes < 0 || r.CataloguePayloadBytes < 0 || r.PayloadBytes < 0 ||
		r.PayloadBytes != r.ParentPayloadBytes+r.CataloguePayloadBytes ||
		d.OrganisationID != s.Purchases.OrganisationID || sd.OrganisationID != s.Purchases.OrganisationID ||
		d.RecordCount < 0 || d.DecisionBytes < 0 || sd.DocumentCount < 0 || sd.StateBytes < 0 {
		return ErrSourceReservation
	}
	return nil
}

func validBASLegacyCashSummary(organisationID string, s *accounting.BASLegacyCashSummary) bool {
	if s == nil || s.OrganisationID != organisationID || s.AssociationCount < 0 || s.JournalCount < 0 || s.LineCount < 0 || s.RowCount < 0 || s.Bytes < 0 {
		return false
	}
	if s.AssociationCount > math.MaxInt64-s.JournalCount || s.AssociationCount+s.JournalCount > math.MaxInt64-s.LineCount {
		return false
	}
	rows := s.AssociationCount + s.JournalCount + s.LineCount
	return rows == s.RowCount && rows <= maximumBASSourcePayloadBytes/512 && s.Bytes >= rows*512 && s.Bytes <= maximumBASSourcePayloadBytes
}

func validateBASInvoiceCapture(p *invoices.BASReportingPreflight, c *invoices.BASReportingCapture) error {
	if p == nil || c == nil || len(c.Fingerprint) != 32 || c.CandidateCount != p.CandidateCount || len(c.Candidates) != c.CandidateCount || c.ReceiptCount != p.ReceiptCount || c.JournalCount != p.JournalCount || c.BasePayloadBytes != p.BasePayloadBytes || c.PayloadBytes != p.PayloadBytes || !sameBASStringSlice(c.JournalIDs, p.JournalIDs) || !sameBASByteMap(c.JournalPayloadBytes, p.JournalPayloadBytes) {
		return ErrSourceReservation
	}
	return nil
}
func validateBASPurchaseCapture(org string, p *purchases.PurchaseCoverageReportingPreflight, c *purchases.PurchaseCoverageReportingCapture) error {
	if p == nil || c == nil || c.OrganisationID != org || len(c.Fingerprint) != 32 || c.RootCount != p.RootCount || c.ReceiptCount != p.ReceiptCount || c.JournalCount != p.JournalCount || c.BasePayloadBytes != p.BasePayloadBytes || c.PayloadBytes != p.PayloadBytes || c.TaxFactPayloadBytes != p.TaxFactPayloadBytes || !sameBASStringSlice(c.JournalIDs, p.JournalIDs) || !sameBASByteMap(c.JournalPayloadBytes, p.JournalPayloadBytes) {
		return ErrSourceReservation
	}
	return nil
}
func validateBASAccountingCapture(p *accounting.TaxFactReportingPreflight, c *accounting.TaxFactReportingCapture) error {
	if p == nil || c == nil || len(c.Fingerprint) != 32 || c.FactCount != p.FactCount || c.FactCount != len(c.Facts) || c.JournalCount != p.JournalCount || c.BasePayloadBytes != p.BasePayloadBytes || c.PayloadBytes != p.PayloadBytes || !sameBASStringSlice(c.JournalIDs, p.JournalIDs) || !sameBASByteMap(c.JournalPayloadBytes, p.JournalPayloadBytes) {
		return ErrSourceReservation
	}
	return nil
}
func validateBASSourceCaptures(
	org string, ip *invoices.BASReportingPreflight, pp *purchases.PurchaseCoverageReportingPreflight,
	ap *accounting.TaxFactReportingPreflight, rp *artefacts.RuleBundleReportingPreflight,
	i *invoices.BASReportingCapture, p *purchases.PurchaseCoverageReportingCapture,
	a *accounting.TaxFactReportingCapture, r *artefacts.RuleBundleReportingRecord,
) error {
	if err := validateBASInvoiceCapture(ip, i); err != nil {
		return err
	}
	if err := validateBASPurchaseCapture(org, pp, p); err != nil {
		return err
	}
	if err := validateBASAccountingCapture(ap, a); err != nil {
		return err
	}
	if rp == nil || r == nil || r.OrganisationID != org || len(r.ContentHash) != 32 || r.PayloadBytes != rp.PayloadBytes || r.Bundle.Source == nil || r.Bundle.Source.Id != rp.BundleID || len(r.Catalogue) != rp.CatalogueCount {
		return ErrSourceReservation
	}
	return nil
}
func sameBASStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func sameBASByteMap(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		other, ok := b[k]
		if !ok || other != v {
			return false
		}
	}
	return true
}

// basRebindSharedJournalBodies makes the proven physical identity sharing true
// in the returned object graph. It runs only after exact charge+hash agreement.
func basRebindSharedJournalBodies(
	i *invoices.BASReportingCapture,
	p *purchases.PurchaseCoverageReportingCapture,
	a *accounting.TaxFactReportingCapture,
) error {
	if i == nil || p == nil || a == nil {
		return ErrSourceReservation
	}
	canonical := make(map[string]*accounting.JournalReportingRecord)
	add := func(proof *accounting.JournalReportingRecord) error {
		if proof == nil || proof.Journal == nil || proof.Journal.Id == "" {
			return ErrSourceReservation
		}
		if old := canonical[proof.Journal.Id]; old == nil {
			canonical[proof.Journal.Id] = proof
		} else if old.PayloadBytes != proof.PayloadBytes || !bytes.Equal(old.ContentHash, proof.ContentHash) {
			return ErrSourceReservation
		}
		return nil
	}
	// Stable precedence is irrelevant to identity; accounting, purchase,
	// invoice are used so all references end at one shared retained proof.
	for _, proof := range a.Journals {
		if err := add(proof); err != nil {
			return err
		}
	}
	for _, proof := range p.Journals {
		if err := add(proof); err != nil {
			return err
		}
	}
	for _, candidate := range i.Candidates {
		if candidate.Posting != nil && candidate.Posting.Journal != nil {
			if err := add(candidate.Posting.Journal); err != nil {
				return err
			}
		}
		for _, receipt := range candidate.Receipts {
			if receipt.Journal != nil {
				if err := add(receipt.Journal); err != nil {
					return err
				}
			}
		}
	}
	bind := func(proof *accounting.JournalReportingRecord) (*accounting.JournalReportingRecord, error) {
		if proof == nil || proof.Journal == nil {
			return nil, ErrSourceReservation
		}
		want := canonical[proof.Journal.Id]
		if want == nil || want.PayloadBytes != proof.PayloadBytes || !bytes.Equal(want.ContentHash, proof.ContentHash) {
			return nil, ErrSourceReservation
		}
		return want, nil
	}
	for index, proof := range p.Journals {
		rebound, err := bind(proof)
		if err != nil {
			return err
		}
		p.Journals[index] = rebound
	}
	for _, candidate := range i.Candidates {
		if candidate.Posting != nil && candidate.Posting.Journal != nil {
			rebound, err := bind(candidate.Posting.Journal)
			if err != nil {
				return err
			}
			candidate.Posting.Journal = rebound
		}
		for index := range candidate.Receipts {
			if candidate.Receipts[index].Journal != nil {
				rebound, err := bind(candidate.Receipts[index].Journal)
				if err != nil {
					return err
				}
				candidate.Receipts[index].Journal = rebound
			}
		}
	}
	for index, proof := range a.Journals {
		rebound, err := bind(proof)
		if err != nil {
			return err
		}
		a.Journals[index] = rebound
	}
	for index := range a.Facts {
		fact := &a.Facts[index]
		if fact.Journal == nil || fact.Journal.Id == "" {
			return ErrSourceReservation
		}
		proof := canonical[fact.Journal.Id]
		if proof == nil || proof.Journal.OrganisationId != fact.Journal.OrganisationId || !proto.Equal(fact.Journal, proof.Journal) {
			return ErrSourceReservation
		}
		fact.Journal = proof.Journal
	}
	return nil
}

func basDropProvedPurchaseTaxFactBodies(
	capture *purchases.PurchaseCoverageReportingCapture,
	provedBytes int64,
	proof *BASSharedFactProof,
) (*BASPurchaseSourceRecordView, error) {
	if capture == nil || proof == nil || provedBytes < 0 || proof.PurchaseBytes != provedBytes {
		return nil, ErrSourceReservation
	}
	view := *capture
	filtered := make([]purchases.PurchaseReportingRetainedRow, 0, len(capture.Rows))
	removed := 0
	for _, row := range capture.Rows {
		if row.Table == "tax_facts" {
			removed++
			continue
		}
		filtered = append(filtered, row)
	}
	if removed != proof.Count {
		return nil, ErrSourceReservation
	}
	view.Rows = filtered
	return &BASPurchaseSourceRecordView{Capture: &view, OriginalFingerprint: append([]byte(nil), capture.Fingerprint...),
		OriginalPayloadBytes: capture.PayloadBytes, OriginalTaxFactPayloadBytes: capture.TaxFactPayloadBytes, TaxFactBodiesReplaced: true}, nil
}

// No financial classification, final coverage mapping, source snapshot hash,
// or calculation append belongs in this bounded capture layer.
