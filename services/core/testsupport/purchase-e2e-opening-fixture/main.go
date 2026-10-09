//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// This test-only writer seeds one owned, closed packaged workspace through the
// production opening journal and repositories. It is not shipped in the app.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	workspacePassphraseMin = 20
	workspacePassphraseMax = 1024
	fixtureDeadline        = 30 * time.Second
	fixtureAmountMinor     = int64(11000)
)

type request struct {
	UserDataPath   string `json:"userDataPath"`
	WorkspaceID    string `json:"workspaceId"`
	OrganisationID string `json:"organisationId"`
	Passphrase     string `json:"passphrase"`
}

type output struct {
	Opening             json.RawMessage `json:"opening"`
	OpeningPayableID    string          `json:"openingPayableId"`
	OpeningEquityCode   string          `json:"openingEquityCode"`
	OutOfScopeTaxCodeID string          `json:"outOfScopeTaxCodeId"`
}

func main() {
	if err := run(); err != nil {
		// Diagnostics intentionally contain only a fixed stage, never a path,
		// passphrase, database error, or derived key.
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run() (err error) {
	stage := "INPUT"
	defer func() {
		if err != nil {
			err = fmt.Errorf("PURCHASE_OPENING_FIXTURE_FAILED_%s", stage)
		}
	}()

	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 16_385))
	decoder.DisallowUnknownFields()
	var input request
	if decoder.Decode(&input) != nil || !ids.IsCanonicalV7(input.WorkspaceID) ||
		!ids.IsCanonicalV7(input.OrganisationID) || len(input.Passphrase) < workspacePassphraseMin ||
		len(input.Passphrase) > workspacePassphraseMax {
		return errors.New("invalid fixture input")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("extra fixture input")
	}
	if !filepath.IsAbs(input.UserDataPath) || filepath.Clean(input.UserDataPath) != input.UserDataPath ||
		filepath.Base(input.UserDataPath) != "user-data-primary" ||
		filepath.Base(filepath.Dir(input.UserDataPath)) != "electron-raw" {
		return errors.New("not a packaged fixture")
	}
	resolved, err := filepath.EvalSymlinks(input.UserDataPath)
	if err != nil || resolved != input.UserDataPath {
		return errors.New("redirected fixture")
	}

	stage = "INSTALLATION_KEY"
	root := filepath.Join(input.UserDataPath, "local-core", "core")
	masterPath := filepath.Join(root, "installation.key")
	if err := requirePrivateRegularFile(masterPath); err != nil {
		return err
	}
	master, err := os.ReadFile(masterPath)
	if err != nil || len(master) != sqlcipher.KeySize {
		return errors.New("invalid fixture key")
	}
	defer workspace.Zero(master)

	stage = "HEADER"
	headerPath := filepath.Join(root, "workspace", "tammy-workspace.db.header")
	if err := requirePrivateRegularFile(headerPath); err != nil {
		return err
	}
	authKey := deriveLocalKey(master, "workspace-header")
	defer workspace.Zero(authKey)
	header, err := workspace.NewHeaderStore(headerPath, authKey)
	if err != nil {
		return err
	}
	defer header.Close()
	slots, err := header.Slots()
	if err != nil {
		return err
	}
	active := slots[0]
	if active.WorkspaceID != input.WorkspaceID || active.Version == 0 {
		return errors.New("foreign fixture header")
	}

	stage = "UNWRAP"
	passwords, err := workspace.NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		return err
	}
	passphrase := []byte(input.Passphrase)
	input.Passphrase = ""
	defer workspace.Zero(passphrase)
	key, err := workspace.UnwrapWithPassphrase(passwords, passphrase, active.PassphraseWrap,
		active.WorkspaceID, active.Version)
	if err != nil {
		return err
	}
	defer workspace.Zero(key)

	ctx, cancel := context.WithTimeout(context.Background(), fixtureDeadline)
	defer cancel()
	stage = "DATABASE"
	databasePath := filepath.Join(root, "workspace", "tammy-workspace.db")
	if err := requirePrivateRegularFile(databasePath); err != nil {
		return err
	}
	database, err := sqlcipher.Open(ctx, databasePath, key)
	if err != nil {
		return err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	tx, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stage = "OWNERSHIP"
	var owned int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM organisations WHERE id=?`, input.OrganisationID).Scan(&owned); err != nil || owned != 1 {
		return errors.New("foreign fixture organisation")
	}
	if err := requireUnusedPurchaseWorkspace(ctx, tx, input.OrganisationID); err != nil {
		return err
	}

	stage = "ACCOUNTS"
	accounts, err := accounting.NewAccountRepository(tx)
	if err != nil {
		return err
	}
	payable, err := findAccount(ctx, accounts, input.OrganisationID, func(account *tammyv1.Account) bool {
		return account.Code == "2000"
	})
	if err != nil {
		return err
	}
	equity, err := findAccount(ctx, accounts, input.OrganisationID, func(account *tammyv1.Account) bool {
		return account.ReportClassification == "balance_sheet.opening_equity"
	})
	if err != nil || payable == nil || equity == nil {
		return errors.New("fixture opening accounts unavailable")
	}
	rules, err := artefacts.NewRepository(tx)
	if err != nil {
		return err
	}
	codes, err := rules.ListEffectiveTaxCodes(ctx, input.OrganisationID, "2026-10-02", "", 200)
	if err != nil || len(codes) >= 200 {
		return errors.New("fixture tax catalogue incomplete")
	}
	var noTaxID string
	for _, code := range codes {
		if code.Treatment == tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE {
			if noTaxID != "" {
				return errors.New("ambiguous fixture tax code")
			}
			noTaxID = code.Id
		}
	}
	if noTaxID == "" {
		return errors.New("fixture tax code unavailable")
	}

	stage = "OPENING"
	now := time.Now().UTC()
	generator, err := ids.NewGenerator(clock.Func(time.Now), rand.Reader)
	if err != nil {
		return err
	}
	newID := func() (string, error) { return generator.New() }
	conversionID, err := newID()
	if err != nil {
		return err
	}
	journalID, err := newID()
	if err != nil {
		return err
	}
	openingPayableID, err := newID()
	if err != nil {
		return err
	}
	equityLineID, err := newID()
	if err != nil {
		return err
	}
	payableJournalLineID, err := newID()
	if err != nil {
		return err
	}
	equityJournalLineID, err := newID()
	if err != nil {
		return err
	}
	conversionDate := &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 31}
	inputs := []*tammyv1.OpeningBalanceInput{
		{ClientLineId: openingPayableID, Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM,
			AccountId: payable.Id, LedgerBalance: audMoney(fixtureAmountMinor),
			OriginalIssueDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
			OriginalDueDate:   &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 20},
			OutstandingGst:    audMoney(1000), PriorGstAttributed: audMoney(0)},
		{ClientLineId: equityLineID, Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_OPENING_EQUITY,
			AccountId: equity.Id, LedgerBalance: audMoney(-fixtureAmountMinor)},
	}
	revisionRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		return err
	}
	revision, err := revisionRepository.ReserveFinancialRevision(ctx, nil, now)
	if err != nil {
		return err
	}
	journal, flows, err := accounting.BuildOpeningJournal(input.OrganisationID, conversionDate, inputs,
		map[string]*tammyv1.Account{payable.Id: payable, equity.Id: equity}, journalID,
		[]string{payableJournalLineID, equityJournalLineID}, revision, now)
	if err != nil {
		return err
	}
	requestBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(&tammyv1.PostOpeningConversionRequest{
		OrganisationId: input.OrganisationID, ConversionDate: conversionDate, Balances: inputs,
	})
	if err != nil {
		return err
	}
	requestHash := sha256.Sum256(requestBytes)
	openingConversions, err := accounting.NewOpeningConversionRepository(tx)
	if err != nil {
		return err
	}
	conversion, err := openingConversions.Post(ctx, conversionID, input.OrganisationID, conversionDate,
		inputs, journal, flows, requestHash[:], now)
	if err != nil {
		return err
	}
	openingPayables, err := purchases.NewOpeningRepository(tx)
	if err != nil {
		return err
	}
	if err := openingPayables.RecordOpeningPayable(ctx, conversionID, inputs[0], now); err != nil {
		return err
	}
	retained, err := openingPayables.GetOpeningPayable(ctx, input.OrganisationID, openingPayableID)
	if err != nil || retained == nil || retained.Input == nil || retained.ConversionID != conversionID || retained.JournalID != journalID ||
		retained.Input.Source != nil || retained.Input.LedgerBalance.MinorUnits != fixtureAmountMinor ||
		!bytes.Equal(retained.SourceHash, sha256SumProto(inputs[0])) {
		return errors.New("opening source verification failed")
	}

	stage = "OUTPUT"
	openingJSON, err := protojson.Marshal(&tammyv1.PostOpeningConversionResponse{Conversion: conversion, Journal: journal})
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(output{Opening: openingJSON, OpeningPayableID: openingPayableID, OpeningEquityCode: equity.Code, OutOfScopeTaxCodeID: noTaxID})
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(encoded, '\n'))
	return err
}

func requireUnusedPurchaseWorkspace(ctx context.Context, tx *sqlcipher.Transaction, organisationID string) error {
	checks := []struct {
		table string
		query string
	}{
		{table: "opening conversions", query: `SELECT count(*) FROM opening_conversions WHERE organisation_id=?`},
		{table: "purchase postings", query: `SELECT count(*) FROM purchase_postings_v1 WHERE organisation_id=?`},
		{table: "purchase payments", query: `SELECT count(*) FROM purchase_payments_v1 WHERE organisation_id=?`},
	}
	for _, check := range checks {
		var count int
		if err := tx.QueryRowContext(ctx, check.query, organisationID).Scan(&count); err != nil || count != 0 {
			return fmt.Errorf("workspace already contains %s", check.table)
		}
	}
	return nil
}

func findAccount(ctx context.Context, repository *accounting.AccountRepository, organisationID string,
	match func(*tammyv1.Account) bool,
) (*tammyv1.Account, error) {
	var result *tammyv1.Account
	afterCode, afterID := "", ""
	for {
		page, err := repository.List(ctx, organisationID, afterCode, afterID, 200)
		if err != nil {
			return nil, err
		}
		for _, account := range page {
			if !match(account) {
				continue
			}
			if result != nil {
				return nil, errors.New("duplicate fixture account")
			}
			result = account
		}
		if len(page) < 200 {
			return result, nil
		}
		last := page[len(page)-1]
		afterCode, afterID = last.Code, last.Id
	}
}

func audMoney(minorUnits int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minorUnits}
}

func sha256SumProto(message proto.Message) []byte {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		return nil
	}
	digest := sha256.Sum256(encoded)
	return digest[:]
}

func requirePrivateRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("invalid fixture file")
	}
	return nil
}

func deriveLocalKey(master []byte, domain string) []byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.local-composition.v1\x00"))
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(master)
	return digest.Sum(nil)
}
