//go:build darwin && cgo

package workspace

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

static CFStringRef tammy_anchor_string(const char *value, UInt32 length) {
    return CFStringCreateWithBytes(kCFAllocatorDefault, (const UInt8 *)value,
        length, kCFStringEncodingUTF8, false);
}

static CFDictionaryRef tammy_anchor_query(const char *account, UInt32 account_len) {
    const char service_bytes[] = "com.tammy.attempt-journal-anchor.v1";
    CFStringRef service = tammy_anchor_string(service_bytes, sizeof(service_bytes) - 1);
    CFStringRef account_value = tammy_anchor_string(account, account_len);
    if (service == NULL || account_value == NULL) {
        if (service != NULL) CFRelease(service);
        if (account_value != NULL) CFRelease(account_value);
        return NULL;
    }
    const void *keys[] = {kSecClass, kSecAttrService, kSecAttrAccount};
    const void *values[] = {kSecClassGenericPassword, service, account_value};
    CFDictionaryRef query = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 3,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFRelease(service);
    CFRelease(account_value);
    return query;
}

static OSStatus tammy_anchor_get(const char *account, UInt32 account_len,
    UInt32 *value_len, void **value) {
    CFDictionaryRef base = tammy_anchor_query(account, account_len);
    if (base == NULL) return errSecAllocate;
    CFMutableDictionaryRef query = CFDictionaryCreateMutableCopy(kCFAllocatorDefault, 0, base);
    CFRelease(base);
    if (query == NULL) return errSecAllocate;
    CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
    CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);
    CFTypeRef result = NULL;
    OSStatus status = SecItemCopyMatching(query, &result);
    CFRelease(query);
    if (status != errSecSuccess) return status;
    if (result == NULL || CFGetTypeID(result) != CFDataGetTypeID()) {
        if (result != NULL) CFRelease(result);
        return errSecDecode;
    }
    CFDataRef data = (CFDataRef)result;
    CFIndex length = CFDataGetLength(data);
    if (length <= 0 || length > UINT32_MAX) {
        CFRelease(result);
        return errSecDecode;
    }
    void *copy = malloc((size_t)length);
    if (copy == NULL) {
        CFRelease(result);
        return errSecAllocate;
    }
    memcpy(copy, CFDataGetBytePtr(data), (size_t)length);
    *value_len = (UInt32)length;
    *value = copy;
    CFRelease(result);
    return errSecSuccess;
}

static OSStatus tammy_anchor_create(const char *account, UInt32 account_len,
    const void *value, UInt32 value_len) {
    CFDictionaryRef base = tammy_anchor_query(account, account_len);
    CFDataRef data = CFDataCreate(kCFAllocatorDefault, (const UInt8 *)value, value_len);
    if (base == NULL || data == NULL) {
        if (base != NULL) CFRelease(base);
        if (data != NULL) CFRelease(data);
        return errSecAllocate;
    }
    CFMutableDictionaryRef addition = CFDictionaryCreateMutableCopy(kCFAllocatorDefault, 0, base);
    CFRelease(base);
    if (addition == NULL) {
        CFRelease(data);
        return errSecAllocate;
    }
    CFDictionarySetValue(addition, kSecValueData, data);
    OSStatus status = SecItemAdd(addition, NULL);
    CFRelease(addition);
    CFRelease(data);
    return status;
}

static OSStatus tammy_anchor_update(const char *account, UInt32 account_len,
    const void *value, UInt32 value_len) {
    CFDictionaryRef query = tammy_anchor_query(account, account_len);
    CFDataRef data = CFDataCreate(kCFAllocatorDefault, (const UInt8 *)value, value_len);
    if (query == NULL || data == NULL) {
        if (query != NULL) CFRelease(query);
        if (data != NULL) CFRelease(data);
        return errSecAllocate;
    }
    const void *keys[] = {kSecValueData};
    const void *values[] = {data};
    CFDictionaryRef updates = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    OSStatus status = updates == NULL ? errSecAllocate : SecItemUpdate(query, updates);
    if (updates != NULL) CFRelease(updates);
    CFRelease(data);
    CFRelease(query);
    return status;
}
*/
import "C"

import (
	"bytes"
	"runtime"
	"time"
	"unsafe"
)

type darwinAnchorStore struct{}

const platformAnchorMarkerSuffix = "/initialized"

func NewPlatformAnchorStore() (AnchorStore, error) {
	return darwinAnchorStore{}, nil
}

func (darwinAnchorStore) AcquireAttemptJournalLock(label string, timeout time.Duration) (attemptJournalLease, error) {
	path, err := platformAttemptJournalLockPath(label)
	if err != nil {
		return nil, err
	}
	lock, err := acquireAttemptJournalFileLock(path, timeout)
	if err != nil {
		return nil, err
	}
	lock.label = label
	return lock, nil
}

func loadDarwinAnchorAccount(label string) ([]byte, bool, error) {
	account := []byte(label)
	var length C.UInt32
	var data unsafe.Pointer
	status := C.tammy_anchor_get((*C.char)(unsafe.Pointer(&account[0])), C.UInt32(len(account)), &length, &data)
	runtime.KeepAlive(account)
	if status == C.errSecItemNotFound {
		return nil, false, nil
	}
	if status != C.errSecSuccess || data == nil || length == 0 {
		if data != nil {
			C.free(data)
		}
		return nil, true, ErrAttemptJournalAuthentication
	}
	defer C.free(data)
	return C.GoBytes(data, C.int(length)), true, nil
}

func createDarwinAnchorAccount(label string, value []byte) error {
	account := []byte(label)
	status := C.tammy_anchor_create(
		(*C.char)(unsafe.Pointer(&account[0])), C.UInt32(len(account)),
		unsafe.Pointer(&value[0]), C.UInt32(len(value)),
	)
	runtime.KeepAlive(account)
	runtime.KeepAlive(value)
	if status != C.errSecSuccess {
		return ErrAttemptJournalAuthentication
	}
	return nil
}

func (darwinAnchorStore) Load(label string, lease attemptJournalLease) ([]byte, bool, error) {
	if label == "" || !validPlatformAttemptJournalLease(lease, label) {
		return nil, false, ErrAttemptJournalAuthentication
	}
	marker, markerFound, err := loadDarwinAnchorAccount(label + platformAnchorMarkerSuffix)
	if err != nil || (markerFound && !bytes.Equal(marker, []byte{1})) {
		Zero(marker)
		return nil, markerFound, ErrAttemptJournalAuthentication
	}
	Zero(marker)
	value, valueFound, err := loadDarwinAnchorAccount(label)
	if err != nil || (valueFound && len(value) != attemptJournalAnchorSize) {
		Zero(value)
		return nil, markerFound || valueFound, ErrAttemptJournalAuthentication
	}
	if markerFound {
		return value, true, nil
	}
	if !valueFound {
		return nil, false, nil
	}
	// A crash can leave the create-only anchor durable before its marker. Repair
	// that one ordering window before allowing the journal to open.
	if err := createDarwinAnchorAccount(label+platformAnchorMarkerSuffix, []byte{1}); err != nil {
		Zero(value)
		return nil, true, err
	}
	return value, true, nil
}

func (darwinAnchorStore) Initialize(label string, value []byte, lease attemptJournalLease) error {
	if label == "" || !validPlatformAttemptJournalLease(lease, label) || len(value) != attemptJournalAnchorSize {
		return ErrAttemptJournalAuthentication
	}
	previous, initialized, err := (darwinAnchorStore{}).Load(label, lease)
	Zero(previous)
	if err != nil || initialized {
		return ErrAttemptJournalAuthentication
	}
	if err := createDarwinAnchorAccount(label, value); err != nil {
		return err
	}
	return createDarwinAnchorAccount(label+platformAnchorMarkerSuffix, []byte{1})
}

func (darwinAnchorStore) Save(label string, value []byte, lease attemptJournalLease) error {
	if label == "" || !validPlatformAttemptJournalLease(lease, label) || len(value) != attemptJournalAnchorSize {
		return ErrAttemptJournalAuthentication
	}
	previous, initialized, err := (darwinAnchorStore{}).Load(label, lease)
	if err != nil || !initialized || len(previous) != attemptJournalAnchorSize {
		Zero(previous)
		return ErrAttemptJournalAuthentication
	}
	current, currentErr := decodeAttemptJournalAnchor(previous)
	next, nextErr := decodeAttemptJournalAnchor(value)
	Zero(previous)
	if currentErr != nil || nextErr != nil || next.Sequence != current.Sequence+1 {
		return ErrAttemptJournalAuthentication
	}
	account := []byte(label)
	status := C.tammy_anchor_update(
		(*C.char)(unsafe.Pointer(&account[0])), C.UInt32(len(account)),
		unsafe.Pointer(&value[0]), C.UInt32(len(value)),
	)
	runtime.KeepAlive(account)
	runtime.KeepAlive(value)
	if status != C.errSecSuccess {
		return ErrAttemptJournalAuthentication
	}
	return nil
}
