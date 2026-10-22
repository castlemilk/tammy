package audit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var ErrApprovedDestination = errors.New("audit: approved destination failed")

const (
	maximumApprovedDestinations = 10_000
	maximumApprovalNameBytes    = 255
)

type ApprovedDestinationConfig struct {
	BaseDirectory string
	Capacity      int
	NewID         func() (string, error)
	hooks         *destinationFSHooks
}

type destinationFSHooks struct {
	openRoot      func(string) (*os.Root, error)
	writeFile     func(*os.File, []byte) (int, error)
	syncFile      func(*os.File) error
	publish       func(*os.Root, string, string) error
	syncDirectory func(*os.Root) error
}

// ApprovedDestinationRegistry maps opaque UUIDv7 capabilities to basename-only
// approvals beneath one already-open rooted directory handle.
type ApprovedDestinationRegistry struct {
	mu           sync.RWMutex
	root         *os.Root
	capacity     int
	newID        func() (string, error)
	hooks        destinationFSHooks
	destinations map[string]*safeFileDestination
	names        map[string]struct{}
	closed       bool
}

func NewApprovedDestinationRegistry(config ApprovedDestinationConfig) (*ApprovedDestinationRegistry, error) {
	if !validApprovedDestinationConfig(config) {
		return nil, ErrApprovedDestination
	}
	info, err := os.Lstat(config.BaseDirectory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrApprovedDestination
	}
	hooks := normalizedDestinationFSHooks(config.hooks)
	root, err := hooks.openRoot(config.BaseDirectory)
	if err != nil {
		return nil, ErrApprovedDestination
	}
	rootInfo, err := root.Stat(".")
	if err != nil || !rootInfo.IsDir() || !os.SameFile(info, rootInfo) {
		_ = root.Close()
		return nil, ErrApprovedDestination
	}
	return &ApprovedDestinationRegistry{
		root: root, capacity: config.Capacity, newID: config.NewID,
		hooks:        hooks,
		destinations: make(map[string]*safeFileDestination, config.Capacity),
		names:        make(map[string]struct{}, config.Capacity),
	}, nil
}

func validApprovedDestinationConfig(config ApprovedDestinationConfig) bool {
	if config.BaseDirectory == "" || !filepath.IsAbs(config.BaseDirectory) || filepath.Clean(config.BaseDirectory) != config.BaseDirectory ||
		config.Capacity <= 0 || config.Capacity > maximumApprovedDestinations || nilInterface(config.NewID) {
		return false
	}
	info, err := os.Lstat(config.BaseDirectory)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func (registry *ApprovedDestinationRegistry) Approve(name string) (string, error) {
	if registry == nil || !safeApprovalName(name) {
		return "", ErrApprovedDestination
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.closed || registry.root == nil || len(registry.destinations) >= registry.capacity {
		return "", ErrApprovedDestination
	}
	if _, duplicate := registry.names[name]; duplicate {
		return "", ErrApprovedDestination
	}
	reference, err := registry.newID()
	if err != nil || !ids.IsCanonicalV7(reference) {
		return "", ErrApprovedDestination
	}
	if _, duplicate := registry.destinations[reference]; duplicate {
		return "", ErrApprovedDestination
	}
	destination := &safeFileDestination{registry: registry, reference: reference, name: name}
	registry.destinations[reference] = destination
	registry.names[name] = struct{}{}
	return reference, nil
}

func (registry *ApprovedDestinationRegistry) Resolve(reference string) (ExportDestination, error) {
	if registry == nil || !ids.IsCanonicalV7(reference) {
		return nil, ErrApprovedDestination
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	if registry.closed || registry.root == nil {
		return nil, ErrApprovedDestination
	}
	destination := registry.destinations[reference]
	if destination == nil {
		return nil, ErrApprovedDestination
	}
	return destination, nil
}

func (registry *ApprovedDestinationRegistry) Close() error {
	if registry == nil {
		return nil
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.closed {
		return nil
	}
	registry.closed = true
	if registry.root == nil {
		return nil
	}
	err := registry.root.Close()
	registry.root = nil
	if err != nil {
		return ErrApprovedDestination
	}
	return nil
}

func safeApprovalName(name string) bool {
	if name == "" || len(name) > maximumApprovalNameBytes || !utf8.ValidString(name) || !fs.ValidPath(name) ||
		name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00") ||
		strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

type safeFileDestination struct {
	mu        sync.Mutex
	registry  *ApprovedDestinationRegistry
	reference string
	name      string
}

func (destination *safeFileDestination) Reference() string {
	if destination == nil {
		return ""
	}
	return destination.reference
}

func (destination *safeFileDestination) AtomicCommit(ctx context.Context, archive []byte) (resultErr error) {
	if destination == nil || destination.registry == nil || ctx == nil || len(archive) == 0 || len(archive) > maxEvidenceArchiveBytes {
		return ErrApprovedDestination
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrApprovedDestination, err)
	}
	destination.mu.Lock()
	defer destination.mu.Unlock()
	destination.registry.mu.RLock()
	defer destination.registry.mu.RUnlock()
	registry := destination.registry
	if registry.closed || registry.root == nil || !safeApprovalName(destination.name) || !ids.IsCanonicalV7(destination.reference) {
		return ErrApprovedDestination
	}

	if _, err := registry.root.Lstat(destination.name); err == nil {
		committed, readErr := readRootedCommitted(ctx, registry.root, destination.name)
		if readErr == nil && bytes.Equal(committed, archive) {
			return nil
		}
		return ErrApprovedDestination
	} else if !os.IsNotExist(err) {
		return ErrApprovedDestination
	}

	temporaryName := ".tammy-" + destination.reference + ".tmp"
	file, err := registry.root.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrApprovedDestination
	}
	temporaryOwned := true
	published := false
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if resultErr != nil && published {
			_ = registry.root.Remove(destination.name)
			_ = syncRootDirectory(registry.root)
		}
		if temporaryOwned {
			_ = registry.root.Remove(temporaryName)
			_ = syncRootDirectory(registry.root)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return ErrApprovedDestination
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return ErrApprovedDestination
	}
	for offset := 0; offset < len(archive); {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrApprovedDestination, err)
		}
		written, writeErr := registry.hooks.writeFile(file, archive[offset:])
		if written < 0 || written > len(archive)-offset || written == 0 && writeErr == nil {
			return ErrApprovedDestination
		}
		offset += written
		if writeErr != nil {
			return ErrApprovedDestination
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrApprovedDestination, err)
	}
	if err := registry.hooks.syncFile(file); err != nil {
		return ErrApprovedDestination
	}
	if err := file.Close(); err != nil {
		file = nil
		return ErrApprovedDestination
	}
	file = nil
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrApprovedDestination, err)
	}
	if _, err := registry.root.Lstat(destination.name); err == nil || !os.IsNotExist(err) {
		return ErrApprovedDestination
	}
	if err := registry.hooks.publish(registry.root, temporaryName, destination.name); err != nil {
		return ErrApprovedDestination
	}
	published = true
	if err := registry.hooks.syncDirectory(registry.root); err != nil {
		return ErrApprovedDestination
	}
	if err := registry.root.Remove(temporaryName); err != nil {
		return ErrApprovedDestination
	}
	temporaryOwned = false
	if err := registry.hooks.syncDirectory(registry.root); err != nil {
		return ErrApprovedDestination
	}
	published = false
	return nil
}

func (destination *safeFileDestination) ReadCommitted(ctx context.Context) ([]byte, error) {
	if destination == nil || destination.registry == nil || ctx == nil {
		return nil, ErrApprovedDestination
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrApprovedDestination, err)
	}
	destination.mu.Lock()
	defer destination.mu.Unlock()
	destination.registry.mu.RLock()
	defer destination.registry.mu.RUnlock()
	if destination.registry.closed || destination.registry.root == nil || !safeApprovalName(destination.name) ||
		!ids.IsCanonicalV7(destination.reference) {
		return nil, ErrApprovedDestination
	}
	return readRootedCommitted(ctx, destination.registry.root, destination.name)
}

func readRootedCommitted(ctx context.Context, root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() <= 0 ||
		info.Size() > int64(maxEvidenceArchiveBytes) {
		return nil, ErrApprovedDestination
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrApprovedDestination
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm() != 0o600 ||
		openedInfo.Size() != info.Size() || !os.SameFile(info, openedInfo) {
		return nil, ErrApprovedDestination
	}
	contents := make([]byte, int(info.Size()))
	for offset := 0; offset < len(contents); {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrApprovedDestination, err)
		}
		end := min(offset+32*1024, len(contents))
		read, readErr := io.ReadFull(file, contents[offset:end])
		offset += read
		if readErr != nil {
			return nil, ErrApprovedDestination
		}
	}
	var extra [1]byte
	if read, readErr := file.Read(extra[:]); read != 0 || readErr != io.EOF {
		return nil, ErrApprovedDestination
	}
	finalInfo, err := file.Stat()
	if err != nil || finalInfo.Size() != info.Size() || !os.SameFile(info, finalInfo) {
		return nil, ErrApprovedDestination
	}
	return contents, nil
}

func syncRootDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func normalizedDestinationFSHooks(injected *destinationFSHooks) destinationFSHooks {
	hooks := destinationFSHooks{
		openRoot:      os.OpenRoot,
		writeFile:     func(file *os.File, value []byte) (int, error) { return file.Write(value) },
		syncFile:      func(file *os.File) error { return file.Sync() },
		publish:       func(root *os.Root, oldName, newName string) error { return root.Link(oldName, newName) },
		syncDirectory: syncRootDirectory,
	}
	if injected == nil {
		return hooks
	}
	if injected.openRoot != nil {
		hooks.openRoot = injected.openRoot
	}
	if injected.writeFile != nil {
		hooks.writeFile = injected.writeFile
	}
	if injected.syncFile != nil {
		hooks.syncFile = injected.syncFile
	}
	if injected.publish != nil {
		hooks.publish = injected.publish
	}
	if injected.syncDirectory != nil {
		hooks.syncDirectory = injected.syncDirectory
	}
	return hooks
}
