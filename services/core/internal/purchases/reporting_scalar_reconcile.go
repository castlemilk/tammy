package purchases

import (
	"context"
	"errors"
	"fmt"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"math"
	"reflect"
	"strings"
)

// ReconcileBASPhysicalKeys compares every frozen physical identity/reference
// with the complete module-owned row and journal projections. It performs no
// SQL and must precede replacement of shared raw tax-fact proof bodies.
func ReconcileBASPhysicalKeys(ctx context.Context, org string, keys []BASScalarKey, capture *PurchaseCoverageReportingCapture) error {
	if ctx == nil || !ids.IsCanonicalV7(org) || capture == nil || capture.OrganisationID != org {
		return ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrRepository, err)
	}
	type identity struct {
		kind     BASScalarKeyKind
		id       string
		sequence uint64
	}
	idOf := func(key BASScalarKey) identity {
		id := identity{kind: key.Kind, id: key.ID}
		if key.Sequence != nil {
			id.sequence = *key.Sequence
		}
		return id
	}
	selected := make(map[identity]BASScalarKey, len(keys))
	for _, key := range keys {
		if key.OrganisationID != org {
			return ErrRepository
		}
		id := idOf(key)
		if _, dup := selected[id]; dup {
			return ErrRepository
		}
		selected[id] = key
	}
	matched := make(map[identity]bool, len(keys))
	put := func(key BASScalarKey) error {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrRepository, err)
		}
		id := idOf(key)
		expected, ok := selected[id]
		if !ok || matched[id] {
			return fmt.Errorf("%w: BAS physical family %d key set differs", ErrRepository, key.Kind)
		}
		if !reflect.DeepEqual(expected, key) {
			left, right := reflect.ValueOf(expected), reflect.ValueOf(key)
			for index := 0; index < left.NumField(); index++ {
				if !reflect.DeepEqual(left.Field(index).Interface(), right.Field(index).Interface()) {
					return fmt.Errorf("%w: BAS physical family %d field %s differs", ErrRepository, key.Kind, left.Type().Field(index).Name)
				}
			}
			return ErrRepository
		}
		matched[id] = true
		return nil
	}
	byTable := make(map[string][]PurchaseReportingRetainedRow)
	byID := make(map[string]map[string]PurchaseReportingRetainedRow)
	promoted := make(map[string]bool)
	for _, row := range capture.Rows {
		byTable[row.Table] = append(byTable[row.Table], row)
		if id := postingReportingString(row, "id"); id != "" {
			if byID[row.Table] == nil {
				byID[row.Table] = make(map[string]PurchaseReportingRetainedRow)
			}
			if _, dup := byID[row.Table][id]; dup {
				return ErrRepository
			}
			byID[row.Table][id] = row
		}
		if row.Table == "purchase_postings_v1" {
			promoted[postingReportingString(row, "document_id")] = true
		}
	}
	journals := make(map[string]PurchaseReportingRetainedRow, len(capture.Journals))
	proofs := make(map[string]*accounting.JournalReportingRecord, len(capture.Journals))
	lineParents := make(map[string]string)
	for _, proof := range capture.Journals {
		if proof == nil || proof.Journal == nil || proof.Journal.OrganisationId != org || proof.SourceKind < 0 || proof.SourceRevision > math.MaxInt64 || proof.Journal.Version > math.MaxInt64 {
			return ErrRepository
		}
		id := proof.Journal.Id
		if _, dup := journals[id]; dup {
			return ErrRepository
		}
		journals[id] = PurchaseReportingRetainedRow{Table: "journals", Columns: []string{"id", "organisation_id", "version", "source_type", "source_id", "source_revision", "source_kind"}, Values: []PurchaseReportingRetainedValue{
			{Kind: "TEXT", Text: id}, {Kind: "TEXT", Text: org}, {Kind: "INTEGER", Integer: int64(proof.Journal.Version)}, {Kind: "TEXT", Text: proof.SourceType}, {Kind: "TEXT", Text: proof.SourceID}, {Kind: "INTEGER", Integer: int64(proof.SourceRevision)}, {Kind: "INTEGER", Integer: proof.SourceKind},
		}}
		proofs[id] = proof
		for _, line := range proof.Journal.Lines {
			if line == nil || line.Id == "" || line.JournalId != id {
				return ErrRepository
			}
			if _, dup := lineParents[line.Id]; dup {
				return ErrRepository
			}
			lineParents[line.Id] = id
		}
	}
	sources := make(map[string]bool)
	for id := range byID["documents"] {
		sources[id] = true
	}
	for _, row := range byTable["purchase_opening_payables"] {
		conversion := byID["opening_conversions"][postingReportingString(row, "conversion_id")]
		state := postingReportingString(conversion, "state")
		if state == "POSTED" || state == "REPLACED" {
			sources[postingReportingString(row, "id")] = true
		}
	}
	factParents := make(map[string]bool)
	for _, row := range byTable["tax_facts"] {
		if sources[postingReportingString(row, "source_id")] {
			id := lineParents[postingReportingString(row, "journal_line_id")]
			if id == "" {
				return ErrRepository
			}
			factParents[id] = true
		}
	}
	families := append(purchaseBASModernFamilies(), purchaseBASLegacyFamilies()...)
	for _, family := range families {
		parts := strings.Fields(family.from)
		if len(parts) < 3 || parts[1] != "AS" {
			return ErrRepository
		}
		table, alias := parts[0], parts[2]
		rows := byTable[table]
		if family.kind == BASScalarManualJournal {
			rows = nil
			for id, row := range journals {
				proof := proofs[id]
				if proof.SourceKind == 0 && proof.SourceType != "OPENING" && (sources[proof.SourceID] || factParents[id]) {
					rows = append(rows, row)
				}
			}
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrRepository, err)
			}
			aliases := map[string]PurchaseReportingRetainedRow{"j": {}, "journal": {}, "c": {}, "conversion": {}}
			aliases[alias] = row
			journalID := postingReportingString(row, "journal_id")
			if journalID != "" {
				journal, ok := journals[journalID]
				if !ok {
					return ErrRepository
				}
				aliases["j"] = journal
				aliases["journal"] = journal
			}
			conversionID := postingReportingString(row, "conversion_id")
			if family.kind == BASScalarEvent {
				payable := byID["purchase_opening_payables"][postingReportingString(row, "opening_payable_id")]
				conversionID = postingReportingString(payable, "conversion_id")
			}
			if conversionID != "" {
				conversion, ok := byID["opening_conversions"][conversionID]
				if !ok {
					return ErrRepository
				}
				aliases["c"] = conversion
				aliases["conversion"] = conversion
			}
			if family.kind == BASScalarDeferral {
				postingID := postingReportingString(row, "posting_id")
				version, valid := purchaseCoverageNullableInteger(row, "accounting_version")
				matches := 0
				for _, event := range byTable["purchase_accounting_events_v1"] {
					eventVersion, eventValid := purchaseCoverageNullableInteger(event, "accounting_version")
					if valid && eventValid && eventVersion == version && postingReportingString(event, "organisation_id") == org &&
						postingReportingString(event, "scope_kind") == "POSTING" && postingReportingString(event, "scope_id") == postingID &&
						postingReportingString(event, "command_type") == "tammy.v1.PurchaseService.DeferPurchaseGstClaim" {
						aliases["ev"] = event
						matches++
					}
				}
				if matches != 1 {
					return ErrRepository
				}
			}
			key, err := purchaseBASKeyFromRetainedFamily(family, aliases)
			if err != nil {
				return fmt.Errorf("%w: BAS physical family %d projection", err, family.kind)
			}
			switch family.kind {
			case BASScalarPosting:
				key.IsRoot = true
			case BASScalarPayment:
				key.IsReceipt = true
			case BASScalarDocument:
				key.IsRoot = !promoted[key.ID]
			case BASScalarOpeningPayable:
				conversion := aliases["c"]
				state := postingReportingString(conversion, "state")
				key.IsRoot = state == "POSTED" || state == "REPLACED"
			case BASScalarManualJournal:
				proof := proofs[key.ID]
				if proof.State == "POSTED" && proof.SourceType != "REVERSAL" {
					for _, line := range proof.Journal.Lines {
						account := byID["accounts"][line.AccountId]
						class := postingReportingString(account, "report_classification")
						if line.GetCredit().GetMinorUnits() > line.GetDebit().GetMinorUnits() && (class == "balance_sheet.cash" || class == "balance_sheet.cash_equivalent") {
							key.IsReceipt = true
						}
					}
				}
			}
			if key.OrganisationID != org || key.ID == "" {
				return ErrRepository
			}
			if err := put(key); err != nil {
				return err
			}
		}
	}
	if len(matched) != len(selected) {
		return ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrRepository, err)
	}
	return nil
}

// The existing closed family descriptors contain only direct alias.column
// projections and one explicit COALESCE pair. Reusing those field mappings
// keeps raw reference/null semantics aligned; no SQL is evaluated here.
func purchaseBASKeyFromRetainedFamily(family purchaseBASScalarFamily, aliases map[string]PurchaseReportingRetainedRow) (BASScalarKey, error) {
	value := func(expression string) (PurchaseReportingRetainedValue, error) {
		if expression == "" {
			return PurchaseReportingRetainedValue{Kind: "NULL"}, nil
		}
		if expression == "COALESCE(r.document_id,r.opening_payable_id)" {
			row := aliases["r"]
			first, ok := basLegacyRowValue(row, "document_id")
			if !ok {
				return PurchaseReportingRetainedValue{}, ErrRepository
			}
			if first.Kind != "NULL" {
				return first, nil
			}
			second, ok := basLegacyRowValue(row, "opening_payable_id")
			if !ok {
				return PurchaseReportingRetainedValue{}, ErrRepository
			}
			return second, nil
		}
		alias, column, ok := strings.Cut(expression, ".")
		if !ok || strings.Contains(column, ".") {
			return PurchaseReportingRetainedValue{}, ErrRepository
		}
		row, present := aliases[alias]
		if !present {
			return PurchaseReportingRetainedValue{}, ErrRepository
		}
		if row.Table == "" {
			return PurchaseReportingRetainedValue{Kind: "NULL"}, nil
		}
		cell, present := basLegacyRowValue(row, column)
		if !present {
			return PurchaseReportingRetainedValue{}, ErrRepository
		}
		return cell, nil
	}
	key := BASScalarKey{Kind: family.kind}
	stringsOut := make([]*string, len(purchaseBASScalarStringColumns))
	for index, name := range purchaseBASScalarStringColumns {
		cell, err := value(family.columns[name])
		if err != nil {
			return BASScalarKey{}, err
		}
		if cell.Kind == "NULL" {
			continue
		}
		if cell.Kind != "TEXT" {
			return BASScalarKey{}, ErrRepository
		}
		text := cell.Text
		stringsOut[index] = &text
	}
	if stringsOut[0] == nil || stringsOut[1] == nil {
		return BASScalarKey{}, ErrRepository
	}
	key.OrganisationID = *stringsOut[0]
	key.ID = *stringsOut[1]
	targets := []**string{&key.PostingID, &key.DocumentID, &key.OpeningPayableID, &key.ConversionID, &key.PaymentID, &key.AccountID, &key.JournalID, &key.EvidenceID, &key.DeferralID, &key.OperationKey, &key.RelatedID, &key.ScopeKind, &key.ScopeID, &key.SourceKind, &key.SourceType, &key.SourceID, &key.DueDateSourceID, &key.ClaimEvidenceSourceID, &key.ActualPaymentDate}
	if len(targets) != len(stringsOut)-2 {
		return BASScalarKey{}, ErrRepository
	}
	for index, target := range targets {
		*target = stringsOut[index+2]
	}
	integers := []**uint64{&key.Sequence, &key.PostingAllocationSequence, &key.AllocationSequence, &key.ObjectVersion, &key.AccountingVersion, &key.AccountVersion, &key.SourceVersion, &key.DueDateSourceVersion, &key.ClaimEvidenceSourceVersion, &key.JournalSourceKind}
	for index, name := range purchaseBASScalarIntegerColumns {
		cell, err := value(family.columns[name])
		if err != nil {
			return BASScalarKey{}, err
		}
		if cell.Kind == "NULL" {
			continue
		}
		if cell.Kind != "INTEGER" || cell.Integer < 0 {
			return BASScalarKey{}, ErrRepository
		}
		number := uint64(cell.Integer)
		*integers[index] = &number
	}
	hashes := []*[]byte{&key.SourceHash, &key.RecordHash, &key.ReviewedSnapshotHash, &key.DueDateSourceHash, &key.ClaimEvidenceSourceHash}
	for index, name := range purchaseBASScalarHashColumns {
		cell, err := value(family.columns[name])
		if err != nil {
			return BASScalarKey{}, err
		}
		switch cell.Kind {
		case "NULL":
		case "BLOB":
			*hashes[index] = cell.Bytes
		case "TEXT":
			if family.kind != BASScalarOpeningConversion || name != "source_hash" {
				return BASScalarKey{}, ErrRepository
			}
			*hashes[index] = []byte(cell.Text)
		default:
			return BASScalarKey{}, ErrRepository
		}
	}
	return key, nil
}
