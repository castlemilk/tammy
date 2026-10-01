package invoices

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/revisions"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrGSTBasisHistory = errors.New("invoices: GST basis changes require reviewed historical attribution")
var ErrPayment = errors.New("invoices: invalid payment amount, date or bank account")

type AccountingPort interface {
	Get(context.Context, string, string) (*tammyv1.InvoiceAccounting, error)
	Post(context.Context, *tammyv1.Invoice, *tammyv1.Organisation, string) (*tammyv1.InvoiceAccounting, error)
	RecordPayment(context.Context, *tammyv1.Invoice, *tammyv1.Organisation, *tammyv1.RecordInvoicePaymentRequest) (*tammyv1.InvoiceAccounting, *tammyv1.InvoicePayment, error)
}

type AccountingRepository struct {
	executor app.CommandSQLExecutor
	now      func() time.Time
	newID    func() (string, error)
}

func NewAccountingRepository(executor app.CommandSQLExecutor, now func() time.Time, newID func() (string, error)) (*AccountingRepository, error) {
	if executor == nil || now == nil || newID == nil {
		return nil, ErrRepository
	}
	return &AccountingRepository{executor, now, newID}, nil
}

// CheckGSTBasisHistory fails closed on unsupported basis transitions. The
// watermark uses immutable row IDs rather than generation-local audit sequence
// numbers so a retained audit generation rollover cannot hide a later change.
func CheckGSTBasisHistory(ctx context.Context, executor app.CommandSQLExecutor, organisationID string, afterSequence uint64) (uint64, error) {
	if executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return 0, ErrRepository
	}
	if err := checkRetainedGSTBasis(ctx, executor, organisationID); err != nil {
		return 0, err
	}
	rows, err := executor.QueryContext(ctx, `SELECT rowid,payload_proto FROM audit_events_v1 WHERE command_type='tammy.v1.OrganisationService.UpdateOrganisation' AND rowid>? ORDER BY rowid`, afterSequence)
	if err != nil {
		return 0, ErrRepository
	}
	defer rows.Close()
	latest := afterSequence
	for rows.Next() {
		var sequence uint64
		var content []byte
		if rows.Scan(&sequence, &content) != nil {
			return 0, ErrRepository
		}
		if sequence > latest {
			latest = sequence
		}
		var payload tammyv1.AuditEventPayload
		if proto.Unmarshal(content, &payload) != nil {
			return 0, ErrRepository
		}
		changed := payload.GetOrganisationChanged()
		if changed == nil {
			return 0, ErrRepository
		}
		if changed.OrganisationId != organisationID {
			continue
		}
		for _, field := range changed.ChangedFields {
			if field == "gst_basis" {
				return 0, ErrGSTBasisHistory
			}
		}
	}
	if rows.Err() != nil {
		return 0, ErrRepository
	}
	return latest, nil
}

// Local profile commands retain complete immutable result snapshots even when
// the optional global audit sink is unavailable. Require an unbroken profile
// version history before assuming a historical GST basis.
func checkRetainedGSTBasis(ctx context.Context, executor app.CommandSQLExecutor, org string) error {
	profiles, _ := organisations.NewRepository(executor)
	current, err := profiles.Get(ctx, org)
	if err != nil {
		return ErrNotFound
	}
	rows, err := executor.QueryContext(ctx, `SELECT result_type,result_proto FROM command_idempotency_v1 WHERE outcome='COMMITTED' AND result_type IN ('tammy.v1.CreateOrganisationResponse','tammy.v1.UpdateOrganisationResponse','tammy.v1.RecordEntityVerificationResponse')`)
	if err != nil {
		return ErrRepository
	}
	defer rows.Close()
	versions := make(map[uint64]tammyv1.GstBasis)
	for rows.Next() {
		var resultType string
		var content []byte
		if rows.Scan(&resultType, &content) != nil {
			return ErrRepository
		}
		var profile *tammyv1.Organisation
		switch resultType {
		case "tammy.v1.CreateOrganisationResponse":
			value := &tammyv1.CreateOrganisationResponse{}
			if proto.Unmarshal(content, value) != nil {
				return ErrRepository
			}
			profile = value.Organisation
		case "tammy.v1.UpdateOrganisationResponse":
			value := &tammyv1.UpdateOrganisationResponse{}
			if proto.Unmarshal(content, value) != nil {
				return ErrRepository
			}
			profile = value.Organisation
		case "tammy.v1.RecordEntityVerificationResponse":
			value := &tammyv1.RecordEntityVerificationResponse{}
			if proto.Unmarshal(content, value) != nil {
				return ErrRepository
			}
			profile = value.Organisation
		}
		if profile == nil {
			return ErrRepository
		}
		if profile.Id != org {
			continue
		}
		if profile.Version == 0 || profile.Version > current.Version || profile.GstBasis != current.GstBasis {
			return ErrGSTBasisHistory
		}
		if basis, duplicate := versions[profile.Version]; duplicate && basis != profile.GstBasis {
			return ErrGSTBasisHistory
		}
		versions[profile.Version] = profile.GstBasis
	}
	if rows.Err() != nil {
		return ErrRepository
	}
	if current.Version == 1 {
		return nil
	}
	if uint64(len(versions)) != current.Version {
		return ErrGSTBasisHistory
	}
	for version := uint64(1); version <= current.Version; version++ {
		if basis, found := versions[version]; !found || basis != current.GstBasis {
			return ErrGSTBasisHistory
		}
	}
	return nil
}

func (r *AccountingRepository) Get(ctx context.Context, org, id string) (*tammyv1.InvoiceAccounting, error) {
	if r == nil || r.executor == nil || !ids.IsCanonicalV7(org) || !ids.IsCanonicalV7(id) {
		return nil, ErrRepository
	}
	invoices, _ := NewRepository(r.executor)
	invoice, err := invoices.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	value := &tammyv1.InvoiceAccounting{InvoiceId: id, OrganisationId: org, OutstandingMinor: invoice.TotalMinor}
	rows, err := r.executor.QueryContext(ctx, `SELECT revenue_account_id,journal_id,gst_basis FROM invoice_income_postings_v1 WHERE invoice_id=? AND organisation_id=?`, id, org)
	if err != nil {
		return nil, ErrRepository
	}
	if !rows.Next() {
		finish := rows.Err()
		_ = rows.Close()
		if finish != nil {
			return nil, ErrRepository
		}
		return value, nil
	}
	var revenue string
	var journal sql.NullString
	if rows.Scan(&revenue, &journal, &value.GstBasis) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return nil, ErrRepository
	}
	if rows.Close() != nil {
		return nil, ErrRepository
	}
	value.Posted = true
	value.RevenueAccountId = &revenue
	if journal.Valid {
		value.JournalId = &journal.String
	}
	rows, err = r.executor.QueryContext(ctx, `SELECT COALESCE(SUM(amount_minor),0),count(*) FROM invoice_payment_v1 WHERE organisation_id=? AND invoice_id=?`, org, id)
	if err != nil {
		return nil, ErrRepository
	}
	if !rows.Next() || rows.Scan(&value.ReceivedMinor, &value.PaymentCount) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return nil, ErrRepository
	}
	if rows.Close() != nil || value.ReceivedMinor > invoice.TotalMinor {
		return nil, ErrRepository
	}
	value.Version = value.PaymentCount + 1
	value.OutstandingMinor = invoice.TotalMinor - value.ReceivedMinor
	rows, err = r.executor.QueryContext(ctx, `SELECT payment_proto FROM invoice_payment_v1 WHERE organisation_id=? AND invoice_id=? ORDER BY payment_date DESC,created_at DESC,id DESC LIMIT 200`, org, id)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	for rows.Next() {
		var content []byte
		var payment tammyv1.InvoicePayment
		if rows.Scan(&content) != nil || proto.Unmarshal(content, &payment) != nil || payment.InvoiceId != id || payment.OrganisationId != org {
			return nil, ErrRepository
		}
		value.Payments = append(value.Payments, &payment)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return value, nil
}

func (r *AccountingRepository) Post(ctx context.Context, invoice *tammyv1.Invoice, profile *tammyv1.Organisation, operationKey string) (*tammyv1.InvoiceAccounting, error) {
	if r == nil || invoice == nil || profile == nil || invoice.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED || invoice.OrganisationId != profile.Id || !ids.IsCanonicalV7(operationKey) {
		return nil, ErrRepository
	}
	existing, err := r.Get(ctx, invoice.OrganisationId, invoice.Id)
	if err != nil {
		return nil, err
	}
	if existing.Posted {
		return nil, ErrConflict
	}
	if profile.GstBasis < 0 || profile.GstBasis > tammyv1.GstBasis_GST_BASIS_NON_CASH || invoice.GstMinor > 0 && profile.GstBasis == tammyv1.GstBasis_GST_BASIS_UNSPECIFIED {
		return nil, ErrGSTBasisHistory
	}
	sequence, err := CheckGSTBasisHistory(ctx, r.executor, profile.Id, 0)
	if err != nil {
		return nil, err
	}
	civil := incomeCivilDate(invoice.Draft.IssueDate)
	if civil == nil {
		return nil, ErrInvalidDraft
	}
	periods, _ := accounting.NewPeriodRepository(r.executor)
	open, err := periods.IsPostingDateOpen(ctx, profile.Id, civil)
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, accounting.ErrClosedPeriod
	}
	revenue, err := r.revenueAccount(ctx, profile.Id)
	if err != nil {
		return nil, err
	}
	revision, err := r.bump(ctx, operationKey, false)
	if err != nil {
		return nil, err
	}
	var journalID *string
	if invoice.TotalMinor > 0 {
		journal, flows, err := r.makeJournal(profile.Id, civil, "Invoice "+invoice.Draft.Number, tammyv1.JournalSource_JOURNAL_SOURCE_RECEIVABLE, revision)
		if err != nil {
			return nil, err
		}
		receivable, err := r.controlAccount(ctx, profile.Id, "1100")
		if err != nil {
			return nil, err
		}
		if err = r.addLine(journal, flows, receivable.Id, invoice.TotalMinor, 0, accounting.CashFlowNoncash); err != nil {
			return nil, err
		}
		if invoice.SubtotalMinor > 0 {
			if err = r.addLine(journal, flows, revenue.Id, 0, invoice.SubtotalMinor, accounting.CashFlowNoncash); err != nil {
				return nil, err
			}
		}
		if invoice.GstMinor > 0 {
			code := "2100"
			if profile.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH {
				code = "2110"
			}
			gst, err := r.controlAccount(ctx, profile.Id, code)
			if err != nil {
				return nil, err
			}
			if err = r.addLine(journal, flows, gst.Id, 0, invoice.GstMinor, accounting.CashFlowNoncash); err != nil {
				return nil, err
			}
		}
		journals, _ := accounting.NewJournalRepository(r.executor)
		if err = r.postJournal(ctx, journals, journal, "RECEIVABLE", invoice.Id, invoice.Version, flows); err != nil {
			return nil, err
		}
		journalID = &journal.Id
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(invoice)
	if err != nil {
		return nil, ErrRepository
	}
	hash := sha256.Sum256(encoded)
	var basGross int64
	for index, line := range invoice.Draft.Lines {
		if line.TaxTreatment != tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			basGross += invoice.LineTotals[index].TotalMinor
		}
	}
	_, err = r.executor.ExecContext(ctx, `INSERT INTO invoice_income_postings_v1(invoice_id,organisation_id,invoice_version,invoice_date,gst_basis,revenue_account_id,journal_id,invoice_net_minor,invoice_gst_minor,invoice_total_minor,bas_sales_gross_minor,profile_version,basis_audit_sequence,financial_revision,source_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, invoice.Id, profile.Id, invoice.Version, invoice.Draft.IssueDate, profile.GstBasis, revenue.Id, incomeNullable(journalID), invoice.SubtotalMinor, invoice.GstMinor, invoice.TotalMinor, basGross, profile.Version, sequence, revision, hash[:], r.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("%w: retain accounting source: %v", ErrRepository, err)
	}
	for index, line := range invoice.Draft.Lines {
		totals := invoice.LineTotals[index]
		if _, err = r.executor.ExecContext(ctx, `INSERT INTO invoice_income_lines_v1(invoice_id,sequence,gross_minor,net_minor,gst_minor,treatment) VALUES(?,?,?,?,?,?)`, invoice.Id, index+1, totals.TotalMinor, totals.SubtotalMinor, totals.GstMinor, line.TaxTreatment); err != nil {
			return nil, ErrRepository
		}
	}
	return r.Get(ctx, profile.Id, invoice.Id)
}

func (r *AccountingRepository) RecordPayment(ctx context.Context, invoice *tammyv1.Invoice, profile *tammyv1.Organisation, request *tammyv1.RecordInvoicePaymentRequest) (*tammyv1.InvoiceAccounting, *tammyv1.InvoicePayment, error) {
	if r == nil || invoice == nil || profile == nil || request == nil || invoice.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED || invoice.OrganisationId != profile.Id {
		return nil, nil, ErrRepository
	}
	current, err := r.Get(ctx, profile.Id, invoice.Id)
	if err != nil {
		return nil, nil, err
	}
	if !current.Posted || current.Version != request.ExpectedAccountingVersion {
		return nil, nil, ErrConflict
	}
	paymentDate := incomeDateString(request.PaymentDate)
	if request.Amount == nil || request.Amount.CurrencyCode != "AUD" || request.Amount.MinorUnits <= 0 || request.Amount.MinorUnits > current.OutstandingMinor || paymentDate == "" || paymentDate < invoice.Draft.IssueDate || !text(request.Reference, 200, false) {
		return nil, nil, ErrPayment
	}
	var sequence uint64
	rows, err := r.executor.QueryContext(ctx, `SELECT basis_audit_sequence FROM invoice_income_postings_v1 WHERE invoice_id=? AND organisation_id=?`, invoice.Id, profile.Id)
	if err != nil {
		return nil, nil, ErrRepository
	}
	if !rows.Next() || rows.Scan(&sequence) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return nil, nil, ErrRepository
	}
	if rows.Close() != nil {
		return nil, nil, ErrRepository
	}
	if _, err = CheckGSTBasisHistory(ctx, r.executor, profile.Id, sequence); err != nil {
		return nil, nil, err
	}
	if current.GstBasis != profile.GstBasis {
		return nil, nil, ErrGSTBasisHistory
	}
	accounts, _ := accounting.NewAccountRepository(r.executor)
	bank, err := accounts.Get(ctx, request.BankAccountId)
	if err != nil || bank.OrganisationId != profile.Id || bank.Type != tammyv1.AccountType_ACCOUNT_TYPE_ASSET || bank.Status != tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE || !accounting.IsCashAccount(bank) {
		return nil, nil, ErrPayment
	}
	lines := make([]incomeLine, len(invoice.Draft.Lines))
	for index, line := range invoice.Draft.Lines {
		total := invoice.LineTotals[index]
		lines[index] = incomeLine{Gross: total.TotalMinor, Net: total.SubtotalMinor, GST: total.GstMinor, Treatment: line.TaxTreatment}
	}
	beforeGross, beforeGST, err := cumulativeAllocations(lines, current.ReceivedMinor, invoice.TotalMinor)
	if err != nil {
		return nil, nil, err
	}
	afterGross, afterGST, err := cumulativeAllocations(lines, current.ReceivedMinor+request.Amount.MinorUnits, invoice.TotalMinor)
	if err != nil {
		return nil, nil, err
	}
	var allocatedGST, basGross int64
	for index, line := range lines {
		allocatedGST += afterGST[index] - beforeGST[index]
		if line.Treatment != tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			basGross += afterGross[index] - beforeGross[index]
		}
	}
	paymentID, err := r.newID()
	if err != nil {
		return nil, nil, ErrRepository
	}
	revision, err := r.bump(ctx, request.CommandContext.IdempotencyKey, true)
	if err != nil {
		return nil, nil, err
	}
	journal, flows, err := r.makeJournal(profile.Id, request.PaymentDate, "Payment for invoice "+invoice.Draft.Number, tammyv1.JournalSource_JOURNAL_SOURCE_INVOICE_PAYMENT, revision)
	if err != nil {
		return nil, nil, err
	}
	receivable, err := r.controlAccount(ctx, profile.Id, "1100")
	if err != nil {
		return nil, nil, err
	}
	if err = r.addLine(journal, flows, bank.Id, request.Amount.MinorUnits, 0, accounting.CashFlowOperating); err != nil {
		return nil, nil, err
	}
	if err = r.addLine(journal, flows, receivable.Id, 0, request.Amount.MinorUnits, accounting.CashFlowNoncash); err != nil {
		return nil, nil, err
	}
	if current.GstBasis == tammyv1.GstBasis_GST_BASIS_CASH && allocatedGST > 0 {
		deferred, err := r.controlAccount(ctx, profile.Id, "2110")
		if err != nil {
			return nil, nil, err
		}
		payable, err := r.controlAccount(ctx, profile.Id, "2100")
		if err != nil {
			return nil, nil, err
		}
		if err = r.addLine(journal, flows, deferred.Id, allocatedGST, 0, accounting.CashFlowNoncash); err != nil {
			return nil, nil, err
		}
		if err = r.addLine(journal, flows, payable.Id, 0, allocatedGST, accounting.CashFlowNoncash); err != nil {
			return nil, nil, err
		}
	}
	journals, _ := accounting.NewJournalRepository(r.executor)
	if err = r.postJournal(ctx, journals, journal, "BANKING", paymentID, 1, flows); err != nil {
		return nil, nil, err
	}
	payment := &tammyv1.InvoicePayment{Id: paymentID, OrganisationId: profile.Id, InvoiceId: invoice.Id, PaymentDate: proto.Clone(request.PaymentDate).(*tammyv1.CivilDate), Amount: proto.Clone(request.Amount).(*tammyv1.Money), AllocatedGst: incomeMoney(allocatedGST), BasSalesGross: incomeMoney(basGross), BankAccountId: bank.Id, Reference: request.Reference, JournalId: journal.Id, CreatedAt: timestamppb.New(r.now())}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payment)
	if err != nil {
		return nil, nil, ErrRepository
	}
	hash := sha256.Sum256(encoded)
	_, err = r.executor.ExecContext(ctx, `INSERT INTO invoice_payment_v1(id,organisation_id,invoice_id,payment_date,amount_minor,allocated_gst_minor,bas_sales_gross_minor,bank_account_id,journal_id,reference,payment_proto,source_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, payment.Id, profile.Id, invoice.Id, paymentDate, request.Amount.MinorUnits, allocatedGST, basGross, bank.Id, journal.Id, request.Reference, encoded, hash[:], r.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, nil, ErrRepository
	}
	for index := range lines {
		_, err = r.executor.ExecContext(ctx, `INSERT INTO invoice_payment_lines_v1(payment_id,sequence,gross_minor,gst_minor) VALUES(?,?,?,?)`, payment.Id, index+1, afterGross[index]-beforeGross[index], afterGST[index]-beforeGST[index])
		if err != nil {
			return nil, nil, ErrRepository
		}
	}
	result, err := r.Get(ctx, profile.Id, invoice.Id)
	return result, payment, err
}

func (r *AccountingRepository) revenueAccount(ctx context.Context, org string) (*tammyv1.Account, error) {
	accounts, _ := accounting.NewAccountRepository(r.executor)
	rows, err := r.executor.QueryContext(ctx, `SELECT revenue_account_id FROM invoice_income_settings_v1 WHERE organisation_id=?`, org)
	if err != nil {
		return nil, ErrRepository
	}
	var configured string
	found := rows.Next()
	if found && rows.Scan(&configured) != nil {
		_ = rows.Close()
		return nil, ErrRepository
	}
	if rows.Err() != nil {
		_ = rows.Close()
		return nil, ErrRepository
	}
	_ = rows.Close()
	if found {
		account, err := accounts.Get(ctx, configured)
		if err != nil || account.OrganisationId != org || account.Type != tammyv1.AccountType_ACCOUNT_TYPE_REVENUE || accounting.ValidateManualPosting(account) != nil {
			return nil, accounting.ErrAccountNotPostable
		}
		return account, nil
	}
	for code := 4000; code < 4900; code++ {
		rows, err = r.executor.QueryContext(ctx, `SELECT id FROM accounts WHERE organisation_id=? AND code=?`, org, strconv.Itoa(code))
		if err != nil {
			return nil, ErrRepository
		}
		var occupied string
		exists := rows.Next()
		if exists && rows.Scan(&occupied) != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		if rows.Err() != nil {
			_ = rows.Close()
			return nil, ErrRepository
		}
		_ = rows.Close()
		var account *tammyv1.Account
		if exists {
			account, err = accounts.Get(ctx, occupied)
			if err != nil {
				return nil, err
			}
			if code != 4000 || account.Type != tammyv1.AccountType_ACCOUNT_TYPE_REVENUE || accounting.ValidateManualPosting(account) != nil {
				continue
			}
		} else {
			id, err := r.newID()
			if err != nil {
				return nil, ErrRepository
			}
			account = &tammyv1.Account{Id: id, OrganisationId: org, Version: 1, Code: strconv.Itoa(code), Name: "Invoice sales", Type: tammyv1.AccountType_ACCOUNT_TYPE_REVENUE, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "income_statement.sales", CashFlowClassification: "operating"}
			if err = accounts.Create(ctx, account, r.now()); err != nil {
				return nil, err
			}
		}
		if _, err = r.executor.ExecContext(ctx, `INSERT INTO invoice_income_settings_v1(organisation_id,revenue_account_id) VALUES(?,?)`, org, account.Id); err != nil {
			return nil, ErrRepository
		}
		return account, nil
	}
	return nil, accounting.ErrAccountNotPostable
}
func (r *AccountingRepository) controlAccount(ctx context.Context, org, code string) (*tammyv1.Account, error) {
	rows, err := r.executor.QueryContext(ctx, `SELECT id FROM accounts WHERE organisation_id=? AND code=? AND designation='CONTROL' AND status='ACTIVE'`, org, code)
	if err != nil {
		return nil, ErrRepository
	}
	var id string
	if !rows.Next() || rows.Scan(&id) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return nil, accounting.ErrAccountNotPostable
	}
	if rows.Close() != nil {
		return nil, ErrRepository
	}
	accounts, _ := accounting.NewAccountRepository(r.executor)
	return accounts.Get(ctx, id)
}
func (r *AccountingRepository) makeJournal(org string, date *tammyv1.CivilDate, memo string, source tammyv1.JournalSource, revision uint64) (*tammyv1.Journal, map[string][]accounting.CashFlowComponent, error) {
	id, err := r.newID()
	if err != nil {
		return nil, nil, ErrRepository
	}
	return &tammyv1.Journal{Id: id, OrganisationId: org, Version: 1, State: tammyv1.JournalState_JOURNAL_STATE_POSTED, Source: source, PostingDate: proto.Clone(date).(*tammyv1.CivilDate), Memo: memo, FinancialRevision: revision, PostedAt: timestamppb.New(r.now())}, make(map[string][]accounting.CashFlowComponent), nil
}
func (r *AccountingRepository) addLine(journal *tammyv1.Journal, flows map[string][]accounting.CashFlowComponent, account string, debit, credit int64, category accounting.CashFlowCategory) error {
	id, err := r.newID()
	if err != nil {
		return ErrRepository
	}
	journal.Lines = append(journal.Lines, &tammyv1.JournalLine{Id: id, JournalId: journal.Id, AccountId: account, Sequence: uint32(len(journal.Lines) + 1), Debit: incomeMoney(debit), Credit: incomeMoney(credit)})
	flows[id] = []accounting.CashFlowComponent{{Category: category, AmountMinor: debit - credit}}
	return nil
}
func (r *AccountingRepository) postJournal(ctx context.Context, repository *accounting.JournalRepository, journal *tammyv1.Journal, source, id string, version uint64, flows map[string][]accounting.CashFlowComponent) error {
	debit, credit, err := accounting.CheckedJournalTotals(journal.Lines)
	if err != nil {
		return err
	}
	journal.TotalDebits = incomeMoney(debit)
	journal.TotalCredits = incomeMoney(credit)
	return repository.Post(ctx, journal, source, id, version, nil, flows, r.now())
}

type incomeRow struct {
	rows *sql.Rows
	err  error
}

func (row incomeRow) Scan(values ...any) error {
	if row.err != nil {
		return row.err
	}
	defer row.rows.Close()
	if !row.rows.Next() {
		if err := row.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := row.rows.Scan(values...); err != nil {
		return err
	}
	if row.rows.Next() {
		return ErrRepository
	}
	return row.rows.Err()
}
func (r *AccountingRepository) bump(ctx context.Context, key string, payment bool) (uint64, error) {
	repo, err := revisions.NewWithExecutor(r.executor.ExecContext, func(ctx context.Context, query string, args ...any) revisions.RowScanner {
		rows, err := r.executor.QueryContext(ctx, query, args...)
		return incomeRow{rows, err}
	}, clock.Func(r.now))
	if err != nil {
		return 0, err
	}
	snapshot, _, err := repo.Bump(ctx, key, revisions.Domains{Ledger: true, TaxSource: true, Settlement: payment})
	return snapshot.Financial, err
}
func incomeNullable(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
func incomeMoney(value int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: value}
}
func incomeCivilDate(value string) *tammyv1.CivilDate {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	return &tammyv1.CivilDate{Year: int32(parsed.Year()), Month: int32(parsed.Month()), Day: int32(parsed.Day())}
}
func incomeDateString(value *tammyv1.CivilDate) string {
	if value == nil {
		return ""
	}
	formatted := fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
	if !date(formatted) {
		return ""
	}
	return formatted
}
