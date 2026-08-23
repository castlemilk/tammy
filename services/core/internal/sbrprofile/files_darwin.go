//go:build darwin && arm64 && cgo

package sbrprofile

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"golang.org/x/text/unicode/norm"
)

type secureFile struct {
	bytes    []byte
	identity unix.Stat_t
}
type componentResource struct{ bytes []byte }

type descriptorOwner struct{ fd int }

func newDescriptorOwner(fd int) descriptorOwner { return descriptorOwner{fd: fd} }
func (o *descriptorOwner) close() error {
	if o == nil || o.fd < 0 {
		return nil
	}
	fd := o.fd
	o.fd = -1
	return unix.Close(fd)
}
func (o *descriptorOwner) replace(fd int) error {
	if fd < 0 {
		return errors.New("descriptor")
	}
	if err := o.close(); err != nil {
		_ = unix.Close(fd)
		return err
	}
	o.fd = fd
	return nil
}
func (o *descriptorOwner) release() int {
	fd := o.fd
	o.fd = -1
	return fd
}

const darwinOpenExecute = 0x40000000

func AuthenticateAndStage(ctx context.Context, profilePath string, locator ResourceLocator, now time.Time) (_ *StagedResources, err error) {
	if ctx == nil || locator == nil || !filepath.IsAbs(profilePath) || filepath.Clean(profilePath) != profilePath {
		return nil, codedError("SBR_PROFILE_INVALID")
	}
	profileFile, err := openSecureContext(ctx, profilePath, MaxProfileBytes)
	if err != nil {
		return nil, codedError("SBR_PROFILE_UNTRUSTED")
	}
	signatureFile, err := openSecureContext(ctx, profileSignaturePath(profilePath), 128)
	if err != nil {
		return nil, codedError("SBR_PROFILE_UNTRUSTED")
	}
	parsed, err := ParseProfile(profileFile.bytes, now)
	if err != nil {
		return nil, stableProfileError(err)
	}
	if parsed.Profile.Environment == "EVTE" && !evteTrustRootRegistered {
		return nil, codedError("SBR_EVTE_TRUST_ROOT_UNREGISTERED")
	}
	if parsed.Profile.Environment == "SIMULATOR" || parsed.Profile.Environment == "EVTE" {
		if _, authErr := AuthenticateProfile(profileFile.bytes, signatureFile.bytes, now); authErr != nil {
			return nil, codedError("SBR_PROFILE_UNTRUSTED")
		}
	}
	resources, err := locator.Locate(parsed.Profile)
	if err != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	helper, err := openSecureContext(ctx, resources.HelperPath, MaxComponentFileBytes)
	if err != nil {
		return nil, codedError("SBR_HELPER_UNTRUSTED")
	}
	helperHash, hashErr := sha256BytesContext(ctx, helper.bytes)
	if hashErr != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	if hex.EncodeToString(helperHash[:]) != parsed.Profile.HelperSHA256 {
		return nil, codedError("SBR_HELPER_UNTRUSTED")
	}
	if parsed.Profile.Environment == "EVTE" {
		componentFile, openErr := openSecureContext(ctx, resources.ComponentManifestPath, MaxComponentManifestBytes)
		if openErr != nil {
			return nil, codedError("SBR_COMPONENT_MISSING")
		}
		component, parseErr := ParseComponentManifest(componentFile.bytes)
		if parseErr != nil {
			return nil, codedError("SBR_COMPONENT_UNTRUSTED")
		}
		componentResources, loadErr := loadProfileBoundComponent(ctx, resources.ComponentRoot, parsed, component)
		if loadErr != nil {
			return nil, loadErr
		}
		registrationFile, openErr := openSecureContext(ctx, resources.RegistrationManifestPath, maxEvidenceBytes)
		if openErr != nil {
			return nil, codedError("SBR_REGISTRATION_MANIFEST_MISSING")
		}
		registration, parseErr := ParseRegistrationManifest(registrationFile.bytes)
		if parseErr != nil {
			return nil, codedError("SBR_REGISTRATION_MANIFEST_INVALID")
		}
		registrationSignature, openErr := openSecureContext(ctx, resources.RegistrationSignaturePath, 128)
		if openErr != nil {
			return nil, codedError("SBR_REGISTRATION_MANIFEST_UNTRUSTED")
		}
		if authErr := VerifyRegistrationSignature(registration, registrationSignature.bytes); authErr != nil {
			return nil, codedError("SBR_REGISTRATION_MANIFEST_UNTRUSTED")
		}
		if crossErr := authenticatePreEndpointBindings(parsed, registration, component); crossErr != nil {
			return nil, crossErr
		}
		pre, readinessErr := EvaluatePreEndpointReadiness(registration.Manifest, now)
		if readinessErr != nil {
			return nil, readinessErr
		}
		if !pre.Ready {
			return nil, codedError("SBR_" + pre.Code)
		}
		endpointFile, openErr := openSecureContext(ctx, resources.EndpointProfilePath, maxEvidenceBytes)
		if openErr != nil {
			return nil, codedError("SBR_ENDPOINT_PROFILE_MISSING")
		}
		endpoint, parseErr := ParseEndpointProfile(endpointFile.bytes)
		if parseErr != nil {
			return nil, codedError("SBR_ENDPOINT_PROFILE_UNTRUSTED")
		}
		if authErr := AuthenticateEVTE(parsed, registration, endpoint, component); authErr != nil {
			return nil, authErr
		}
		productIDScope, scopeErr := authenticateEVTEProductIDScope(registration, endpoint)
		if scopeErr != nil {
			return nil, scopeErr
		}
		phase := resources.ReadinessPhase
		if phase == "" {
			phase = "PRE_CONFORMANCE"
		}
		readiness, readinessErr := EvaluateReadiness(registration.Manifest, now, phase)
		if readinessErr != nil {
			return nil, readinessErr
		}
		if !readiness.Ready {
			return nil, codedError("SBR_" + readiness.Code)
		}
		if err := ctx.Err(); err != nil {
			return nil, codedError("SBR_HELPER_UNAVAILABLE")
		}
		staged, stageErr := stageAuthenticatedContext(ctx, resources.TrustedRuntimeBase, parsed, helper.bytes, endpointProtocolBytes(endpoint), componentResources)
		if stageErr != nil {
			return nil, stageErr
		}
		staged.authenticatedProductIDScope = &productIDScope
		staged.authenticatedComponentVersion = component.Manifest.ComponentVersion
		staged.validateFresh = func(fresh time.Time) error {
			if _, parseErr := ParseProfile(profileFile.bytes, fresh); parseErr != nil {
				return stableProfileError(parseErr)
			}
			readiness, readinessErr := EvaluateReadiness(registration.Manifest, fresh, phase)
			if readinessErr != nil {
				return readinessErr
			}
			if !readiness.Ready {
				return codedError("SBR_" + readiness.Code)
			}
			return nil
		}
		return staged, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	staged, stageErr := stageAuthenticatedContext(ctx, resources.TrustedRuntimeBase, parsed, helper.bytes, nil, nil)
	if stageErr != nil {
		return nil, stageErr
	}
	staged.validateFresh = func(fresh time.Time) error {
		_, parseErr := ParseProfile(profileFile.bytes, fresh)
		if parseErr != nil {
			return stableProfileError(parseErr)
		}
		return nil
	}
	return staged, nil
}

func profileSignaturePath(profilePath string) string {
	return strings.TrimSuffix(profilePath, filepath.Ext(profilePath)) + ".sig"
}

func loadProfileBoundComponent(ctx context.Context, root string, profile ParsedProfile, component ParsedComponent) (map[string]componentResource, error) {
	if profile.Profile.ComponentManifestSHA256 != component.SHA256 {
		return nil, invalid("REGISTRATION", "COMPONENT_HASH_MISMATCH")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return loadComponentBundleContext(ctx, root, component.Manifest)
}

func loadComponentBundle(root string, manifest ComponentManifest) (map[string]componentResource, error) {
	return loadComponentBundleContext(context.Background(), root, manifest)
}

func loadComponentBundleContext(ctx context.Context, root string, manifest ComponentManifest) (map[string]componentResource, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, codedError("SBR_COMPONENT_UNTRUSTED")
	}
	expected := make(map[string]ComponentFile, len(manifest.Files))
	expectedDirectories := make(map[string]bool)
	for _, file := range manifest.Files {
		expected[file.Path] = file
		parts := strings.Split(file.Path, "/")
		for depth := 1; depth < len(parts); depth++ {
			expectedDirectories[strings.Join(parts[:depth], "/")] = true
		}
	}
	rootFD, err := openOwnedDirectoryContext(ctx, root, false)
	if err != nil {
		return nil, codedError("SBR_COMPONENT_UNTRUSTED")
	}
	defer unix.Close(rootFD)
	state := componentWalkState{expected: expected, expectedDirectories: expectedDirectories, seen: make(map[string]bool, len(expected)), loaded: make(map[string]componentResource, len(expected)), identities: make(map[fileIdentityKey]bool)}
	if err = walkComponentDirectory(ctx, rootFD, "", 1, &state); err != nil || len(state.seen) != len(expected) {
		return nil, codedError("SBR_COMPONENT_UNTRUSTED")
	}
	return state.loaded, nil
}

type fileIdentityKey struct {
	dev int32
	ino uint64
}
type componentWalkState struct {
	expected            map[string]ComponentFile
	expectedDirectories map[string]bool
	seen                map[string]bool
	loaded              map[string]componentResource
	identities          map[fileIdentityKey]bool
	entries             int
}

func walkComponentDirectory(ctx context.Context, directory int, prefix string, depth int, state *componentWalkState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > MaxComponentDepth {
		return errors.New("depth")
	}
	duplicate, err := unix.Dup(directory)
	if err != nil {
		return err
	}
	handle := os.NewFile(uintptr(duplicate), "component-directory")
	entries := make([]os.DirEntry, 0, 16)
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			_ = handle.Close()
			return err
		}
		remaining := MaxComponentEntries - state.entries - len(entries)
		if remaining < 0 {
			_ = handle.Close()
			return errors.New("entries")
		}
		batchSize := remaining + 1
		if batchSize > 64 {
			batchSize = 64
		}
		batch, batchErr := handle.ReadDir(batchSize)
		entries = append(entries, batch...)
		if len(entries) > MaxComponentEntries-state.entries {
			_ = handle.Close()
			return errors.New("entries")
		}
		if errors.Is(batchErr, io.EOF) {
			break
		}
		if batchErr != nil {
			readErr = batchErr
			break
		}
	}
	closeErr := handle.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare([]byte(entries[i].Name()), []byte(entries[j].Name())) < 0 })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		state.entries++
		if state.entries > MaxComponentEntries {
			return errors.New("entries")
		}
		name := entry.Name()
		if !validComponentEntryName(name) {
			return errors.New("name")
		}
		relative := name
		if prefix != "" {
			relative = prefix + "/" + name
		}
		var linkStat unix.Stat_t
		if unix.Fstatat(directory, name, &linkStat, unix.AT_SYMLINK_NOFOLLOW) != nil {
			return errors.New("entry stat")
		}
		switch linkStat.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			if !state.expectedDirectories[relative] || depth+1 > MaxComponentDepth {
				return errors.New("directory")
			}
			child, openErr := unix.Openat(directory, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if openErr != nil {
				return openErr
			}
			var descriptor unix.Stat_t
			if unix.Fstat(child, &descriptor) != nil || !sameStat(linkStat, descriptor) || descriptor.Uid != uint32(os.Geteuid()) || descriptor.Mode&0o022 != 0 {
				unix.Close(child)
				return errors.New("directory authority")
			}
			walkErr := walkComponentDirectory(ctx, child, relative, depth+1, state)
			closeErr := unix.Close(child)
			if walkErr != nil {
				return walkErr
			}
			if closeErr != nil {
				return closeErr
			}
		case unix.S_IFREG:
			expected, ok := state.expected[relative]
			if !ok {
				return errors.New("undeclared")
			}
			fd, openErr := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if openErr != nil {
				return openErr
			}
			opened, openErr := readOwnedComponentFD(ctx, fd, expected, linkStat, state.identities)
			unix.Close(fd)
			if openErr != nil {
				return openErr
			}
			state.seen[relative] = true
			state.loaded[relative] = componentResource{bytes: opened}
		default:
			return errors.New("special")
		}
	}
	return nil
}

func readOwnedComponentFD(ctx context.Context, fd int, expected ComponentFile, pathStat unix.Stat_t, identities map[fileIdentityKey]bool) ([]byte, error) {
	var before unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !sameStat(pathStat, before) || before.Uid != uint32(os.Geteuid()) || before.Mode&0o022 != 0 || before.Nlink != 1 || before.Size != expected.ByteLength {
		return nil, errors.New("file authority")
	}
	identity := fileIdentityKey{before.Dev, before.Ino}
	if identities[identity] {
		return nil, errors.New("hardlink alias")
	}
	identities[identity] = true
	data := make([]byte, before.Size)
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := offset + 64<<10
		if end > len(data) {
			end = len(data)
		}
		read, err := unix.Read(fd, data[offset:end])
		if err != nil || read <= 0 {
			return nil, errors.New("read")
		}
		offset += read
	}
	var trailing [1]byte
	if read, err := unix.Read(fd, trailing[:]); err != nil || read != 0 {
		return nil, errors.New("size")
	}
	var after unix.Stat_t
	if unix.Fstat(fd, &after) != nil || !sameStat(before, after) {
		return nil, errors.New("changed")
	}
	sum, err := sha256BytesContext(ctx, data)
	if err != nil {
		return nil, err
	}
	if hex.EncodeToString(sum[:]) != expected.SHA256 {
		return nil, errors.New("hash")
	}
	return data, nil
}

func validComponentEntryName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/") && utf8.ValidString(name) && norm.NFC.IsNormalString(name) && !hasControlName(name)
}
func hasControlName(name string) bool {
	for _, r := range name {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}

func stableProfileError(err error) error {
	if strings.Contains(err.Error(), "EXPIRED") {
		return codedError("SBR_PROFILE_EXPIRED")
	}
	return codedError("SBR_PROFILE_INVALID")
}

func openSecure(path string, maximum int64) (secureFile, error) {
	return openSecureContext(context.Background(), path, maximum)
}

func openSecureContext(ctx context.Context, path string, maximum int64) (secureFile, error) {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return secureFile{}, errors.New("invalid")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return secureFile{}, err
	}
	parent := newDescriptorOwner(root)
	defer parent.close()
	euid := uint32(os.Geteuid())
	for index, part := range parts {
		if err := ctx.Err(); err != nil {
			return secureFile{}, err
		}
		if part == "" || part == "." || part == ".." {
			return secureFile{}, errors.New("invalid")
		}
		last := index == len(parts)-1
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
		if !last {
			flags |= unix.O_DIRECTORY
		}
		fd, openErr := unix.Openat(parent.fd, part, flags, 0)
		if openErr != nil {
			return secureFile{}, openErr
		}
		opened := newDescriptorOwner(fd)
		defer opened.close()
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil {
			return secureFile{}, errors.New("stat")
		}
		if !last {
			if stat.Mode&unix.S_IFMT != unix.S_IFDIR || (stat.Uid != 0 && stat.Uid != euid) || stat.Mode&0o022 != 0 {
				return secureFile{}, errors.New("ancestor")
			}
			if err := parent.replace(opened.release()); err != nil {
				return secureFile{}, err
			}
			continue
		}
		var pathStat unix.Stat_t
		if unix.Fstatat(parent.fd, part, &pathStat, unix.AT_SYMLINK_NOFOLLOW) != nil || pathStat.Dev != stat.Dev || pathStat.Ino != stat.Ino || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != euid || stat.Mode&0o022 != 0 || stat.Nlink != 1 || stat.Size < 0 || stat.Size > maximum {
			return secureFile{}, errors.New("leaf")
		}
		data := make([]byte, stat.Size)
		for offset := 0; offset < len(data); {
			if err := ctx.Err(); err != nil {
				return secureFile{}, err
			}
			end := offset + 64<<10
			if end > len(data) {
				end = len(data)
			}
			read, readErr := unix.Read(opened.fd, data[offset:end])
			if readErr != nil {
				return secureFile{}, readErr
			}
			if read == 0 {
				return secureFile{}, errors.New("short read")
			}
			offset += read
		}
		var trailing [1]byte
		if read, readErr := unix.Read(opened.fd, trailing[:]); readErr != nil || read != 0 {
			return secureFile{}, errors.New("size changed")
		}
		var after unix.Stat_t
		if err := ctx.Err(); err != nil {
			return secureFile{}, err
		}
		if unix.Fstat(opened.fd, &after) != nil || !sameStat(stat, after) {
			return secureFile{}, errors.New("changed")
		}
		return secureFile{bytes: data, identity: stat}, nil
	}
	return secureFile{}, errors.New("invalid")
}

func sameStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func stageAuthenticated(base string, profile ParsedProfile, helper, endpoint []byte, component map[string]componentResource) (result *StagedResources, err error) {
	return stageAuthenticatedContext(context.Background(), base, profile, helper, endpoint, component)
}

func stageAuthenticatedContext(ctx context.Context, base string, profile ParsedProfile, helper, endpoint []byte, component map[string]componentResource) (result *StagedResources, err error) {
	return stageAuthenticatedContextWithFinalValidation(ctx, base, profile, helper, endpoint, component, nil)
}

func stageAuthenticatedContextWithFinalValidation(ctx context.Context, base string, profile ParsedProfile, helper, endpoint []byte, component map[string]componentResource, beforeFinalValidation func()) (result *StagedResources, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(base) {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	helperExpected, hashErr := sha256BytesContext(ctx, helper)
	if hashErr != nil {
		return nil, hashErr
	}
	baseFD, statErr := openTrustedBaseContext(ctx, base)
	if statErr != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	var baseStat unix.Stat_t
	if unix.Fstat(baseFD, &baseStat) != nil || !trustedPrivateDirectory(baseStat) {
		_ = unix.Close(baseFD)
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	baseOwned := true
	defer func() {
		if baseOwned {
			_ = unix.Close(baseFD)
		}
	}()
	var name string
	for attempts := 0; attempts < 8; attempts++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var random [12]byte
		if _, readErr := rand.Read(random[:]); readErr != nil {
			return nil, codedError("SBR_HELPER_UNAVAILABLE")
		}
		name = "tammy-sbr-runtime-" + hex.EncodeToString(random[:])
		if mkdirErr := unix.Mkdirat(baseFD, name, 0o700); mkdirErr == nil {
			break
		} else if !errors.Is(mkdirErr, unix.EEXIST) {
			return nil, codedError("SBR_HELPER_UNAVAILABLE")
		}
		name = ""
	}
	if name == "" {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	root := filepath.Join(base, name)
	rootFD, openRootErr := unix.Openat(baseFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if openRootErr != nil {
		_ = unix.Unlinkat(baseFD, name, unix.AT_REMOVEDIR)
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	var rootStat unix.Stat_t
	var rootPathStat unix.Stat_t
	if unix.Fstat(rootFD, &rootStat) != nil || unix.Fstatat(baseFD, name, &rootPathStat, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameStat(rootStat, rootPathStat) || !trustedPrivateDirectory(rootStat) {
		_ = unix.Close(rootFD)
		_ = unix.Unlinkat(baseFD, name, unix.AT_REMOVEDIR)
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	result = &StagedResources{Profile: profile, RuntimeRoot: root, HelperPath: filepath.Join(root, "sbr-helper"), ReadOnlyPaths: []string{}, EndpointProfile: append([]byte(nil), endpoint...), Fingerprints: map[string]string{"helper_sha256": profile.Profile.HelperSHA256}, baseFile: os.NewFile(uintptr(baseFD), "sbr-runtime-base"), rootFile: os.NewFile(uintptr(rootFD), "sbr-runtime-root"), helperExpected: helperExpected}
	baseOwned = false
	stagedForCleanup := result
	stagedForCleanup.close = func() error { return cleanupStagedResources(stagedForCleanup, name) }
	failed := true
	defer func() {
		if failed {
			_ = stagedForCleanup.Close()
		}
	}()
	result.createdFiles = append(result.createdFiles, "sbr-helper")
	if writeErr := writeStagedAtContext(ctx, rootFD, "sbr-helper", helper, 0o500); writeErr != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	helperFD, openHelperErr := unix.Openat(rootFD, "sbr-helper", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if openHelperErr != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	result.helperFile = os.NewFile(uintptr(helperFD), "sbr-helper")
	execFD, openExecErr := unix.Openat(rootFD, "sbr-helper", darwinOpenExecute|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if openExecErr != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	var readStat, execStat unix.Stat_t
	var helperPathStat unix.Stat_t
	if unix.Fstat(helperFD, &readStat) != nil || unix.Fstat(execFD, &execStat) != nil || unix.Fstatat(rootFD, "sbr-helper", &helperPathStat, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameStat(readStat, execStat) || !sameStat(readStat, helperPathStat) || !trustedStagedRegular(readStat, 0o500) {
		unix.Close(execFD)
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	result.helperExecFile = os.NewFile(uintptr(execFD), "sbr-helper-exec")
	expectedHashes := map[string][sha256.Size]byte{}
	for relative, resource := range component {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data := resource.bytes
		target := filepath.Join(root, "component", filepath.FromSlash(relative))
		if !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return nil, codedError("SBR_COMPONENT_UNTRUSTED")
		}
		mode := os.FileMode(0o400)
		result.ReadOnlyPaths = append(result.ReadOnlyPaths, target)
		stagedRelative := filepath.ToSlash(filepath.Join("component", filepath.FromSlash(relative)))
		result.createdFiles = append(result.createdFiles, stagedRelative)
		parts := strings.Split(stagedRelative, "/")
		for depth := 1; depth < len(parts); depth++ {
			result.createdDirs = appendUnique(result.createdDirs, strings.Join(parts[:depth], "/"))
		}
		if writeErr := writeComponentAtContext(ctx, rootFD, relative, data, mode); writeErr != nil {
			return nil, codedError("SBR_COMPONENT_UNAVAILABLE")
		}
		expectedHash, hashErr := sha256BytesContext(ctx, data)
		if hashErr != nil {
			return nil, hashErr
		}
		expectedHashes[stagedRelative] = expectedHash
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if unix.Fsync(rootFD) != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if unix.Fsync(baseFD) != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.revalidateCtx = func(revalidateCtx context.Context) error {
		if validateErr := revalidateRuntimeAuthorityContext(revalidateCtx, result, name, baseStat, rootStat, readStat); validateErr != nil {
			if revalidateCtx.Err() != nil {
				return revalidateCtx.Err()
			}
			return codedError("SBR_HELPER_UNTRUSTED")
		}
		for relative, expected := range expectedHashes {
			if err := revalidateCtx.Err(); err != nil {
				return err
			}
			opened, openErr := readStagedRelativeContext(revalidateCtx, int(result.rootFile.Fd()), relative, 0o400)
			actual, hashErr := sha256BytesContext(revalidateCtx, opened)
			if openErr != nil || hashErr != nil || actual != expected {
				return codedError("SBR_HELPER_UNTRUSTED")
			}
		}
		return nil
	}
	result.revalidate = func() error { return result.revalidateCtx(context.Background()) }
	if beforeFinalValidation != nil {
		beforeFinalValidation()
	}
	if revalidateErr := result.RevalidateContext(ctx); revalidateErr != nil {
		return nil, revalidateErr
	}
	failed = false
	return result, nil
}

func revalidateRuntimeAuthority(s *StagedResources, runtimeName string, baseExpected, rootExpected, helperExpected unix.Stat_t) error {
	return revalidateRuntimeAuthorityContext(context.Background(), s, runtimeName, baseExpected, rootExpected, helperExpected)
}

func revalidateRuntimeAuthorityContext(ctx context.Context, s *StagedResources, runtimeName string, baseExpected, rootExpected, helperExpected unix.Stat_t) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.baseFile == nil || s.rootFile == nil || s.helperFile == nil || s.helperExecFile == nil {
		return errors.New("closed")
	}
	baseFD := int(s.baseFile.Fd())
	rootFD := int(s.rootFile.Fd())
	var baseCurrent, rootCurrent, rootPath, helperRead, helperExec, helperPath unix.Stat_t
	if unix.Fstat(baseFD, &baseCurrent) != nil || !sameDirectoryObject(baseExpected, baseCurrent) || !trustedPrivateDirectory(baseCurrent) {
		return errors.New("base authority")
	}
	if unix.Fstat(rootFD, &rootCurrent) != nil || !sameDirectoryObject(rootExpected, rootCurrent) || !trustedPrivateDirectory(rootCurrent) {
		return errors.New("root authority")
	}
	if unix.Fstatat(baseFD, runtimeName, &rootPath, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameDirectoryObject(rootCurrent, rootPath) {
		return errors.New("root path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if unix.Fstat(int(s.helperFile.Fd()), &helperRead) != nil || unix.Fstat(int(s.helperExecFile.Fd()), &helperExec) != nil || !sameStat(helperExpected, helperRead) || !sameStat(helperExpected, helperExec) {
		return errors.New("helper descriptor")
	}
	if unix.Fstatat(rootFD, "sbr-helper", &helperPath, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameStat(helperExpected, helperPath) {
		return errors.New("helper path")
	}
	pathFD, err := unix.Openat(rootFD, "sbr-helper", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	pathFile := os.NewFile(uintptr(pathFD), "sbr-helper-path")
	defer pathFile.Close()
	var opened unix.Stat_t
	if unix.Fstat(pathFD, &opened) != nil || !sameStat(helperExpected, opened) || !trustedStagedRegular(opened, 0o500) {
		return errors.New("helper reopened")
	}
	helperBytes, err := readRetainedFileContext(ctx, pathFile, MaxComponentFileBytes)
	actual, hashErr := sha256BytesContext(ctx, helperBytes)
	if err != nil || hashErr != nil || actual != s.helperExpected {
		return errors.New("helper hash")
	}
	return nil
}

func trustedPrivateDirectory(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Uid == uint32(os.Geteuid()) && stat.Mode&0o7777 == 0o700
}

func trustedStagedRegular(stat unix.Stat_t, mode uint32) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Uid == uint32(os.Geteuid()) && stat.Mode&0o7777 == uint16(mode) && stat.Nlink == 1
}

func sameDirectoryObject(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid
}

func readStagedRelative(rootFD int, relative string, mode uint32) ([]byte, error) {
	return readStagedRelativeContext(context.Background(), rootFD, relative, mode)
}

func readStagedRelativeContext(ctx context.Context, rootFD int, relative string, mode uint32) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("context")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	parentFD := rootFD
	parent := newDescriptorOwner(-1)
	defer parent.close()
	for _, part := range parts[:len(parts)-1] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		next, err := unix.Openat(parentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		if err := parent.replace(next); err != nil {
			return nil, err
		}
		parentFD = parent.fd
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(parentFD, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	leaf := newDescriptorOwner(fd)
	defer leaf.close()
	var stat unix.Stat_t
	if unix.Fstat(leaf.fd, &stat) != nil || !trustedStagedRegular(stat, mode) {
		return nil, errors.New("mode")
	}
	file := os.NewFile(uintptr(leaf.release()), relative)
	defer file.Close()
	return readRetainedFileContext(ctx, file, MaxComponentFileBytes)
}

func (s *StagedResources) OpenHelperExecutable() (*os.File, error) {
	return s.OpenHelperExecutableContext(context.Background())
}

func (s *StagedResources) OpenHelperExecutableContext(ctx context.Context) (*os.File, error) {
	if s == nil || s.helperFile == nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	if ctx == nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	if err := s.RevalidateContext(ctx); err != nil {
		return nil, err
	}
	if s.helperExecFile == nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	// The child inherits a readable identity guard at fd 3; Darwin cannot execute
	// it through /dev/fd. The separately retained O_EXEC descriptor remains part
	// of every launch-boundary identity check.
	fd, err := unix.Dup(int(s.helperFile.Fd()))
	if err != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	if err := ctx.Err(); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), "sbr-helper-exec"), nil
}

func (s *StagedResources) VerifyHelperProcessPath(ctx context.Context, path string) error {
	if s == nil || path != s.HelperPath {
		return codedError("SBR_HELPER_UNTRUSTED")
	}
	if err := s.RevalidateContext(ctx); err != nil {
		return err
	}
	opened, err := openSecureContext(ctx, path, MaxComponentFileBytes)
	actual, hashErr := sha256BytesContext(ctx, opened.bytes)
	if err != nil || hashErr != nil || actual != s.helperExpected {
		return codedError("SBR_HELPER_UNTRUSTED")
	}
	return nil
}

func sha256BytesContext(ctx context.Context, data []byte) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if ctx == nil {
		return result, errors.New("context")
	}
	hasher := sha256.New()
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		end := offset + 64<<10
		if end > len(data) {
			end = len(data)
		}
		_, _ = hasher.Write(data[offset:end])
		offset = end
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	copy(result[:], hasher.Sum(nil))
	return result, nil
}

func (s *StagedResources) CreatePrivateRuntimeFile(name string, data []byte) (*os.File, error) {
	return s.CreatePrivateRuntimeFileContext(context.Background(), name, data)
}

func (s *StagedResources) CreatePrivateRuntimeFileContext(ctx context.Context, name string, data []byte) (*os.File, error) {
	if s == nil || s.rootFile == nil || name != "sandbox.sb" || len(data) > MaxProfileBytes {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	fd, err := unix.Openat(int(s.rootFile.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	file := os.NewFile(uintptr(fd), name)
	s.createdFiles = append(s.createdFiles, name)
	if err = writeAllFileContext(ctx, file, data); err != nil {
		file.Close()
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	if err = ctx.Err(); err != nil {
		file.Close()
		return nil, err
	}
	if err = unix.Fsync(int(s.rootFile.Fd())); err != nil {
		file.Close()
		return nil, codedError("SBR_HELPER_UNAVAILABLE")
	}
	if err = ctx.Err(); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func writeAllFile(file *os.File, data []byte) error {
	return writeAllFileContext(context.Background(), file, data)
}

func writeAllFileContext(ctx context.Context, file *os.File, data []byte) error {
	if err := writeFullyAndSyncContext(ctx, data, file.Write, file.Sync); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o7777 != 0o600 || stat.Size != int64(len(data)) {
		return errors.New("identity")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := file.Seek(0, 0)
	if err != nil {
		return err
	}
	return ctx.Err()
}

func readRetainedFile(file *os.File, maximum int64) ([]byte, error) {
	return readRetainedFileContext(context.Background(), file, maximum)
}

func readRetainedFileContext(ctx context.Context, file *os.File, maximum int64) ([]byte, error) {
	if file == nil {
		return nil, errors.New("closed")
	}
	var before unix.Stat_t
	if unix.Fstat(int(file.Fd()), &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != uint32(os.Geteuid()) || before.Mode&0o022 != 0 || before.Nlink != 1 || before.Size < 0 || before.Size > maximum {
		return nil, errors.New("authority")
	}
	data := make([]byte, before.Size)
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := offset + 64<<10
		if end > len(data) {
			end = len(data)
		}
		read, err := unix.Pread(int(file.Fd()), data[offset:end], int64(offset))
		if err != nil || read <= 0 {
			return nil, errors.New("read")
		}
		offset += read
	}
	var trailing [1]byte
	if read, err := unix.Pread(int(file.Fd()), trailing[:], before.Size); err != nil || read != 0 {
		return nil, errors.New("size")
	}
	var after unix.Stat_t
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if unix.Fstat(int(file.Fd()), &after) != nil || !sameStat(before, after) {
		return nil, errors.New("changed")
	}
	return data, nil
}

func cleanupStagedResources(s *StagedResources, runtimeName string) error {
	var failure error
	if s.helperFile != nil {
		if err := s.helperFile.Close(); err != nil {
			failure = err
		}
		s.helperFile = nil
	}
	if s.helperExecFile != nil {
		if err := s.helperExecFile.Close(); err != nil && failure == nil {
			failure = err
		}
		s.helperExecFile = nil
	}
	sort.Slice(s.createdFiles, func(i, j int) bool {
		return strings.Count(s.createdFiles[i], "/") > strings.Count(s.createdFiles[j], "/")
	})
	for _, relative := range s.createdFiles {
		if err := unlinkRelative(int(s.rootFile.Fd()), relative, false); err != nil && !errors.Is(err, unix.ENOENT) && failure == nil {
			failure = err
		}
	}
	sort.Slice(s.createdDirs, func(i, j int) bool {
		return strings.Count(s.createdDirs[i], "/") > strings.Count(s.createdDirs[j], "/")
	})
	for _, relative := range s.createdDirs {
		if err := unlinkRelative(int(s.rootFile.Fd()), relative, true); err != nil && !errors.Is(err, unix.ENOENT) && failure == nil {
			failure = err
		}
	}
	rootFD := int(s.rootFile.Fd())
	baseFD := int(s.baseFile.Fd())
	if err := unix.Fsync(rootFD); err != nil && failure == nil {
		failure = err
	}
	if err := s.rootFile.Close(); err != nil && failure == nil {
		failure = err
	}
	s.rootFile = nil
	if err := unix.Unlinkat(baseFD, runtimeName, unix.AT_REMOVEDIR); err != nil && failure == nil {
		failure = err
	}
	if err := unix.Fsync(baseFD); err != nil && failure == nil {
		failure = err
	}
	if err := s.baseFile.Close(); err != nil && failure == nil {
		failure = err
	}
	s.baseFile = nil
	if failure != nil {
		return codedError("SBR_HELPER_UNAVAILABLE")
	}
	return nil
}

func unlinkRelative(rootFD int, relative string, directory bool) error {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	parentFD := rootFD
	parent := newDescriptorOwner(-1)
	defer parent.close()
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(parentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		if err := parent.replace(next); err != nil {
			return err
		}
		parentFD = parent.fd
	}
	flags := 0
	if directory {
		flags = unix.AT_REMOVEDIR
	}
	return unix.Unlinkat(parentFD, parts[len(parts)-1], flags)
}
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func openTrustedBase(path string) (int, error) {
	return openTrustedBaseContext(context.Background(), path)
}
func openTrustedBaseContext(ctx context.Context, path string) (int, error) {
	return openOwnedDirectoryContext(ctx, path, true)
}
func openOwnedDirectory(path string, private bool) (int, error) {
	return openOwnedDirectoryContext(context.Background(), path, private)
}
func openOwnedDirectoryContext(ctx context.Context, path string, private bool) (int, error) {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return -1, errors.New("context")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	parent := newDescriptorOwner(root)
	defer parent.close()
	euid := uint32(os.Geteuid())
	for index, part := range parts {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		fd, openErr := unix.Openat(parent.fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return -1, openErr
		}
		opened := newDescriptorOwner(fd)
		defer opened.close()
		var stat unix.Stat_t
		leaf := index == len(parts)-1
		if unix.Fstat(fd, &stat) != nil || (stat.Uid != 0 && stat.Uid != euid) || stat.Mode&0o022 != 0 || (leaf && stat.Uid != euid) || (leaf && private && stat.Mode&0o7777 != 0o700) {
			return -1, errors.New("untrusted base")
		}
		if err := parent.replace(opened.release()); err != nil {
			return -1, err
		}
	}
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	return parent.release(), nil
}

func writeStagedAt(directory int, name string, data []byte, mode os.FileMode) error {
	return writeStagedAtContext(context.Background(), directory, name, data, mode)
}

func writeStagedAtContext(ctx context.Context, directory int, name string, data []byte, mode os.FileMode) error {
	fd, err := unix.Openat(directory, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode))
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return writeFullyAndSyncContext(ctx, data, func(value []byte) (int, error) { return unix.Write(fd, value) }, func() error { return unix.Fsync(fd) })
}

func writeFullyAndSync(data []byte, write func([]byte) (int, error), sync func() error) error {
	return writeFullyAndSyncContext(context.Background(), data, write, sync)
}

func writeFullyAndSyncContext(ctx context.Context, data []byte, write func([]byte) (int, error), sync func() error) error {
	if write == nil || sync == nil {
		return errors.New("write")
	}
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := offset + 64<<10
		if end > len(data) {
			end = len(data)
		}
		written, err := write(data[offset:end])
		if err != nil || written <= 0 || written > end-offset {
			return errors.New("write")
		}
		offset += written
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sync(); err != nil {
		return err
	}
	return ctx.Err()
}

func writeComponentAt(rootFD int, relative string, data []byte, mode os.FileMode) error {
	return writeComponentAtContext(context.Background(), rootFD, relative, data, mode)
}

func writeComponentAtContext(ctx context.Context, rootFD int, relative string, data []byte, mode os.FileMode) error {
	if ctx == nil {
		return errors.New("context")
	}
	segments := append([]string{"component"}, strings.Split(relative, "/")...)
	parentFD := rootFD
	parent := newDescriptorOwner(-1)
	defer parent.close()
	for _, segment := range segments[:len(segments)-1] {
		if err := ctx.Err(); err != nil {
			return err
		}
		if mkdirErr := unix.Mkdirat(parentFD, segment, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
			return mkdirErr
		}
		next, openErr := unix.Openat(parentFD, segment, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return openErr
		}
		var stat unix.Stat_t
		if unix.Fstat(next, &stat) != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o7777 != 0o700 {
			unix.Close(next)
			return errors.New("component directory")
		}
		if err := parent.replace(next); err != nil {
			return err
		}
		parentFD = parent.fd
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := writeStagedAtContext(ctx, parentFD, segments[len(segments)-1], data, mode)
	if err == nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		err = unix.Fsync(parentFD)
		if err == nil {
			err = ctx.Err()
		}
	}
	return err
}
