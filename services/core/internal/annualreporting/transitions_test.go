package annualreporting

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestCanTransitionFinancialCloseMatchesAuthority(t *testing.T) {
	states := []tammyv1.FinancialCloseState{
		tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING,
		tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_BLOCKED,
		tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY,
		tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_FROZEN,
	}
	allowed := map[[2]tammyv1.FinancialCloseState]bool{
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_BLOCKED}:      true,
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY}: true,
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_BLOCKED, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING}:      true,
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_BLOCKED, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY}:    true,
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING}: true,
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_BLOCKED}:    true,
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_REVIEW_READY, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_FROZEN}:     true,
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_FROZEN, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING}:       true,
	}

	for _, from := range states {
		for _, to := range states {
			want := allowed[[2]tammyv1.FinancialCloseState{from, to}]
			if got := CanTransitionFinancialClose(from, to); got != want {
				t.Errorf("CanTransitionFinancialClose(%s, %s) = %t, want %t", from, to, got, want)
			}
		}
	}

	for _, edge := range [][2]tammyv1.FinancialCloseState{
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_UNSPECIFIED, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING},
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING, tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_UNSPECIFIED},
		{tammyv1.FinancialCloseState(999), tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING},
		{tammyv1.FinancialCloseState_FINANCIAL_CLOSE_STATE_COLLECTING, tammyv1.FinancialCloseState(999)},
	} {
		if CanTransitionFinancialClose(edge[0], edge[1]) {
			t.Errorf("unknown or unspecified financial-close edge %d -> %d was allowed", edge[0], edge[1])
		}
	}
}

func TestCanTransitionCompanyReturnMatchesAuthority(t *testing.T) {
	states := []tammyv1.CompanyReturnState{
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REVIEW_READY,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT,
	}
	allowed := map[[2]tammyv1.CompanyReturnState]bool{}
	add := func(from tammyv1.CompanyReturnState, destinations ...tammyv1.CompanyReturnState) {
		for _, to := range destinations {
			allowed[[2]tammyv1.CompanyReturnState{from, to}] = true
		}
	}
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REVIEW_READY)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REVIEW_READY)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REVIEW_READY,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED)
	add(tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT)

	for _, from := range states {
		for _, to := range states {
			want := allowed[[2]tammyv1.CompanyReturnState{from, to}]
			if got := CanTransitionCompanyReturn(from, to); got != want {
				t.Errorf("CanTransitionCompanyReturn(%s, %s) = %t, want %t", from, to, got, want)
			}
		}
	}

	for _, terminal := range []tammyv1.CompanyReturnState{
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_REPLACED,
		tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT,
	} {
		for _, to := range states {
			if CanTransitionCompanyReturn(terminal, to) {
				t.Errorf("terminal company return state %s transitioned to %s", terminal, to)
			}
		}
	}

	for _, edge := range [][2]tammyv1.CompanyReturnState{
		{tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED},
		{tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN, tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED},
		{tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN, tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE},
		{tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING},
		{tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING, tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED},
		{tammyv1.CompanyReturnState(999), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING},
		{tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_COLLECTING, tammyv1.CompanyReturnState(999)},
	} {
		if CanTransitionCompanyReturn(edge[0], edge[1]) {
			t.Errorf("illegal company-return edge %d -> %d was allowed", edge[0], edge[1])
		}
	}
}

func TestCanTransitionCompanyReturnAttemptMatchesAuthority(t *testing.T) {
	states := []tammyv1.CompanyReturnAttemptState{
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED,
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING,
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED,
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED,
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN,
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_COMMITTED,
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED,
	}
	allowed := map[[2]tammyv1.CompanyReturnAttemptState]bool{
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING}:            true,
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED}:                true,
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED}:      true,
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED}:     true,
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN}:     true,
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED}:         true,
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED}:          true,
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED}: true,
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_COMMITTED}:       true,
	}

	for _, from := range states {
		for _, to := range states {
			want := allowed[[2]tammyv1.CompanyReturnAttemptState{from, to}]
			if got := CanTransitionCompanyReturnAttempt(from, to); got != want {
				t.Errorf("CanTransitionCompanyReturnAttempt(%s, %s) = %t, want %t", from, to, got, want)
			}
		}
	}

	for _, terminal := range []tammyv1.CompanyReturnAttemptState{
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_COMMITTED,
		tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED,
	} {
		for _, to := range states {
			if CanTransitionCompanyReturnAttempt(terminal, to) {
				t.Errorf("terminal attempt state %s transitioned to %s", terminal, to)
			}
		}
	}

	for _, edge := range [][2]tammyv1.CompanyReturnAttemptState{
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_UNSPECIFIED, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED},
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_UNSPECIFIED},
		{tammyv1.CompanyReturnAttemptState(999), tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED},
		{tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_PREPARED, tammyv1.CompanyReturnAttemptState(999)},
	} {
		if CanTransitionCompanyReturnAttempt(edge[0], edge[1]) {
			t.Errorf("unknown or unspecified attempt edge %d -> %d was allowed", edge[0], edge[1])
		}
	}
}

func TestResolveReportTransitionAppliesOperationOutcomeAuthority(t *testing.T) {
	prelodge := tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE
	lodge := tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_LODGE
	status := tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_STATUS
	reconcile := tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_RECONCILE
	resultRecorded := tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED
	committed := tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_COMMITTED
	unknown := tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN

	tests := []struct {
		name       string
		current    tammyv1.CompanyReturnState
		resolution AttemptResolution
		want       tammyv1.CompanyReturnState
	}{
		{"pre-lodge success", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE},
		{"pre-lodge warnings", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, committed, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_WARNINGS)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW},
		{"pre-lodge validation rejection", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_REJECTED)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED},
		{"pre-lodge unresolved", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, unknown, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_OUTCOME_UNKNOWN)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN},
		{"lodge accepted", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING, resolution(lodge, lodge, committed, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED},
		{"lodge rejected", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING, resolution(lodge, lodge, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_REJECTED)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED},
		{"lodge unresolved", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING, resolution(lodge, lodge, unknown, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_OUTCOME_UNKNOWN)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN},
		{"status resolves pre-lodge success", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN, resolution(prelodge, status, committed, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE},
		{"reconcile resolves pre-lodge warnings", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN, resolution(prelodge, reconcile, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_WARNINGS)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_REVIEW},
		{"reconcile resolves pre-lodge validation rejection", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN, resolution(prelodge, reconcile, committed, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_REJECTED)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_BLOCKED},
		{"status proves pre-lodge non-acceptance", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN, resolution(prelodge, status, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED, nil), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED},
		{"reconcile proves pre-lodge abort", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN, resolution(prelodge, reconcile, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED, nil), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED},
		{"status resolves lodge accepted", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN, resolution(lodge, status, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DELIVERED},
		{"reconcile resolves lodge rejected", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN, resolution(lodge, reconcile, committed, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_REJECTED)), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_REJECTED},
		{"pre-lodge not dispatched", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED, nil), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED},
		{"pre-lodge aborted", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED, nil), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_DECLARED},
		{"lodge not dispatched", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING, resolution(lodge, lodge, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED, nil), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE},
		{"lodge aborted", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING, resolution(lodge, lodge, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED, nil), tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_READY_TO_LODGE},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ResolveReportTransition(test.current, test.resolution)
			if !ok || got != test.want {
				t.Fatalf("ResolveReportTransition() = (%s, %t), want (%s, true)", got, ok, test.want)
			}
			if !CanTransitionCompanyReturn(test.current, got) {
				t.Fatalf("resolved transition %s -> %s is outside company-return authority", test.current, got)
			}
		})
	}
}

func TestResolveReportTransitionRejectsIdentityStateAndOperationConfusion(t *testing.T) {
	prelodge := tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_PRELODGE
	lodge := tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_LODGE
	status := tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_STATUS
	success := outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS)
	resultRecorded := tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED

	tests := []struct {
		name       string
		current    tammyv1.CompanyReturnState
		resolution AttemptResolution
	}{
		{"attempt ID mismatch", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, AttemptResolution{OriginalAttemptID: "attempt-1", RequestedAttemptID: "attempt-2", OriginalOperation: prelodge, ResolvingOperation: prelodge, AttemptState: resultRecorded, Outcome: success}},
		{"operation class mismatch", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(lodge, prelodge, resultRecorded, success)},
		{"direct operation mismatch", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, lodge, resultRecorded, success)},
		{"status before unknown", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, status, resultRecorded, success)},
		{"pre-lodge cannot deliver", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN, resolution(prelodge, status, resultRecorded, success)},
		{"lodge cannot restore ready from recorded result", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_LODGE_PENDING, resolution(lodge, lodge, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_WARNINGS))},
		{"recorded result requires outcome", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, resultRecorded, nil)},
		{"rollback forbids outcome", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_ABORTED, success)},
		{"rollback requires pending report", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN, resolution(prelodge, prelodge, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED, nil)},
		{"unknown attempt requires unknown outcome", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN, success)},
		{"unknown outcome requires unknown attempt", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_OUTCOME_UNKNOWN))},
		{"unspecified current state", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_UNSPECIFIED, resolution(prelodge, prelodge, resultRecorded, success)},
		{"unknown current state", tammyv1.CompanyReturnState(999), resolution(prelodge, prelodge, resultRecorded, success)},
		{"unspecified original operation", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(tammyv1.CompanyReturnOperationType_COMPANY_RETURN_OPERATION_TYPE_UNSPECIFIED, prelodge, resultRecorded, success)},
		{"unknown resolving operation", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, tammyv1.CompanyReturnOperationType(999), resultRecorded, success)},
		{"unspecified attempt state", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, tammyv1.CompanyReturnAttemptState_COMPANY_RETURN_ATTEMPT_STATE_UNSPECIFIED, success)},
		{"unspecified outcome", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome_COMPANY_RETURN_OPERATION_OUTCOME_UNSPECIFIED))},
		{"unknown outcome", tammyv1.CompanyReturnState_COMPANY_RETURN_STATE_PRELODGE_PENDING, resolution(prelodge, prelodge, resultRecorded, outcome(tammyv1.CompanyReturnOperationOutcome(999)))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, ok := ResolveReportTransition(test.current, test.resolution); ok {
				t.Fatalf("ResolveReportTransition() = (%s, true), want rejection", got)
			}
		})
	}
}

func resolution(original, resolving tammyv1.CompanyReturnOperationType, attemptState tammyv1.CompanyReturnAttemptState, result *tammyv1.CompanyReturnOperationOutcome) AttemptResolution {
	return AttemptResolution{
		OriginalAttemptID:  "attempt-1",
		RequestedAttemptID: "attempt-1",
		OriginalOperation:  original,
		ResolvingOperation: resolving,
		AttemptState:       attemptState,
		Outcome:            result,
	}
}

func outcome(value tammyv1.CompanyReturnOperationOutcome) *tammyv1.CompanyReturnOperationOutcome {
	return &value
}
