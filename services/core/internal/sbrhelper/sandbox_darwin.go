//go:build darwin && arm64 && cgo

package sbrhelper

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/sys/unix"
)

const stagingRootPrefix = "tammy-sbr-runtime-"

var (
	ErrSandboxProfileInvalid   = errors.New("SBR_SANDBOX_PROFILE_INVALID")
	ErrSandboxAuthorityChanged = errors.New("SBR_SANDBOX_AUTHORITY_CHANGED")
	ErrSandboxAuthorityClosed  = errors.New("SBR_SANDBOX_AUTHORITY_CLOSED")
)

type SandboxProfileInput struct {
	TrustedBase         string
	StagedRoot          string
	StagedExecutables   []string
	StagedReadOnlyFiles []string
	SelectedReadFiles   []string
}

type SandboxProfile struct {
	contents string
	guard    *SandboxProfileGuard
}

// PrepareSpawn is the launcher boundary: it revalidates every retained path
// immediately before sandbox-exec consumption. The next launcher task must
// hold the guard through spawn. Rendering does not authenticate component bytes.
func (p SandboxProfile) PrepareSpawn() (string, error) {
	if p.guard == nil {
		return "", ErrSandboxAuthorityClosed
	}
	if err := p.guard.Revalidate(); err != nil {
		return "", err
	}
	return p.contents, nil
}

func (SandboxProfile) FileMode() fs.FileMode { return 0o600 }
func (SandboxProfile) OwnerUID() int         { return os.Geteuid() }

type SandboxProfileGuard struct {
	mu    sync.Mutex
	paths []*retainedPath
	close bool
}

func RenderDevelopmentSandboxProfile(input SandboxProfileInput) (SandboxProfile, *SandboxProfileGuard, error) {
	if len(input.StagedExecutables) == 0 || !validTrustedBasePath(input.TrustedBase) || !validAbsolute(input.StagedRoot) ||
		filepath.Dir(input.StagedRoot) != input.TrustedBase || !validRuntimeName(filepath.Base(input.StagedRoot)) {
		return SandboxProfile{}, nil, ErrSandboxProfileInvalid
	}
	all := append([]string{input.TrustedBase, input.StagedRoot}, input.StagedExecutables...)
	all = append(all, input.StagedReadOnlyFiles...)
	selectedStart := len(all)
	all = append(all, input.SelectedReadFiles...)
	guards := make([]*retainedPath, 0, len(all))
	fail := func() (SandboxProfile, *SandboxProfileGuard, error) {
		for _, guard := range guards {
			_ = guard.Close()
		}
		return SandboxProfile{}, nil, ErrSandboxProfileInvalid
	}
	for index, path := range all {
		regular := index >= 2
		guard, err := openRetainedPath(path, regular)
		if err != nil || (index < selectedStart && !guard.trustedAncestors()) {
			if guard != nil {
				_ = guard.Close()
			}
			return fail()
		}
		guards = append(guards, guard)
	}
	euid := uint64(os.Geteuid())
	if !guards[0].leafDirectory(euid, 0o700) || !guards[1].leafDirectory(euid, 0o700) {
		return fail()
	}
	root := filepath.Clean(input.StagedRoot) + string(filepath.Separator)
	for index, path := range input.StagedExecutables {
		if !strings.HasPrefix(path, root) || !guards[2+index].leafRegularExact(euid, 0o500) {
			return fail()
		}
	}
	readOnlyStart := 2 + len(input.StagedExecutables)
	for index, path := range input.StagedReadOnlyFiles {
		if !strings.HasPrefix(path, root) || !guards[readOnlyStart+index].leafRegularExact(euid, 0o400) {
			return fail()
		}
	}
	for _, path := range input.SelectedReadFiles {
		if strings.HasPrefix(path, root) {
			return fail()
		}
	}
	guard := &SandboxProfileGuard{paths: guards}
	var profile strings.Builder
	profile.WriteString("(version 1)\n(import \"system.sb\")\n(deny default)\n")
	profile.WriteString("(allow process-exec")
	for _, path := range input.StagedExecutables {
		profile.WriteString(" (literal \"")
		profile.WriteString(schemeString(path))
		profile.WriteString("\")")
	}
	profile.WriteString(")\n(allow file-read*")
	readable := append([]string{}, input.StagedExecutables...)
	readable = append(readable, input.StagedReadOnlyFiles...)
	readable = append(readable, input.SelectedReadFiles...)
	for _, path := range readable {
		profile.WriteString(" (literal \"")
		profile.WriteString(schemeString(path))
		profile.WriteString("\")")
	}
	profile.WriteString(")\n")
	profile.WriteString("(allow file-read-data file-read-metadata (literal \"/dev/null\") (literal \"/dev/urandom\"))\n")
	profile.WriteString("(allow file-write-data (literal \"/dev/null\"))\n")
	// securityd is a platform bootstrap necessity, not item isolation. Future
	// packaging enforces helper-specific Keychain ACL/access-group isolation.
	profile.WriteString("(allow mach-lookup (global-name \"com.apple.securityd\"))\n")
	profile.WriteString("(deny network*)\n")
	result := SandboxProfile{contents: profile.String(), guard: guard}
	if _, err := result.PrepareSpawn(); err != nil {
		_ = guard.Close()
		return SandboxProfile{}, nil, ErrSandboxProfileInvalid
	}
	return result, guard, nil
}

func (g *SandboxProfileGuard) Revalidate() error {
	if g == nil {
		return ErrSandboxAuthorityClosed
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.close {
		return ErrSandboxAuthorityClosed
	}
	for _, path := range g.paths {
		if err := path.revalidate(); err != nil {
			return err
		}
	}
	return nil
}

func (g *SandboxProfileGuard) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.close {
		return nil
	}
	for _, path := range g.paths {
		_ = path.Close()
	}
	g.close = true
	return nil
}

type pathIdentity struct {
	dev, ino, mode, uid uint64
	size                int64
	mtimeSec, mtimeNsec int64
	ctimeSec, ctimeNsec int64
}

type retainedComponent struct {
	name      string
	directory bool
	fd        int
	identity  pathIdentity
}

type retainedPath struct {
	mu         sync.Mutex
	components []retainedComponent
	closed     bool
}

func openRetainedPath(path string, regular bool) (*retainedPath, error) {
	parts, ok := pathParts(path)
	if !ok || (regular && len(parts) == 0) {
		return nil, ErrSandboxProfileInvalid
	}
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrSandboxProfileInvalid
	}
	rootID, ok := descriptorIdentity(root)
	if !ok {
		_ = unix.Close(root)
		return nil, ErrSandboxProfileInvalid
	}
	result := &retainedPath{components: []retainedComponent{{directory: true, fd: root, identity: rootID}}}
	parent := root
	for index, name := range parts {
		directory := index < len(parts)-1 || !regular
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
		if directory {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(parent, name, flags, 0)
		if err != nil {
			_ = result.Close()
			return nil, ErrSandboxProfileInvalid
		}
		identity, valid := descriptorIdentity(fd)
		var pathStat unix.Stat_t
		pathValid := unix.Fstatat(parent, name, &pathStat, unix.AT_SYMLINK_NOFOLLOW) == nil
		pathIdentity := statIdentity(&pathStat)
		typeMode := uint32(identity.mode) & unix.S_IFMT
		expected := uint32(unix.S_IFREG)
		if directory {
			expected = uint32(unix.S_IFDIR)
		}
		if !valid || !pathValid || !sameAuthority(pathIdentity, identity, directory) || typeMode != expected {
			_ = unix.Close(fd)
			_ = result.Close()
			return nil, ErrSandboxProfileInvalid
		}
		result.components = append(result.components, retainedComponent{name: name, directory: directory, fd: fd, identity: identity})
		parent = fd
	}
	return result, nil
}

func (p *retainedPath) revalidate() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrSandboxAuthorityClosed
	}
	opened := make([]int, 0, len(p.components))
	defer func() {
		for index := len(opened) - 1; index >= 0; index-- {
			_ = unix.Close(opened[index])
		}
	}()
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return ErrSandboxAuthorityChanged
	}
	opened = append(opened, root)
	identity, ok := descriptorIdentity(root)
	if !ok || !sameAuthority(p.components[0].identity, identity, true) {
		return ErrSandboxAuthorityChanged
	}
	parent := root
	for index := 1; index < len(p.components); index++ {
		expected := p.components[index]
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
		if expected.directory {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(parent, expected.name, flags, 0)
		if err != nil {
			return ErrSandboxAuthorityChanged
		}
		opened = append(opened, fd)
		identity, ok = descriptorIdentity(fd)
		var pathStat unix.Stat_t
		if !ok || unix.Fstatat(parent, expected.name, &pathStat, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameAuthority(statIdentity(&pathStat), identity, expected.directory) || !sameAuthority(expected.identity, identity, expected.directory) {
			return ErrSandboxAuthorityChanged
		}
		parent = fd
	}
	return nil
}

func (p *retainedPath) trustedAncestors() bool {
	euid := uint64(os.Geteuid())
	for index, component := range p.components {
		if !component.directory {
			continue
		}
		mode := component.identity.mode & 0o7777
		if component.identity.uid != 0 && component.identity.uid != euid || mode&0o022 != 0 {
			return false
		}
		if index == len(p.components)-1 && mode == 0 {
			return false
		}
	}
	return true
}

func (p *retainedPath) leafDirectory(uid uint64, mode uint64) bool {
	leaf := p.components[len(p.components)-1].identity
	return leaf.mode&unix.S_IFMT == unix.S_IFDIR && leaf.uid == uid && leaf.mode&0o7777 == mode
}

func (p *retainedPath) leafRegularExact(uid, mode uint64) bool {
	leaf := p.components[len(p.components)-1].identity
	return leaf.mode&unix.S_IFMT == unix.S_IFREG && leaf.uid == uid && leaf.mode&0o7777 == mode
}

func (p *retainedPath) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	for index := len(p.components) - 1; index >= 0; index-- {
		_ = unix.Close(p.components[index].fd)
		p.components[index].fd = -1
	}
	p.closed = true
	return nil
}

func descriptorIdentity(fd int) (pathIdentity, bool) {
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil {
		return pathIdentity{}, false
	}
	return statIdentity(&stat), true
}

func statIdentity(stat *unix.Stat_t) pathIdentity {
	return pathIdentity{dev: uint64(stat.Dev), ino: uint64(stat.Ino), mode: uint64(stat.Mode), uid: uint64(stat.Uid), size: stat.Size,
		mtimeSec: stat.Mtim.Sec, mtimeNsec: stat.Mtim.Nsec, ctimeSec: stat.Ctim.Sec, ctimeNsec: stat.Ctim.Nsec}
}

func sameAuthority(expected, actual pathIdentity, directory bool) bool {
	if !directory {
		return expected == actual
	}
	return expected.dev == actual.dev && expected.ino == actual.ino && expected.mode == actual.mode && expected.uid == actual.uid
}

func validAbsolute(path string) bool {
	_, ok := pathParts(path)
	return ok
}

func validTrustedBasePath(path string) bool {
	if !validAbsolute(path) {
		return false
	}
	home, _ := os.UserHomeDir()
	for _, broad := range []string{"/", "/tmp", "/private", "/private/tmp", "/Applications", "/Library", "/System", "/Users", "/Volumes", home} {
		if broad != "" && path == broad {
			return false
		}
	}
	return true
}

func pathParts(path string) ([]string, bool) {
	if path == "" || len(path) > 4<<10 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false
	}
	for _, value := range path {
		if unicode.IsControl(value) {
			return nil, false
		}
	}
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return nil, true
	}
	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, false
		}
	}
	return parts, true
}

func validRuntimeName(name string) bool {
	if !strings.HasPrefix(name, stagingRootPrefix) {
		return false
	}
	suffix := strings.TrimPrefix(name, stagingRootPrefix)
	if len(suffix) < 6 || len(suffix) > 48 {
		return false
	}
	for _, value := range suffix {
		if !((value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || value == '-' || value == '_') {
			return false
		}
	}
	return true
}

func schemeString(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(value)
}
