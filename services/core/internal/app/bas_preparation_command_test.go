package app

import (
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	"testing"
)

func TestBASOrdinaryOperationRegistryUsesCanonicalRequestAndResultTypes(t *testing.T) {
	cases := []struct {
		operation OrdinaryOperation
		request   string
		result    string
	}{
		{OrdinaryOperationSetBasObligationsProfile, "tammy.v1.SetBasObligationsProfileRequest", "tammy.v1.SetBasObligationsProfileResponse"},
		{OrdinaryOperationConfirmBasPeriod, "tammy.v1.ConfirmBasPeriodRequest", "tammy.v1.ConfirmBasPeriodResponse"},
		{OrdinaryOperationSaveBasPaygInputs, "tammy.v1.SaveBasPaygInputsRequest", "tammy.v1.SaveBasPaygInputsResponse"},
		{OrdinaryOperationRegenerateBasPreparation, "tammy.v1.RegenerateBasPreparationRequest", "tammy.v1.RegenerateBasPreparationResponse"},
		{OrdinaryOperationValidateBasPreparation, "tammy.v1.ValidateBasPreparationRequest", "tammy.v1.ValidateBasPreparationResponse"},
		{OrdinaryOperationAcknowledgeBasWarnings, "tammy.v1.AcknowledgeBasWarningsRequest", "tammy.v1.AcknowledgeBasWarningsResponse"},
	}
	for _, test := range cases {
		definition, ok := ordinaryOperationDefinitionFor(test.operation)
		if !ok || definition.action != authorisation.ActionPrepareTax ||
			definition.requestType != test.request || definition.resultType != test.result {
			t.Fatalf("operation %q = %#v, %t", test.operation, definition, ok)
		}
	}
}
