package baspreparation

import (
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestIncomeYearChoiceRequiresSelectedMethodAndCanonicalConfirmedRange(t *testing.T) {
	identity := func(id string) *tammyv1.BasIncomeYearIdentity {
		return &tammyv1.BasIncomeYearIdentity{IncomeYearId: id,
			Dates:        &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2027, 6, 30)},
			Confirmation: &tammyv1.SourceRef{Type: "ato_income_year", Id: testUUID(9540), Revision: 1, ContentHash: testHash(54)}}
	}
	profile := &tammyv1.BasObligationsProfileInput{Paygi: &tammyv1.BasPaygiProfileInput{
		Method: tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT, ConfirmedIncomeYear: identity(testUUID(9541)),
	}}
	choice, err := incomeYearChoiceFromProfile(profile)
	if err != nil || choice == nil || choice.yearID != testUUID(9541) || choice.start != "2026-07-01" ||
		choice.end != "2027-06-30" || choice.method != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT {
		t.Fatalf("valid notified-amount choice=%#v, err=%v", choice, err)
	}
	profile.Paygi.Method = tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE
	choice, err = incomeYearChoiceFromProfile(profile)
	if err != nil || choice == nil || choice.method != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE {
		t.Fatalf("valid notified-rate choice=%#v, err=%v", choice, err)
	}
	profile.Paygi.ConfirmedIncomeYear = nil
	if _, err := incomeYearChoiceFromProfile(profile); !errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("rate without a confirmed year was accepted: %v", err)
	}
	profile.Paygi.Method = tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE
	if choice, err := incomeYearChoiceFromProfile(profile); err != nil || choice != nil {
		t.Fatalf("non-PAYGI method created a year pin: %#v, %v", choice, err)
	}
	profile.Paygi.ConfirmedIncomeYear = identity(testUUID(9541))
	if _, err := incomeYearChoiceFromProfile(profile); !errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("non-selected method with an income-year identity was accepted: %v", err)
	}
	profile.Paygi.Method = tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT
	profile.Paygi.ConfirmedIncomeYear = identity("not-a-uuid-v7")
	if _, err := incomeYearChoiceFromProfile(profile); !errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("noncanonical income-year ID was accepted: %v", err)
	}
	profile.Paygi.ConfirmedIncomeYear = identity(testUUID(9541))
	profile.Paygi.ConfirmedIncomeYear.Dates.EndDate = testCivilDate(2026, 6, 30)
	if _, err := incomeYearChoiceFromProfile(profile); !errors.Is(err, ErrIncomeYearPinConflict) {
		t.Fatalf("reversed income-year range was accepted: %v", err)
	}
}

func TestIncomeYearChoiceRejectsMalformedConfirmationProof(t *testing.T) {
	valid := &tammyv1.SourceRef{Type: "ato_income_year", Id: testUUID(9570), Revision: 1, ContentHash: testHash(70)}
	for _, mutate := range []func(*tammyv1.SourceRef){
		func(ref *tammyv1.SourceRef) { ref.Type = "" },
		func(ref *tammyv1.SourceRef) { ref.Type = "invalid source type!" },
		func(ref *tammyv1.SourceRef) { ref.Id = "invalid" },
		func(ref *tammyv1.SourceRef) { ref.Revision = 0 },
		func(ref *tammyv1.SourceRef) { ref.ContentHash = nil },
	} {
		confirmation := proto.Clone(valid).(*tammyv1.SourceRef)
		mutate(confirmation)
		profile := &tammyv1.BasObligationsProfileInput{Paygi: &tammyv1.BasPaygiProfileInput{
			Method: tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT,
			ConfirmedIncomeYear: &tammyv1.BasIncomeYearIdentity{IncomeYearId: testUUID(9571),
				Dates: &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2027, 6, 30)}, Confirmation: confirmation},
		}}
		if _, err := incomeYearChoiceFromProfile(profile); !errors.Is(err, ErrIncomeYearPinConflict) {
			t.Fatalf("malformed confirmation accepted: %v, error=%v", confirmation, err)
		}
	}
}

func TestIncomeYearChoiceAcceptsCanonicalRetainedConfirmationTypes(t *testing.T) {
	profile := &tammyv1.BasObligationsProfileInput{Paygi: &tammyv1.BasPaygiProfileInput{
		Method: tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT,
		ConfirmedIncomeYear: &tammyv1.BasIncomeYearIdentity{IncomeYearId: testUUID(9575),
			Dates:        &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2027, 6, 30)},
			Confirmation: &tammyv1.SourceRef{Type: "document", Id: testUUID(9576), Revision: 1, ContentHash: testHash(76)}},
	}}
	if choice, err := incomeYearChoiceFromProfile(profile); err != nil || choice == nil {
		t.Fatalf("canonical retained document confirmation rejected: %v", err)
	}
}
