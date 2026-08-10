//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package main

import (
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
)

func newConfiguredComposition(info buildinfo.Info, dataRoot string) (*app.Composition, error) {
	if dataRoot == "" {
		return app.NewBootComposition(info)
	}
	return app.NewLocalComposition(app.LocalCompositionConfig{Info: info, Root: dataRoot})
}
