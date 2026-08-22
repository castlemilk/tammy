//go:build darwin && cgo

package vault

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework CoreFoundation -framework Security
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>

static CFStringRef sbr_string(const char *value) { return CFStringCreateWithCString(kCFAllocatorDefault, value, kCFStringEncodingUTF8); }
static CFMutableDataRef sbr_owned_data(const void *value, size_t length) {
  CFMutableDataRef data = CFDataCreateMutable(kCFAllocatorDefault, (CFIndex)length);
  if (!data) return NULL;
  CFDataSetLength(data, (CFIndex)length);
  if (length) memcpy(CFDataGetMutableBytePtr(data), value, length);
  return data;
}
static int sbr_clear_data(CFMutableDataRef data) {
  if (!data) return 1;
  CFIndex length = CFDataGetLength(data);
  UInt8 *bytes = CFDataGetMutableBytePtr(data);
  if (bytes && length > 0) {
    volatile UInt8 *cursor = bytes;
    for (CFIndex index = 0; index < length; index++) cursor[index] = 0;
    for (CFIndex index = 0; index < length; index++) if (bytes[index] != 0) { CFRelease(data); return 0; }
  }
  CFRelease(data);
  return 1;
}
static int sbr_is_development(const char *group) { return strcmp(group, "com.tammy.desktop.sbr.development.tests") == 0; }

static CFMutableDictionaryRef sbr_query(SecKeychainRef keychain, const char *service, const char *group, const char *account, const char *expected, int return_data, CFMutableDataRef *expected_data_out) {
  *expected_data_out = NULL;
  CFStringRef service_value = sbr_string(service), group_value = sbr_string(group), account_value = sbr_string(account);
  if (!service_value || !group_value || !account_value) { if (service_value) CFRelease(service_value); if (group_value) CFRelease(group_value); if (account_value) CFRelease(account_value); return NULL; }
  CFMutableDictionaryRef query = CFDictionaryCreateMutable(kCFAllocatorDefault, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
  if (query) {
    CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword); CFDictionarySetValue(query, kSecAttrService, service_value);
    if (!sbr_is_development(group)) CFDictionarySetValue(query, kSecAttrAccessGroup, group_value); CFDictionarySetValue(query, kSecAttrAccount, account_value);
    CFDictionarySetValue(query, kSecAttrSynchronizable, kCFBooleanFalse); CFDictionarySetValue(query, kSecUseAuthenticationUI, kSecUseAuthenticationUIFail);
    if (keychain) { const void *items[1] = { keychain }; CFArrayRef search = CFArrayCreate(kCFAllocatorDefault, items, 1, &kCFTypeArrayCallBacks); if (!search) { CFRelease(query); query = NULL; } else { CFDictionarySetValue(query, kSecMatchSearchList, search); CFRelease(search); } }
    if (query && expected) { *expected_data_out = sbr_owned_data(expected, strlen(expected)); if (!*expected_data_out) { CFRelease(query); query = NULL; } else { CFDictionarySetValue(query, kSecAttrGeneric, *expected_data_out); } }
    if (query && return_data) { CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne); CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue); }
  }
  CFRelease(service_value); CFRelease(group_value); CFRelease(account_value); return query;
}

static OSStatus sbr_access(const char *label, const char *requirement_text, int current_process, SecAccessRef *access_out) {
  SecRequirementRef requirement = NULL; OSStatus status; CFDataRef requirement_data = NULL; SecTrustedApplicationRef application = NULL;
  if (current_process) {
    status = SecTrustedApplicationCreateFromPath(NULL, &application);
  } else {
    CFStringRef requirement_value = sbr_string(requirement_text); if (!requirement_value) return errSecAllocate;
    status = SecRequirementCreateWithString(requirement_value, kSecCSDefaultFlags, &requirement); CFRelease(requirement_value);
    if (status == errSecSuccess) { SecCodeRef self_code = NULL; status = SecCodeCopySelf(kSecCSDefaultFlags, &self_code); if (status == errSecSuccess) status = SecCodeCheckValidity(self_code, kSecCSStrictValidate, requirement); if (self_code) CFRelease(self_code); }
    if (status == errSecSuccess) status = SecRequirementCopyData(requirement, kSecCSDefaultFlags, &requirement_data);
    if (requirement) CFRelease(requirement);
    if (status == errSecSuccess) status = SecTrustedApplicationCreateFromPath(NULL, &application);
  }
  if (status != errSecSuccess) { if (requirement_data) CFRelease(requirement_data); if (application) CFRelease(application); return status; }
  if (!current_process) { status = SecTrustedApplicationSetData(application, requirement_data); CFRelease(requirement_data); }
  if (status != errSecSuccess) { if (application) CFRelease(application); return status; }
  const void *applications[1] = { application }; CFArrayRef trusted = CFArrayCreate(kCFAllocatorDefault, applications, 1, &kCFTypeArrayCallBacks); CFRelease(application);
  if (!trusted) return errSecAllocate; CFStringRef label_value = sbr_string(label); if (!label_value) { CFRelease(trusted); return errSecAllocate; }
  status = SecAccessCreate(label_value, trusted, access_out); CFRelease(label_value); CFRelease(trusted); return status;
}

static OSStatus sbr_copy(SecKeychainRef keychain, const char *service, const char *group, const char *account, void **bytes_out, size_t *length_out) {
  *bytes_out = NULL; *length_out = 0; CFMutableDataRef expected_data = NULL; CFMutableDictionaryRef query = sbr_query(keychain, service, group, account, NULL, 1, &expected_data); if (!query) return errSecAllocate;
  CFTypeRef result = NULL; OSStatus status = SecItemCopyMatching(query, &result); CFRelease(query); if (status != errSecSuccess) return status;
  if (!result || CFGetTypeID(result) != CFDataGetTypeID()) { if (result) CFRelease(result); return errSecDecode; }
  CFIndex length = CFDataGetLength((CFDataRef)result); if (length < 0) { CFRelease(result); return errSecDecode; }
  void *copy = length ? malloc((size_t)length) : NULL; if (length && !copy) { CFRelease(result); return errSecAllocate; }
  if (length) memcpy(copy, CFDataGetBytePtr((CFDataRef)result), (size_t)length); CFRelease(result); *bytes_out = copy; *length_out = (size_t)length; return errSecSuccess;
}

static OSStatus sbr_add(SecKeychainRef keychain, const char *service, const char *group, const char *account, const void *bytes, size_t length, const char *digest, const char *requirement, int current_process, int *wiped_out) {
  *wiped_out = 1;
  SecAccessRef access = NULL; OSStatus status = sbr_access(service, requirement, current_process, &access); if (status != errSecSuccess) return status;
  CFStringRef service_value=sbr_string(service),group_value=sbr_string(group),account_value=sbr_string(account);CFMutableDataRef digest_value=sbr_owned_data(digest,strlen(digest)),data=sbr_owned_data(bytes,length);
  if(!service_value||!group_value||!account_value||!digest_value||!data){if(service_value)CFRelease(service_value);if(group_value)CFRelease(group_value);if(account_value)CFRelease(account_value);sbr_clear_data(digest_value);sbr_clear_data(data);CFRelease(access);return errSecAllocate;}
  CFMutableDictionaryRef attrs=CFDictionaryCreateMutable(kCFAllocatorDefault,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
  if(attrs){CFDictionarySetValue(attrs,kSecClass,kSecClassGenericPassword);CFDictionarySetValue(attrs,kSecAttrService,service_value);CFDictionarySetValue(attrs,kSecAttrAccount,account_value);CFDictionarySetValue(attrs,kSecValueData,data);CFDictionarySetValue(attrs,kSecAttrGeneric,digest_value);CFDictionarySetValue(attrs,kSecAttrAccess,access);CFDictionarySetValue(attrs,kSecAttrSynchronizable,kCFBooleanFalse);if(keychain)CFDictionarySetValue(attrs,kSecUseKeychain,keychain);if(!sbr_is_development(group))CFDictionarySetValue(attrs,kSecAttrAccessGroup,group_value);}
  CFRelease(service_value);CFRelease(group_value);CFRelease(account_value);CFRelease(access);
  if(!attrs){*wiped_out=sbr_clear_data(data)&sbr_clear_data(digest_value);return errSecAllocate;}status=SecItemAdd(attrs,NULL);CFRelease(attrs);*wiped_out=sbr_clear_data(data)&sbr_clear_data(digest_value);return status;
}

static OSStatus sbr_update(SecKeychainRef keychain,const char *service,const char *group,const char *account,const char *expected,const void *bytes,size_t length,const char *digest,const char *requirement,int current_process,int *wiped_out){
  *wiped_out=1;
  (void)requirement;(void)current_process;
  CFMutableDataRef expected_data=NULL;CFMutableDictionaryRef query=sbr_query(keychain,service,group,account,expected,0,&expected_data);if(!query)return errSecAllocate;
  CFMutableDataRef data=sbr_owned_data(bytes,length),digest_value=sbr_owned_data(digest,strlen(digest));if(!data||!digest_value){sbr_clear_data(data);sbr_clear_data(digest_value);sbr_clear_data(expected_data);CFRelease(query);return errSecAllocate;}
  const void *keys[2]={kSecValueData,kSecAttrGeneric};const void *values[2]={data,digest_value};CFDictionaryRef attrs=CFDictionaryCreate(kCFAllocatorDefault,keys,values,2,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
  if(!attrs){CFRelease(query);*wiped_out=sbr_clear_data(data)&sbr_clear_data(digest_value)&sbr_clear_data(expected_data);return errSecAllocate;}OSStatus status=SecItemUpdate(query,attrs);CFRelease(attrs);CFRelease(query);*wiped_out=sbr_clear_data(data)&sbr_clear_data(digest_value)&sbr_clear_data(expected_data);return status;
}
static OSStatus sbr_delete(SecKeychainRef keychain,const char *service,const char *group,const char *account,const char *expected,int *wiped_out){*wiped_out=1;CFMutableDataRef expected_data=NULL;CFMutableDictionaryRef query=sbr_query(keychain,service,group,account,expected,0,&expected_data);if(!query)return errSecAllocate;OSStatus status=SecItemDelete(query);CFRelease(query);*wiped_out=sbr_clear_data(expected_data);return status;}
static OSStatus sbr_open_keychain(const char *path,SecKeychainRef *out){return SecKeychainOpen(path,out);}
static OSStatus sbr_delete_keychain(SecKeychainRef keychain){return SecKeychainDelete(keychain);}
static void sbr_release_keychain(SecKeychainRef keychain){if(keychain)CFRelease(keychain);}
static void sbr_zero_free(void *bytes,size_t length){if(bytes){volatile unsigned char *cursor=(volatile unsigned char*)bytes;while(length--)*cursor++=0;free(bytes);}}
*/
import "C"

import (
	"errors"
	"regexp"
	"strings"
	"unsafe"
)

type keychainStatusError int32

func (e keychainStatusError) Error() string        { return ErrVaultInaccessible.Error() }
func (e keychainStatusError) Is(target error) bool { return target == ErrVaultInaccessible }
func (e keychainStatusError) status() int32        { return int32(e) }

type KeychainStore struct {
	service string
	policy  AccessPolicy
	native  keychainNative
}
type keychainNative interface {
	Read(service, group, account string) ([]byte, error)
	Create(service, group, account string, value []byte, digest string, policy AccessPolicy) error
	CompareAndReplace(service, group, account, expected string, value []byte, digest string, policy AccessPolicy) error
	Delete(service, group, account, expected string) error
}
type securityFrameworkNative struct {
	keychain     C.SecKeychainRef
	wipeObserver func(bool)
}

func newProductionKeychainStore(teamID string) (*KeychainStore, error) {
	channel, err := productionChannel(teamID)
	if err != nil {
		return nil, err
	}
	return newKeychainStore(channel, ""), nil
}
func newDevelopmentKeychainStore(suffix string) (*KeychainStore, error) {
	if suffix != "" && !regexp.MustCompile(`^[A-Za-z0-9.-]+$`).MatchString(suffix) {
		return nil, ErrVaultInvalidInput
	}
	return newKeychainStore(developmentChannel(), suffix), nil
}
func openIsolatedDevelopmentKeychainStore(path, suffix string) (*KeychainStore, error) {
	store, err := newDevelopmentKeychainStore(suffix)
	if err != nil {
		return nil, err
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var keychain C.SecKeychainRef
	if status := C.sbr_open_keychain(cPath, &keychain); status != C.errSecSuccess {
		return nil, keychainError(status)
	}
	store.native = securityFrameworkNative{keychain: keychain}
	return store, nil
}
func (s *KeychainStore) closeIsolated() error {
	native, ok := s.native.(securityFrameworkNative)
	if !ok || native.keychain == 0 {
		return nil
	}
	err := keychainError(C.sbr_delete_keychain(native.keychain))
	C.sbr_release_keychain(native.keychain)
	s.native = nil
	return err
}
func newKeychainStore(channel vaultChannel, suffix string) *KeychainStore {
	service := "com.tammy.sbr." + string(channel.namespace)
	if suffix != "" {
		service += "." + suffix
	}
	return &KeychainStore{service: service, policy: channel.policy, native: securityFrameworkNative{}}
}

func (s *KeychainStore) Read(account string) ([]byte, error) {
	if err := s.validate(account); err != nil {
		return nil, err
	}
	value, err := s.native.Read(s.service, s.policy.AccessGroup, account)
	if err != nil {
		return nil, err
	}
	owned := append([]byte(nil), value...)
	clear(value)
	return owned, nil
}
func (s *KeychainStore) Create(account string, value []byte, policy AccessPolicy) error {
	if err := s.validateMutation(account, value, policy); err != nil {
		return err
	}
	owned := append([]byte(nil), value...)
	defer clear(owned)
	return s.native.Create(s.service, s.policy.AccessGroup, account, owned, hashValue(owned), policy)
}
func (s *KeychainStore) Replace(account string, value []byte, policy AccessPolicy) error {
	current, err := s.Read(account)
	if err != nil {
		return err
	}
	digest := hashValue(current)
	clear(current)
	return s.CompareAndReplace(account, digest, value, policy)
}
func (s *KeychainStore) Delete(account string) error {
	current, err := s.Read(account)
	if err != nil {
		return err
	}
	digest := hashValue(current)
	clear(current)
	return s.CompareAndDelete(account, digest)
}
func (s *KeychainStore) CompareAndReplace(account, expected string, value []byte, policy AccessPolicy) error {
	if err := s.validateMutation(account, value, policy); err != nil {
		return err
	}
	if !validDigestHex(expected) {
		return ErrVaultInvalidInput
	}
	owned := append([]byte(nil), value...)
	defer clear(owned)
	err := s.native.CompareAndReplace(s.service, s.policy.AccessGroup, account, expected, owned, hashValue(owned), policy)
	if errors.Is(err, ErrVaultMissing) {
		return ErrVaultCASConflict
	}
	return err
}
func (s *KeychainStore) CompareAndDelete(account, expected string) error {
	if err := s.validate(account); err != nil {
		return err
	}
	if !validDigestHex(expected) {
		return ErrVaultInvalidInput
	}
	err := s.native.Delete(s.service, s.policy.AccessGroup, account, expected)
	if errors.Is(err, ErrVaultMissing) {
		return ErrVaultCASConflict
	}
	return err
}
func (s *KeychainStore) validate(account string) error {
	if s == nil || s.native == nil || account == "" || len(account) > 512 || strings.ContainsRune(account, 0) || !regexp.MustCompile(`^[A-Za-z0-9./-]+$`).MatchString(account) {
		return ErrVaultInvalidInput
	}
	if !strings.HasPrefix(account, "tammy.sbr."+string(s.policy.Namespace)+"/") {
		return ErrVaultInvalidInput
	}
	return nil
}
func (s *KeychainStore) validateMutation(account string, value []byte, policy AccessPolicy) error {
	if err := s.validate(account); err != nil {
		return err
	}
	if len(value) > 8<<20 || policy != s.policy {
		return ErrVaultInvalidInput
	}
	_, err := policy.CodeRequirement()
	return err
}
func validDigestHex(value string) bool {
	return regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(value)
}

func (native securityFrameworkNative) Read(serviceName, group, account string) ([]byte, error) {
	service := C.CString(serviceName)
	defer C.free(unsafe.Pointer(service))
	accessGroup := C.CString(group)
	defer C.free(unsafe.Pointer(accessGroup))
	key := C.CString(account)
	defer C.free(unsafe.Pointer(key))
	var output unsafe.Pointer
	var length C.size_t
	status := C.sbr_copy(native.keychain, service, accessGroup, key, &output, &length)
	if status != C.errSecSuccess {
		return nil, keychainError(status)
	}
	defer C.sbr_zero_free(output, length)
	if uint64(length) > 8<<20 {
		return nil, ErrVaultInaccessible
	}
	return C.GoBytes(output, C.int(length)), nil
}
func nativePolicy(policy AccessPolicy) (*C.char, C.int) {
	requirement, _ := policy.CodeRequirement()
	return C.CString(requirement), C.int(boolInt(policy.CurrentProcess))
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func (native securityFrameworkNative) reportWipe(wiped C.int) {
	if native.wipeObserver != nil {
		native.wipeObserver(wiped != 0)
	}
}
func (native securityFrameworkNative) Create(serviceName, group, account string, value []byte, digest string, policy AccessPolicy) error {
	service, accessGroup, key, digestValue := C.CString(serviceName), C.CString(group), C.CString(account), C.CString(digest)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(accessGroup))
	defer C.free(unsafe.Pointer(key))
	defer C.free(unsafe.Pointer(digestValue))
	requirement, current := nativePolicy(policy)
	defer C.free(unsafe.Pointer(requirement))
	var bytes unsafe.Pointer
	if len(value) > 0 {
		bytes = unsafe.Pointer(&value[0])
	}
	var wiped C.int
	status := C.sbr_add(native.keychain, service, accessGroup, key, bytes, C.size_t(len(value)), digestValue, requirement, current, &wiped)
	native.reportWipe(wiped)
	return keychainError(status)
}
func (native securityFrameworkNative) CompareAndReplace(serviceName, group, account, expected string, value []byte, digest string, policy AccessPolicy) error {
	service, accessGroup, key, expectedValue, digestValue := C.CString(serviceName), C.CString(group), C.CString(account), C.CString(expected), C.CString(digest)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(accessGroup))
	defer C.free(unsafe.Pointer(key))
	defer C.free(unsafe.Pointer(expectedValue))
	defer C.free(unsafe.Pointer(digestValue))
	requirement, current := nativePolicy(policy)
	defer C.free(unsafe.Pointer(requirement))
	var bytes unsafe.Pointer
	if len(value) > 0 {
		bytes = unsafe.Pointer(&value[0])
	}
	var wiped C.int
	status := C.sbr_update(native.keychain, service, accessGroup, key, expectedValue, bytes, C.size_t(len(value)), digestValue, requirement, current, &wiped)
	native.reportWipe(wiped)
	return keychainError(status)
}
func (native securityFrameworkNative) Delete(serviceName, group, account, expected string) error {
	service, accessGroup, key, expectedValue := C.CString(serviceName), C.CString(group), C.CString(account), C.CString(expected)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(accessGroup))
	defer C.free(unsafe.Pointer(key))
	defer C.free(unsafe.Pointer(expectedValue))
	var wiped C.int
	status := C.sbr_delete(native.keychain, service, accessGroup, key, expectedValue, &wiped)
	native.reportWipe(wiped)
	return keychainError(status)
}
func keychainError(status C.OSStatus) error {
	switch status {
	case C.errSecSuccess:
		return nil
	case C.errSecItemNotFound:
		return ErrVaultMissing
	case C.errSecDuplicateItem:
		return ErrVaultCollision
	default:
		return keychainStatusError(status)
	}
}

var _ Store = (*KeychainStore)(nil)
