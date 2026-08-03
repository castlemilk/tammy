//go:build darwin && cgo

package workspace

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

static CFStringRef tammy_keychain_string(const char *value, UInt32 length) {
    return CFStringCreateWithBytes(kCFAllocatorDefault, (const UInt8 *)value,
        length, kCFStringEncodingUTF8, false);
}

static CFDictionaryRef tammy_keychain_query(const char *service, UInt32 service_len,
    const char *account, UInt32 account_len) {
    CFStringRef service_value = tammy_keychain_string(service, service_len);
    CFStringRef account_value = tammy_keychain_string(account, account_len);
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

static OSStatus tammy_keychain_put(const char *service, UInt32 service_len,
    const char *account, UInt32 account_len, const void *secret, UInt32 secret_len) {
    CFDictionaryRef query = tammy_keychain_query(service, service_len, account, account_len);
    CFDataRef secret_value = CFDataCreate(kCFAllocatorDefault, (const UInt8 *)secret, secret_len);
    if (query == NULL || secret_value == NULL) {
        if (query != NULL) CFRelease(query);
        if (secret_value != NULL) CFRelease(secret_value);
        return errSecAllocate;
    }
    const void *update_keys[] = {kSecValueData};
    const void *update_values[] = {secret_value};
    CFDictionaryRef updates = CFDictionaryCreate(kCFAllocatorDefault, update_keys,
        update_values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    OSStatus status = updates == NULL ? errSecAllocate : SecItemUpdate(query, updates);
    if (status == errSecItemNotFound) {
        CFMutableDictionaryRef addition = CFDictionaryCreateMutableCopy(kCFAllocatorDefault, 0, query);
        if (addition == NULL) {
            status = errSecAllocate;
        } else {
            CFDictionarySetValue(addition, kSecValueData, secret_value);
            status = SecItemAdd(addition, NULL);
            CFRelease(addition);
        }
    }
    if (updates != NULL) CFRelease(updates);
    CFRelease(secret_value);
    CFRelease(query);
    return status;
}

static OSStatus tammy_keychain_get(const char *service, UInt32 service_len,
    const char *account, UInt32 account_len, UInt32 *secret_len, void **secret) {
    CFDictionaryRef base = tammy_keychain_query(service, service_len, account, account_len);
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
    *secret_len = (UInt32)length;
    *secret = copy;
    CFRelease(result);
    return errSecSuccess;
}

static OSStatus tammy_keychain_delete(const char *service, UInt32 service_len,
    const char *account, UInt32 account_len) {
    CFDictionaryRef query = tammy_keychain_query(service, service_len, account, account_len);
    if (query == NULL) return errSecAllocate;
    OSStatus status = SecItemDelete(query);
    CFRelease(query);
    if (status == errSecItemNotFound) return errSecSuccess;
    return status;
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

type darwinSecretStore struct{}

func newPlatformSecretStore() (SecretStore, error) {
	return darwinSecretStore{}, nil
}

func (darwinSecretStore) Put(label string, secret []byte) error {
	if label == "" || len(secret) == 0 {
		return ErrRememberedKeyUnavailable
	}
	service := []byte("com.tammy.workspace")
	account := []byte(label)
	status := C.tammy_keychain_put(
		(*C.char)(unsafe.Pointer(&service[0])), C.UInt32(len(service)),
		(*C.char)(unsafe.Pointer(&account[0])), C.UInt32(len(account)),
		unsafe.Pointer(&secret[0]), C.UInt32(len(secret)),
	)
	runtime.KeepAlive(service)
	runtime.KeepAlive(account)
	runtime.KeepAlive(secret)
	if status != C.errSecSuccess {
		return ErrRememberedKeyUnavailable
	}
	return nil
}

func (darwinSecretStore) Get(label string) ([]byte, error) {
	if label == "" {
		return nil, ErrRememberedKeyUnavailable
	}
	service := []byte("com.tammy.workspace")
	account := []byte(label)
	var length C.UInt32
	var data unsafe.Pointer
	status := C.tammy_keychain_get(
		(*C.char)(unsafe.Pointer(&service[0])), C.UInt32(len(service)),
		(*C.char)(unsafe.Pointer(&account[0])), C.UInt32(len(account)), &length, &data,
	)
	runtime.KeepAlive(service)
	runtime.KeepAlive(account)
	if status != C.errSecSuccess || data == nil || length == 0 {
		return nil, ErrRememberedKeyUnavailable
	}
	defer C.free(data)
	return C.GoBytes(data, C.int(length)), nil
}

func (darwinSecretStore) Delete(label string) error {
	if label == "" {
		return ErrRememberedKeyUnavailable
	}
	service := []byte("com.tammy.workspace")
	account := []byte(label)
	status := C.tammy_keychain_delete(
		(*C.char)(unsafe.Pointer(&service[0])), C.UInt32(len(service)),
		(*C.char)(unsafe.Pointer(&account[0])), C.UInt32(len(account)),
	)
	runtime.KeepAlive(service)
	runtime.KeepAlive(account)
	if status != C.errSecSuccess {
		return ErrRememberedKeyUnavailable
	}
	return nil
}
