//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// This test-only observer reads an owned, closed packaged fixture. It is never
// shipped in the app and cannot post, settle or modify business records.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type request struct {
	UserDataPath   string `json:"userDataPath"`
	WorkspaceID    string `json:"workspaceId"`
	OrganisationID string `json:"organisationId"`
	Passphrase     string `json:"passphrase"`
}
type fact struct {
	ID             string `json:"id"`
	JournalID      string `json:"journalId"`
	JournalLineID  string `json:"journalLineId"`
	PaymentID      string `json:"paymentId"`
	PaymentDate    string `json:"paymentDate"`
	Category       string `json:"category"`
	Sequence       int64  `json:"sequence"`
	AmountMinor    string `json:"amountMinor"`
	SourceRevision string `json:"sourceRevision"`
}
type evidence struct {
	WorkspaceID                string            `json:"workspaceId"`
	OrganisationID             string            `json:"organisationId"`
	Facts                      []fact            `json:"facts"`
	Projection                 map[string]string `json:"projection"`
	SbrSimulatorTransportCount int64             `json:"sbrSimulatorTransportCount"`
}

func main() {
	if err := run(); err != nil {
		// No paths, passphrase, derived keys or database diagnostics enter artifacts.
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
func run() (err error) {
	stage := "INPUT"
	defer func() {
		if err != nil {
			err = fmt.Errorf("PURCHASE_PACKAGED_INSPECTION_FAILED_%s", stage)
		}
	}()
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 16_385))
	decoder.DisallowUnknownFields()
	var input request
	if decoder.Decode(&input) != nil || !ids.IsCanonicalV7(input.WorkspaceID) || !ids.IsCanonicalV7(input.OrganisationID) || len(input.Passphrase) < 20 || len(input.Passphrase) > 1024 {
		return errors.New("invalid fixture input")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("extra fixture input")
	}
	if !filepath.IsAbs(input.UserDataPath) || filepath.Clean(input.UserDataPath) != input.UserDataPath || filepath.Base(input.UserDataPath) != "user-data-primary" || filepath.Base(filepath.Dir(input.UserDataPath)) != "electron-raw" {
		return errors.New("not a packaged fixture")
	}
	resolved, err := filepath.EvalSymlinks(input.UserDataPath)
	if err != nil || resolved != input.UserDataPath {
		return errors.New("redirected fixture")
	}
	stage = "INSTALLATION_KEY"
	root := filepath.Join(input.UserDataPath, "local-core", "core")
	masterPath := filepath.Join(root, "installation.key")
	info, err := os.Lstat(masterPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("invalid fixture key")
	}
	master, err := os.ReadFile(masterPath)
	if err != nil || len(master) != 32 {
		return errors.New("invalid fixture key")
	}
	defer workspace.Zero(master)
	hash := sha256.New()
	_, _ = hash.Write([]byte("tammy.local-composition.v1\x00workspace-header\x00"))
	_, _ = hash.Write(master)
	authKey := hash.Sum(nil)
	defer workspace.Zero(authKey)
	stage = "HEADER"
	databasePath := filepath.Join(root, "workspace", "tammy-workspace.db")
	info, err = os.Lstat(databasePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("missing fixture database")
	}
	header, err := workspace.NewHeaderStore(databasePath+".header", authKey)
	if err != nil {
		return err
	}
	defer header.Close()
	slots, err := header.Slots()
	if err != nil {
		return err
	}
	slot := slots[0]
	if slot.WorkspaceID != input.WorkspaceID || slot.Version == 0 {
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
	key, err := workspace.UnwrapWithPassphrase(passwords, passphrase, slot.PassphraseWrap, slot.WorkspaceID, slot.Version)
	if err != nil {
		return err
	}
	defer workspace.Zero(key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stage = "DATABASE"
	db, err := sqlcipher.Open(ctx, databasePath, key)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return err
	}
	tx, err := db.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stage = "ORGANISATION"
	var owned int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM organisations WHERE id=?", input.OrganisationID).Scan(&owned); err != nil || owned != 1 {
		return errors.New("foreign fixture organisation")
	}
	out := evidence{WorkspaceID: input.WorkspaceID, OrganisationID: input.OrganisationID, Facts: []fact{}, Projection: map[string]string{}}
	stage = "BANK_FACTS"
	rows, err := tx.QueryContext(ctx, `SELECT f.id,j.id,l.id,p.id,p.payment_date,f.category,f.sequence,f.amount_minor,f.source_revision,j.source_revision
FROM cash_flow_facts f JOIN journal_lines l ON l.id=f.journal_line_id
JOIN journals j ON j.id=l.journal_id AND j.organisation_id=f.organisation_id
JOIN purchase_payments_v1 p ON p.journal_id=j.id AND p.organisation_id=j.organisation_id AND p.bank_account_id=l.account_id
WHERE j.organisation_id=? AND j.source_kind=7 AND j.source_id=p.id ORDER BY p.created_at,p.id,f.sequence`, input.OrganisationID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var value fact
		var amount, revision, journalRevision int64
		if err = rows.Scan(&value.ID, &value.JournalID, &value.JournalLineID, &value.PaymentID, &value.PaymentDate, &value.Category, &value.Sequence, &amount, &revision, &journalRevision); err != nil {
			_ = rows.Close()
			return err
		}
		if amount >= 0 || revision != journalRevision || value.Category == "NONCASH" {
			_ = rows.Close()
			return errors.New("invalid retained bank fact")
		}
		value.AmountMinor, value.SourceRevision = strconv.FormatInt(amount, 10), strconv.FormatInt(revision, 10)
		out.Facts = append(out.Facts, value)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	// This independent SQL projection reads actual signed facts, rather than
	// reconstructing classifications or amounts from the purchase response.
	stage = "PROJECTION"
	rows, err = tx.QueryContext(ctx, `SELECT f.category,SUM(f.amount_minor) FROM cash_flow_facts f
JOIN journal_lines l ON l.id=f.journal_line_id
JOIN journals j ON j.id=l.journal_id AND j.organisation_id=f.organisation_id
JOIN purchase_payments_v1 p ON p.journal_id=l.journal_id AND p.bank_account_id=l.account_id AND p.organisation_id=f.organisation_id
WHERE f.organisation_id=? AND j.source_kind=7 AND j.source_id=p.id GROUP BY f.category ORDER BY f.category`, input.OrganisationID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var category string
		var amount int64
		if err = rows.Scan(&category, &amount); err != nil {
			_ = rows.Close()
			return err
		}
		out.Projection[category] = strconv.FormatInt(amount, 10)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	// Purchase journeys must not create simulator transport attempts. This
	// scoped aggregate reads the immutable workspace/org history without
	// exposing request data or allowing the inspector to modify it.
	stage = "SBR_TRANSPORTS"
	var simulatorTransportCount int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sbr_simulator_transports_v1
WHERE workspace_id=? AND organisation_id=?`, input.WorkspaceID, input.OrganisationID).Scan(&simulatorTransportCount); err != nil || simulatorTransportCount < 0 || simulatorTransportCount > 10000 {
		return errors.New("invalid SBR simulator transport count")
	}
	out.SbrSimulatorTransportCount = simulatorTransportCount
	stage = "OUTPUT"
	return json.NewEncoder(os.Stdout).Encode(out)
}
