package accounting

import (
	"context"
	"errors"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/artefacts"
)

var ErrSetup = errors.New("accounting: initial setup failed")

type RuleBundleRetainer interface {
	Retain(context.Context, string, artefacts.RuleBundle, time.Time) error
}

// InitialSetup installs the pinned AU rule catalogue and protected account
// template inside the organisation command's caller-owned transaction.
type InitialSetup struct {
	Accounts *AccountRepository
	Rules    RuleBundleRetainer
}

func (setup InitialSetup) Install(ctx context.Context, organisationID string, now time.Time) error {
	if setup.Accounts == nil || setup.Rules == nil {
		return ErrSetup
	}
	bundle, err := artefacts.LoadAUGSTV1()
	if err != nil {
		return err
	}
	if err := setup.Rules.Retain(ctx, organisationID, bundle, now); err != nil {
		return err
	}
	template, err := LoadAUSmallBusinessV1()
	if err != nil {
		return err
	}
	return setup.Accounts.InstallTemplate(ctx, organisationID, template, now)
}
