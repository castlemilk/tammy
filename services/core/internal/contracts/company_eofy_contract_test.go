package contracts_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type financialCloseFieldContract struct {
	name           protoreflect.Name
	kind           protoreflect.Kind
	referencedType protoreflect.FullName
	repeated       bool
	optional       bool
	required       bool
}

func TestCompanyTaxPreparationContractHasExactServiceAndEnums(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/company_tax.proto")
	if err != nil {
		t.Fatalf("company tax descriptor missing: %v", err)
	}
	wantEnums := map[protoreflect.Name][]string{
		"RequiredAnswer":                      {"REQUIRED_ANSWER_UNSPECIFIED", "REQUIRED_ANSWER_YES", "REQUIRED_ANSWER_NO"},
		"ReturnFactProvenanceKind":            {"RETURN_FACT_PROVENANCE_KIND_UNSPECIFIED", "RETURN_FACT_PROVENANCE_KIND_FROZEN_BOOK", "RETURN_FACT_PROVENANCE_KIND_REVIEWED_TAX_ADJUSTMENT", "RETURN_FACT_PROVENANCE_KIND_VERIFIED_PROFILE", "RETURN_FACT_PROVENANCE_KIND_EXPLICIT_EVIDENCED_INPUT", "RETURN_FACT_PROVENANCE_KIND_BUNDLE_ELECTION", "RETURN_FACT_PROVENANCE_KIND_CALCULATION_RULE"},
		"ReturnFactValidationStatus":          {"RETURN_FACT_VALIDATION_STATUS_UNSPECIFIED", "RETURN_FACT_VALIDATION_STATUS_VALID", "RETURN_FACT_VALIDATION_STATUS_BLOCKER", "RETURN_FACT_VALIDATION_STATUS_WARNING", "RETURN_FACT_VALIDATION_STATUS_INFORMATION", "RETURN_FACT_VALIDATION_STATUS_UNSUPPORTED"},
		"ReturnValidationSeverity":            {"RETURN_VALIDATION_SEVERITY_UNSPECIFIED", "RETURN_VALIDATION_SEVERITY_BLOCKER", "RETURN_VALIDATION_SEVERITY_WARNING", "RETURN_VALIDATION_SEVERITY_INFORMATION", "RETURN_VALIDATION_SEVERITY_UNSUPPORTED"},
		"TaxAdjustmentType":                   {"TAX_ADJUSTMENT_TYPE_UNSPECIFIED", "TAX_ADJUSTMENT_TYPE_NON_DEDUCTIBLE_EXPENSE", "TAX_ADJUSTMENT_TYPE_EXEMPT_NON_ASSESSABLE_INCOME", "TAX_ADJUSTMENT_TYPE_ACCOUNTING_TAX_DEPRECIATION", "TAX_ADJUSTMENT_TYPE_PROVISION_ACCRUAL_REVERSAL", "TAX_ADJUSTMENT_TYPE_TAX_PAYMENT_CREDIT", "TAX_ADJUSTMENT_TYPE_CURRENT_YEAR_REVENUE_LOSS", "TAX_ADJUSTMENT_TYPE_CARRIED_FORWARD_REVENUE_LOSS"},
		"TaxAdjustmentTiming":                 {"TAX_ADJUSTMENT_TIMING_UNSPECIFIED", "TAX_ADJUSTMENT_TIMING_PERMANENT", "TAX_ADJUSTMENT_TIMING_TEMPORARY"},
		"HoldingCompanyKind":                  {"HOLDING_COMPANY_KIND_UNSPECIFIED", "HOLDING_COMPANY_KIND_NONE", "HOLDING_COMPANY_KIND_AUSTRALIAN", "HOLDING_COMPANY_KIND_FOREIGN"},
		"BaseRatePassiveIncomeClassification": {"BASE_RATE_PASSIVE_INCOME_CLASSIFICATION_UNSPECIFIED", "BASE_RATE_PASSIVE_INCOME_CLASSIFICATION_PASSIVE", "BASE_RATE_PASSIVE_INCOME_CLASSIFICATION_NON_PASSIVE"},
		"SmallBusinessEntityChoice":           {"SMALL_BUSINESS_ENTITY_CHOICE_UNSPECIFIED", "SMALL_BUSINESS_ENTITY_CHOICE_APPLY", "SMALL_BUSINESS_ENTITY_CHOICE_DO_NOT_APPLY"},
		"DepreciationChoice":                  {"DEPRECIATION_CHOICE_UNSPECIFIED", "DEPRECIATION_CHOICE_STANDARD", "DEPRECIATION_CHOICE_SUPPORTED_SMALL_BUSINESS"},
		"CompanyReturnExportKind":             {"COMPANY_RETURN_EXPORT_KIND_UNSPECIFIED", "COMPANY_RETURN_EXPORT_KIND_REDACTED_REVIEW_PDF", "COMPANY_RETURN_EXPORT_KIND_ENCRYPTED_HANDOFF_ARCHIVE"},
	}
	for name, values := range wantEnums {
		assertFinancialCloseEnum(t, name, values)
	}
	service := file.Services().ByName("CompanyTaxService")
	if service == nil {
		t.Fatal("tammy.v1.CompanyTaxService missing")
	}
	wantRPCs := []string{"GetCompanyTaxProfile", "SetCompanyTaxProfile", "CreateCompanyReturn", "GetCompanyReturn", "ListCompanyReturnFacts", "SetCompanyReturnInput", "UpsertTaxAdjustment", "RemoveTaxAdjustment", "UpsertTaxElection", "RemoveTaxElection", "ValidateCompanyReturn", "AcknowledgeReturnWarning", "DeclareCompanyReturn", "WithdrawCompanyReturnDeclaration", "ExportCompanyReturnPack", "CreateCompanyReturnReplacement", "CreateCompanyReturnAmendment"}
	if service.Methods().Len() != len(wantRPCs) {
		t.Fatalf("CompanyTaxService method count = %d, want %d", service.Methods().Len(), len(wantRPCs))
	}
	for index, want := range wantRPCs {
		method := service.Methods().Get(index)
		if string(method.Name()) != want || method.Input().FullName() != protoreflect.FullName("tammy.v1."+want+"Request") || method.Output().FullName() != protoreflect.FullName("tammy.v1."+want+"Response") {
			t.Errorf("CompanyTaxService method %d = %s(%s) returns %s", index, method.FullName(), method.Input().FullName(), method.Output().FullName())
		}
		if method.IsStreamingClient() || method.IsStreamingServer() {
			t.Errorf("%s must be unary", method.FullName())
		}
	}
	for index := 0; index < file.Messages().Len(); index++ {
		message := file.Messages().Get(index)
		for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
			field := message.Fields().Get(fieldIndex)
			if field.IsMap() {
				t.Errorf("%s must not be a map", field.FullName())
			}
			if field.IsList() && fieldRules(t, field).GetRepeated().GetMaxItems() == 0 {
				t.Errorf("%s repeated field must have a maximum", field.FullName())
			}
			if field.Kind() == protoreflect.MessageKind && (field.Message().FullName() == "google.protobuf.Any" || field.Message().FullName() == "google.protobuf.Struct" || field.Message().FullName() == "google.protobuf.Value") {
				t.Errorf("%s uses prohibited dynamic type %s", field.FullName(), field.Message().FullName())
			}
		}
	}
}

func TestCompanyTaxPreparationMessagesHaveExactFieldOrder(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/company_tax.proto")
	if err != nil {
		t.Fatalf("company tax descriptor missing: %v", err)
	}
	want := map[protoreflect.Name][]protoreflect.Name{
		"AddressInput":                      {"line_1", "line_2", "locality", "state", "postcode", "country_code"},
		"RelatedEntityTurnoverContribution": {"entity_name", "entity_abn", "amount", "evidence", "reviewed_control_or_affiliate_basis"},
		"PassiveIncomeClassificationInput":  {"income_source", "classification", "bundle_rule_id", "evidence", "reviewed_by_user_id"},
		"ApplicabilityAnswers":              {"tofa_applies", "psi_applies", "interposed_entity_election_applies", "consolidated_group_member", "research_and_development_incentive", "international_dealings", "reportable_tax_position", "life_insurance_business", "cgt_schedule_required", "losses_schedule_required", "other_schedule_required", "fb_or_unsupported_payroll_effect", "division_7a_unresolved", "unsupported_inventory", "unsupported_multicurrency", "unsupported_crypto"},
		"PriorRevenueLossInput":             {"opening_balance", "ownership_continuity_confirmed", "same_or_similar_business_judgement_required", "evidence"},
		"CompanyTaxProfileInput":            {"legal_name", "tfn", "current_postal_address", "prior_postal_address", "main_business_address", "australian_resident", "private_company", "main_business_activity_code", "main_business_activity_description", "refund_bsb", "refund_account_number", "final_return", "holding_company_kind", "immediate_holding_name", "ultimate_holding_name", "related_turnover", "passive_income_classifications", "small_business_entity_choice", "depreciation_choice", "prior_revenue_loss", "applicability"},
		"CompanyReturnInput":                {"loss_amount_to_apply", "external_summary_evidence", "payroll_summary_evidence", "review_note"},
		"MaskedCompanyTaxProfile":           {"organisation_id", "version", "legal_name", "masked_tfn", "verified_abn", "current_postal_address", "prior_postal_address", "main_business_address", "australian_resident", "private_company", "main_business_activity_code", "main_business_activity_description", "masked_refund_bsb", "masked_refund_account", "final_return", "holding_company_kind", "immediate_holding_name", "ultimate_holding_name", "related_turnover", "passive_income_classifications", "small_business_entity_choice", "depreciation_choice", "prior_revenue_loss", "applicability", "updated_by_user_id", "updated_at"},
		"TaxAdjustment":                     {"id", "return_id", "version", "type", "bundle_rule_id", "amount", "timing", "explanation", "sources", "evidence", "created_by_user_id", "reviewed_by_user_id", "updated_at"},
		"TaxElectionChoice":                 {"boolean_value", "string_value", "decimal_value"},
		"TaxElection":                       {"id", "return_id", "version", "bundle_election_id", "choice", "explanation", "evidence", "created_by_user_id", "reviewed_by_user_id", "updated_at"},
		"ReturnFactValue":                   {"string_value", "boolean_value", "integer_value", "money_value", "decimal_value", "date_value"},
		"ReturnFact":                        {"fact_id", "value", "submitted_value", "provenance", "mapping_id", "rule_id", "sources", "evidence", "validation_status"},
		"TaxReconciliationTerm":             {"stable_id", "rule_id", "amount", "sources", "evidence"},
		"TaxReconciliation":                 {"content_hash", "accounting_profit_before_tax", "additions", "subtractions", "eligible_applied_losses", "taxable_income_or_loss", "gross_tax", "payg_and_credits", "net_tax_payable_or_refund"},
		"ReturnValidationOutcome":           {"id", "validation_revision", "severity", "stable_code", "fact_ids", "sources", "safe_message", "acknowledged"},
		"ValidationAcknowledgement":         {"id", "return_id", "warning_id", "validation_revision", "actor_user_id", "fresh_factor_assertion_id", "acknowledged_at"},
		"Declaration":                       {"id", "return_id", "report_hash", "validation_revision", "acknowledgement_ids", "declaration_wording_version", "declaration_wording_hash", "terms_version", "privacy_reference_version", "actor_user_id", "fresh_factor_assertion_id", "declared_at", "supersedes_declaration_id"},
		"CompanyReturnDeliverySummary":      {"latest_attempt_id", "operation_type", "outcome", "safe_status_code", "delivered_at", "receipt_id"},
		"CompanyReturn":                     {"id", "organisation_id", "income_year", "period_start", "period_end", "relationship_kind", "root_return_id", "predecessor_return_id", "successor_return_id", "related_attempt_id", "preparation_bundle_id", "preparation_bundle_fingerprint", "source_close_id", "source_close_hash", "tax_reconciliation_hash", "state", "version", "validation_revision", "declared_snapshot_hash", "current_declaration_id", "delivery", "created_at", "updated_at"},
		"TaxAdjustmentInput":                {"adjustment_id", "type", "bundle_rule_id", "amount", "timing", "explanation", "sources", "evidence"},
		"TaxElectionInput":                  {"election_id", "bundle_election_id", "choice", "explanation", "evidence"},
		"GetCompanyTaxProfileRequest":       {"authentication", "organisation_id"}, "GetCompanyTaxProfileResponse": {"profile"},
		"SetCompanyTaxProfileRequest": {"command_context", "organisation_id", "expected_version", "input"}, "SetCompanyTaxProfileResponse": {"profile"},
		"CreateCompanyReturnRequest": {"command_context", "organisation_id", "source_close_id", "input"}, "CreateCompanyReturnResponse": {"company_return", "tax_reconciliation", "validation"},
		"GetCompanyReturnRequest": {"authentication", "organisation_id", "return_id"}, "GetCompanyReturnResponse": {"company_return", "tax_reconciliation", "validation"},
		"ListCompanyReturnFactsRequest": {"authentication", "organisation_id", "return_id", "page"}, "ListCompanyReturnFactsResponse": {"facts", "page"},
		"SetCompanyReturnInputRequest": {"command_context", "organisation_id", "return_id", "expected_version", "input"}, "SetCompanyReturnInputResponse": {"company_return", "tax_reconciliation", "validation"},
		"UpsertTaxAdjustmentRequest": {"command_context", "organisation_id", "return_id", "expected_version", "adjustment"}, "UpsertTaxAdjustmentResponse": {"company_return", "adjustment", "tax_reconciliation", "validation"},
		"RemoveTaxAdjustmentRequest": {"command_context", "organisation_id", "return_id", "expected_version", "adjustment_id"}, "RemoveTaxAdjustmentResponse": {"company_return", "tax_reconciliation", "validation"},
		"UpsertTaxElectionRequest": {"command_context", "organisation_id", "return_id", "expected_version", "election"}, "UpsertTaxElectionResponse": {"company_return", "election", "tax_reconciliation", "validation"},
		"RemoveTaxElectionRequest": {"command_context", "organisation_id", "return_id", "expected_version", "election_id"}, "RemoveTaxElectionResponse": {"company_return", "tax_reconciliation", "validation"},
		"ValidateCompanyReturnRequest": {"command_context", "organisation_id", "return_id", "expected_version"}, "ValidateCompanyReturnResponse": {"company_return", "validation"},
		"AcknowledgeReturnWarningRequest": {"command_context", "organisation_id", "return_id", "expected_version", "warning_id", "validation_revision"}, "AcknowledgeReturnWarningResponse": {"company_return", "acknowledgement", "validation"},
		"DeclareCompanyReturnRequest": {"command_context", "organisation_id", "return_id", "expected_version", "validation_revision"}, "DeclareCompanyReturnResponse": {"company_return", "declaration"},
		"WithdrawCompanyReturnDeclarationRequest": {"command_context", "organisation_id", "return_id", "expected_version", "reason"}, "WithdrawCompanyReturnDeclarationResponse": {"company_return", "retained_declaration"},
		"ExportCompanyReturnPackRequest": {"command_context", "organisation_id", "return_id", "expected_version", "kind", "export_passphrase"}, "ExportCompanyReturnPackResponse": {"export_id", "content_hash", "safe_filename", "kind"},
		"CreateCompanyReturnReplacementRequest": {"command_context", "organisation_id", "predecessor_return_id", "expected_predecessor_version", "source_close_id", "reason"}, "CreateCompanyReturnReplacementResponse": {"predecessor", "replacement"},
		"CreateCompanyReturnAmendmentRequest": {"command_context", "organisation_id", "effective_original_return_id", "latest_accepted_return_id", "expected_latest_version", "source_close_id", "reason"}, "CreateCompanyReturnAmendmentResponse": {"effective_original", "amendment"},
	}
	if file.Messages().Len() != len(want) {
		t.Fatalf("company tax message count = %d, want %d", file.Messages().Len(), len(want))
	}
	for messageName, fieldNames := range want {
		message := file.Messages().ByName(messageName)
		if message == nil {
			t.Errorf("message tammy.v1.%s missing", messageName)
			continue
		}
		if message.Fields().Len() != len(fieldNames) {
			t.Errorf("%s field count = %d, want %d", message.FullName(), message.Fields().Len(), len(fieldNames))
			continue
		}
		for index, wantName := range fieldNames {
			field := message.Fields().Get(index)
			if field.Name() != wantName || field.Number() != protoreflect.FieldNumber(index+1) {
				t.Errorf("%s field %d = %s number %d, want %s number %d", message.FullName(), index, field.Name(), field.Number(), wantName, index+1)
			}
		}
	}
}

func financialCloseScalar(name protoreflect.Name, kind protoreflect.Kind) financialCloseFieldContract {
	return financialCloseFieldContract{name: name, kind: kind}
}

func financialCloseOptional(name protoreflect.Name, kind protoreflect.Kind) financialCloseFieldContract {
	return financialCloseFieldContract{name: name, kind: kind, optional: true}
}

func financialCloseMessage(name protoreflect.Name, referencedType protoreflect.FullName, required bool) financialCloseFieldContract {
	return financialCloseFieldContract{name: name, kind: protoreflect.MessageKind, referencedType: referencedType, required: required}
}

func financialCloseRepeated(name protoreflect.Name, kind protoreflect.Kind, referencedType protoreflect.FullName) financialCloseFieldContract {
	return financialCloseFieldContract{name: name, kind: kind, referencedType: referencedType, repeated: true}
}

func financialCloseEnum(name protoreflect.Name, referencedType protoreflect.FullName) financialCloseFieldContract {
	return financialCloseFieldContract{name: name, kind: protoreflect.EnumKind, referencedType: referencedType}
}

func TestFinancialCloseContractHasExactBoundedSurface(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/financial_close.proto")
	if err != nil {
		t.Fatalf("financial close descriptor missing: %v", err)
	}

	assertFinancialCloseEnum(t, "CloseCheckSeverity", []string{
		"CLOSE_CHECK_SEVERITY_UNSPECIFIED", "CLOSE_CHECK_SEVERITY_BLOCKER", "CLOSE_CHECK_SEVERITY_WARNING",
	})
	assertFinancialCloseEnum(t, "CloseCheckResult", []string{
		"CLOSE_CHECK_RESULT_UNSPECIFIED", "CLOSE_CHECK_RESULT_FAILED", "CLOSE_CHECK_RESULT_PASSED", "CLOSE_CHECK_RESULT_RESOLVED",
	})
	assertFinancialCloseEnum(t, "FinancialStatementKind", []string{
		"FINANCIAL_STATEMENT_KIND_UNSPECIFIED", "FINANCIAL_STATEMENT_KIND_PROFIT_AND_LOSS", "FINANCIAL_STATEMENT_KIND_BALANCE_SHEET",
		"FINANCIAL_STATEMENT_KIND_CASH_FLOW", "FINANCIAL_STATEMENT_KIND_TRIAL_BALANCE", "FINANCIAL_STATEMENT_KIND_GENERAL_LEDGER",
		"FINANCIAL_STATEMENT_KIND_GST_DETAIL", "FINANCIAL_STATEMENT_KIND_FIXED_ASSET_SCHEDULE", "FINANCIAL_STATEMENT_KIND_FRANKING_RECONCILIATION",
	})

	wantMessages := map[protoreflect.Name][]financialCloseFieldContract{
		"CloseCheck": {
			financialCloseScalar("id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseScalar("rule_id", protoreflect.StringKind),
			financialCloseEnum("severity", "tammy.v1.CloseCheckSeverity"),
			financialCloseEnum("result", "tammy.v1.CloseCheckResult"),
			financialCloseScalar("source_revision", protoreflect.Uint64Kind),
			financialCloseRepeated("affected_sources", protoreflect.MessageKind, "tammy.v1.SourceRef"),
			financialCloseOptional("resolution", protoreflect.StringKind),
			financialCloseOptional("resolved_by_user_id", protoreflect.StringKind),
			financialCloseMessage("resolved_at", "google.protobuf.Timestamp", false),
		},
		"StatementHash": {
			financialCloseEnum("kind", "tammy.v1.FinancialStatementKind"),
			financialCloseScalar("content_hash", protoreflect.BytesKind),
		},
		"FinancialStatementApproval": {
			financialCloseScalar("id", protoreflect.StringKind),
			financialCloseMessage("period_start", "tammy.v1.CivilDate", true),
			financialCloseMessage("period_end", "tammy.v1.CivilDate", true),
			financialCloseScalar("financial_revision", protoreflect.Uint64Kind),
			financialCloseScalar("approval_wording_version", protoreflect.StringKind),
			financialCloseScalar("approval_wording_hash", protoreflect.BytesKind),
			financialCloseRepeated("statement_hashes", protoreflect.MessageKind, "tammy.v1.StatementHash"),
			financialCloseScalar("approved_by_user_id", protoreflect.StringKind),
			financialCloseScalar("fresh_factor_assertion_id", protoreflect.StringKind),
			financialCloseMessage("approved_at", "google.protobuf.Timestamp", true),
		},
		"SourceRevision": {
			financialCloseScalar("owner", protoreflect.StringKind),
			financialCloseScalar("revision", protoreflect.Uint64Kind),
			financialCloseScalar("content_hash", protoreflect.BytesKind),
		},
		"FinancialCloseSnapshot": {
			financialCloseScalar("id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("verified_abn", protoreflect.StringKind),
			financialCloseScalar("income_year", protoreflect.Int32Kind),
			financialCloseMessage("period_start", "tammy.v1.CivilDate", true),
			financialCloseMessage("period_end", "tammy.v1.CivilDate", true),
			financialCloseScalar("currency", protoreflect.StringKind),
			financialCloseScalar("snapshot_hash", protoreflect.BytesKind),
			financialCloseScalar("financial_revision", protoreflect.Uint64Kind),
			financialCloseRepeated("subledger_revisions", protoreflect.MessageKind, "tammy.v1.SourceRevision"),
			financialCloseRepeated("statement_hashes", protoreflect.MessageKind, "tammy.v1.StatementHash"),
			financialCloseScalar("trial_balance_hash", protoreflect.BytesKind),
			financialCloseScalar("checklist_hash", protoreflect.BytesKind),
			financialCloseScalar("reconciliation_hash", protoreflect.BytesKind),
			financialCloseScalar("accounting_rule_fingerprint", protoreflect.BytesKind),
			financialCloseScalar("gst_rule_fingerprint", protoreflect.BytesKind),
			financialCloseScalar("asset_rule_fingerprint", protoreflect.BytesKind),
			financialCloseScalar("evidence_manifest_hash", protoreflect.BytesKind),
			financialCloseScalar("audit_head_hash", protoreflect.BytesKind),
			financialCloseMessage("approval", "tammy.v1.FinancialStatementApproval", true),
			financialCloseOptional("corrects_close_id", protoreflect.StringKind),
			financialCloseMessage("frozen_at", "google.protobuf.Timestamp", true),
		},
		"FinancialClose": {
			financialCloseScalar("id", protoreflect.StringKind),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("income_year", protoreflect.Int32Kind),
			financialCloseMessage("period_start", "tammy.v1.CivilDate", true),
			financialCloseMessage("period_end", "tammy.v1.CivilDate", true),
			financialCloseScalar("currency", protoreflect.StringKind),
			financialCloseScalar("version", protoreflect.Uint64Kind),
			financialCloseEnum("state", "tammy.v1.FinancialCloseState"),
			financialCloseScalar("financial_revision", protoreflect.Uint64Kind),
			financialCloseMessage("latest_frozen_snapshot", "tammy.v1.FinancialCloseSnapshot", false),
			financialCloseMessage("created_at", "google.protobuf.Timestamp", true),
			financialCloseMessage("updated_at", "google.protobuf.Timestamp", true),
		},
		"FinancialStatementLine": {
			financialCloseScalar("stable_code", protoreflect.StringKind),
			financialCloseScalar("label", protoreflect.StringKind),
			financialCloseMessage("amount", "tammy.v1.Money", true),
			financialCloseRepeated("sources", protoreflect.MessageKind, "tammy.v1.SourceRef"),
		},
		"FinancialStatement": {
			financialCloseEnum("kind", "tammy.v1.FinancialStatementKind"),
			financialCloseScalar("content_hash", protoreflect.BytesKind),
			financialCloseRepeated("lines", protoreflect.MessageKind, "tammy.v1.FinancialStatementLine"),
		},
		"FinancialStatements": {
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseScalar("snapshot_id", protoreflect.StringKind),
			financialCloseScalar("financial_revision", protoreflect.Uint64Kind),
			financialCloseRepeated("statements", protoreflect.MessageKind, "tammy.v1.FinancialStatement"),
		},
		"CreateFinancialCloseRequest": {
			financialCloseMessage("command_context", "tammy.v1.CommandContext", true),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("income_year", protoreflect.Int32Kind),
			financialCloseMessage("period_start", "tammy.v1.CivilDate", true),
			financialCloseMessage("period_end", "tammy.v1.CivilDate", true),
		},
		"CreateFinancialCloseResponse": {financialCloseMessage("close", "tammy.v1.FinancialClose", true)},
		"GetFinancialCloseRequest": {
			financialCloseMessage("authentication", "tammy.v1.AuthenticationContext", true),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
		},
		"GetFinancialCloseResponse": {financialCloseMessage("close", "tammy.v1.FinancialClose", true)},
		"ListCloseChecksRequest": {
			financialCloseMessage("authentication", "tammy.v1.AuthenticationContext", true),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseMessage("page", "tammy.v1.PageRequest", true),
		},
		"ListCloseChecksResponse": {
			financialCloseRepeated("checks", protoreflect.MessageKind, "tammy.v1.CloseCheck"),
			financialCloseMessage("page", "tammy.v1.PageInfo", true),
		},
		"ResolveCloseWarningRequest": {
			financialCloseMessage("command_context", "tammy.v1.CommandContext", true),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseScalar("expected_version", protoreflect.Uint64Kind),
			financialCloseScalar("check_id", protoreflect.StringKind),
			financialCloseScalar("resolution", protoreflect.StringKind),
		},
		"ResolveCloseWarningResponse": {
			financialCloseMessage("close", "tammy.v1.FinancialClose", true),
			financialCloseMessage("check", "tammy.v1.CloseCheck", true),
		},
		"FreezeFinancialCloseRequest": {
			financialCloseMessage("command_context", "tammy.v1.CommandContext", true),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseScalar("expected_version", protoreflect.Uint64Kind),
		},
		"FreezeFinancialCloseResponse": {
			financialCloseMessage("close", "tammy.v1.FinancialClose", true),
			financialCloseMessage("snapshot", "tammy.v1.FinancialCloseSnapshot", true),
		},
		"ReopenFinancialCloseRequest": {
			financialCloseMessage("command_context", "tammy.v1.CommandContext", true),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseScalar("expected_version", protoreflect.Uint64Kind),
			financialCloseScalar("reason", protoreflect.StringKind),
		},
		"ReopenFinancialCloseResponse": {
			financialCloseMessage("close", "tammy.v1.FinancialClose", true),
			financialCloseScalar("preserved_snapshot_id", protoreflect.StringKind),
		},
		"StartFinancialCloseCorrectionRequest": {
			financialCloseMessage("command_context", "tammy.v1.CommandContext", true),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseScalar("expected_version", protoreflect.Uint64Kind),
			financialCloseScalar("reason", protoreflect.StringKind),
		},
		"StartFinancialCloseCorrectionResponse": {
			financialCloseMessage("original_close", "tammy.v1.FinancialClose", true),
			financialCloseMessage("correction_close", "tammy.v1.FinancialClose", true),
		},
		"GetFinancialStatementsRequest": {
			financialCloseMessage("authentication", "tammy.v1.AuthenticationContext", true),
			financialCloseScalar("organisation_id", protoreflect.StringKind),
			financialCloseScalar("close_id", protoreflect.StringKind),
			financialCloseScalar("snapshot_id", protoreflect.StringKind),
		},
		"GetFinancialStatementsResponse": {financialCloseMessage("statements", "tammy.v1.FinancialStatements", true)},
	}
	assertExactFinancialCloseMessages(t, file, wantMessages)
	assertFinancialCloseFieldRules(t, file)
	assertFinancialCloseService(t, file)
}

func assertFinancialCloseEnum(t *testing.T, name protoreflect.Name, values []string) {
	t.Helper()
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1." + protoreflect.FullName(name))
	if err != nil {
		t.Errorf("enum tammy.v1.%s missing: %v", name, err)
		return
	}
	enum := descriptor.(protoreflect.EnumDescriptor)
	if enum.Values().Len() != len(values) {
		t.Errorf("%s value count = %d, want %d", enum.FullName(), enum.Values().Len(), len(values))
		return
	}
	for index, want := range values {
		if got := string(enum.Values().Get(index).Name()); got != want {
			t.Errorf("%s value %d = %q, want %q", enum.FullName(), index, got, want)
		}
	}
}

func assertExactFinancialCloseMessages(t *testing.T, file protoreflect.FileDescriptor, want map[protoreflect.Name][]financialCloseFieldContract) {
	t.Helper()
	if file.Messages().Len() != len(want) {
		t.Errorf("financial close message count = %d, want %d", file.Messages().Len(), len(want))
	}
	for name, fields := range want {
		message := file.Messages().ByName(name)
		if message == nil {
			t.Errorf("message tammy.v1.%s missing", name)
			continue
		}
		if message.Fields().Len() != len(fields) {
			t.Errorf("%s field count = %d, want %d", message.FullName(), message.Fields().Len(), len(fields))
			continue
		}
		for index, expected := range fields {
			field := message.Fields().Get(index)
			var referencedType protoreflect.FullName
			switch field.Kind() {
			case protoreflect.MessageKind:
				referencedType = field.Message().FullName()
			case protoreflect.EnumKind:
				referencedType = field.Enum().FullName()
			}
			got := financialCloseFieldContract{
				name: field.Name(), kind: field.Kind(), referencedType: referencedType, repeated: field.IsList(),
				optional: field.Kind() != protoreflect.MessageKind && field.HasPresence(), required: sbrValidationRules(field).GetRequired(),
			}
			if field.Number() != protoreflect.FieldNumber(index+1) || got != expected {
				t.Errorf("%s field %d = %+v number %d, want %+v number %d", message.FullName(), index, got, field.Number(), expected, index+1)
			}
			if field.IsMap() {
				t.Errorf("%s must not be a map", field.FullName())
			}
			if field.Kind() == protoreflect.MessageKind {
				for _, prohibited := range []protoreflect.FullName{"google.protobuf.Any", "google.protobuf.Struct", "google.protobuf.Value"} {
					if field.Message().FullName() == prohibited {
						t.Errorf("%s uses prohibited dynamic type %s", field.FullName(), prohibited)
					}
				}
			}
		}
	}
}

func assertFinancialCloseFieldRules(t *testing.T, file protoreflect.FileDescriptor) {
	t.Helper()
	uuidPattern := "^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"
	stableCodePattern := "^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$"
	uuidFields := map[protoreflect.Name][]protoreflect.Name{
		"CloseCheck":                           {"id", "close_id", "resolved_by_user_id"},
		"FinancialStatementApproval":           {"id", "approved_by_user_id", "fresh_factor_assertion_id"},
		"FinancialCloseSnapshot":               {"id", "close_id", "organisation_id", "corrects_close_id"},
		"FinancialClose":                       {"id", "organisation_id"},
		"FinancialStatements":                  {"close_id", "snapshot_id"},
		"CreateFinancialCloseRequest":          {"organisation_id"},
		"GetFinancialCloseRequest":             {"organisation_id", "close_id"},
		"ListCloseChecksRequest":               {"organisation_id", "close_id"},
		"ResolveCloseWarningRequest":           {"organisation_id", "close_id", "check_id"},
		"FreezeFinancialCloseRequest":          {"organisation_id", "close_id"},
		"ReopenFinancialCloseRequest":          {"organisation_id", "close_id"},
		"ReopenFinancialCloseResponse":         {"preserved_snapshot_id"},
		"StartFinancialCloseCorrectionRequest": {"organisation_id", "close_id"},
		"GetFinancialStatementsRequest":        {"organisation_id", "close_id", "snapshot_id"},
	}
	for messageName, fieldNames := range uuidFields {
		for _, fieldName := range fieldNames {
			if got := fieldRules(t, file.Messages().ByName(messageName).Fields().ByName(fieldName)).GetString_().GetPattern(); got != uuidPattern {
				t.Errorf("%s.%s UUIDv7 pattern = %q", messageName, fieldName, got)
			}
		}
	}
	if got := fieldRules(t, file.Messages().ByName("CloseCheck").Fields().ByName("rule_id")).GetString_().GetPattern(); got != stableCodePattern {
		t.Errorf("CloseCheck.rule_id pattern = %q, want %q", got, stableCodePattern)
	}

	for _, owner := range []struct{ message, field protoreflect.Name }{
		{"StatementHash", "content_hash"}, {"FinancialStatementApproval", "approval_wording_hash"},
		{"SourceRevision", "content_hash"}, {"FinancialCloseSnapshot", "snapshot_hash"},
		{"FinancialCloseSnapshot", "trial_balance_hash"}, {"FinancialCloseSnapshot", "checklist_hash"},
		{"FinancialCloseSnapshot", "reconciliation_hash"}, {"FinancialCloseSnapshot", "accounting_rule_fingerprint"},
		{"FinancialCloseSnapshot", "gst_rule_fingerprint"}, {"FinancialCloseSnapshot", "asset_rule_fingerprint"},
		{"FinancialCloseSnapshot", "evidence_manifest_hash"}, {"FinancialCloseSnapshot", "audit_head_hash"},
		{"FinancialStatement", "content_hash"},
	} {
		if got := fieldRules(t, file.Messages().ByName(owner.message).Fields().ByName(owner.field)).GetBytes().GetLen(); got != 32 {
			t.Errorf("%s.%s byte length = %d, want 32", owner.message, owner.field, got)
		}
	}

	for _, owner := range []struct{ message, field protoreflect.Name }{
		{"CloseCheck", "source_revision"}, {"FinancialStatementApproval", "financial_revision"}, {"SourceRevision", "revision"},
		{"FinancialCloseSnapshot", "financial_revision"}, {"FinancialClose", "version"}, {"FinancialClose", "financial_revision"},
		{"FinancialStatements", "financial_revision"}, {"ResolveCloseWarningRequest", "expected_version"},
		{"FreezeFinancialCloseRequest", "expected_version"}, {"ReopenFinancialCloseRequest", "expected_version"},
		{"StartFinancialCloseCorrectionRequest", "expected_version"},
	} {
		if got := fieldRules(t, file.Messages().ByName(owner.message).Fields().ByName(owner.field)).GetUint64().GetGte(); got != 1 {
			t.Errorf("%s.%s minimum = %d, want 1", owner.message, owner.field, got)
		}
	}
	for _, messageName := range []protoreflect.Name{"FinancialCloseSnapshot", "FinancialClose", "CreateFinancialCloseRequest"} {
		if got := fieldRules(t, file.Messages().ByName(messageName).Fields().ByName("income_year")).GetInt32().GetConst(); got != 2026 {
			t.Errorf("%s.income_year const = %d, want 2026", messageName, got)
		}
	}
	for _, messageName := range []protoreflect.Name{"FinancialCloseSnapshot", "FinancialClose"} {
		if got := fieldRules(t, file.Messages().ByName(messageName).Fields().ByName("currency")).GetString_().GetConst(); got != "AUD" {
			t.Errorf("%s.currency const = %q, want AUD", messageName, got)
		}
	}
	if got := fieldRules(t, file.Messages().ByName("FinancialCloseSnapshot").Fields().ByName("verified_abn")).GetString_().GetPattern(); got != "^[0-9]{11}$" {
		t.Errorf("FinancialCloseSnapshot.verified_abn pattern = %q", got)
	}

	stringBounds := []struct {
		message, field protoreflect.Name
		min, max       uint64
	}{
		{"CloseCheck", "rule_id", 1, 128}, {"CloseCheck", "resolution", 1, 2000},
		{"FinancialStatementApproval", "approval_wording_version", 1, 128}, {"SourceRevision", "owner", 1, 64},
		{"FinancialStatementLine", "stable_code", 1, 128}, {"FinancialStatementLine", "label", 1, 256},
		{"ResolveCloseWarningRequest", "resolution", 1, 2000}, {"ReopenFinancialCloseRequest", "reason", 1, 2000},
		{"StartFinancialCloseCorrectionRequest", "reason", 1, 2000},
	}
	for _, bound := range stringBounds {
		rules := fieldRules(t, file.Messages().ByName(bound.message).Fields().ByName(bound.field)).GetString_()
		if rules.GetMinLen() != bound.min || rules.GetMaxLen() != bound.max {
			t.Errorf("%s.%s bounds = %d..%d, want %d..%d", bound.message, bound.field, rules.GetMinLen(), rules.GetMaxLen(), bound.min, bound.max)
		}
	}

	repeatedBounds := []struct {
		message, field protoreflect.Name
		min, max       uint64
	}{
		{"CloseCheck", "affected_sources", 0, 100}, {"FinancialStatementApproval", "statement_hashes", 1, 16},
		{"FinancialCloseSnapshot", "subledger_revisions", 0, 32}, {"FinancialCloseSnapshot", "statement_hashes", 4, 16},
		{"FinancialStatementLine", "sources", 0, 100}, {"FinancialStatement", "lines", 0, 2000},
		{"FinancialStatements", "statements", 4, 8}, {"ListCloseChecksResponse", "checks", 0, 200},
	}
	for _, bound := range repeatedBounds {
		rules := fieldRules(t, file.Messages().ByName(bound.message).Fields().ByName(bound.field)).GetRepeated()
		if rules.GetMinItems() != bound.min || rules.GetMaxItems() != bound.max {
			t.Errorf("%s.%s item bounds = %d..%d, want %d..%d", bound.message, bound.field, rules.GetMinItems(), rules.GetMaxItems(), bound.min, bound.max)
		}
	}

	for messageIndex := 0; messageIndex < file.Messages().Len(); messageIndex++ {
		message := file.Messages().Get(messageIndex)
		for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
			field := message.Fields().Get(fieldIndex)
			if field.Kind() == protoreflect.EnumKind {
				rules := fieldRules(t, field).GetEnum()
				if !rules.GetDefinedOnly() || fmt.Sprint(rules.GetNotIn()) != "[0]" {
					t.Errorf("%s must be defined_only and reject zero", field.FullName())
				}
			}
			lower := strings.ToLower(string(field.Name()))
			for _, prohibited := range []string{"payload", "secret", "credential", "path"} {
				if strings.Contains(lower, prohibited) {
					t.Errorf("%s exposes prohibited %s field", field.FullName(), prohibited)
				}
			}
		}
	}
}

func assertFinancialCloseService(t *testing.T, file protoreflect.FileDescriptor) {
	t.Helper()
	if file.Services().Len() != 1 {
		t.Errorf("financial close service count = %d, want 1", file.Services().Len())
		return
	}
	service := file.Services().ByName("FinancialCloseService")
	if service == nil {
		t.Error("tammy.v1.FinancialCloseService missing")
		return
	}
	want := []struct {
		name, input, output protoreflect.FullName
	}{
		{"CreateFinancialClose", "tammy.v1.CreateFinancialCloseRequest", "tammy.v1.CreateFinancialCloseResponse"},
		{"GetFinancialClose", "tammy.v1.GetFinancialCloseRequest", "tammy.v1.GetFinancialCloseResponse"},
		{"ListCloseChecks", "tammy.v1.ListCloseChecksRequest", "tammy.v1.ListCloseChecksResponse"},
		{"ResolveCloseWarning", "tammy.v1.ResolveCloseWarningRequest", "tammy.v1.ResolveCloseWarningResponse"},
		{"FreezeFinancialClose", "tammy.v1.FreezeFinancialCloseRequest", "tammy.v1.FreezeFinancialCloseResponse"},
		{"ReopenFinancialClose", "tammy.v1.ReopenFinancialCloseRequest", "tammy.v1.ReopenFinancialCloseResponse"},
		{"StartFinancialCloseCorrection", "tammy.v1.StartFinancialCloseCorrectionRequest", "tammy.v1.StartFinancialCloseCorrectionResponse"},
		{"GetFinancialStatements", "tammy.v1.GetFinancialStatementsRequest", "tammy.v1.GetFinancialStatementsResponse"},
	}
	if service.Methods().Len() != len(want) {
		t.Fatalf("FinancialCloseService method count = %d, want %d", service.Methods().Len(), len(want))
	}
	for index, expected := range want {
		method := service.Methods().Get(index)
		if method.FullName() != "tammy.v1.FinancialCloseService."+expected.name || method.Input().FullName() != expected.input || method.Output().FullName() != expected.output {
			t.Errorf("FinancialCloseService method %d = %s(%s) returns %s", index, method.FullName(), method.Input().FullName(), method.Output().FullName())
		}
		if method.IsStreamingClient() || method.IsStreamingServer() {
			t.Errorf("%s must be unary", method.FullName())
		}
	}
}

func TestFinancialCloseProtovalidateEnforcesExactIncomeYearPeriodAndAUD(t *testing.T) {
	validMessages := []proto.Message{
		validFinancialCloseCreateRequest(),
		validFinancialClose(tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING),
		validFinancialCloseSnapshot(),
		validFinancialStatementApproval(),
		&tammyv1.FinancialStatementLine{
			StableCode: "revenue", Label: "Revenue", Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 100},
		},
	}
	for _, message := range validMessages {
		if err := protovalidate.Validate(message); err != nil {
			t.Fatalf("valid %T rejected: %v", message, err)
		}
	}

	wrongYear := validFinancialCloseCreateRequest()
	wrongYear.IncomeYear = 2025
	assertFinancialCloseValidationRejects(t, "wrong income year", wrongYear)

	wrongCreatePeriod := validFinancialCloseCreateRequest()
	wrongCreatePeriod.PeriodStart = financialCloseDate(2025, 7, 2)
	assertFinancialCloseValidationRejects(t, "wrong create period", wrongCreatePeriod)

	wrongClosePeriod := validFinancialClose(tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING)
	wrongClosePeriod.PeriodEnd = financialCloseDate(2026, 6, 29)
	assertFinancialCloseValidationRejects(t, "wrong close period", wrongClosePeriod)

	wrongSnapshotPeriod := validFinancialCloseSnapshot()
	wrongSnapshotPeriod.PeriodStart = financialCloseDate(2024, 7, 1)
	assertFinancialCloseValidationRejects(t, "wrong snapshot period", wrongSnapshotPeriod)

	wrongApprovalPeriod := validFinancialStatementApproval()
	wrongApprovalPeriod.PeriodEnd = financialCloseDate(2026, 6, 1)
	assertFinancialCloseValidationRejects(t, "wrong approval period", wrongApprovalPeriod)

	nonAUDClose := validFinancialClose(tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING)
	nonAUDClose.Currency = "USD"
	assertFinancialCloseValidationRejects(t, "non-AUD close currency", nonAUDClose)

	nonAUDLine := &tammyv1.FinancialStatementLine{
		StableCode: "revenue", Label: "Revenue", Amount: &tammyv1.Money{CurrencyCode: "USD", MinorUnits: 100},
	}
	assertFinancialCloseValidationRejects(t, "non-AUD statement amount", nonAUDLine)
}

func TestFinancialCloseProtovalidateEnforcesFrozenSnapshotRevisionInvariant(t *testing.T) {
	frozen := validFinancialClose(tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_FROZEN)
	if err := protovalidate.Validate(frozen); err != nil {
		t.Fatalf("valid frozen close rejected: %v", err)
	}

	reopened := proto.Clone(frozen).(*tammyv1.FinancialClose)
	reopened.State = tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING
	reopened.FinancialRevision = frozen.FinancialRevision + 1
	if err := protovalidate.Validate(reopened); err != nil {
		t.Fatalf("valid reopened close retaining its snapshot rejected: %v", err)
	}

	missingSnapshot := validFinancialClose(tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING)
	missingSnapshot.State = tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_FROZEN
	assertFinancialCloseValidationRejects(t, "frozen close without snapshot", missingSnapshot)

	mismatchedSnapshotRevision := proto.Clone(frozen).(*tammyv1.FinancialClose)
	mismatchedSnapshotRevision.LatestFrozenSnapshot.FinancialRevision++
	assertFinancialCloseValidationRejects(t, "frozen close with mismatched snapshot revision", mismatchedSnapshotRevision)

	mismatchedApprovalRevision := proto.Clone(frozen).(*tammyv1.FinancialClose)
	mismatchedApprovalRevision.LatestFrozenSnapshot.Approval.FinancialRevision++
	assertFinancialCloseValidationRejects(t, "frozen close with mismatched approval revision", mismatchedApprovalRevision)
}

func TestFinancialCloseProtovalidateEnforcesCloseCheckResolutionTuple(t *testing.T) {
	resolved := validFinancialCloseCheck(tammyv1.CloseCheckResult_CLOSE_CHECK_RESULT_RESOLVED)
	resolution := "Reviewed against retained evidence"
	resolved.Resolution = &resolution
	resolvedBy := financialCloseID()
	resolved.ResolvedByUserId = &resolvedBy
	resolved.ResolvedAt = financialCloseTimestamp()
	if err := protovalidate.Validate(resolved); err != nil {
		t.Fatalf("valid resolved close check rejected: %v", err)
	}

	for name, mutate := range map[string]func(*tammyv1.CloseCheck){
		"resolution":          func(check *tammyv1.CloseCheck) { check.Resolution = nil },
		"resolved_by_user_id": func(check *tammyv1.CloseCheck) { check.ResolvedByUserId = nil },
		"resolved_at":         func(check *tammyv1.CloseCheck) { check.ResolvedAt = nil },
	} {
		t.Run("resolved missing "+name, func(t *testing.T) {
			check := proto.Clone(resolved).(*tammyv1.CloseCheck)
			mutate(check)
			assertFinancialCloseValidationRejects(t, "incomplete resolved tuple", check)
		})
	}

	for _, result := range []tammyv1.CloseCheckResult{
		tammyv1.CloseCheckResult_CLOSE_CHECK_RESULT_FAILED,
		tammyv1.CloseCheckResult_CLOSE_CHECK_RESULT_PASSED,
	} {
		clean := validFinancialCloseCheck(result)
		if err := protovalidate.Validate(clean); err != nil {
			t.Fatalf("valid %s close check rejected: %v", result, err)
		}
		for name, mutate := range map[string]func(*tammyv1.CloseCheck){
			"resolution": func(check *tammyv1.CloseCheck) {
				value := "not permitted"
				check.Resolution = &value
			},
			"resolved_by_user_id": func(check *tammyv1.CloseCheck) {
				value := financialCloseID()
				check.ResolvedByUserId = &value
			},
			"resolved_at": func(check *tammyv1.CloseCheck) { check.ResolvedAt = financialCloseTimestamp() },
		} {
			t.Run(result.String()+" carrying "+name, func(t *testing.T) {
				check := proto.Clone(clean).(*tammyv1.CloseCheck)
				mutate(check)
				assertFinancialCloseValidationRejects(t, "non-resolved check carrying resolution metadata", check)
			})
		}
	}
}

func TestFinancialCloseProtovalidateRejectsInvalidRuleIdentifiers(t *testing.T) {
	for _, ruleID := range []string{"A", "trial_balance:balanced/v1-2._"} {
		check := validFinancialCloseCheck(tammyv1.CloseCheckResult_CLOSE_CHECK_RESULT_FAILED)
		check.RuleId = ruleID
		if err := protovalidate.Validate(check); err != nil {
			t.Fatalf("valid rule identifier %q rejected: %v", ruleID, err)
		}
	}

	for _, ruleID := range []string{"trial balance", "réconciliation", ".trial_balance"} {
		t.Run(ruleID, func(t *testing.T) {
			check := validFinancialCloseCheck(tammyv1.CloseCheckResult_CLOSE_CHECK_RESULT_FAILED)
			check.RuleId = ruleID
			assertFinancialCloseValidationRejects(t, "invalid rule identifier", check)
		})
	}
}

func TestFinancialCloseProtovalidateRequiresOperationSpecificFreshFactor(t *testing.T) {
	tests := []struct {
		name    string
		purpose string
		build   func(*tammyv1.CommandContext) proto.Message
	}{
		{
			name: "freeze", purpose: "financial_close_freeze",
			build: func(context *tammyv1.CommandContext) proto.Message {
				return &tammyv1.FreezeFinancialCloseRequest{CommandContext: context, OrganisationId: financialCloseID(), CloseId: financialCloseID(), ExpectedVersion: 1}
			},
		},
		{
			name: "reopen", purpose: "financial_close_reopen",
			build: func(context *tammyv1.CommandContext) proto.Message {
				return &tammyv1.ReopenFinancialCloseRequest{CommandContext: context, OrganisationId: financialCloseID(), CloseId: financialCloseID(), ExpectedVersion: 1, Reason: "Source correction"}
			},
		},
		{
			name: "start correction", purpose: "financial_close_start_correction",
			build: func(context *tammyv1.CommandContext) proto.Message {
				return &tammyv1.StartFinancialCloseCorrectionRequest{CommandContext: context, OrganisationId: financialCloseID(), CloseId: financialCloseID(), ExpectedVersion: 1, Reason: "Prior-year correction"}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			valid := test.build(validFinancialCloseCommandContext(test.purpose))
			if err := protovalidate.Validate(valid); err != nil {
				t.Fatalf("valid request rejected: %v", err)
			}
			missing := test.build(validFinancialCloseCommandContext(""))
			assertFinancialCloseValidationRejects(t, "missing fresh factor", missing)
			wrongPurpose := test.build(validFinancialCloseCommandContext("another_financial_close_operation"))
			assertFinancialCloseValidationRejects(t, "wrong fresh-factor purpose", wrongPurpose)
		})
	}
}

func assertFinancialCloseValidationRejects(t *testing.T, name string, message proto.Message) {
	t.Helper()
	if err := protovalidate.Validate(message); err == nil {
		t.Fatalf("%s passed runtime validation", name)
	}
}

func validFinancialCloseCreateRequest() *tammyv1.CreateFinancialCloseRequest {
	return &tammyv1.CreateFinancialCloseRequest{
		CommandContext: validFinancialCloseCommandContext(""), OrganisationId: financialCloseID(), IncomeYear: 2026,
		PeriodStart: financialCloseDate(2025, 7, 1), PeriodEnd: financialCloseDate(2026, 6, 30),
	}
}

func validFinancialClose(state tammyv1.FinancialCloseState) *tammyv1.FinancialClose {
	close := &tammyv1.FinancialClose{
		Id: financialCloseID(), OrganisationId: financialCloseID(), IncomeYear: 2026,
		PeriodStart: financialCloseDate(2025, 7, 1), PeriodEnd: financialCloseDate(2026, 6, 30),
		Currency: "AUD", Version: 1, State: state, FinancialRevision: 1,
		CreatedAt: financialCloseTimestamp(), UpdatedAt: financialCloseTimestamp(),
	}
	if state == tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_FROZEN {
		close.LatestFrozenSnapshot = validFinancialCloseSnapshot()
	}
	return close
}

func validFinancialCloseSnapshot() *tammyv1.FinancialCloseSnapshot {
	statementHashes := []*tammyv1.StatementHash{
		{Kind: tammyv1.FinancialStatementKind_FINANCIAL_STATEMENT_KIND_PROFIT_AND_LOSS, ContentHash: financialCloseHash()},
		{Kind: tammyv1.FinancialStatementKind_FINANCIAL_STATEMENT_KIND_BALANCE_SHEET, ContentHash: financialCloseHash()},
		{Kind: tammyv1.FinancialStatementKind_FINANCIAL_STATEMENT_KIND_CASH_FLOW, ContentHash: financialCloseHash()},
		{Kind: tammyv1.FinancialStatementKind_FINANCIAL_STATEMENT_KIND_TRIAL_BALANCE, ContentHash: financialCloseHash()},
	}
	approval := validFinancialStatementApproval()
	approval.StatementHashes = proto.Clone(&tammyv1.FinancialCloseSnapshot{StatementHashes: statementHashes}).(*tammyv1.FinancialCloseSnapshot).StatementHashes
	return &tammyv1.FinancialCloseSnapshot{
		Id: financialCloseID(), CloseId: financialCloseID(), OrganisationId: financialCloseID(), VerifiedAbn: "51824753556",
		IncomeYear: 2026, PeriodStart: financialCloseDate(2025, 7, 1), PeriodEnd: financialCloseDate(2026, 6, 30), Currency: "AUD",
		SnapshotHash: financialCloseHash(), FinancialRevision: 1, StatementHashes: statementHashes,
		TrialBalanceHash: financialCloseHash(), ChecklistHash: financialCloseHash(), ReconciliationHash: financialCloseHash(),
		AccountingRuleFingerprint: financialCloseHash(), GstRuleFingerprint: financialCloseHash(), AssetRuleFingerprint: financialCloseHash(),
		EvidenceManifestHash: financialCloseHash(), AuditHeadHash: financialCloseHash(), Approval: approval, FrozenAt: financialCloseTimestamp(),
	}
}

func validFinancialStatementApproval() *tammyv1.FinancialStatementApproval {
	return &tammyv1.FinancialStatementApproval{
		Id: financialCloseID(), PeriodStart: financialCloseDate(2025, 7, 1), PeriodEnd: financialCloseDate(2026, 6, 30),
		FinancialRevision: 1, ApprovalWordingVersion: "company-close-approval-v1", ApprovalWordingHash: financialCloseHash(),
		StatementHashes:  []*tammyv1.StatementHash{{Kind: tammyv1.FinancialStatementKind_FINANCIAL_STATEMENT_KIND_PROFIT_AND_LOSS, ContentHash: financialCloseHash()}},
		ApprovedByUserId: financialCloseID(), FreshFactorAssertionId: financialCloseID(), ApprovedAt: financialCloseTimestamp(),
	}
}

func validFinancialCloseCheck(result tammyv1.CloseCheckResult) *tammyv1.CloseCheck {
	return &tammyv1.CloseCheck{
		Id: financialCloseID(), CloseId: financialCloseID(), RuleId: "trial_balance_balanced",
		Severity: tammyv1.CloseCheckSeverity_CLOSE_CHECK_SEVERITY_WARNING, Result: result, SourceRevision: 1,
	}
}

func validFinancialCloseCommandContext(purpose string) *tammyv1.CommandContext {
	context := &tammyv1.CommandContext{
		IdempotencyKey: financialCloseID(),
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: financialCloseID(), SessionId: financialCloseID()},
	}
	if purpose != "" {
		context.FreshFactor = &tammyv1.FreshFactorContext{AssertionId: financialCloseID(), Purpose: purpose, AssertedAt: financialCloseTimestamp()}
	}
	return context
}

func financialCloseID() string {
	return "01890f1e-7c40-7cc0-8ef9-5d7707d34123"
}

func financialCloseDate(year, month, day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: year, Month: month, Day: day}
}

func financialCloseHash() []byte {
	return bytes.Repeat([]byte{0x42}, 32)
}

func financialCloseTimestamp() *timestamppb.Timestamp {
	return timestamppb.New(time.Unix(1, 0))
}

func TestCompanyTaxProtovalidateEnforcesSupportedProfileAndAmounts(t *testing.T) {
	profile := validCompanyTaxProfileInput()
	if err := protovalidate.Validate(profile); err != nil {
		t.Fatalf("valid profile without prior loss rejected: %v", err)
	}

	missingAnswer := proto.Clone(profile).(*tammyv1.CompanyTaxProfileInput)
	missingAnswer.AustralianResident = tammyv1.RequiredAnswer_REQUIRED_ANSWER_UNSPECIFIED
	assertFinancialCloseValidationRejects(t, "omitted mandatory answer", missingAnswer)

	emptyAddress := proto.Clone(profile).(*tammyv1.CompanyTaxProfileInput)
	emptyAddress.CurrentPostalAddress.Line_1 = ""
	assertFinancialCloseValidationRejects(t, "empty address identity", emptyAddress)

	withTurnover := proto.Clone(profile).(*tammyv1.CompanyTaxProfileInput)
	withTurnover.RelatedTurnover = []*tammyv1.RelatedEntityTurnoverContribution{{
		EntityName: "Related Pty Ltd", EntityAbn: "51824753556", Amount: companyTaxMoney("USD", 100),
		Evidence: []*tammyv1.SourceRef{companyTaxSource()}, ReviewedControlOrAffiliateBasis: "Reviewed control relationship",
	}}
	assertFinancialCloseValidationRejects(t, "non-AUD profile amount", withTurnover)

	for name, mutate := range map[string]func(*tammyv1.PriorRevenueLossInput){
		"zero balance":     func(loss *tammyv1.PriorRevenueLossInput) { loss.OpeningBalance.MinorUnits = 0 },
		"missing evidence": func(loss *tammyv1.PriorRevenueLossInput) { loss.Evidence = nil },
		"unknown continuity": func(loss *tammyv1.PriorRevenueLossInput) {
			loss.OwnershipContinuityConfirmed = tammyv1.RequiredAnswer_REQUIRED_ANSWER_UNSPECIFIED
		},
	} {
		t.Run(name, func(t *testing.T) {
			loss := validPriorRevenueLossInput()
			mutate(loss)
			assertFinancialCloseValidationRejects(t, name, loss)
		})
	}
	if err := protovalidate.Validate(validPriorRevenueLossInput()); err != nil {
		t.Fatalf("valid prior loss rejected: %v", err)
	}
}

func TestCompanyTaxProtovalidateEnforces2026ReturnBundleAndAUD(t *testing.T) {
	companyReturn := validCompanyReturn()
	if err := protovalidate.Validate(companyReturn); err != nil {
		t.Fatalf("valid company return rejected: %v", err)
	}

	wrongYear := proto.Clone(companyReturn).(*tammyv1.CompanyReturn)
	wrongYear.IncomeYear = 2025
	assertFinancialCloseValidationRejects(t, "wrong return year", wrongYear)
	wrongPeriod := proto.Clone(companyReturn).(*tammyv1.CompanyReturn)
	wrongPeriod.PeriodStart.Day = 2
	assertFinancialCloseValidationRejects(t, "wrong return period", wrongPeriod)
	wrongBundle := proto.Clone(companyReturn).(*tammyv1.CompanyReturn)
	wrongBundle.PreparationBundleId = "different-bundle"
	assertFinancialCloseValidationRejects(t, "wrong preparation bundle", wrongBundle)

	adjustment := &tammyv1.TaxAdjustmentInput{
		Type:         tammyv1.TaxAdjustmentType_TAX_ADJUSTMENT_TYPE_NON_DEDUCTIBLE_EXPENSE,
		BundleRuleId: "non_deductible_expense", Amount: companyTaxMoney("AUD", 100),
		Timing: tammyv1.TaxAdjustmentTiming_TAX_ADJUSTMENT_TIMING_PERMANENT,
	}
	if err := protovalidate.Validate(adjustment); err != nil {
		t.Fatalf("valid tax adjustment input rejected: %v", err)
	}
	omittedTiming := proto.Clone(adjustment).(*tammyv1.TaxAdjustmentInput)
	omittedTiming.Timing = tammyv1.TaxAdjustmentTiming_TAX_ADJUSTMENT_TIMING_UNSPECIFIED
	assertFinancialCloseValidationRejects(t, "omitted adjustment timing", omittedTiming)
	nonAUDAdjustment := proto.Clone(adjustment).(*tammyv1.TaxAdjustmentInput)
	nonAUDAdjustment.Amount.CurrencyCode = "USD"
	assertFinancialCloseValidationRejects(t, "non-AUD adjustment", nonAUDAdjustment)

	returnInput := &tammyv1.CompanyReturnInput{LossAmountToApply: companyTaxMoney("AUD", 0)}
	if err := protovalidate.Validate(returnInput); err != nil {
		t.Fatalf("valid company return input rejected: %v", err)
	}
	returnInput.LossAmountToApply.CurrencyCode = "USD"
	assertFinancialCloseValidationRejects(t, "non-AUD return input", returnInput)
}

func TestCompanyTaxProtovalidateEnforcesExportShapeAndFreshFactorPurposes(t *testing.T) {
	passphrase := &tammyv1.SecretInput{Utf8: []byte("correct horse battery staple")}
	validEncrypted := &tammyv1.ExportCompanyReturnPackRequest{
		CommandContext: validFinancialCloseCommandContext("company_return_export"), OrganisationId: financialCloseID(), ReturnId: financialCloseID(),
		ExpectedVersion: 1, Kind: tammyv1.CompanyReturnExportKind_COMPANY_RETURN_EXPORT_KIND_ENCRYPTED_HANDOFF_ARCHIVE, ExportPassphrase: passphrase,
	}
	if err := protovalidate.Validate(validEncrypted); err != nil {
		t.Fatalf("valid encrypted export rejected: %v", err)
	}
	missingPassphrase := proto.Clone(validEncrypted).(*tammyv1.ExportCompanyReturnPackRequest)
	missingPassphrase.ExportPassphrase = nil
	assertFinancialCloseValidationRejects(t, "encrypted export without passphrase", missingPassphrase)
	redactedWithPassphrase := proto.Clone(validEncrypted).(*tammyv1.ExportCompanyReturnPackRequest)
	redactedWithPassphrase.Kind = tammyv1.CompanyReturnExportKind_COMPANY_RETURN_EXPORT_KIND_REDACTED_REVIEW_PDF
	assertFinancialCloseValidationRejects(t, "redacted export with passphrase", redactedWithPassphrase)
	validExportResponse := &tammyv1.ExportCompanyReturnPackResponse{ExportId: financialCloseID(), ContentHash: financialCloseHash(), SafeFilename: "company-return-2026.pdf", Kind: tammyv1.CompanyReturnExportKind_COMPANY_RETURN_EXPORT_KIND_REDACTED_REVIEW_PDF}
	if err := protovalidate.Validate(validExportResponse); err != nil {
		t.Fatalf("valid export response rejected: %v", err)
	}
	for _, unsafeFilename := range []string{"nested/return.pdf", `nested\return.pdf`, "return\n.pdf"} {
		response := proto.Clone(validExportResponse).(*tammyv1.ExportCompanyReturnPackResponse)
		response.SafeFilename = unsafeFilename
		assertFinancialCloseValidationRejects(t, "unsafe export filename", response)
	}

	tests := []struct {
		name, purpose string
		build         func(*tammyv1.CommandContext) proto.Message
	}{
		{"profile secrets", "company_tax_edit_secrets", func(context *tammyv1.CommandContext) proto.Message {
			return &tammyv1.SetCompanyTaxProfileRequest{CommandContext: context, OrganisationId: financialCloseID(), Input: validCompanyTaxProfileInput()}
		}},
		{"warning acknowledgement", "company_return_acknowledge_warning", func(context *tammyv1.CommandContext) proto.Message {
			return &tammyv1.AcknowledgeReturnWarningRequest{CommandContext: context, OrganisationId: financialCloseID(), ReturnId: financialCloseID(), ExpectedVersion: 1, WarningId: financialCloseID(), ValidationRevision: 1}
		}},
		{"declaration", "company_return_declare", func(context *tammyv1.CommandContext) proto.Message {
			return &tammyv1.DeclareCompanyReturnRequest{CommandContext: context, OrganisationId: financialCloseID(), ReturnId: financialCloseID(), ExpectedVersion: 1, ValidationRevision: 1}
		}},
		{"declaration withdrawal", "company_return_withdraw_declaration", func(context *tammyv1.CommandContext) proto.Message {
			return &tammyv1.WithdrawCompanyReturnDeclarationRequest{CommandContext: context, OrganisationId: financialCloseID(), ReturnId: financialCloseID(), ExpectedVersion: 1, Reason: "Replace prior declaration"}
		}},
		{"export", "company_return_export", func(context *tammyv1.CommandContext) proto.Message {
			return &tammyv1.ExportCompanyReturnPackRequest{CommandContext: context, OrganisationId: financialCloseID(), ReturnId: financialCloseID(), ExpectedVersion: 1, Kind: tammyv1.CompanyReturnExportKind_COMPANY_RETURN_EXPORT_KIND_REDACTED_REVIEW_PDF}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := protovalidate.Validate(test.build(validFinancialCloseCommandContext(test.purpose))); err != nil {
				t.Fatalf("valid request rejected: %v", err)
			}
			assertFinancialCloseValidationRejects(t, "missing fresh factor", test.build(validFinancialCloseCommandContext("")))
			assertFinancialCloseValidationRejects(t, "wrong fresh-factor purpose", test.build(validFinancialCloseCommandContext(test.purpose+"_wrong")))
		})
	}
}

func validCompanyTaxProfileInput() *tammyv1.CompanyTaxProfileInput {
	return &tammyv1.CompanyTaxProfileInput{
		LegalName: "Example Pty Ltd", Tfn: &tammyv1.SecretInput{Utf8: []byte("123456789")},
		CurrentPostalAddress: companyTaxAddress(), PriorPostalAddress: companyTaxAddress(), MainBusinessAddress: companyTaxAddress(),
		AustralianResident: tammyv1.RequiredAnswer_REQUIRED_ANSWER_YES, PrivateCompany: tammyv1.RequiredAnswer_REQUIRED_ANSWER_YES,
		MainBusinessActivityCode: "700000", MainBusinessActivityDescription: "Management services",
		FinalReturn: tammyv1.RequiredAnswer_REQUIRED_ANSWER_NO, HoldingCompanyKind: tammyv1.HoldingCompanyKind_HOLDING_COMPANY_KIND_NONE,
		SmallBusinessEntityChoice: tammyv1.SmallBusinessEntityChoice_SMALL_BUSINESS_ENTITY_CHOICE_DO_NOT_APPLY,
		DepreciationChoice:        tammyv1.DepreciationChoice_DEPRECIATION_CHOICE_STANDARD, Applicability: companyTaxApplicability(),
	}
}

func validPriorRevenueLossInput() *tammyv1.PriorRevenueLossInput {
	return &tammyv1.PriorRevenueLossInput{OpeningBalance: companyTaxMoney("AUD", 100), OwnershipContinuityConfirmed: tammyv1.RequiredAnswer_REQUIRED_ANSWER_YES, SameOrSimilarBusinessJudgementRequired: tammyv1.RequiredAnswer_REQUIRED_ANSWER_NO, Evidence: []*tammyv1.SourceRef{companyTaxSource()}}
}

func validCompanyReturn() *tammyv1.CompanyReturn {
	return &tammyv1.CompanyReturn{Id: financialCloseID(), OrganisationId: financialCloseID(), IncomeYear: 2026, PeriodStart: financialCloseDate(2025, 7, 1), PeriodEnd: financialCloseDate(2026, 6, 30), RelationshipKind: tammyv1.CompanyReturnRelationshipKind_COMPANY_RETURN_RELATIONSHIP_KIND_ORIGINAL, RootReturnId: financialCloseID(), PreparationBundleId: "au-company-return-2026-preparation-v1", PreparationBundleFingerprint: financialCloseHash(), SourceCloseId: financialCloseID(), SourceCloseHash: financialCloseHash(), TaxReconciliationHash: financialCloseHash(), State: tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING, Version: 1, ValidationRevision: 1, CreatedAt: financialCloseTimestamp(), UpdatedAt: financialCloseTimestamp()}
}

func companyTaxAddress() *tammyv1.AddressInput {
	return &tammyv1.AddressInput{Line_1: "1 Example Street", Locality: "Melbourne", State: "VIC", Postcode: "3000", CountryCode: "AU"}
}
func companyTaxMoney(currency string, units int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: currency, MinorUnits: units}
}
func companyTaxSource() *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: "document", Id: financialCloseID(), Revision: 1, ContentHash: financialCloseHash()}
}

func companyTaxApplicability() *tammyv1.ApplicabilityAnswers {
	no := tammyv1.RequiredAnswer_REQUIRED_ANSWER_NO
	return &tammyv1.ApplicabilityAnswers{TofaApplies: no, PsiApplies: no, InterposedEntityElectionApplies: no, ConsolidatedGroupMember: no, ResearchAndDevelopmentIncentive: no, InternationalDealings: no, ReportableTaxPosition: no, LifeInsuranceBusiness: no, CgtScheduleRequired: no, LossesScheduleRequired: no, OtherScheduleRequired: no, FbOrUnsupportedPayrollEffect: no, Division_7AUnresolved: no, UnsupportedInventory: no, UnsupportedMulticurrency: no, UnsupportedCrypto: no}
}

type companyEOFYTransitionFixture struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Transitions   []companyEOFYTransitionEdge `json:"transitions"`
}

type companyEOFYTransitionEdge struct {
	Enum       string `json:"enum"`
	Transition string `json:"transition"`
}

func TestCompanyEOFYLifecycleEnumsHaveExactNames(t *testing.T) {
	tests := []struct {
		name   protoreflect.FullName
		values []string
	}{
		{"tammy.v1.FinancialCloseState", []string{"FINANCIAL_CLOSE_STATE_UNSPECIFIED", "FINANCIAL_CLOSE_STATE_COLLECTING", "FINANCIAL_CLOSE_STATE_BLOCKED", "FINANCIAL_CLOSE_STATE_REVIEW_READY", "FINANCIAL_CLOSE_STATE_FROZEN"}},
		{"tammy.v1.CompanyReturnState", []string{"COMPANY_RETURN_STATE_UNSPECIFIED", "COMPANY_RETURN_STATE_COLLECTING", "COMPANY_RETURN_STATE_BLOCKED", "COMPANY_RETURN_STATE_REVIEW_READY", "COMPANY_RETURN_STATE_DECLARED", "COMPANY_RETURN_STATE_PRELODGE_PENDING", "COMPANY_RETURN_STATE_PRELODGE_REVIEW", "COMPANY_RETURN_STATE_READY_TO_LODGE", "COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN", "COMPANY_RETURN_STATE_LODGE_PENDING", "COMPANY_RETURN_STATE_DELIVERED", "COMPANY_RETURN_STATE_LODGE_REJECTED", "COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN", "COMPANY_RETURN_STATE_REPLACED", "COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT"}},
		{"tammy.v1.CompanyReturnRelationshipKind", []string{"COMPANY_RETURN_RELATIONSHIP_KIND_UNSPECIFIED", "COMPANY_RETURN_RELATIONSHIP_KIND_ORIGINAL", "COMPANY_RETURN_RELATIONSHIP_KIND_REPLACEMENT", "COMPANY_RETURN_RELATIONSHIP_KIND_AMENDMENT"}},
		{"tammy.v1.CompanyReturnOperationType", []string{"COMPANY_RETURN_OPERATION_TYPE_UNSPECIFIED", "COMPANY_RETURN_OPERATION_TYPE_PRELODGE", "COMPANY_RETURN_OPERATION_TYPE_LODGE", "COMPANY_RETURN_OPERATION_TYPE_STATUS", "COMPANY_RETURN_OPERATION_TYPE_RECONCILE"}},
		{"tammy.v1.CompanyReturnOperationOutcome", []string{"COMPANY_RETURN_OPERATION_OUTCOME_UNSPECIFIED", "COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS", "COMPANY_RETURN_OPERATION_OUTCOME_WARNINGS", "COMPANY_RETURN_OPERATION_OUTCOME_REJECTED", "COMPANY_RETURN_OPERATION_OUTCOME_OUTCOME_UNKNOWN"}},
		{"tammy.v1.CompanyReturnAttemptState", []string{"COMPANY_RETURN_ATTEMPT_STATE_UNSPECIFIED", "COMPANY_RETURN_ATTEMPT_STATE_PREPARED", "COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING", "COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED", "COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED", "COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN", "COMPANY_RETURN_ATTEMPT_STATE_COMMITTED", "COMPANY_RETURN_ATTEMPT_STATE_ABORTED"}},
	}

	for _, test := range tests {
		t.Run(string(test.name), func(t *testing.T) {
			descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(test.name)
			if err != nil {
				t.Fatalf("enum %s missing: %v", test.name, err)
			}
			enum, ok := descriptor.(protoreflect.EnumDescriptor)
			if !ok {
				t.Fatalf("descriptor %s is not an enum", test.name)
			}
			if enum.Values().Len() != len(test.values) {
				t.Fatalf("%s value count = %d, want %d", test.name, enum.Values().Len(), len(test.values))
			}
			for index, want := range test.values {
				if got := string(enum.Values().Get(index).Name()); got != want {
					t.Errorf("%s value %d = %q, want %q", test.name, index, got, want)
				}
			}
		})
	}
}

func TestCompanyEOFYProtoDependencyDirectionIsAcyclic(t *testing.T) {
	tax, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/company_tax.proto")
	if err != nil {
		t.Fatalf("company tax descriptor missing: %v", err)
	}
	for index := range tax.Imports().Len() {
		if tax.Imports().Get(index).Path() == "tammy/v1/company_return_submission.proto" {
			t.Fatal("company_tax.proto must not import company_return_submission.proto")
		}
	}

	submission, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/company_return_submission.proto")
	if err != nil {
		t.Fatalf("company return submission descriptor missing: %v", err)
	}
	foundTax := false
	for index := range submission.Imports().Len() {
		foundTax = foundTax || submission.Imports().Get(index).Path() == "tammy/v1/company_tax.proto"
	}
	if !foundTax {
		t.Fatal("company_return_submission.proto must import company_tax.proto")
	}
}

func TestCompanyEOFYTransitionFixturesMatchExactAuthority(t *testing.T) {
	assertCompanyEOFYTransitionFixture(t, "reporting", map[string][]string{
		"tammy.v1.FinancialCloseState": {
			"FINANCIAL_CLOSE_STATE_BLOCKED->FINANCIAL_CLOSE_STATE_COLLECTING",
			"FINANCIAL_CLOSE_STATE_BLOCKED->FINANCIAL_CLOSE_STATE_REVIEW_READY",
			"FINANCIAL_CLOSE_STATE_COLLECTING->FINANCIAL_CLOSE_STATE_BLOCKED",
			"FINANCIAL_CLOSE_STATE_COLLECTING->FINANCIAL_CLOSE_STATE_REVIEW_READY",
			"FINANCIAL_CLOSE_STATE_FROZEN->FINANCIAL_CLOSE_STATE_COLLECTING",
			"FINANCIAL_CLOSE_STATE_REVIEW_READY->FINANCIAL_CLOSE_STATE_BLOCKED",
			"FINANCIAL_CLOSE_STATE_REVIEW_READY->FINANCIAL_CLOSE_STATE_COLLECTING",
			"FINANCIAL_CLOSE_STATE_REVIEW_READY->FINANCIAL_CLOSE_STATE_FROZEN",
		},
	})
	assertCompanyEOFYTransitionFixture(t, "tax", map[string][]string{
		"tammy.v1.CompanyReturnAttemptState": {
			"COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING->COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED",
			"COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING->COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN",
			"COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING->COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED",
			"COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED->COMPANY_RETURN_ATTEMPT_STATE_ABORTED",
			"COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED->COMPANY_RETURN_ATTEMPT_STATE_PREPARED",
			"COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN->COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED",
			"COMPANY_RETURN_ATTEMPT_STATE_PREPARED->COMPANY_RETURN_ATTEMPT_STATE_ABORTED",
			"COMPANY_RETURN_ATTEMPT_STATE_PREPARED->COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING",
			"COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED->COMPANY_RETURN_ATTEMPT_STATE_COMMITTED",
		},
		"tammy.v1.CompanyReturnState": {
			"COMPANY_RETURN_STATE_BLOCKED->COMPANY_RETURN_STATE_COLLECTING",
			"COMPANY_RETURN_STATE_BLOCKED->COMPANY_RETURN_STATE_REVIEW_READY",
			"COMPANY_RETURN_STATE_COLLECTING->COMPANY_RETURN_STATE_BLOCKED",
			"COMPANY_RETURN_STATE_COLLECTING->COMPANY_RETURN_STATE_REVIEW_READY",
			"COMPANY_RETURN_STATE_DECLARED->COMPANY_RETURN_STATE_PRELODGE_PENDING",
			"COMPANY_RETURN_STATE_DECLARED->COMPANY_RETURN_STATE_REPLACED",
			"COMPANY_RETURN_STATE_DELIVERED->COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT",
			"COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_DELIVERED",
			"COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_LODGE_REJECTED",
			"COMPANY_RETURN_STATE_LODGE_PENDING->COMPANY_RETURN_STATE_DELIVERED",
			"COMPANY_RETURN_STATE_LODGE_PENDING->COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN",
			"COMPANY_RETURN_STATE_LODGE_PENDING->COMPANY_RETURN_STATE_LODGE_REJECTED",
			"COMPANY_RETURN_STATE_LODGE_PENDING->COMPANY_RETURN_STATE_READY_TO_LODGE",
			"COMPANY_RETURN_STATE_LODGE_REJECTED->COMPANY_RETURN_STATE_REPLACED",
			"COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_BLOCKED",
			"COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_DECLARED",
			"COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_PRELODGE_REVIEW",
			"COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_READY_TO_LODGE",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_BLOCKED",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_DECLARED",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_PRELODGE_REVIEW",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_READY_TO_LODGE",
			"COMPANY_RETURN_STATE_PRELODGE_REVIEW->COMPANY_RETURN_STATE_DECLARED",
			"COMPANY_RETURN_STATE_PRELODGE_REVIEW->COMPANY_RETURN_STATE_REPLACED",
			"COMPANY_RETURN_STATE_READY_TO_LODGE->COMPANY_RETURN_STATE_LODGE_PENDING",
			"COMPANY_RETURN_STATE_READY_TO_LODGE->COMPANY_RETURN_STATE_REPLACED",
			"COMPANY_RETURN_STATE_REVIEW_READY->COMPANY_RETURN_STATE_BLOCKED",
			"COMPANY_RETURN_STATE_REVIEW_READY->COMPANY_RETURN_STATE_COLLECTING",
			"COMPANY_RETURN_STATE_REVIEW_READY->COMPANY_RETURN_STATE_DECLARED",
		},
	})
}

func assertCompanyEOFYTransitionFixture(t *testing.T, slice string, want map[string][]string) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve company EOFY transition fixture path")
	}
	fixturePath := filepath.Join(filepath.Dir(sourceFile), "../../../../test/fixtures", slice, "transitions.pb.json")
	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read %s transition fixture: %v", slice, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	var fixture companyEOFYTransitionFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode %s transition fixture: %v", slice, err)
	}
	if fixture.SchemaVersion != 1 {
		t.Fatalf("%s transition fixture schemaVersion = %d, want 1", slice, fixture.SchemaVersion)
	}

	got := make([]string, 0, len(fixture.Transitions))
	for _, transition := range fixture.Transitions {
		id := transition.Enum + "." + transition.Transition
		got = append(got, id)
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(transition.Enum))
		if err != nil {
			t.Fatalf("find transition enum %q: %v", transition.Enum, err)
		}
		enum, ok := descriptor.(protoreflect.EnumDescriptor)
		if !ok {
			t.Fatalf("transition descriptor %q is not an enum", transition.Enum)
		}
		values := strings.Split(transition.Transition, "->")
		if len(values) != 2 {
			t.Fatalf("transition %q must contain exactly one ->", id)
		}
		for _, value := range values {
			if enum.Values().ByName(protoreflect.Name(value)) == nil {
				t.Fatalf("transition %q references unknown enum value %q", id, value)
			}
			if strings.HasSuffix(value, "_UNSPECIFIED") {
				t.Fatalf("transition %q references an unspecified sentinel", id)
			}
		}
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("%s transition fixture is not sorted by fully-qualified transition ID", slice)
	}

	wantIDs := make([]string, 0, len(fixture.Transitions))
	for enum, transitions := range want {
		for _, transition := range transitions {
			wantIDs = append(wantIDs, enum+"."+transition)
		}
	}
	sort.Strings(wantIDs)
	if strings.Join(got, "\n") != strings.Join(wantIDs, "\n") {
		t.Fatalf("%s transition fixture differs from exact lifecycle authority\ngot:\n%s\nwant:\n%s", slice, strings.Join(got, "\n"), strings.Join(wantIDs, "\n"))
	}
}
