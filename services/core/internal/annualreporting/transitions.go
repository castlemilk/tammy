// Package annualreporting owns the pure company EOFY lifecycle authority.
package annualreporting

import tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"

// AttemptResolution binds a durable attempt result to the original operation identity.
type AttemptResolution struct {
	OriginalAttemptID  string
	RequestedAttemptID string
	OriginalOperation  tammyv1.CompanyReturnOperationType
	ResolvingOperation tammyv1.CompanyReturnOperationType
	AttemptState       tammyv1.CompanyReturnAttemptState
	Outcome            *tammyv1.CompanyReturnOperationOutcome
}

// CanTransitionFinancialClose reports whether the financial-close edge is authoritative.
func CanTransitionFinancialClose(from, to tammyv1.FinancialCloseState) bool {
	switch from {
	case tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING:
		switch to {
		case tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_BLOCKED,
			tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY:
			return true
		}
	case tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_BLOCKED:
		switch to {
		case tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING,
			tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY:
			return true
		}
	case tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY:
		switch to {
		case tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING,
			tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_BLOCKED,
			tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_FROZEN:
			return true
		}
	case tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_FROZEN:
		return to == tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING
	}
	return false
}

// CanTransitionCompanyReturn reports whether the company-return edge is authoritative.
func CanTransitionCompanyReturn(from, to tammyv1.CompanyReturnState) bool {
	switch from {
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REVIEW_READY:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REVIEW_READY:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REVIEW_READY:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN:
		switch to {
		case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED,
			tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED:
			return true
		}
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED:
		return to == tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED:
		return to == tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT
	case tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT:
		return false
	}
	return false
}

// CanTransitionCompanyReturnAttempt reports whether the durable-attempt edge is authoritative.
func CanTransitionCompanyReturnAttempt(from, to tammyv1.CompanyReturnAttemptState) bool {
	switch from {
	case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED:
		switch to {
		case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING,
			tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED:
			return true
		}
	case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING:
		switch to {
		case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED,
			tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED,
			tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN:
			return true
		}
	case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED:
		switch to {
		case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED,
			tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED:
			return true
		}
	case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN:
		return to == tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED
	case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED:
		return to == tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_COMMITTED
	case tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_COMMITTED,
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED:
		return false
	}
	return false
}

// ResolveReportTransition derives the only report edge granted by a bound attempt resolution.
func ResolveReportTransition(current tammyv1.CompanyReturnState, resolution AttemptResolution) (tammyv1.CompanyReturnState, bool) {
	if resolution.OriginalAttemptID == "" || resolution.OriginalAttemptID != resolution.RequestedAttemptID {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}
	if resolution.OriginalOperation != tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE &&
		resolution.OriginalOperation != tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_LODGE {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}

	if resolution.AttemptState == tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED ||
		resolution.AttemptState == tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED {
		if resolution.Outcome != nil {
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
		}
		isDirectRollback := resolution.ResolvingOperation == resolution.OriginalOperation &&
			pendingStateMatchesOperation(current, resolution.OriginalOperation)
		isPrelodgeReconciliation := resolution.OriginalOperation == tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE &&
			current == tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN &&
			(resolution.ResolvingOperation == tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_STATUS ||
				resolution.ResolvingOperation == tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_RECONCILE)
		if !isDirectRollback && !isPrelodgeReconciliation {
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
		}
		return rollbackReportTransition(current, resolution.OriginalOperation)
	}

	if resolution.Outcome == nil {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}
	if resolution.AttemptState == tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN {
		if *resolution.Outcome != tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_OUTCOME_UNKNOWN ||
			resolution.ResolvingOperation != resolution.OriginalOperation {
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
		}
		return unresolvedReportTransition(current, resolution.OriginalOperation)
	}
	if resolution.AttemptState != tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED &&
		resolution.AttemptState != tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_COMMITTED {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}
	if *resolution.Outcome == tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_UNSPECIFIED ||
		*resolution.Outcome == tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_OUTCOME_UNKNOWN {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}

	isDirect := resolution.ResolvingOperation == resolution.OriginalOperation
	isReconciliation := resolution.ResolvingOperation == tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_STATUS ||
		resolution.ResolvingOperation == tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_RECONCILE
	if !isDirect && !isReconciliation {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}
	if isDirect && !pendingStateMatchesOperation(current, resolution.OriginalOperation) {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}
	if isReconciliation && !unknownStateMatchesOperation(current, resolution.OriginalOperation) {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}

	target, ok := reportStateForOutcome(resolution.OriginalOperation, *resolution.Outcome)
	if !ok || !CanTransitionCompanyReturn(current, target) {
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}
	return target, true
}

func rollbackReportTransition(current tammyv1.CompanyReturnState, operation tammyv1.CompanyReturnOperationType) (tammyv1.CompanyReturnState, bool) {
	var target tammyv1.CompanyReturnState
	switch operation {
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE:
		if current != tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING &&
			current != tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN {
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
		}
		target = tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_LODGE:
		if current != tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING {
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
		}
		target = tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE
	default:
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}
	return target, CanTransitionCompanyReturn(current, target)
}

func unresolvedReportTransition(current tammyv1.CompanyReturnState, operation tammyv1.CompanyReturnOperationType) (tammyv1.CompanyReturnState, bool) {
	var target tammyv1.CompanyReturnState
	switch operation {
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE:
		if current != tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING {
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
		}
		target = tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_LODGE:
		if current != tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING {
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
		}
		target = tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN
	default:
		return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
	}
	return target, CanTransitionCompanyReturn(current, target)
}

func pendingStateMatchesOperation(current tammyv1.CompanyReturnState, operation tammyv1.CompanyReturnOperationType) bool {
	switch operation {
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE:
		return current == tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_LODGE:
		return current == tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING
	default:
		return false
	}
}

func unknownStateMatchesOperation(current tammyv1.CompanyReturnState, operation tammyv1.CompanyReturnOperationType) bool {
	switch operation {
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE:
		return current == tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_LODGE:
		return current == tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN
	default:
		return false
	}
}

func reportStateForOutcome(operation tammyv1.CompanyReturnOperationType, outcome tammyv1.CompanyReturnOperationOutcome) (tammyv1.CompanyReturnState, bool) {
	switch operation {
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE:
		switch outcome {
		case tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS:
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE, true
		case tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_WARNINGS:
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW, true
		case tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_REJECTED:
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED, true
		}
	case tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_LODGE:
		switch outcome {
		case tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS:
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED, true
		case tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_REJECTED:
			return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED, true
		}
	}
	return tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, false
}
