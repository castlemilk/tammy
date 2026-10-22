package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"math"
	"reflect"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
)

type basInvoicePhysicalIdentity struct {
	kind             invoices.BASScalarKeyKind
	invoice, receipt string
	sequence         uint64
}

func reconcileBASInvoicePhysicalKeys(ctx context.Context, org string, keys []invoices.BASScalarKey, capture *invoices.BASReportingCapture) error {
	if ctx == nil || capture == nil {
		return ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrSourceReservation, err)
	}
	selected := make(map[basInvoicePhysicalIdentity]invoices.BASScalarKey, len(keys))
	identity := func(key invoices.BASScalarKey) basInvoicePhysicalIdentity {
		id := basInvoicePhysicalIdentity{kind: key.Kind, invoice: key.InvoiceID}
		if key.ReceiptID != nil {
			id.receipt = *key.ReceiptID
		}
		if key.Sequence != nil {
			id.sequence = *key.Sequence
		}
		return id
	}
	for _, key := range keys {
		id := identity(key)
		if key.OrganisationID != org {
			return ErrSourceReservation
		}
		if _, dup := selected[id]; dup {
			return ErrSourceReservation
		}
		selected[id] = key
	}
	matched := make(map[basInvoicePhysicalIdentity]bool, len(keys))
	put := func(key invoices.BASScalarKey) error {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrSourceReservation, err)
		}
		id := identity(key)
		expected, ok := selected[id]
		if !ok || matched[id] || !reflect.DeepEqual(key, expected) {
			return ErrSourceReservation
		}
		matched[id] = true
		return nil
	}
	for _, candidate := range capture.Candidates {
		if candidate.ID == "" || candidate.OrganisationID != org {
			return ErrSourceReservation
		}
		version := candidate.Version
		if err := put(invoices.BASScalarKey{Kind: invoices.BASScalarInvoice, OrganisationID: org, InvoiceID: candidate.ID, RootVersion: &version}); err != nil {
			return err
		}
		if posting := candidate.Posting; posting != nil {
			if posting.InvoiceID != candidate.ID || posting.OrganisationID != org {
				return ErrSourceReservation
			}
			revision := posting.InvoiceVersion
			if err := put(invoices.BASScalarKey{Kind: invoices.BASScalarPosting, OrganisationID: org, InvoiceID: candidate.ID, PostingInvoiceVersion: &revision, JournalID: posting.JournalID, SourceHash: posting.SourceHash}); err != nil {
				return err
			}
			for _, line := range posting.IncomeLines {
				if line.InvoiceID != candidate.ID {
					return ErrSourceReservation
				}
				seq := line.Sequence
				if err := put(invoices.BASScalarKey{Kind: invoices.BASScalarIncomeLine, OrganisationID: org, InvoiceID: candidate.ID, Sequence: &seq}); err != nil {
					return err
				}
			}
		}
		for _, receipt := range candidate.Receipts {
			if receipt.InvoiceID != candidate.ID || receipt.OrganisationID != org {
				return ErrSourceReservation
			}
			id, journal := receipt.ID, receipt.JournalID
			if err := put(invoices.BASScalarKey{Kind: invoices.BASScalarReceipt, OrganisationID: org, InvoiceID: candidate.ID, ReceiptID: &id, JournalID: &journal, SourceHash: receipt.SourceHash}); err != nil {
				return err
			}
			for _, line := range receipt.Allocations {
				if line.PaymentID != receipt.ID {
					return ErrSourceReservation
				}
				seq := line.Sequence
				if err := put(invoices.BASScalarKey{Kind: invoices.BASScalarAllocation, OrganisationID: org, InvoiceID: candidate.ID, ReceiptID: &id, Sequence: &seq}); err != nil {
					return err
				}
			}
		}
	}
	if len(matched) != len(selected) {
		return ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrSourceReservation, err)
	}
	return nil
}

func reconcileBASTaxFactPhysicalKeys(ctx context.Context, org string, keys []accounting.TaxFactBASScalarKey, capture *accounting.TaxFactReportingCapture) error {
	if ctx == nil || capture == nil {
		return ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrSourceReservation, err)
	}
	selected := make(map[string]accounting.TaxFactBASScalarKey, len(keys))
	for _, key := range keys {
		if key.ID == "" || key.OrganisationID != org {
			return ErrSourceReservation
		}
		if _, dup := selected[key.ID]; dup {
			return ErrSourceReservation
		}
		selected[key.ID] = key
	}
	matched := make(map[string]bool, len(keys))
	for _, fact := range capture.Facts {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrSourceReservation, err)
		}
		key, ok := selected[fact.ID]
		if !ok || matched[fact.ID] || fact.OrganisationID != org || fact.Journal == nil || fact.Journal.OrganisationId != org ||
			fact.SourceRevision == 0 || fact.SourceRevision > math.MaxInt64 || fact.SourceContentHashPresent != (fact.SourceContentHash != nil) ||
			(fact.SourceContentHashPresent && len(fact.SourceContentHash) != 32) {
			return ErrSourceReservation
		}
		lineNumber := int64(0)
		for _, line := range fact.Journal.Lines {
			if line != nil && line.Id == fact.JournalLineID {
				if lineNumber != 0 || line.JournalId != fact.Journal.Id {
					return ErrSourceReservation
				}
				lineNumber = int64(line.Sequence)
			}
		}
		if lineNumber < 1 || key.ID != fact.ID || key.OrganisationID != fact.OrganisationID || key.JournalLineID != fact.JournalLineID || key.JournalID != fact.Journal.Id || key.LineNumber != lineNumber ||
			key.SourceType != fact.SourceType || key.SourceID != fact.SourceID || key.SourceRevision != fact.SourceRevision ||
			key.SourceContentHashPresent != fact.SourceContentHashPresent || (key.SourceContentHash == nil) != (fact.SourceContentHash == nil) || !bytes.Equal(key.SourceContentHash, fact.SourceContentHash) {
			return ErrSourceReservation
		}
		matched[fact.ID] = true
	}
	if len(matched) != len(selected) {
		return ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrSourceReservation, err)
	}
	return nil
}
