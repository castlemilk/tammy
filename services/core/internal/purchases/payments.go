package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrPayment = errors.New("purchases: invalid payment amount, date or bank account")

// RecordPayment operates entirely in the elected caller transaction. The
// immutable accounting history, rather than a displayed page, owns the balance.
func (r *Repository) RecordPayment(ctx context.Context, request *tammyv1.RecordPurchasePaymentRequest) (*tammyv1.RecordPurchasePaymentResponse, error) {
	if r == nil || r.executor == nil || ctx == nil || request == nil || request.CommandContext == nil || request.CommandContext.Authentication == nil || !ids.IsCanonicalV7(request.CommandContext.IdempotencyKey) || !ids.IsCanonicalV7(request.CommandContext.Authentication.ActorUserId) || request.ExpectedAccountingVersion == 0 || request.ExpectedAccountingVersion > math.MaxInt64 || !validPurchaseMoney(request.Amount) || request.Amount.MinorUnits <= 0 || purchaseDateString(request.ActualPaymentDate) == "" || !ids.IsCanonicalV7(request.BankAccountId) || !purchaseReviewText(request.Reference, 512, false) {
		return nil, ErrPayment
	}
	now := r.now().UTC()
	if now.IsZero() {
		return nil, ErrRepository
	}
	state, err := r.getAt(ctx, request.OrganisationId, request.DocumentId, false, now)
	if err != nil {
		return nil, err
	}
	if !state.Posted || state.Posting == nil || state.Posting.Gross.MinorUnits == 0 {
		return nil, ErrPayment
	}
	if state.AccountingVersion != request.ExpectedAccountingVersion || state.AccountingVersion >= math.MaxInt64 {
		return nil, ErrConflict
	}
	posting := state.Posting
	if request.Amount.MinorUnits > state.Outstanding.MinorUnits || purchaseDateKey(request.ActualPaymentDate) < purchaseDateKey(posting.InvoiceDate) {
		return nil, ErrPayment
	}
	profiles, _ := organisations.NewRepository(r.executor)
	profile, err := profiles.Get(ctx, posting.OrganisationId)
	if err != nil {
		return nil, err
	}
	if profile.GstBasis != posting.GstBasis {
		return nil, invoices.ErrGSTBasisHistory
	}
	if _, err := invoices.CheckGSTBasisHistory(ctx, r.executor, posting.OrganisationId, posting.GstHistoryWatermark); err != nil {
		return nil, err
	}
	evidence, err := r.retainedEvidence(ctx, posting)
	if err != nil || evidence == nil {
		return nil, ErrRepository
	}
	readiness, err := r.evidenceReadiness(ctx, posting.OrganisationId, posting.DocumentId, evidence.Review, request.ActualPaymentDate, purchaseAsOf(now))
	if err != nil {
		return nil, err
	}
	writer, _ := NewJournalWriter(r.executor, func() time.Time { return now }, r.newID)
	if err := writer.IsPostingDateOpen(ctx, posting.OrganisationId, request.ActualPaymentDate); err != nil {
		return nil, err
	}
	accounts, _ := accounting.NewAccountRepository(r.executor)
	bank, err := accounts.Get(ctx, request.BankAccountId)
	if err != nil {
		return nil, err
	}
	if accounting.ValidateAccount(bank) != nil || bank.OrganisationId != posting.OrganisationId || bank.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE || !accounting.IsCashAccount(bank) || bank.Type != tammyv1.AccountType_ACCOUNT_TYPE_ASSET {
		return nil, ErrPayment
	}
	if bank.Designation != tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY {
		spec, err := writer.spec(ctx, posting.OrganisationId, posting.Id, request.ActualPaymentDate, posting.FinancialRevision, map[string]*tammyv1.Account{bank.Id: bank})
		if err != nil {
			return nil, err
		}
		installed, err := purchaseControl(spec, "1000")
		if err != nil || installed.Id != bank.Id {
			return nil, ErrPayment
		}
	}
	deltas, err := paymentDeltas(posting, state.Paid.MinorUnits, request.Amount.MinorUnits)
	if err != nil {
		return nil, err
	}
	var eligible int64
	for _, delta := range deltas {
		if !addPurchaseAmount(&eligible, delta.EligibleGST) {
			return nil, ErrPayment
		}
	}
	// The additional GST transfer increases both journal totals on cash basis.
	// Check that bound before reserving a revision or writing any source.
	if posting.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH && request.Amount.MinorUnits > math.MaxInt64-eligible {
		return nil, ErrPayment
	}
	id, err := r.newID()
	if err != nil || !ids.IsCanonicalV7(id) {
		return nil, ErrRepository
	}
	revision, err := ReservePurchaseRevision(ctx, r.executor, request.CommandContext.IdempotencyKey, purchaseService+"RecordPurchasePayment", true, now)
	if err != nil {
		return nil, err
	}
	journal, writtenDeltas, err := writer.PostPayment(ctx, id, posting, state.Paid.MinorUnits, request.Amount.MinorUnits, request.ActualPaymentDate, bank.Id, readiness, revision.Financial)
	if err != nil {
		return nil, err
	}
	payment := &tammyv1.PurchasePayment{Id: id, OrganisationId: posting.OrganisationId, PostingId: posting.Id, DocumentId: posting.DocumentId, AccountingVersion: state.AccountingVersion + 1, ActualPaymentDate: proto.Clone(request.ActualPaymentDate).(*tammyv1.CivilDate), Amount: proto.Clone(request.Amount).(*tammyv1.Money), BankAccountId: bank.Id, Reference: request.Reference, JournalId: journal.Id, FinancialRevision: revision.Financial, ActorUserId: request.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(now)}
	for i, delta := range writtenDeltas {
		a := posting.Allocations[i]
		payment.Allocations = append(payment.Allocations, &tammyv1.PurchasePaymentAllocation{Sequence: a.Sequence, AccountId: a.AccountId, AccountVersion: a.AccountVersion, Gross: purchaseJournalMoney(delta.Gross), SupplierGst: purchaseJournalMoney(delta.SupplierGST), EligibleGst: purchaseJournalMoney(delta.EligibleGST), Use: a.Use, BasClassification: a.BasClassification, CashFlowClassification: a.CashFlowClassification})
	}
	payment.SourceHash, err = purchaseHash(payment)
	if err != nil {
		return nil, err
	}
	content, err := purchaseBytes(payment)
	if err != nil || len(content) > 65536 {
		return nil, ErrRepository
	}
	_, err = r.executor.ExecContext(ctx, `INSERT INTO purchase_payments_v1(id,posting_id,organisation_id,accounting_version,payment_date,amount_minor,currency_code,bank_account_id,journal_id,reference,payment_proto,source_hash,actor_user_id,created_at) VALUES(?,?,?,?,?,?,'AUD',?,?,?,?,?,?,?)`, payment.Id, posting.Id, posting.OrganisationId, payment.AccountingVersion, purchaseDateString(payment.ActualPaymentDate), payment.Amount.MinorUnits, payment.BankAccountId, payment.JournalId, payment.Reference, content, payment.SourceHash, payment.ActorUserId, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	for _, allocation := range payment.Allocations {
		content, err := purchaseBytes(allocation)
		if err != nil {
			return nil, err
		}
		_, err = r.executor.ExecContext(ctx, `INSERT INTO purchase_payment_allocations_v1(payment_id,posting_id,organisation_id,sequence,posting_allocation_sequence,gross_minor,supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,cash_flow_classification,allocation_proto) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, payment.Id, posting.Id, posting.OrganisationId, allocation.Sequence, allocation.Sequence, allocation.Gross.MinorUnits, allocation.SupplierGst.MinorUnits, allocation.EligibleGst.MinorUnits, allocation.Use, allocation.BasClassification, allocation.CashFlowClassification, content)
		if err != nil {
			return nil, errors.Join(ErrRepository, err)
		}
	}
	if err := applyPaymentState(state, payment, readiness); err != nil {
		return nil, err
	}
	if err := r.refreshClaimOutcomes(ctx, state, now); err != nil {
		return nil, err
	}
	response := &tammyv1.RecordPurchasePaymentResponse{Payment: payment, Journal: journal, Accounting: state}
	err = RecordAccountingEvent(ctx, r.executor, AccountingEvent{OperationKey: request.CommandContext.IdempotencyKey, OrganisationID: posting.OrganisationId, ScopeKind: "POSTING", ScopeID: posting.Id, CommandType: purchaseService + "RecordPurchasePayment", AccountingVersion: payment.AccountingVersion, Revision: revision, Result: response, JournalID: &payment.JournalId, Now: now})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func paymentDeltas(posting *tammyv1.PurchasePosting, before, amount int64) ([]AllocationAmounts, error) {
	lines := make([]AllocationAmounts, len(posting.Allocations))
	for i, a := range posting.Allocations {
		lines[i] = AllocationAmounts{a.Gross.MinorUnits, a.SupplierGst.MinorUnits, a.EligibleGst.MinorUnits}
	}
	return PaymentAllocationDeltas(lines, before, amount, posting.Gross.MinorUnits)
}

func applyPaymentState(state *tammyv1.PurchaseAccounting, payment *tammyv1.PurchasePayment, readiness EvidenceReadiness) error {
	if payment.Amount.MinorUnits > state.Outstanding.MinorUnits || !addPurchaseAmount(&state.Paid.MinorUnits, payment.Amount.MinorUnits) || state.PaymentCount == math.MaxUint64 {
		return ErrRepository
	}
	state.Outstanding.MinorUnits -= payment.Amount.MinorUnits
	state.PaymentCount++
	state.AccountingVersion = payment.AccountingVersion
	if state.Posting.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH {
		var eligible int64
		for _, a := range payment.Allocations {
			if !addPurchaseAmount(&eligible, a.EligibleGst.MinorUnits) {
				return ErrRepository
			}
		}
		if eligible > state.DeferredGst.MinorUnits {
			return ErrRepository
		}
		state.DeferredGst.MinorUnits -= eligible
		target := state.SuspenseGst
		if readiness == EvidenceNormalSupported {
			target = state.AttributedGst
		}
		if !addPurchaseAmount(&target.MinorUnits, eligible) {
			return ErrRepository
		}
	}
	return nil
}

func (r *Repository) loadPayments(ctx context.Context, posting *tammyv1.PurchasePosting) ([]*tammyv1.PurchasePayment, error) {
	sqlText := `SELECT id,accounting_version,payment_date,amount_minor,currency_code,bank_account_id,journal_id,reference,payment_proto,source_hash,actor_user_id,created_at FROM purchase_payments_v1 WHERE posting_id=? AND organisation_id=? ORDER BY accounting_version,id`
	args := []any{posting.Id, posting.OrganisationId}
	if r.reportingRootOwners {
		sqlText, args = postingReportingReadSQL(purchaseReportingPaymentReplayQuery(posting.OrganisationId, posting.Id))
	}
	rows, err := r.executor.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, ErrRepository
	}
	scanner := r.reportingOwnerScanner(rows)
	var payments []*tammyv1.PurchasePayment
	for rows.Next() {
		payment, err := scanPayment(scanner, posting)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		payments = append(payments, payment)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrRepository
	}
	return payments, nil
}

type paymentScanner interface{ Scan(...any) error }

func scanPayment(row paymentScanner, posting *tammyv1.PurchasePosting) (*tammyv1.PurchasePayment, error) {
	var id, date, currency, bank, journal, reference, actor, stamp string
	var version uint64
	var amount int64
	var content, hash []byte
	if row.Scan(&id, &version, &date, &amount, &currency, &bank, &journal, &reference, &content, &hash, &actor, &stamp) != nil {
		return nil, ErrRepository
	}
	p := &tammyv1.PurchasePayment{}
	if proto.Unmarshal(content, p) != nil || len(content) > 65536 || !canonicalPurchaseBytes(p, content) || !verifyPurchaseHash(p, hash) || !bytes.Equal(p.SourceHash, hash) || p.Id != id || !ids.IsCanonicalV7(id) || p.OrganisationId != posting.OrganisationId || p.PostingId != posting.Id || p.DocumentId != posting.DocumentId || p.AccountingVersion != version || version < 2 || version > math.MaxInt64 || !validPurchaseMoney(p.Amount) || p.Amount.MinorUnits != amount || amount <= 0 || currency != "AUD" || purchaseDateString(p.ActualPaymentDate) != date || purchaseDateKey(p.ActualPaymentDate) < purchaseDateKey(posting.InvoiceDate) || p.BankAccountId != bank || !ids.IsCanonicalV7(bank) || p.JournalId != journal || !ids.IsCanonicalV7(journal) || p.Reference != reference || !purchaseReviewText(reference, 512, false) || p.ActorUserId != actor || !ids.IsCanonicalV7(actor) || p.FinancialRevision == 0 || p.FinancialRevision > math.MaxInt64 || p.RecordedAt == nil || p.RecordedAt.CheckValid() != nil || p.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) != stamp || len(p.Allocations) != len(posting.Allocations) {
		return nil, ErrRepository
	}
	return p, nil
}

func (r *Repository) verifyPaymentAllocations(ctx context.Context, posting *tammyv1.PurchasePosting, p *tammyv1.PurchasePayment, paidBefore int64) error {
	deltas, err := paymentDeltas(posting, paidBefore, p.Amount.MinorUnits)
	if err != nil {
		return ErrRepository
	}
	sqlText := `SELECT sequence,posting_allocation_sequence,gross_minor,supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,cash_flow_classification,allocation_proto FROM purchase_payment_allocations_v1 WHERE payment_id=? AND posting_id=? AND organisation_id=? ORDER BY sequence`
	args := []any{p.Id, posting.Id, posting.OrganisationId}
	if r.reportingRootOwners {
		sqlText, args = postingReportingReadSQL(purchaseReportingPaymentAllocationReplayQuery(posting.OrganisationId, posting.Id, p.Id))
	}
	rows, err := r.executor.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return ErrRepository
	}
	index := 0
	scanner := r.reportingOwnerScanner(rows)
	for rows.Next() {
		var sequence, postingSequence uint32
		var gross, supplier, eligible int64
		var use, bas, flow int32
		var content []byte
		if scanner.Scan(&sequence, &postingSequence, &gross, &supplier, &eligible, &use, &bas, &flow, &content) != nil || index >= len(deltas) {
			_ = rows.Close()
			return ErrRepository
		}
		a, delta := posting.Allocations[index], deltas[index]
		expected := &tammyv1.PurchasePaymentAllocation{Sequence: a.Sequence, AccountId: a.AccountId, AccountVersion: a.AccountVersion, Gross: purchaseJournalMoney(delta.Gross), SupplierGst: purchaseJournalMoney(delta.SupplierGST), EligibleGst: purchaseJournalMoney(delta.EligibleGST), Use: a.Use, BasClassification: a.BasClassification, CashFlowClassification: a.CashFlowClassification}
		if !proto.Equal(expected, p.Allocations[index]) || !canonicalPurchaseBytes(expected, content) || sequence != a.Sequence || postingSequence != a.Sequence || gross != delta.Gross || supplier != delta.SupplierGST || eligible != delta.EligibleGST || use != int32(a.Use) || bas != int32(a.BasClassification) || flow != int32(a.CashFlowClassification) {
			_ = rows.Close()
			return ErrRepository
		}
		index++
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil || index != len(deltas) {
		return ErrRepository
	}
	return nil
}

type retainedPaymentEvent struct {
	operation, command, stamp                   string
	postingID, documentID, openingID, journalID sql.NullString
	version, financial, ledger, tax, settlement uint64
	content, hash                               []byte
}

func (r *Repository) paymentEvents(ctx context.Context, posting *tammyv1.PurchasePosting) ([]retainedPaymentEvent, error) {
	sqlText := `SELECT operation_key,posting_id,document_id,opening_payable_id,accounting_version,command_type,financial_revision,ledger_revision,tax_source_revision,settlement_revision,result_proto,result_sha256,journal_id,created_at FROM purchase_accounting_events_v1 WHERE organisation_id=? AND scope_kind='POSTING' AND scope_id=? AND accounting_version>1 ORDER BY accounting_version`
	args := []any{posting.OrganisationId, posting.Id}
	if r.reportingRootOwners {
		sqlText, args = postingReportingReadSQL(purchaseReportingEventReplayQuery(posting.OrganisationId, posting.Id, false))
	}
	rows, err := r.executor.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, ErrRepository
	}
	var result []retainedPaymentEvent
	scanner := r.reportingOwnerScanner(rows)
	for rows.Next() {
		var e retainedPaymentEvent
		if scanner.Scan(&e.operation, &e.postingID, &e.documentID, &e.openingID, &e.version, &e.command, &e.financial, &e.ledger, &e.tax, &e.settlement, &e.content, &e.hash, &e.journalID, &e.stamp) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		result = append(result, e)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrRepository
	}
	return result, nil
}

func (r *Repository) verifyPaymentHistory(ctx context.Context, posting *tammyv1.PurchasePosting, pending bool, state *tammyv1.PurchaseAccounting) error {
	if _, err := r.verifyEvents(ctx, posting, pending, state); err != nil {
		return err
	}
	payments, err := r.loadPayments(ctx, posting)
	if err != nil {
		return err
	}
	evidences, err := r.retainedEvidenceHistory(ctx, posting)
	if err != nil {
		return err
	}
	events, err := r.paymentEvents(ctx, posting)
	if err != nil {
		return err
	}
	attributions, err := r.loadAttributions(ctx, posting)
	if err != nil {
		return err
	}
	deferrals, err := r.loadDeferrals(ctx, posting)
	if err != nil {
		return err
	}
	index, evidenceIndex := 0, 1
	attributionIndex, deferralIndex := 0, 0
	selected := evidences[0]
	for _, e := range events {
		digest := sha256.Sum256(e.content)
		instant, err := time.Parse(time.RFC3339Nano, e.stamp)
		if err != nil || instant.UTC().Format(time.RFC3339Nano) != e.stamp || e.version != state.AccountingVersion+1 || !ids.IsCanonicalV7(e.operation) || !e.postingID.Valid || e.postingID.String != posting.Id || e.documentID.Valid || e.openingID.Valid || !bytes.Equal(digest[:], e.hash) {
			return ErrRepository
		}
		var historical *tammyv1.PurchaseAccounting
		var eventActor string
		switch e.command {
		case purchaseService + "RecordPurchasePayment":
			if index >= len(payments) || payments[index].AccountingVersion != e.version || !e.journalID.Valid {
				return ErrRepository
			}
			payment := payments[index]
			if payment.FinancialRevision != e.financial || payment.JournalId != e.journalID.String || payment.RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) != e.stamp {
				return ErrRepository
			}
			if err := r.verifyPaymentAllocations(ctx, posting, payment, state.Paid.MinorUnits); err != nil {
				return err
			}
			readiness, err := r.evidenceReadiness(ctx, posting.OrganisationId, posting.DocumentId, selected.Review, payment.ActualPaymentDate, purchaseAsOf(instant))
			if err != nil {
				return ErrRepository
			}
			journal, err := r.verifyPaymentJournal(ctx, posting, payment, readiness)
			if err != nil {
				return err
			}
			response := &tammyv1.RecordPurchasePaymentResponse{}
			if proto.Unmarshal(e.content, response) != nil || !canonicalPurchaseBytes(response, e.content) || !proto.Equal(response.Payment, payment) || !proto.Equal(response.Journal, journal) {
				return ErrRepository
			}
			if err := applyPaymentState(state, payment, readiness); err != nil {
				return err
			}
			historical = response.Accounting
			eventActor = payment.ActorUserId
			index++
		case purchaseService + "RecordPurchaseTaxEvidence":
			if e.journalID.Valid || evidenceIndex >= len(evidences) || evidences[evidenceIndex].AccountingVersion != e.version || evidences[evidenceIndex].RecordedAt.AsTime().UTC().Format(time.RFC3339Nano) != e.stamp {
				return ErrRepository
			}
			selected = evidences[evidenceIndex]
			response := &tammyv1.RecordPurchaseTaxEvidenceResponse{}
			if proto.Unmarshal(e.content, response) != nil || !canonicalPurchaseBytes(response, e.content) || !proto.Equal(response.Evidence, selected) {
				return ErrRepository
			}
			state.AccountingVersion = e.version
			historical = response.Accounting
			eventActor = selected.ActorUserId
			evidenceIndex++
		case purchaseService + "AttributePurchaseGst":
			var consumed int
			historical, consumed, err = r.replayAttributionEvent(ctx, posting, e, attributions[attributionIndex:], deferrals, state, instant)
			if err != nil {
				return err
			}
			attributionIndex += consumed
			eventActor = attributions[attributionIndex-1].value.ActorUserId
		case purchaseService + "DeferPurchaseGstClaim":
			if deferralIndex >= len(deferrals) {
				return ErrRepository
			}
			historical, err = r.replayDeferralEvent(ctx, posting, e, deferrals[deferralIndex], state, instant)
			if err != nil {
				return err
			}
			deferralIndex++
			eventActor = deferrals[deferralIndex-1].ActorUserId
		default:
			return ErrRepository
		}
		if err := r.verifyElectedEventActor(ctx, e.operation, e.command, eventActor); err != nil {
			return err
		}
		err = r.refreshClaimOutcomes(ctx, state, instant)
		if err != nil || !proto.Equal(historical, state) {
			return ErrRepository
		}
		rows, err := r.executor.QueryContext(ctx, `SELECT domain_mask,financial_revision,ledger_revision,tax_source_revision,settlement_revision FROM financial_revision_claims WHERE operation_key=?`, e.operation)
		if err != nil {
			return ErrRepository
		}
		var mask uint8
		var financial, ledger, tax, settlement uint64
		if (purchaseRevisionRow{rows: rows}).Scan(&mask, &financial, &ledger, &tax, &settlement) != nil {
			return ErrRepository
		}
		want := uint8(8)
		if e.journalID.Valid {
			want |= 1
			if e.command == purchaseService+"RecordPurchasePayment" {
				want |= 2
			}
		}
		if mask != want || financial != e.financial || ledger != e.ledger || tax != e.tax || settlement != e.settlement {
			return ErrRepository
		}
	}
	if index != len(payments) || evidenceIndex != len(evidences) || attributionIndex != len(attributions) || deferralIndex != len(deferrals) {
		return ErrRepository
	}
	return nil
}

func (r *Repository) verifyPaymentJournal(ctx context.Context, posting *tammyv1.PurchasePosting, p *tammyv1.PurchasePayment, readiness EvidenceReadiness) (*tammyv1.Journal, error) {
	journals, _ := accounting.NewJournalRepository(r.executor)
	journal, err := journals.Get(ctx, p.JournalId)
	if err != nil || journal.OrganisationId != p.OrganisationId || journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE_PAYMENT || journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || journal.FinancialRevision != p.FinancialRevision || !proto.Equal(journal.PostingDate, p.ActualPaymentDate) || !proto.Equal(journal.PostedAt, p.RecordedAt) {
		return nil, ErrRepository
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT count(*) FROM journals WHERE id=? AND organisation_id=? AND source_type='BANKING' AND source_kind=7 AND source_id=? AND source_revision=1`, p.JournalId, p.OrganisationId, p.Id)
	var owners int
	if err != nil || (purchaseRevisionRow{rows: rows}).Scan(&owners) != nil || owners != 1 {
		return nil, ErrRepository
	}
	writer, _ := NewJournalWriter(r.executor, func() time.Time { return p.RecordedAt.AsTime() }, r.newID)
	spec, err := writer.spec(ctx, p.OrganisationId, p.Id, p.ActualPaymentDate, p.FinancialRevision, nil)
	if err != nil {
		return nil, ErrRepository
	}
	payable, err := purchaseControl(spec, "2000")
	if err != nil {
		return nil, ErrRepository
	}
	type movement struct {
		account       string
		debit, credit int64
		flows         []accounting.CashFlowComponent
	}
	want := []movement{{payable.Id, p.Amount.MinorUnits, 0, []accounting.CashFlowComponent{{Category: accounting.CashFlowNoncash, AmountMinor: p.Amount.MinorUnits}}}, {p.BankAccountId, 0, p.Amount.MinorUnits, nil}}
	var categories [3]int64
	var eligible int64
	for _, a := range p.Allocations {
		if !addPurchaseAmount(&categories[int(a.CashFlowClassification)-1], a.Gross.MinorUnits) || !addPurchaseAmount(&eligible, a.EligibleGst.MinorUnits) {
			return nil, ErrRepository
		}
	}
	for i, amount := range categories {
		if amount > 0 {
			want[1].flows = append(want[1].flows, accounting.CashFlowComponent{Category: accounting.CashFlowCategory(i + 1), AmountMinor: -amount})
		}
	}
	if posting.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH && eligible > 0 {
		claim, err := purchaseControl(spec, claimControl(readiness))
		if err != nil {
			return nil, ErrRepository
		}
		deferred, err := purchaseControl(spec, "1160")
		if err != nil {
			return nil, ErrRepository
		}
		want = append(want, movement{claim.Id, eligible, 0, []accounting.CashFlowComponent{{Category: accounting.CashFlowNoncash, AmountMinor: eligible}}}, movement{deferred.Id, 0, eligible, []accounting.CashFlowComponent{{Category: accounting.CashFlowNoncash, AmountMinor: -eligible}}})
	}
	if len(journal.Lines) != len(want) {
		return nil, ErrRepository
	}
	for i, line := range journal.Lines {
		m := want[i]
		if line.AccountId != m.account || line.Debit == nil || line.Credit == nil || line.Debit.CurrencyCode != "AUD" || line.Credit.CurrencyCode != "AUD" || line.Debit.MinorUnits != m.debit || line.Credit.MinorUnits != m.credit || line.TaxCodeId != nil {
			return nil, ErrRepository
		}
		rows, err := r.executor.QueryContext(ctx, `SELECT sequence,category,amount_minor,source_revision FROM cash_flow_facts WHERE journal_line_id=? AND organisation_id=? ORDER BY sequence`, line.Id, p.OrganisationId)
		if err != nil {
			return nil, ErrRepository
		}
		index := 0
		for rows.Next() {
			var sequence, revision uint64
			var category string
			var amount int64
			if rows.Scan(&sequence, &category, &amount, &revision) != nil || index >= len(m.flows) {
				_ = rows.Close()
				return nil, ErrRepository
			}
			names := []string{"", "OPERATING", "INVESTING", "FINANCING", "TRANSFER", "NONCASH"}
			if sequence != uint64(index+1) || revision != 1 || category != names[m.flows[index].Category] || amount != m.flows[index].AmountMinor {
				_ = rows.Close()
				return nil, ErrRepository
			}
			index++
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil || index != len(m.flows) {
			return nil, ErrRepository
		}
	}
	return journal, nil
}
