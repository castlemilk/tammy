//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	"github.com/tammyapp/tammy/services/core/internal/localproduct"
	"github.com/tammyapp/tammy/services/core/internal/sbr"
	"github.com/tammyapp/tammy/services/core/internal/sbrhelper"
	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
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
	sbrConfig := sbr.ModuleConfig{}
	if config.sbrProfile != "" && runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		const profileSuffix = "/sbr/simulator/sbr-profile-v1.json"
		cleanProfile := filepath.Clean(config.sbrProfile)
		if !strings.HasSuffix(cleanProfile, profileSuffix) {
			return nil, errProcessConfig
		}
		resourcesRoot := strings.TrimSuffix(cleanProfile, profileSuffix)
		runtimeBase := filepath.Join(config.dataRoot, "core", "sbr-runtime")
		if err := os.MkdirAll(runtimeBase, 0o700); err != nil || os.Chmod(runtimeBase, 0o700) != nil {
			return nil, errProcessConfig
		}
		locator, locatorErr := sbrprofile.NewDirectoryResourceLocator(resourcesRoot, runtimeBase)
		if locatorErr != nil {
			return nil, locatorErr
		}
		profilePort, profileErr := sbrhelper.NewAuthenticatedProfilePort(cleanProfile, locator)
		if profileErr != nil {
			return nil, profileErr
		}
		helperPort, helperErr := sbrhelper.NewSBRPort(sbrhelper.NewLauncher(locator), cleanProfile, time.Now)
		if helperErr != nil {
			return nil, helperErr
		}
		auditPort, auditErr := sbr.NewRedactedSQLAuditAppender(time.Now)
		if auditErr != nil {
			return nil, auditErr
		}
		sbrConfig = sbr.ModuleConfig{Helper: helperPort, Profiles: profilePort, Audit: auditPort}
	}
	sbrModule, err := sbr.NewSbrModule(sbrConfig)
	if err != nil {
		return nil, err
	}
	localConfig := app.LocalCompositionConfig{
		Info: info, Root: config.dataRoot, Modules: []app.LocalWorkspaceModule{ledger, sbrModule},
	}
	if config.developmentMemoryAnchors {
		if err := prepareDevelopmentAttemptJournals(config); err != nil {
			return nil, err
		}
		localConfig.AttemptAnchors = workspace.NewMemoryAnchorStore()
	}
	return app.NewLocalComposition(localConfig)
}
