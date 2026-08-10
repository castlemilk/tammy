//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package main

import (
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	"github.com/tammyapp/tammy/services/core/internal/localproduct"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

func newConfiguredComposition(info buildinfo.Info, config processConfig) (*app.Composition, error) {
	if config.dataRoot == "" {
		return app.NewBootComposition(info)
	}
	ledger, err := localproduct.NewLedgerModule()
	if err != nil {
		return nil, err
	}
	localConfig := app.LocalCompositionConfig{
		Info: info, Root: config.dataRoot, Modules: []app.LocalWorkspaceModule{ledger},
	}
	if config.developmentMemoryAnchors {
		localConfig.AttemptAnchors = workspace.NewMemoryAnchorStore()
	}
	return app.NewLocalComposition(localConfig)
}
