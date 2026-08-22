//go:build darwin && arm64 && cgo

package sbrhelper

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <libproc.h>
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	unsigned char cdhash[64];
	CFIndex cdhash_len;
	char identifier[256];
	CFIndex identifier_len;
	char team[256];
	CFIndex team_len;
} tammy_code_identity;

static OSStatus tammy_copy_string(CFStringRef value, char *output, CFIndex capacity, CFIndex *length) {
	if (value == NULL) {
		*length = 0;
		return errSecSuccess;
	}
	if (CFGetTypeID(value) != CFStringGetTypeID()) return errSecParam;
	CFIndex characters = CFStringGetLength(value);
	CFIndex used = 0;
	CFIndex converted = CFStringGetBytes(value, CFRangeMake(0, characters), kCFStringEncodingUTF8, 0, false, (UInt8 *)output, capacity - 1, &used);
	if (converted != characters || used < 0 || used >= capacity) return errSecBufferTooSmall;
	output[used] = '\0';
	*length = used;
	return errSecSuccess;
}

static OSStatus tammy_fill_code_identity(SecCodeRef code, tammy_code_identity *output) {
	memset(output, 0, sizeof(*output));
	CFDictionaryRef info = NULL;
	OSStatus status = SecCodeCopySigningInformation(code, kSecCSSigningInformation, &info);
	if (status != errSecSuccess) return status;
	CFDataRef cdhash = (CFDataRef)CFDictionaryGetValue(info, kSecCodeInfoUnique);
	if (cdhash == NULL || CFGetTypeID(cdhash) != CFDataGetTypeID()) {
		CFRelease(info);
		return errSecCSUnsigned;
	}
	CFIndex length = CFDataGetLength(cdhash);
	if (length <= 0 || length > (CFIndex)sizeof(output->cdhash)) {
		CFRelease(info);
		return errSecParam;
	}
	memcpy(output->cdhash, CFDataGetBytePtr(cdhash), (size_t)length);
	output->cdhash_len = length;
	status = tammy_copy_string((CFStringRef)CFDictionaryGetValue(info, kSecCodeInfoIdentifier), output->identifier, sizeof(output->identifier), &output->identifier_len);
	if (status == errSecSuccess) {
		status = tammy_copy_string((CFStringRef)CFDictionaryGetValue(info, kSecCodeInfoTeamIdentifier), output->team, sizeof(output->team), &output->team_len);
	}
	CFRelease(info);
	return status;
}

static OSStatus tammy_static_code_identity(const char *path, size_t path_len, tammy_code_identity *output) {
	CFURLRef url = CFURLCreateFromFileSystemRepresentation(kCFAllocatorDefault, (const UInt8 *)path, (CFIndex)path_len, false);
	if (url == NULL) return errSecParam;
	SecStaticCodeRef code = NULL;
	OSStatus status = SecStaticCodeCreateWithPath(url, kSecCSDefaultFlags, &code);
	CFRelease(url);
	if (status != errSecSuccess) return status;
	status = tammy_fill_code_identity((SecCodeRef)code, output);
	CFRelease(code);
	return status;
}

static OSStatus tammy_live_code_identity(pid_t pid, tammy_code_identity *output) {
	CFNumberRef pid_value = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &pid);
	if (pid_value == NULL) return errSecParam;
	const void *keys[] = { kSecGuestAttributePid };
	const void *values[] = { pid_value };
	CFDictionaryRef attributes = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFRelease(pid_value);
	if (attributes == NULL) return errSecParam;
	SecCodeRef code = NULL;
	OSStatus status = SecCodeCopyGuestWithAttributes(NULL, attributes, kSecCSDefaultFlags, &code);
	CFRelease(attributes);
	if (status != errSecSuccess) return status;
	status = tammy_fill_code_identity(code, output);
	CFRelease(code);
	return status;
}
*/
import "C"

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
	"golang.org/x/sys/unix"
)

func captureStaticCodeIdentity(path string) (codeIdentity, error) {
	if path == "" {
		return codeIdentity{}, errors.New("code path")
	}
	pathBytes := []byte(path)
	cPath := C.CBytes(pathBytes)
	defer C.free(cPath)
	var identity C.tammy_code_identity
	if status := C.tammy_static_code_identity((*C.char)(cPath), C.size_t(len(pathBytes)), &identity); status != C.errSecSuccess {
		return codeIdentity{}, errors.New("static code identity unavailable")
	}
	return codeIdentityFromC(identity)
}

func captureLiveCodeIdentity(pid int) (codeIdentity, error) {
	if pid <= 0 {
		return codeIdentity{}, errors.New("process identity")
	}
	var identity C.tammy_code_identity
	if status := C.tammy_live_code_identity(C.pid_t(pid), &identity); status != C.errSecSuccess {
		return codeIdentity{}, errors.New("live code identity unavailable")
	}
	return codeIdentityFromC(identity)
}

func captureStaticCodeIdentityContext(ctx context.Context, path string) (codeIdentity, error) {
	if ctx == nil {
		return codeIdentity{}, errors.New("context")
	}
	if err := ctx.Err(); err != nil {
		return codeIdentity{}, err
	}
	identity, err := captureStaticCodeIdentity(path)
	if err != nil {
		return codeIdentity{}, err
	}
	if err := ctx.Err(); err != nil {
		return codeIdentity{}, err
	}
	return identity, nil
}

func captureLiveCodeIdentityContext(ctx context.Context, pid int) (codeIdentity, error) {
	if ctx == nil {
		return codeIdentity{}, errors.New("context")
	}
	if err := ctx.Err(); err != nil {
		return codeIdentity{}, err
	}
	identity, err := captureLiveCodeIdentity(pid)
	if err != nil {
		return codeIdentity{}, err
	}
	if err := ctx.Err(); err != nil {
		return codeIdentity{}, err
	}
	return identity, nil
}

func codeIdentityFromC(identity C.tammy_code_identity) (codeIdentity, error) {
	cdHashLength := int(identity.cdhash_len)
	identifierLength := int(identity.identifier_len)
	teamLength := int(identity.team_len)
	if cdHashLength < 1 || cdHashLength > 64 || identifierLength < 0 || identifierLength > 255 || teamLength < 0 || teamLength > 255 {
		return codeIdentity{}, errors.New("invalid code identity")
	}
	return codeIdentity{
		cdHash:     C.GoBytes(unsafe.Pointer(&identity.cdhash[0]), C.int(cdHashLength)),
		identifier: C.GoStringN((*C.char)(unsafe.Pointer(&identity.identifier[0])), C.int(identifierLength)),
		team:       C.GoStringN((*C.char)(unsafe.Pointer(&identity.team[0])), C.int(teamLength)),
	}, nil
}

func sameCodeIdentity(expected, live codeIdentity) bool {
	if len(expected.cdHash) == 0 || !bytes.Equal(expected.cdHash, live.cdHash) {
		return false
	}
	if expected.identifier != "" && expected.identifier != live.identifier {
		return false
	}
	return expected.team == "" || expected.team == live.team
}

func waitLiveCodeIdentity(ctx context.Context, pid int) (codeIdentity, error) {
	if ctx == nil {
		return codeIdentity{}, errors.New("context")
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return codeIdentity{}, err
		}
		identity, err := captureLiveCodeIdentityContext(ctx, pid)
		if err == nil {
			return identity, nil
		}
		if killErr := unix.Kill(pid, 0); errors.Is(killErr, unix.ESRCH) {
			return codeIdentity{}, errors.New("process exited before authentication")
		}
		select {
		case <-ctx.Done():
			return codeIdentity{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func captureAuthenticatedStagedCodeIdentity(ctx context.Context, staged *sbrprofile.StagedResources) (codeIdentity, error) {
	if staged == nil {
		return codeIdentity{}, errors.New("staged helper")
	}
	if err := staged.RevalidateContext(ctx); err != nil {
		return codeIdentity{}, err
	}
	retained, err := staged.OpenHelperExecutableContext(ctx)
	if err != nil {
		return codeIdentity{}, err
	}
	defer retained.Close()
	identity, err := captureStaticCodeIdentityContext(ctx, "/dev/fd/"+strconv.Itoa(int(retained.Fd())))
	if err != nil {
		return codeIdentity{}, err
	}
	if err := staged.RevalidateContext(ctx); err != nil {
		return codeIdentity{}, err
	}
	return identity, nil
}

func verifyAuthenticatedHelperProcess(ctx context.Context, pid int, staged *sbrprofile.StagedResources, expected codeIdentity, allowInitialSandbox bool) error {
	if ctx == nil || pid <= 0 || staged == nil || len(expected.cdHash) == 0 {
		return errors.New("process authority")
	}
	if err := waitForExpectedLiveCodeIdentity(ctx, pid, expected, staged.HelperPath, allowInitialSandbox); err != nil {
		return err
	}
	return staged.VerifyHelperProcessPath(ctx, staged.HelperPath)
}

func waitForExpectedLiveCodeIdentity(ctx context.Context, pid int, expected codeIdentity, helperPath string, allowInitialSandbox bool) error {
	return waitForExpectedLiveCodeIdentityWithSamplers(ctx, pid, expected, helperPath, allowInitialSandbox, darwinProcessPath, captureLiveCodeIdentityContext)
}

func waitForExpectedLiveCodeIdentityWithSamplers(ctx context.Context, pid int, expected codeIdentity, helperPath string, allowInitialSandbox bool, samplePath func(int) (string, error), sampleIdentity func(context.Context, int) (codeIdentity, error)) error {
	if ctx == nil || pid <= 0 || len(expected.cdHash) == 0 || helperPath == "" {
		return errors.New("process authority")
	}
	if samplePath == nil || sampleIdentity == nil {
		return errors.New("process sampler")
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		firstPath, firstPathErr := samplePath(pid)
		if err := ctx.Err(); err != nil {
			return err
		}
		firstIdentity, firstIdentityErr := sampleIdentity(ctx, pid)
		if err := ctx.Err(); err != nil {
			return err
		}
		secondPath, secondPathErr := samplePath(pid)
		if err := ctx.Err(); err != nil {
			return err
		}
		secondIdentity, secondIdentityErr := sampleIdentity(ctx, pid)
		if err := ctx.Err(); err != nil {
			return err
		}
		if firstPathErr == nil && firstIdentityErr == nil && secondPathErr == nil && secondIdentityErr == nil {
			if firstPath != secondPath || !equalCodeIdentity(firstIdentity, secondIdentity) {
				continue
			}
			if firstPath == helperPath && sameCodeIdentity(expected, firstIdentity) {
				return nil
			}
			initialSandbox := firstPath == "/usr/bin/sandbox-exec" || strings.HasSuffix(firstPath, "/sandbox-exec")
			if !(allowInitialSandbox && initialSandbox) {
				return errors.New("wrong process executable")
			}
		} else if killErr := unix.Kill(pid, 0); errors.Is(killErr, unix.ESRCH) {
			return errors.New("process exited before authentication")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func equalCodeIdentity(left, right codeIdentity) bool {
	return bytes.Equal(left.cdHash, right.cdHash) && left.identifier == right.identifier && left.team == right.team
}

func darwinProcessPath(pid int) (string, error) {
	buffer := make([]byte, int(C.PROC_PIDPATHINFO_MAXSIZE))
	written := C.proc_pidpath(C.int(pid), unsafe.Pointer(&buffer[0]), C.uint32_t(len(buffer)))
	if written <= 0 {
		return "", errors.New("process path unavailable")
	}
	return string(buffer[:bytesBeforeNUL(buffer, int(written))]), nil
}

func bytesBeforeNUL(value []byte, limit int) int {
	if limit > len(value) {
		limit = len(value)
	}
	for index := 0; index < limit; index++ {
		if value[index] == 0 {
			return index
		}
	}
	return limit
}
