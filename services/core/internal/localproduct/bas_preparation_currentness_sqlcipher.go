//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

// Currentness is an ephemeral projection. Unavailable live sources do not
// prevent retrieval of the exact, independently verified retained snapshot.
func (handler *basPreparationHandler) basLiveCurrentness(ctx context.Context, tx app.CommandSQLExecutor,
	owner *baspreparation.PreparationRetainedSnapshot, calculation *tammyv1.BasCalculation) tammyv1.BasLiveCurrentness {
	if calculation == nil {
		return tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_NOT_ASSESSED
	}
	if handler == nil || handler.now == nil || handler.newID == nil || owner == nil || owner.Preparation == nil {
		return tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_UNAVAILABLE
	}
	capture, err := baspreparation.NewBASCommandCapture(tx, handler.cursors, handler.now, handler.newID)
	if err != nil {
		return tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_UNAVAILABLE
	}
	live, err := capture.CaptureCurrent(ctx, owner, calculation)
	if errors.Is(err, baspreparation.ErrCommandConflict) || errors.Is(err, baspreparation.ErrProfileNotEffective) {
		return tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_SOURCE_CHANGED
	}
	if err != nil {
		return tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_UNAVAILABLE
	}
	prep := owner.Preparation
	if live.InputRevision != prep.InputRevision || !bytes.Equal(live.InputHash, prep.InputHash) ||
		!bytes.Equal(live.SourceSnapshotHash, prep.SourceSnapshotHash) || !bytes.Equal(live.SourceSnapshotHash, calculation.SourceSnapshotHash) {
		return tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_SOURCE_CHANGED
	}
	return tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_CURRENT
}
