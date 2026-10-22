//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package organisations_test

import (
	"context"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestRepositoryPersistsOneOrganisationAndOptimisticUpdates(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	profile := repositoryTestProfile("018f0000-0000-7000-8000-000000000020")
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	if err := repository.Create(context.Background(), profile, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.Get(context.Background(), profile.Id)
	if err != nil || !proto.Equal(loaded, profile) {
		t.Fatalf("Get() = %#v, %v; want %#v", loaded, err, profile)
	}

	updated := proto.Clone(profile).(*tammyv1.Organisation)
	updated.Version++
	updated.DisplayName = "Tammy Books"
	updated.VerificationState = tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED
	if err := repository.Update(context.Background(), profile.Version, updated, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repository.Update(context.Background(), profile.Version, updated, now.Add(2*time.Minute)); !errors.Is(err, organisations.ErrRepositoryConflict) {
		t.Fatalf("stale Update() error = %v; want conflict", err)
	}

	second := repositoryTestProfile("018f0000-0000-7000-8000-000000000021")
	if err := repository.Create(context.Background(), second, now); !errors.Is(err, organisations.ErrOrganisationExists) {
		t.Fatalf("second Create() error = %v; want singleton conflict", err)
	}
}

func repositoryTestProfile(id string) *tammyv1.Organisation {
	return &tammyv1.Organisation{
		Id: id, Version: 1, Abn: "51824753556", LegalName: "Tammy Pty Ltd",
		DisplayName: "Tammy", EntityType: "AU_PRIVATE_COMPANY",
		GstBasis:              tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6,
		VerificationState:     tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED,
		OwnerUserId:           "018f0000-0000-7000-8000-000000000010",
		ActiveTaxRuleBundle:   &tammyv1.SourceRef{Type: "rule_bundle", Id: "018f0000-0000-7000-8000-000000000030", Revision: 1, ContentHash: make([]byte, 32)},
	}
}
