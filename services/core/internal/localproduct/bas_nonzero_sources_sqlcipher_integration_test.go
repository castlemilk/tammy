//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNonzeroCashBASSourcesComposeThroughRealTLS(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx := context.Background()

	// A public invoice issue posts income, followed by one receipt in each
	// confirmed quarter.
	invoice, receiptQ3, receiptQ4 := createAndPartPayBASInvoiceThroughTLS(t, server)

	// The existing reviewed-purchase fixture posts an actual supplier invoice;
	// two public payments split its eligible GST across Q3 and Q4.
	document := server.reviewedPurchase(t)
	postRequest := server.postRequest(t, document)
	postRequest.CommandContext.IdempotencyKey = testBASCommandKey(0xa001)
	posted, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(postRequest, server.capability))
	if err != nil || posted.Msg.Posting == nil || !posted.Msg.Accounting.Posted ||
		posted.Msg.Accounting.AccountingVersion != 1 {
		t.Fatalf("public reviewed purchase post=%v err=%v", posted, err)
	}
	purchaseQ3 := recordBASPurchasePaymentTLS(t, server, document.Id, 1, 0xa002, 2026, 9, 30, 5500)
	purchaseQ4 := recordBASPurchasePaymentTLS(t, server, document.Id, 2, 0xa003, 2026, 10, 1, 5500)

	profileInput := localBASSupportedGSTProfileInput()
	profileInput.Gst.GstBasis = tammyv1.BasGstBasis_BAS_GST_BASIS_CASH
	profileInput.Paygw.Applicability = &tammyv1.BasObligationAnswer{
		Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
		Reason:        "Reviewed small quarterly withholding scope",
		Evidence:      []*tammyv1.SourceRef{nonzeroBASRef("paygw_registration", 0xa010)},
	}
	profileInput.Paygw.Category = tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_SMALL
	profileInput.Paygw.Cadence = tammyv1.BasCadence_BAS_CADENCE_QUARTERLY
	profileInput.Paygw.PrefillRequirement = tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_NOT_REQUIRED
	profileInput.Paygw.StatementRequirementEvidence = []*tammyv1.SourceRef{nonzeroBASRef("paygw_statement_requirement", 0xa011)}
	incomeYear := &tammyv1.BasIncomeYearIdentity{
		IncomeYearId: testBASCommandKey(0xa012),
		Dates: &tammyv1.BasCivilDateRange{
			StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
			EndDate:   &tammyv1.CivilDate{Year: 2027, Month: 6, Day: 30},
		},
		Confirmation: nonzeroBASRef("ato_income_year", 0xa013),
	}
	profileInput.Paygi.Applicability = &tammyv1.BasObligationAnswer{
		Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE,
		Reason:        "Reviewed operative notified-rate instalment applies",
		Evidence:      []*tammyv1.SourceRef{nonzeroBASRef("ato_paygi_profile", 0xa014)},
	}
	profileInput.Paygi.Method = tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE
	profileInput.Paygi.Cadence = tammyv1.BasCadence_BAS_CADENCE_QUARTERLY
	profileInput.Paygi.ConfirmedIncomeYear = incomeYear
	profileInput.Paygi.Evidence = []*tammyv1.SourceRef{nonzeroBASRef("ato_paygi_profile", 0xa015)}
	profile, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(
		&tammyv1.SetBasObligationsProfileRequest{
			CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(0xa016)},
			OrganisationId: server.organisationID, Profile: profileInput,
		}, server.capability))
	if err != nil || profile.Msg.Profile == nil {
		t.Fatalf("set supported profile=%v err=%v", profile, err)
	}
	profileRef := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: profile.Msg.Profile.ProfileId,
		Revision: profile.Msg.Profile.Revision, ContentHash: bytes.Clone(profile.Msg.Profile.ContentHash)}

	// Confirm both periods first so source capture sees stable profile/period
	// identities. Each period has exactly one receipt/payment for each source.
	periodQ3 := confirmNonzeroBASPeriodTLS(t, server, profileRef, 0xa020,
		2026, 7, 1, 2026, 9, 30, tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP, 2026, 10, 28)
	periodQ4 := confirmNonzeroBASPeriodTLS(t, server, profileRef, 0xa030,
		2026, 10, 1, 2026, 12, 31, tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC, 2027, 2, 28)

	paygw, paygi := nonzeroBASPaygInputs(incomeYear)
	preparationQ3, calculationQ3 := regenerateNonzeroBASPeriodTLS(t, server, profileRef, periodQ3, paygw, paygi, 0xa100)
	_, calculationQ4 := regenerateNonzeroBASPeriodTLS(t, server, profileRef, periodQ4, paygw, paygi, 0xa200)

	assertNonzeroBASLabels(t, calculationQ3)
	assertNonzeroBASLabels(t, calculationQ4)
	assertNonzeroBASSourceLines(t, calculationQ3, invoice.Id, receiptQ3.Payment.Id, document,
		posted.Msg.Posting.Id, purchaseQ3.Payment.Id, 2026, 9, 30)
	assertNonzeroBASSourceLines(t, calculationQ4, invoice.Id, receiptQ4.Payment.Id, document,
		posted.Msg.Posting.Id, purchaseQ4.Payment.Id, 2026, 10, 1)

	validated, err := server.basPreparation.ValidateBasPreparation(ctx, brandingRequest(
		validateBASReviewRequest(server, preparationQ3, calculationQ3, 0xa301), server.capability))
	if err != nil || validated.Msg.Preparation == nil || validated.Msg.Validation == nil ||
		validated.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		len(validated.Msg.Validation.Findings) != 0 ||
		!bytes.Equal(validated.Msg.Validation.SourceSnapshotHash, calculationQ3.SourceSnapshotHash) {
		t.Fatalf("validate nonzero supported Q3 statement=%v err=%v", validated, err)
	}
	assigned := addBusinessLodgerRolePreservingExistingRoles(t, server, server.authentication, 0xa302)
	if !basFreezeHasRole(assigned.Roles, tammyv1.Role_ROLE_BUSINESS_LODGER) {
		t.Fatalf("test actor lacks business-lodger role after assignment: %v", assigned.Roles)
	}
	chain := readBASFreezeChain(t, server, server.authentication, validated.Msg.Preparation)
	factor := assertBASFreezeApprovalFactor(t, server, server.authentication, basFreezeApprovalPassword, 0xa303)
	frozen, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(
		makeBASFreezeRequest(server, server.authentication, validated.Msg.Preparation, calculationQ3,
			validated.Msg.Validation, chain, factor, 0xa304), server.capability))
	if err != nil || frozen.Msg.FrozenBas == nil ||
		frozen.Msg.FrozenBas.Calculation == nil ||
		frozen.Msg.FrozenBas.Calculation.Id != calculationQ3.CalculationId ||
		!bytes.Equal(frozen.Msg.FrozenBas.Calculation.ContentHash, calculationQ3.ContentHash) {
		t.Fatalf("freeze nonzero supported Q3 statement=%v err=%v", frozen, err)
	}
	assertBASFrozenOutput(t, frozen.Msg, validated.Msg.Preparation, calculationQ3,
		validated.Msg.Validation, factor, server.authentication.ActorUserId)
}

func createAndPartPayBASInvoiceThroughTLS(t *testing.T, server documentPageServer) (
	*tammyv1.Invoice, *tammyv1.RecordInvoicePaymentResponse, *tammyv1.RecordInvoicePaymentResponse,
) {
	t.Helper()
	ctx := context.Background()
	saved, err := server.invoices.SaveInvoiceDraft(ctx, brandingRequest(&tammyv1.SaveInvoiceDraftRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(0xa040)},
		OrganisationId: server.organisationID,
		Draft: &tammyv1.InvoiceDraft{
			Number: "BAS-CASH-RECEIPTS-1", IssueDate: "2026-09-30", DueDate: "2026-10-30",
			CustomerName: "BAS customer", CustomerAddress: "1 Customer Street",
			SellerAddress: "27 Cambridge Road", GstRegistered: true,
			Lines: []*tammyv1.InvoiceLineInput{{
				Description: "Taxable consulting services", QuantityMilli: 1000, UnitPriceMinor: 20000,
				TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE,
			}},
		},
	}, server.capability))
	if err != nil || saved.Msg.Invoice == nil || saved.Msg.Invoice.TotalMinor != 22000 || saved.Msg.Invoice.GstMinor != 2000 {
		t.Fatalf("save actual taxable invoice=%v err=%v", saved, err)
	}
	issued, err := server.invoices.IssueInvoice(ctx, brandingRequest(&tammyv1.IssueInvoiceRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(0xa041)},
		OrganisationId: server.organisationID, InvoiceId: saved.Msg.Invoice.Id, ExpectedVersion: saved.Msg.Invoice.Version,
	}, server.capability))
	if err != nil || issued.Msg.Invoice == nil || issued.Msg.Invoice.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED {
		t.Fatalf("public invoice issue/post=%v err=%v", issued, err)
	}
	accounting, err := server.invoices.GetInvoiceAccounting(ctx, brandingRequest(&tammyv1.GetInvoiceAccountingRequest{
		Authentication: server.authentication, OrganisationId: server.organisationID, InvoiceId: issued.Msg.Invoice.Id,
	}, server.capability))
	if err != nil || !accounting.Msg.Accounting.Posted || accounting.Msg.Accounting.OutstandingMinor != 22000 {
		t.Fatalf("read posted invoice accounting=%v err=%v", accounting, err)
	}
	q3, err := server.invoices.RecordInvoicePayment(ctx, brandingRequest(&tammyv1.RecordInvoicePaymentRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(0xa042)},
		OrganisationId: server.organisationID, InvoiceId: issued.Msg.Invoice.Id, ExpectedAccountingVersion: 1,
		PaymentDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
		Amount:        &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 11000},
		BankAccountId: "018f0000-0000-7000-8000-000000000101", Reference: "Q3 customer receipt",
	}, server.capability))
	if err != nil || q3.Msg.Payment == nil || q3.Msg.Accounting.Version != 2 {
		t.Fatalf("record Q3 invoice receipt=%v err=%v", q3, err)
	}
	q4, err := server.invoices.RecordInvoicePayment(ctx, brandingRequest(&tammyv1.RecordInvoicePaymentRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(0xa043)},
		OrganisationId: server.organisationID, InvoiceId: issued.Msg.Invoice.Id, ExpectedAccountingVersion: 2,
		PaymentDate:   &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1},
		Amount:        &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 11000},
		BankAccountId: "018f0000-0000-7000-8000-000000000101", Reference: "Q4 customer receipt",
	}, server.capability))
	if err != nil || q4.Msg.Payment == nil || q4.Msg.Accounting.Version != 3 || q4.Msg.Accounting.OutstandingMinor != 0 {
		t.Fatalf("record Q4 invoice receipt=%v err=%v", q4, err)
	}
	return issued.Msg.Invoice, q3.Msg, q4.Msg
}

func recordBASPurchasePaymentTLS(t *testing.T, server documentPageServer, documentID string,
	expected uint64, key uint64, year, month, day int32, amount int64,
) *tammyv1.RecordPurchasePaymentResponse {
	t.Helper()
	response, err := server.purchases.RecordPurchasePayment(context.Background(), brandingRequest(
		&tammyv1.RecordPurchasePaymentRequest{
			CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(key)},
			OrganisationId: server.organisationID, DocumentId: documentID, ExpectedAccountingVersion: expected,
			ActualPaymentDate: &tammyv1.CivilDate{Year: year, Month: month, Day: day},
			Amount:            &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: amount},
			BankAccountId:     "018f0000-0000-7000-8000-000000000101",
			Reference:         "Reviewed partial cash payment",
		}, server.capability))
	if err != nil || response.Msg.Payment == nil || response.Msg.Accounting.AccountingVersion != expected+1 {
		t.Fatalf("record actual purchase payment=%v err=%v", response, err)
	}
	return response.Msg
}

func nonzeroBASPaygInputs(year *tammyv1.BasIncomeYearIdentity) (*tammyv1.BasPaygwInputs, *tammyv1.BasPaygiInputs) {
	payroll := nonzeroBASRef("paygw_statement", 0xa050)
	notice := nonzeroBASRef("ato_paygi_notice", 0xa051)
	t1source := nonzeroBASRef("prior_income_year_statement", 0xa052)
	amount := func(cents int64, ref *tammyv1.SourceRef) *tammyv1.BasAmountInput {
		state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
		if cents == 0 {
			state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
		}
		return &tammyv1.BasAmountInput{State: state, Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: cents},
			Evidence: []*tammyv1.SourceRef{proto.Clone(ref).(*tammyv1.SourceRef)}, Reason: "Synthetic retained source was explicitly reviewed"}
	}
	return &tammyv1.BasPaygwInputs{
			Category:        tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_SMALL,
			W1TotalPayments: amount(100000, payroll), W2AmountWithheld: amount(5000, payroll),
			W3OtherWithholding: amount(0, payroll), W4NoAbnWithholding: amount(0, payroll),
			PayrollStatementEvidence: []*tammyv1.SourceRef{proto.Clone(payroll).(*tammyv1.SourceRef)},
			PrefillReview:            tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_NOT_REQUIRED,
			ReviewReason:             "Synthetic retained small-category payroll statement supplies known W1-W4 values",
		}, &tammyv1.BasPaygiInputs{
			Method:                tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE,
			Notice:                proto.Clone(notice).(*tammyv1.SourceRef),
			NoticeOperativeStatus: tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_OPERATIVE,
			PriorVariation:        tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_NONE,
			UnsupportedState:      tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_NONE,
			T1ReviewDecision:      tammyv1.BasT1ReviewDecision_BAS_T1_REVIEW_DECISION_REVIEWED,
			RateT1:                amount(201099, t1source), RateT2: &tammyv1.Decimal{Coefficient: 17, Scale: 1},
			ConfirmedIncomeYear: proto.Clone(year).(*tammyv1.BasIncomeYearIdentity),
			Evidence:            []*tammyv1.SourceRef{proto.Clone(notice).(*tammyv1.SourceRef)},
			ReviewReason:        "Synthetic canonical notice and prior-year statement evidence support a reviewed 1.7 rate",
		}
}

func nonzeroBASRef(kind string, sequence uint64) *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: kind, Id: testBASCommandKey(sequence), Revision: 1,
		ContentHash: bytes.Repeat([]byte{byte(sequence)}, 32)}
}

func confirmNonzeroBASPeriodTLS(t *testing.T, server documentPageServer, profile *tammyv1.SourceRef,
	key uint64, sy, sm, sd, ey, em, ed int32, window tammyv1.BasPeriodWindow, dy, dm, dd int32,
) *tammyv1.SourceRef {
	t.Helper()
	start, end, due := &tammyv1.CivilDate{Year: sy, Month: sm, Day: sd},
		&tammyv1.CivilDate{Year: ey, Month: em, Day: ed}, &tammyv1.CivilDate{Year: dy, Month: dm, Day: dd}
	response, err := server.basPreparation.ConfirmBasPeriod(context.Background(), brandingRequest(
		&tammyv1.ConfirmBasPeriodRequest{
			CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(key)},
			OrganisationId: server.organisationID,
			Period: &tammyv1.BasPeriodConfirmationInput{
				Period: &tammyv1.BasCivilDateRange{StartDate: start, EndDate: end}, Window: window,
				Profile: proto.Clone(profile).(*tammyv1.SourceRef), NominalDueDate: proto.Clone(due).(*tammyv1.CivilDate),
				DueDateRuleVersion: baspreparation.DueDateRuleVersion,
				DueDateRuleHash:    baspreparation.DueDateRuleFingerprint(),
				ConfirmedDueDate:   proto.Clone(due).(*tammyv1.CivilDate),
				DueDateKind:        tammyv1.BasDueDateKind_BAS_DUE_DATE_KIND_NORMAL_SUGGESTION,
				DueDateEvidence:    []*tammyv1.SourceRef{nonzeroBASRef("ato_statement", key+0x800)},
				SupportState:       tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED,
				Reason:             "Confirmed supported registered cash-basis quarterly period",
			},
		}, server.capability))
	if err != nil || response.Msg.Period == nil {
		t.Fatalf("confirm period %v to %v=%v err=%v", start, end, response, err)
	}
	value := response.Msg.Period
	return &tammyv1.SourceRef{Type: "bas_period", Id: value.PeriodId, Revision: value.Version,
		ContentHash: bytes.Clone(value.ContentHash)}
}

func regenerateNonzeroBASPeriodTLS(t *testing.T, server documentPageServer, profile, period *tammyv1.SourceRef,
	paygw *tammyv1.BasPaygwInputs, paygi *tammyv1.BasPaygiInputs, base uint64,
) (*tammyv1.BasPreparation, *tammyv1.BasCalculation) {
	t.Helper()
	ctx := context.Background()
	absent := func() *tammyv1.BasExpectedIdentity {
		return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
	}
	initialRequest := &tammyv1.RegenerateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(base + 1)},
		OrganisationId: server.organisationID, FamilyExpectation: absent(), PreparationExpectation: absent(),
		Profile: proto.Clone(profile).(*tammyv1.SourceRef), Period: proto.Clone(period).(*tammyv1.SourceRef),
	}
	initial, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(initialRequest, server.capability))
	if err != nil || initial.Msg.Preparation == nil || initial.Msg.Calculation == nil {
		tx, txErr := server.module.db.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if txErr != nil {
			t.Fatal(txErr)
		}
		defer tx.Rollback()
		projector, factoryErr := newBASOverviewProjector(server.activation.WorkspaceID, server.activation.InstallationKey)
		if factoryErr != nil {
			t.Fatal(factoryErr)
		}
		callbacks, factoryErr := baspreparation.NewBASCommandCapture(tx, projector.cursors, server.activation.Now, server.activation.NewID)
		if factoryErr != nil {
			t.Fatal(factoryErr)
		}
		_, cause := callbacks.Regenerate(ctx, initialRequest, server.authentication.ActorUserId, timestamppb.New(server.activation.Now()))
		t.Fatalf("initial bounded capture=%v err=%v internal=%v", initial, err, cause)
	}
	prep := initial.Msg.Preparation
	saved, err := server.basPreparation.SaveBasPaygInputs(ctx, brandingRequest(
		&tammyv1.SaveBasPaygInputsRequest{
			CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(base + 2)},
			OrganisationId: server.organisationID, PreparationId: prep.PreparationId,
			ExpectedVersion: prep.Version, PreparationHash: bytes.Clone(prep.ContentHash),
			Paygw: proto.Clone(paygw).(*tammyv1.BasPaygwInputs), Paygi: proto.Clone(paygi).(*tammyv1.BasPaygiInputs),
		}, server.capability))
	if err != nil || saved.Msg.Preparation == nil {
		t.Fatalf("save retained PAYGW/PAYGI inputs=%v err=%v", saved, err)
	}
	prep = saved.Msg.Preparation
	version, familyVersion := prep.Version, prep.Family.Revision
	exact := &tammyv1.SourceRef{Type: "bas_preparation", Id: prep.PreparationId, Revision: prep.Version,
		ContentHash: bytes.Clone(prep.ContentHash)}
	regenerated, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(
		&tammyv1.RegenerateBasPreparationRequest{
			CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: testBASCommandKey(base + 3)},
			OrganisationId: server.organisationID, ExpectedVersion: version,
			FamilyExpectation: &tammyv1.BasExpectedIdentity{
				ExpectedState:   &tammyv1.BasExpectedIdentity_Retained{Retained: proto.Clone(prep.Family).(*tammyv1.SourceRef)},
				ExpectedVersion: &familyVersion,
			},
			PreparationExpectation: &tammyv1.BasExpectedIdentity{
				ExpectedState: &tammyv1.BasExpectedIdentity_Retained{Retained: exact}, ExpectedVersion: &version,
			},
			Profile: proto.Clone(profile).(*tammyv1.SourceRef), Period: proto.Clone(period).(*tammyv1.SourceRef),
		}, server.capability))
	if err != nil || regenerated.Msg.Preparation == nil || regenerated.Msg.Calculation == nil {
		t.Fatalf("regenerate complete statement=%v err=%v", regenerated, err)
	}
	return regenerated.Msg.Preparation, regenerated.Msg.Calculation
}

func assertNonzeroBASLabels(t *testing.T, calculation *tammyv1.BasCalculation) {
	t.Helper()
	if calculation == nil || calculation.Result == nil ||
		calculation.Result.Outcome != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE ||
		len(calculation.Result.Labels) != 18 || len(calculation.Findings) != 0 {
		t.Fatalf("supported result incomplete: %#v", calculation)
	}
	expected := []struct {
		label   tammyv1.BasFormLabel
		state   tammyv1.BasAmountState
		cents   string
		dollars int64
	}{
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_G1, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "11000", 110},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_1A, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "1000", 10},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_1B, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "500", 5},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_W1, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "100000", 1000},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_W2, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "5000", 50},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_W3, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO, "0", 0},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_W4, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO, "0", 0},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_W5, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "5000", 50},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_4, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "5000", 50},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_T1, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "201099", 2010},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_T7, tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNSET, "", 0},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_T11, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "3417", 34},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_5A, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "3417", 34},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_5B, tammyv1.BasAmountState_BAS_AMOUNT_STATE_NOT_APPLICABLE, "", 0},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_8A, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "9417", 94},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_8B, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "500", 5},
		{tammyv1.BasFormLabel_BAS_FORM_LABEL_9, tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN, "8917", 89},
	}
	for _, want := range expected {
		trace := nonzeroBASLabel(t, calculation, want.label)
		if trace.State != want.state {
			t.Fatalf("%s state=%v want=%v", want.label, trace.State, want.state)
		}
		if want.cents != "" && (trace.GetExactCentsNumerator() != want.cents ||
			trace.GetExactCentsDenominator() != "1" || trace.GetReportedWholeDollars() != want.dollars) {
			t.Fatalf("%s trace exact/reportable=%s/%s $%d, want %s/1 $%d",
				want.label, trace.GetExactCentsNumerator(), trace.GetExactCentsDenominator(),
				trace.GetReportedWholeDollars(), want.cents, want.dollars)
		}
	}
	t2 := nonzeroBASLabel(t, calculation, tammyv1.BasFormLabel_BAS_FORM_LABEL_T2)
	if t2.State != tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN ||
		t2.GetExactRateNumerator() != "17" || t2.GetExactRateDenominator() != "10" ||
		t2.ExactCentsNumerator != nil || t2.ExactCentsDenominator != nil {
		t.Fatalf("T2 must remain an exact 1.7 rate, not cents: %v", t2)
	}
	if calculation.Result.GetLabel_9Direction() != tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_PAYABLE {
		t.Fatalf("label 9 direction=%v want payable", calculation.Result.GetLabel_9Direction())
	}
}

func nonzeroBASLabel(t *testing.T, calculation *tammyv1.BasCalculation, kind tammyv1.BasFormLabel) *tammyv1.BasLabelTrace {
	t.Helper()
	var found *tammyv1.BasLabelTrace
	for _, trace := range calculation.Result.Labels {
		if trace.Label == kind {
			if found != nil {
				t.Fatalf("calculation repeats label %s", kind)
			}
			found = trace
		}
	}
	if found == nil {
		t.Fatalf("calculation omits label %s", kind)
	}
	return found
}

func assertNonzeroBASSourceLines(t *testing.T, calculation *tammyv1.BasCalculation,
	invoiceID, receiptID string, document *tammyv1.Document, postingID, purchasePaymentID string,
	year, month, day int32,
) {
	t.Helper()
	wantDate := &tammyv1.CivilDate{Year: year, Month: month, Day: day}
	workpaper := calculation.GetWorkpaperSnapshot()
	if workpaper == nil || workpaper.SalesG1 == nil || workpaper.GstOnSales_1A == nil ||
		workpaper.GstCredits_1B == nil || workpaper.SalesG1.MinorUnits != 11000 ||
		workpaper.GstOnSales_1A.MinorUnits != 1000 || workpaper.GstCredits_1B.MinorUnits != 500 ||
		len(workpaper.SalesSources) != 1 || len(workpaper.PurchaseSources) != 1 {
		t.Fatalf("unexpected nonzero workpaper source totals/rows: %v", workpaper)
	}
	sale := workpaper.SalesSources[0]
	if sale.InvoiceId != invoiceID || sale.PaymentId == nil || *sale.PaymentId != receiptID ||
		sale.SourceRevision != 1 || len(sale.SourceHash) != 32 ||
		!proto.Equal(sale.TaxDate, wantDate) || sale.Gross.MinorUnits != 11000 || sale.Gst.MinorUnits != 1000 {
		t.Fatalf("sales line lost exact public receipt provenance: %v", sale)
	}
	purchase := workpaper.PurchaseSources[0]
	if purchase.DocumentId != document.Id || purchase.PostingId != postingID ||
		purchase.PaymentId == nil || *purchase.PaymentId != purchasePaymentID ||
		purchase.PaymentHash == nil || len(purchase.PaymentHash) != 32 ||
		len(purchase.PostingHash) != 32 || len(purchase.OriginalContentHash) != 32 ||
		len(purchase.ReviewedSnapshotHash) != 32 || purchase.ClaimedGst == nil ||
		purchase.ClaimedGst.MinorUnits != 500 || !proto.Equal(purchase.ActualPaymentDate, wantDate) {
		t.Fatalf("purchase line lost exact payment/allocation/hash provenance: %v", purchase)
	}
	if workpaper.NetGstPayable == nil || workpaper.NetGstPayable.MinorUnits != 500 {
		t.Fatalf("workpaper net GST=%v want 500 cents", workpaper.NetGstPayable)
	}
	if !nonzeroBASTraceHasSource(nonzeroBASLabel(t, calculation, tammyv1.BasFormLabel_BAS_FORM_LABEL_W2),
		"paygw_statement", testBASCommandKey(0xa050)) ||
		!nonzeroBASTraceHasSource(nonzeroBASLabel(t, calculation, tammyv1.BasFormLabel_BAS_FORM_LABEL_T1),
			"prior_income_year_statement", testBASCommandKey(0xa052)) ||
		!nonzeroBASTraceHasSource(nonzeroBASLabel(t, calculation, tammyv1.BasFormLabel_BAS_FORM_LABEL_T1),
			"ato_paygi_notice", testBASCommandKey(0xa051)) {
		t.Fatal("PAYGW/PAYGI traces lost the exact synthetic reviewed evidence references")
	}
}

func nonzeroBASTraceHasSource(trace *tammyv1.BasLabelTrace, kind, id string) bool {
	for _, ref := range trace.GetEvidence() {
		if ref.GetType() == kind && ref.GetId() == id && ref.GetRevision() == 1 && len(ref.GetContentHash()) == 32 {
			return true
		}
	}
	return false
}
