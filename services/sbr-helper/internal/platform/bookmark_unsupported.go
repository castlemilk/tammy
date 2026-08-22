//go:build !darwin || !arm64 || !cgo

package platform

import "errors"

var ErrBookmarkInvalid = errors.New("SBR_BOOKMARK_INVALID")

type BookmarkResolver struct{}
type ScopedFile struct{}

func NewBookmarkResolver() *BookmarkResolver                          { return &BookmarkResolver{} }
func (*BookmarkResolver) Resolve([]byte, string) (*ScopedFile, error) { return nil, ErrBookmarkInvalid }
func (*ScopedFile) Path() string                                      { return "" }
func (*ScopedFile) Revalidate() error                                 { return ErrBookmarkInvalid }
func (*ScopedFile) ReadAll(int) ([]byte, error)                       { return nil, ErrBookmarkInvalid }
func (*ScopedFile) Close() error                                      { return nil }
