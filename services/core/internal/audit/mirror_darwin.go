//go:build darwin && cgo

package audit

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

static CFStringRef tammy_audit_mirror_string(const char *value, UInt32 length) {
    return CFStringCreateWithBytes(kCFAllocatorDefault, (const UInt8 *)value,
        length, kCFStringEncodingUTF8, false);
}

static CFDictionaryRef tammy_audit_mirror_query(const char *service, UInt32 service_len,
    const char *account, UInt32 account_len) {
    CFStringRef service_value = tammy_audit_mirror_string(service, service_len);
    CFStringRef account_value = tammy_audit_mirror_string(account, account_len);
    if (service_value == NULL || account_value == NULL) {
        if (service_value != NULL) CFRelease(service_value);
        if (account_value != NULL) CFRelease(account_value);
        return NULL;
    }
    const void *keys[] = {kSecClass, kSecAttrService, kSecAttrAccount};
    const void *values[] = {kSecClassGenericPassword, service_value, account_value};
    CFDictionaryRef query = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 3,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFRelease(service_value);
    CFRelease(account_value);
    return query;
}

static OSStatus tammy_audit_mirror_put(const char *service, UInt32 service_len,
    const char *account, UInt32 account_len, const void *value, UInt32 value_len) {
    CFDictionaryRef query = tammy_audit_mirror_query(service, service_len, account, account_len);
    CFDataRef data = CFDataCreate(kCFAllocatorDefault, (const UInt8 *)value, value_len);
    if (query == NULL || data == NULL) {
        if (query != NULL) CFRelease(query);
        if (data != NULL) CFRelease(data);
        return errSecAllocate;
    }
    const void *update_keys[] = {kSecValueData};
    const void *update_values[] = {data};
    CFDictionaryRef updates = CFDictionaryCreate(kCFAllocatorDefault, update_keys,
        update_values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    OSStatus status = updates == NULL ? errSecAllocate : SecItemUpdate(query, updates);
    if (status == errSecItemNotFound) {
        CFMutableDictionaryRef addition = CFDictionaryCreateMutableCopy(kCFAllocatorDefault, 0, query);
        if (addition == NULL) {
            status = errSecAllocate;
        } else {
            CFDictionarySetValue(addition, kSecValueData, data);
            status = SecItemAdd(addition, NULL);
            CFRelease(addition);
        }
    }
    if (updates != NULL) CFRelease(updates);
    CFRelease(data);
    CFRelease(query);
    return status;
}

static OSStatus tammy_audit_mirror_get(const char *service, UInt32 service_len,
    const char *account, UInt32 account_len, UInt32 *value_len, void **value) {
    CFDictionaryRef base = tammy_audit_mirror_query(service, service_len, account, account_len);
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
    if (length <= 0 || length > 4096) {
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
*/
import "C"

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const auditMirrorService = "com.tammy.audit-mirror"

type darwinMirrorCredentials struct{}

func NewPlatformMirrorStore() (MirrorStore, error) {
	return newEncodedMirrorStore(darwinMirrorCredentials{}), nil
}

func (darwinMirrorCredentials) put(label string, value []byte) error {
	if label == "" || len(value) == 0 || len(value) > 4096 {
		return ErrMirrorInvalid
	}
	service, account := []byte(auditMirrorService), []byte(label)
	status := C.tammy_audit_mirror_put((*C.char)(unsafe.Pointer(&service[0])), C.UInt32(len(service)),
		(*C.char)(unsafe.Pointer(&account[0])), C.UInt32(len(account)), unsafe.Pointer(&value[0]), C.UInt32(len(value)))
	runtime.KeepAlive(service)
	runtime.KeepAlive(account)
	runtime.KeepAlive(value)
	if status != C.errSecSuccess {
		return ErrMirrorInvalid
	}
	return nil
}

func (darwinMirrorCredentials) get(label string) ([]byte, error) {
	if label == "" {
		return nil, ErrMirrorInvalid
	}
	service, account := []byte(auditMirrorService), []byte(label)
	var length C.UInt32
	var data unsafe.Pointer
	status := C.tammy_audit_mirror_get((*C.char)(unsafe.Pointer(&service[0])), C.UInt32(len(service)),
		(*C.char)(unsafe.Pointer(&account[0])), C.UInt32(len(account)), &length, &data)
	runtime.KeepAlive(service)
	runtime.KeepAlive(account)
	if status == C.errSecItemNotFound {
		return nil, ErrMirrorMissing
	}
	if status != C.errSecSuccess || data == nil || length == 0 {
		return nil, ErrMirrorInvalid
	}
	defer C.free(data)
	return C.GoBytes(data, C.int(length)), nil
}

func (credentials darwinMirrorCredentials) compareAndSwap(label string, expected, replacement []byte) (bool, error) {
	if label == "" || len(replacement) == 0 || len(replacement) > 4096 {
		return false, ErrMirrorInvalid
	}
	var swapped bool
	err := withDarwinMirrorLock(label, func() error {
		current, err := credentials.get(label)
		if errors.Is(err, ErrMirrorMissing) {
			if expected != nil {
				return nil
			}
		} else if err != nil {
			return err
		} else {
			if bytes.Equal(current, replacement) {
				swapped = true
				return nil
			}
			if expected == nil || !bytes.Equal(current, expected) {
				return nil
			}
		}
		if err := credentials.put(label, replacement); err != nil {
			return err
		}
		swapped = true
		return nil
	})
	return swapped, err
}

func withDarwinMirrorLock(label string, operation func() error) error {
	if label == "" || operation == nil {
		return ErrMirrorInvalid
	}
	digest := sha256.Sum256([]byte(label))
	path := fmt.Sprintf("/tmp/com.tammy.audit-mirror.%d.%x.lock", os.Getuid(), digest[:])
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return ErrMirrorInvalid
	}
	defer syscall.Close(fd)
	var info syscall.Stat_t
	if syscall.Fstat(fd, &info) != nil || info.Uid != uint32(os.Getuid()) || info.Mode&syscall.S_IFMT != syscall.S_IFREG || info.Mode&0o077 != 0 {
		return ErrMirrorInvalid
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			return ErrMirrorInvalid
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	return operation()
}
