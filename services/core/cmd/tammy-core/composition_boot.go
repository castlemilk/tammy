//go:build !tammy_sqlcipher || !cgo || ((!darwin || !arm64) && (!windows || !amd64))

package main

import (
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
)

func newConfiguredComposition(info buildinfo.Info, config processConfig) (*app.Composition, error) {
	if config != (processConfig{}) {
		return nil, errProcessConfig
	}
	return app.NewBootComposition(info)
}
