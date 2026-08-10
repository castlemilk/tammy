//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package main

import (
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	"github.com/tammyapp/tammy/services/core/internal/localproduct"
)

func newConfiguredComposition(info buildinfo.Info, dataRoot string) (*app.Composition, error) {
	if dataRoot == "" {
		return app.NewBootComposition(info)
	}
	ledger, err := localproduct.NewLedgerModule()
	if err != nil {
		return nil, err
	}
	return app.NewLocalComposition(app.LocalCompositionConfig{
		Info: info, Root: dataRoot, Modules: []app.LocalWorkspaceModule{ledger},
	})
}
