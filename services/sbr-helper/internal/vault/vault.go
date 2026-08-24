package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/platform"
)

const (
	installationIDBytes = 16
	secretBytes         = 32
	nonceBytes          = 12
	maximumCredential   = 4 << 20
	maximumPassword     = 1 << 10
	maximumOpaqueRecord = 4 << 20
	maximumReceipt      = 4 << 10
)

var (
	ErrVaultMissing         = errors.New("SBR_VAULT_MISSING")
	ErrVaultCollision       = errors.New("SBR_VAULT_COLLISION")
	ErrVaultInaccessible    = errors.New("SBR_VAULT_INACCESSIBLE")
	ErrVaultInvalidInput    = errors.New("SBR_VAULT_INVALID_INPUT")
	ErrVaultAuthentication  = errors.New("SBR_VAULT_AUTHENTICATION_FAILED")
	ErrVaultVersion         = errors.New("SBR_VAULT_VERSION_UNSUPPORTED")
	ErrVaultUnsupported     = errors.New("SBR_VAULT_UNAVAILABLE_ON_TARGET")
	ErrVaultBindingMismatch = errors.New("SBR_VAULT_BINDING_MISMATCH")
	ErrVaultPending         = errors.New("SBR_VAULT_MUTATION_PENDING")
	ErrVaultCASConflict     = errors.New("SBR_VAULT_CAS_CONFLICT")
)

type Namespace string

const (
	ProductionNamespace      Namespace = "production"
	DevelopmentNamespace     Namespace = "development"
	simulatorKeychainVersion           = "simulator-v2"
)

type RecordKind uint8

const (
	CredentialKind RecordKind = 1
	ProductIDKind  RecordKind = 2
)

type PendingState uint8

const (
	PendingNone PendingState = iota
	PendingCreate
	PendingReplace
	PendingDelete
	PendingCommitted
)

type pendingPhase uint8

const (
	pendingPrepared pendingPhase = iota + 1
	pendingApplied
)

type AccessPolicy struct {
	Namespace      Namespace
	Identifier     string
	TeamID         string
	AccessGroup    string
	CurrentProcess bool
}

func (p AccessPolicy) CodeRequirement() (string, error) {
	if !validNamespace(p.Namespace) || !regexp.MustCompile(`^[A-Za-z0-9.-]+$`).MatchString(p.AccessGroup) {
		return "", ErrVaultInvalidInput
	}
	if p.CurrentProcess {
		if p.Namespace != DevelopmentNamespace || p.Identifier != "" || p.TeamID != "" {
			return "", ErrVaultInvalidInput
		}
		return "current-process-designated-requirement", nil
	}
	if p.Namespace != ProductionNamespace || !regexp.MustCompile(`^[A-Za-z0-9.-]+$`).MatchString(p.Identifier) || !regexp.MustCompile(`^[A-Z0-9]{10}$`).MatchString(p.TeamID) {
		return "", ErrVaultInvalidInput
	}
	return fmt.Sprintf(`identifier %q and anchor apple generic and certificate leaf[subject.OU] = %q`, p.Identifier, p.TeamID), nil
}

type Store interface {
	Read(account string) ([]byte, error)
	Create(account string, value []byte, policy AccessPolicy) error
	Replace(account string, value []byte, policy AccessPolicy) error
	Delete(account string) error
	CompareAndReplace(account, expectedDigest string, value []byte, policy AccessPolicy) error
	CompareAndDelete(account, expectedDigest string) error
}

type CredentialFileOpener interface {
	Open(path string, maximum int) ([]byte, error)
}

type platformOpener struct{}

func (platformOpener) Open(path string, maximum int) ([]byte, error) {
	data, err := platform.ReadSecureRegular(path, maximum)
	if err != nil {
		return nil, ErrVaultInvalidInput
	}
	return data, nil
}

type CredentialComponent interface {
	Import(original, password []byte) (CredentialRecord, error)
	Unlock(opaque, password []byte) ([]byte, error)
}

type vaultConfig struct {
	Store     Store
	Channel   vaultChannel
	Component CredentialComponent
	Opener    CredentialFileOpener
}

type vaultChannel struct {
	namespace Namespace
	policy    AccessPolicy
}

func productionChannel(teamID string) (vaultChannel, error) {
	policy := AccessPolicy{Namespace: ProductionNamespace, Identifier: "com.tammy.desktop.sbr-helper", TeamID: teamID, AccessGroup: teamID + ".com.tammy.desktop.sbr"}
	if _, err := policy.CodeRequirement(); err != nil {
		return vaultChannel{}, err
	}
	return vaultChannel{namespace: ProductionNamespace, policy: policy}, nil
}

func developmentChannel() vaultChannel {
	policy := AccessPolicy{Namespace: DevelopmentNamespace, AccessGroup: "com.tammy.desktop.sbr.development.tests", CurrentProcess: true}
	return vaultChannel{namespace: DevelopmentNamespace, policy: policy}
}

type Scope struct {
	WorkspaceID    string
	OrganisationID string
	CanonicalABN   string
}

type CredentialMetadata struct {
	Fingerprint       string
	CanonicalABN      string
	CreatedUnixMillis int64
	ExpiresUnixMillis int64
	ComponentVersion  string
}

type CredentialRecord struct {
	Opaque   []byte
	Metadata CredentialMetadata
}

type CredentialMutation struct {
	OperationID  string
	Scope        Scope
	SelectedPath string
	Password     []byte
}

type MutationKind uint8

const (
	ImportCredentialMutation MutationKind = iota + 1
	ReplaceCredentialMutation
	RemoveCredentialMutation
	ImportProductIDMutation
	RemoveProductIDMutation
)

func (kind MutationKind) valid() bool {
	return kind >= ImportCredentialMutation && kind <= RemoveProductIDMutation
}

type Mutation struct {
	Kind         MutationKind
	OperationID  string
	Scope        Scope
	ProductScope ProductScope
	SelectedPath string
	Password     []byte
	ProductID    []byte
}

type StagedResult struct {
	Kind       MutationKind
	Credential CredentialMetadata
	ProductID  ProductIDStatusResult
}

// CommitReceipt is the encrypted, redacted proof retained after a target has
// been applied. The helper receives no durable acknowledgement from core, so
// every receipt is bounded to maximumReceipt and is never silently evicted.
type CommitReceipt struct {
	OperationID    string
	MutationKind   MutationKind
	TargetAccount  string
	ExpectedDigest string
	DesiredDigest  string
	Credential     CredentialMetadata
	ProductID      ProductIDStatusResult
}

type Vault struct {
	store           Store
	random          io.Reader
	namespace       Namespace
	policy          AccessPolicy
	component       CredentialComponent
	opener          CredentialFileOpener
	installationID  []byte
	installationKey []byte
	wrappingSecret  []byte
	closed          bool
}

func newVault(config vaultConfig, entropy io.Reader) (*Vault, error) {
	if config.Store == nil || entropy == nil || config.Component == nil || !validNamespace(config.Channel.namespace) {
		return nil, ErrVaultInvalidInput
	}
	policy := config.Channel.policy
	if _, err := policy.CodeRequirement(); err != nil {
		return nil, err
	}
	if config.Opener == nil {
		config.Opener = platformOpener{}
	}
	v := &Vault{store: config.Store, random: entropy, namespace: config.Channel.namespace, policy: policy, component: config.Component, opener: config.Opener}
	if err := v.loadOrCreateInstallation(); err != nil {
		return nil, err
	}
	return v, nil
}

func newTestChannelVault(store Store, entropy io.Reader, component CredentialComponent, opener CredentialFileOpener) (*Vault, error) {
	return newVault(vaultConfig{Store: store, Channel: developmentChannel(), Component: component, Opener: opener}, entropy)
}

func newProductionVault(teamID string, component CredentialComponent, opener CredentialFileOpener) (*Vault, error) {
	channel, err := productionChannel(teamID)
	if err != nil {
		return nil, err
	}
	store, err := newProductionKeychainStore(teamID)
	if err != nil {
		return nil, err
	}
	return newVault(vaultConfig{Store: store, Channel: channel, Component: component, Opener: opener}, rand.Reader)
}

func newDevelopmentVault(component CredentialComponent, opener CredentialFileOpener) (*Vault, error) {
	store, err := newSimulatorDevelopmentKeychainStore()
	if err != nil {
		return nil, err
	}
	return newVault(vaultConfig{Store: store, Channel: developmentChannel(), Component: component, Opener: opener}, rand.Reader)
}

func newSimulatorDevelopmentKeychainStore() (*KeychainStore, error) {
	return newDevelopmentKeychainStore(simulatorKeychainVersion)
}

func validNamespace(namespace Namespace) bool {
	return namespace == ProductionNamespace || namespace == DevelopmentNamespace
}

func (v *Vault) prefix() string { return "tammy.sbr." + string(v.namespace) + "/" }

func (v *Vault) loadOrCreateInstallation() error {
	markerAccount := v.prefix() + "installation-generation"
	marker, err := v.store.Read(markerAccount)
	if err == nil {
		defer clear(marker)
		return v.loadInstallationGeneration(string(marker))
	}
	clear(marker)
	if !errors.Is(err, ErrVaultMissing) {
		return err
	}
	generationBytes := make([]byte, 16)
	values := [][]byte{make([]byte, installationIDBytes), make([]byte, secretBytes), make([]byte, secretBytes)}
	defer clear(generationBytes)
	defer clearNested(values)
	if _, err := io.ReadFull(v.random, generationBytes); err != nil {
		return ErrVaultInaccessible
	}
	for _, value := range values {
		if _, err := io.ReadFull(v.random, value); err != nil {
			return ErrVaultInaccessible
		}
	}
	generation := hex.EncodeToString(generationBytes)
	accounts := v.installationGenerationAccounts(generation)
	for index, account := range accounts {
		if err := v.store.Create(account, values[index], v.policy); err != nil {
			return err
		}
	}
	if err := v.store.Create(markerAccount, []byte(generation), v.policy); err != nil {
		if !errors.Is(err, ErrVaultCollision) {
			return err
		}
		for _, account := range accounts {
			_ = v.store.Delete(account)
		}
		winner, readErr := v.store.Read(markerAccount)
		if readErr != nil {
			clear(winner)
			return readErr
		}
		defer clear(winner)
		return v.loadInstallationGeneration(string(winner))
	}
	v.installationID = append([]byte(nil), values[0]...)
	v.installationKey = append([]byte(nil), values[1]...)
	v.wrappingSecret = append([]byte(nil), values[2]...)
	return nil
}

func (v *Vault) installationGenerationAccounts(generation string) []string {
	base := v.prefix() + "installation/" + generation + "/"
	return []string{base + "id", base + "hmac-key", base + "wrapping-secret"}
}

func (v *Vault) loadInstallationGeneration(generation string) error {
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(generation) {
		return ErrVaultInaccessible
	}
	accounts := v.installationGenerationAccounts(generation)
	values := make([][]byte, 3)
	defer clearNested(values)
	for index, account := range accounts {
		value, err := v.store.Read(account)
		if err != nil {
			clear(value)
			return ErrVaultInaccessible
		}
		values[index] = append([]byte(nil), value...)
		clear(value)
	}
	if len(values[0]) != installationIDBytes || len(values[1]) != secretBytes || len(values[2]) != secretBytes {
		return ErrVaultInaccessible
	}
	v.installationID = append([]byte(nil), values[0]...)
	v.installationKey = append([]byte(nil), values[1]...)
	v.wrappingSecret = append([]byte(nil), values[2]...)
	return nil
}

func DeriveScope(installationKey []byte, scope Scope) (string, error) {
	canonicalABN, err := canonicalABN(scope.CanonicalABN)
	if err != nil || len(installationKey) != secretBytes || !validBinding(scope.WorkspaceID) || !validBinding(scope.OrganisationID) {
		return "", ErrVaultInvalidInput
	}
	mac := hmac.New(sha256.New, installationKey)
	_, _ = mac.Write([]byte(scope.WorkspaceID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(scope.OrganisationID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(canonicalABN))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func validBinding(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsRune(value, 0)
}

func canonicalABN(value string) (string, error) {
	var digits strings.Builder
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
			digits.WriteRune(character)
		case character == ' ' || character == '-':
		default:
			return "", ErrVaultInvalidInput
		}
	}
	if digits.Len() != 11 {
		return "", ErrVaultInvalidInput
	}
	canonical := digits.String()
	weights := [...]int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	sum := 0
	for index, character := range canonical {
		digit := int(character - '0')
		if index == 0 {
			digit--
		}
		sum += digit * weights[index]
	}
	if sum%89 != 0 {
		return "", ErrVaultInvalidInput
	}
	return canonical, nil
}

func (v *Vault) credentialAccount(scope Scope) (string, error) {
	if v.closed {
		return "", ErrVaultInaccessible
	}
	digest, err := DeriveScope(v.installationKey, scope)
	if err != nil {
		return "", err
	}
	return v.prefix() + "credential/" + digest, nil
}

func (v *Vault) StageCreate(mutation Mutation) (StagedResult, error) {
	switch mutation.Kind {
	case ImportCredentialMutation:
		metadata, err := v.stageCredential(CredentialMutation{OperationID: mutation.OperationID, Scope: mutation.Scope, SelectedPath: mutation.SelectedPath, Password: mutation.Password}, PendingCreate)
		return StagedResult{Kind: mutation.Kind, Credential: metadata}, err
	case ImportProductIDMutation:
		status, err := v.stageProductID(ProductIDMutation{OperationID: mutation.OperationID, Scope: mutation.ProductScope, Value: mutation.ProductID})
		return StagedResult{Kind: mutation.Kind, ProductID: status}, err
	default:
		clear(mutation.Password)
		clear(mutation.ProductID)
		return StagedResult{}, ErrVaultInvalidInput
	}
}

func (v *Vault) StageReplace(mutation Mutation) (StagedResult, error) {
	if mutation.Kind != ReplaceCredentialMutation {
		clear(mutation.Password)
		clear(mutation.ProductID)
		return StagedResult{}, ErrVaultInvalidInput
	}
	metadata, err := v.stageCredential(CredentialMutation{OperationID: mutation.OperationID, Scope: mutation.Scope, SelectedPath: mutation.SelectedPath, Password: mutation.Password}, PendingReplace)
	return StagedResult{Kind: mutation.Kind, Credential: metadata}, err
}

func (v *Vault) stageCredential(mutation CredentialMutation, action PendingState) (CredentialMetadata, error) {
	defer clear(mutation.Password)
	if !validOperationID(mutation.OperationID) || len(mutation.Password) > maximumPassword || len(mutation.SelectedPath) == 0 || len(mutation.SelectedPath) > 4<<10 {
		return CredentialMetadata{}, ErrVaultInvalidInput
	}
	account, err := v.credentialAccount(mutation.Scope)
	if err != nil {
		return CredentialMetadata{}, err
	}
	exists, err := v.exists(account)
	if err != nil {
		return CredentialMetadata{}, err
	}
	if (action == PendingCreate && exists) || (action == PendingReplace && !exists) {
		if exists {
			return CredentialMetadata{}, ErrVaultCollision
		}
		return CredentialMetadata{}, ErrVaultMissing
	}
	original, err := v.opener.Open(mutation.SelectedPath, maximumCredential)
	if err != nil {
		return CredentialMetadata{}, ErrVaultInvalidInput
	}
	defer clear(original)
	record, err := v.component.Import(original, mutation.Password)
	if err != nil {
		clear(record.Opaque)
		return CredentialMetadata{}, ErrVaultAuthentication
	}
	defer clear(record.Opaque)
	if err := validateCredentialRecord(record); err != nil {
		return CredentialMetadata{}, err
	}
	scopeABN, _ := canonicalABN(mutation.Scope.CanonicalABN)
	recordABN, _ := canonicalABN(record.Metadata.CanonicalABN)
	if scopeABN != recordABN {
		return CredentialMetadata{}, ErrVaultBindingMismatch
	}
	record.Metadata.CanonicalABN = recordABN
	plain := encodeCredentialRecord(record)
	defer clear(plain)
	scopeDigest := strings.TrimPrefix(account, v.prefix()+"credential/")
	envelope, err := v.seal(scopeDigest, CredentialKind, record.Metadata.ComponentVersion, plain)
	if err != nil {
		return CredentialMetadata{}, err
	}
	defer clear(envelope)
	if err := v.stagePending(pendingMutation{OperationID: mutation.OperationID, Action: action, Kind: CredentialKind, Account: account, Envelope: envelope}); err != nil {
		return CredentialMetadata{}, err
	}
	return record.Metadata, nil
}

func (v *Vault) StageDelete(mutation Mutation) error {
	if !validOperationID(mutation.OperationID) {
		return ErrVaultInvalidInput
	}
	var account string
	var err error
	switch mutation.Kind {
	case RemoveCredentialMutation:
		account, err = v.credentialAccount(mutation.Scope)
	case RemoveProductIDMutation:
		account, err = v.productAccount(mutation.ProductScope)
	default:
		return ErrVaultInvalidInput
	}
	if err != nil {
		return err
	}
	exists, err := v.exists(account)
	if err != nil {
		return err
	}
	if !exists {
		return ErrVaultMissing
	}
	recordKind := CredentialKind
	if mutation.Kind == RemoveProductIDMutation {
		recordKind = ProductIDKind
	}
	return v.stagePending(pendingMutation{OperationID: mutation.OperationID, Action: PendingDelete, Kind: recordKind, Account: account})
}

func (v *Vault) PendingStatus(operationID string) (PendingState, error) {
	if v.closed {
		return PendingNone, ErrVaultInaccessible
	}
	if !validOperationID(operationID) {
		return PendingNone, ErrVaultInvalidInput
	}
	pending, err := v.readPending(operationID)
	if errors.Is(err, ErrVaultMissing) {
		if _, receiptErr := v.CommittedReceipt(operationID); receiptErr == nil {
			return PendingCommitted, nil
		} else if !errors.Is(receiptErr, ErrVaultMissing) {
			return PendingNone, receiptErr
		}
		return PendingNone, nil
	}
	if err != nil {
		return PendingNone, err
	}
	clear(pending.Envelope)
	return pending.Action, nil
}

func (v *Vault) Promote(operationID string) error {
	if v.closed {
		return ErrVaultInaccessible
	}
	pending, err := v.readPending(operationID)
	if errors.Is(err, ErrVaultMissing) {
		if _, receiptErr := v.CommittedReceipt(operationID); receiptErr == nil {
			return nil
		} else if !errors.Is(receiptErr, ErrVaultMissing) {
			return receiptErr
		}
	}
	if err != nil {
		return err
	}
	defer clear(pending.Envelope)
	if pending.Phase == pendingApplied {
		if err := v.writeCommittedReceipt(pending); err != nil {
			return err
		}
		if err := v.releaseReservation(pending); err != nil && !errors.Is(err, ErrVaultCASConflict) {
			return err
		}
		return v.removePendingRecord(operationID)
	}
	if err := v.ensureReservation(pending); err != nil {
		return err
	}
	switch pending.Action {
	case PendingCreate:
		err = v.store.Create(pending.Account, pending.Envelope, v.policy)
		if errors.Is(err, ErrVaultCollision) {
			err = v.requireStoredDigest(pending.Account, pending.DesiredDigest)
		}
	case PendingReplace:
		err = v.store.CompareAndReplace(pending.Account, pending.ExpectedDigest, pending.Envelope, v.policy)
		if errors.Is(err, ErrVaultCASConflict) {
			err = v.requireStoredDigest(pending.Account, pending.DesiredDigest)
		}
	case PendingDelete:
		err = v.store.CompareAndDelete(pending.Account, pending.ExpectedDigest)
		if errors.Is(err, ErrVaultMissing) || errors.Is(err, ErrVaultCASConflict) {
			err = v.requireStoredDigest(pending.Account, pending.DesiredDigest)
		}
	default:
		err = ErrVaultAuthentication
	}
	if err != nil {
		return err
	}
	if err := v.markPendingApplied(pending); err != nil {
		return err
	}
	if err := v.writeCommittedReceipt(pending); err != nil {
		return err
	}
	if err := v.releaseReservation(pending); err != nil {
		return err
	}
	return v.removePendingRecord(operationID)
}

func (v *Vault) Abort(operationID string) error {
	if v.closed {
		return ErrVaultInaccessible
	}
	if !validOperationID(operationID) {
		return ErrVaultInvalidInput
	}
	pending, err := v.readPending(operationID)
	if errors.Is(err, ErrVaultMissing) {
		if _, receiptErr := v.CommittedReceipt(operationID); receiptErr == nil {
			return ErrVaultCASConflict
		} else if !errors.Is(receiptErr, ErrVaultMissing) {
			return receiptErr
		}
	}
	if err != nil {
		return err
	}
	defer clear(pending.Envelope)
	if err := v.releaseReservation(pending); err != nil && !errors.Is(err, ErrVaultCASConflict) {
		return err
	}
	return v.removePendingRecord(operationID)
}

func (v *Vault) ReadMetadata(scope Scope) (CredentialMetadata, error) {
	account, err := v.credentialAccount(scope)
	if err != nil {
		return CredentialMetadata{}, err
	}
	record, err := v.readCredentialWithAAD(account, scope)
	if err != nil {
		return CredentialMetadata{}, err
	}
	defer clear(record.Opaque)
	return record.Metadata, nil
}

func (v *Vault) Unlock(scope Scope, password []byte) (CredentialMetadata, error) {
	defer clear(password)
	if len(password) > maximumPassword {
		return CredentialMetadata{}, ErrVaultInvalidInput
	}
	account, err := v.credentialAccount(scope)
	if err != nil {
		return CredentialMetadata{}, err
	}
	record, err := v.readCredentialWithAAD(account, scope)
	if err != nil {
		return CredentialMetadata{}, err
	}
	defer clear(record.Opaque)
	scopeABN, _ := canonicalABN(scope.CanonicalABN)
	recordABN, recordABNErr := canonicalABN(record.Metadata.CanonicalABN)
	if recordABNErr != nil || recordABN != scopeABN {
		return CredentialMetadata{}, ErrVaultBindingMismatch
	}
	decryptedKey, err := v.component.Unlock(record.Opaque, password)
	defer clear(decryptedKey)
	if err != nil {
		return CredentialMetadata{}, ErrVaultAuthentication
	}
	return record.Metadata, nil
}

func (v *Vault) readCredentialWithAAD(account string, scope Scope) (CredentialRecord, error) {
	envelope, err := v.store.Read(account)
	if err != nil {
		return CredentialRecord{}, err
	}
	defer clear(envelope)
	scopeDigest, err := DeriveScope(v.installationKey, scope)
	if err != nil {
		return CredentialRecord{}, err
	}
	plain, version, err := v.open(scopeDigest, CredentialKind, envelope)
	if err != nil {
		return CredentialRecord{}, err
	}
	defer clear(plain)
	record, err := decodeCredentialRecord(plain)
	if err != nil || record.Metadata.ComponentVersion != version {
		clear(record.Opaque)
		return CredentialRecord{}, ErrVaultAuthentication
	}
	return record, nil
}

func (v *Vault) exists(account string) (bool, error) {
	value, err := v.store.Read(account)
	clear(value)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrVaultMissing) {
		return false, nil
	}
	return false, err
}

func (v *Vault) requireStoredDigest(account string, expectedDigest string) error {
	actual, err := v.store.Read(account)
	if expectedDigest == "missing" && errors.Is(err, ErrVaultMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	defer clear(actual)
	if expectedDigest == "missing" {
		return ErrVaultCASConflict
	}
	if hashValue(actual) != expectedDigest {
		return ErrVaultCASConflict
	}
	return nil
}

func (v *Vault) seal(scope string, kind RecordKind, componentVersion string, plain []byte) ([]byte, error) {
	if v.closed {
		return nil, ErrVaultInaccessible
	}
	if len(componentVersion) == 0 || len(componentVersion) > 128 {
		return nil, ErrVaultInvalidInput
	}
	key := append([]byte(nil), v.wrappingSecret...)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrVaultInaccessible
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrVaultInaccessible
	}
	nonce := make([]byte, nonceBytes)
	if _, err := io.ReadFull(v.random, nonce); err != nil {
		clear(nonce)
		return nil, ErrVaultInaccessible
	}
	aad, err := envelopeAADExact(v.installationID, scope, kind, componentVersion)
	if err != nil {
		clear(nonce)
		return nil, err
	}
	defer clear(aad)
	ciphertext := gcm.Seal(nil, nonce, plain, aad)
	result := make([]byte, 1+2+len(componentVersion)+nonceBytes+len(ciphertext))
	result[0] = 1
	binary.BigEndian.PutUint16(result[1:3], uint16(len(componentVersion)))
	copy(result[3:], componentVersion)
	copy(result[3+len(componentVersion):], nonce)
	copy(result[3+len(componentVersion)+nonceBytes:], ciphertext)
	clear(nonce)
	clear(ciphertext)
	return result, nil
}

func (v *Vault) open(scope string, kind RecordKind, envelope []byte) ([]byte, string, error) {
	if v.closed {
		return nil, "", ErrVaultInaccessible
	}
	if len(envelope) < 3 || envelope[0] != 1 {
		return nil, "", ErrVaultVersion
	}
	versionLength := int(binary.BigEndian.Uint16(envelope[1:3]))
	if versionLength == 0 || versionLength > 128 || len(envelope) < 3+versionLength+nonceBytes+16 {
		return nil, "", ErrVaultAuthentication
	}
	componentVersion := string(envelope[3 : 3+versionLength])
	nonce := envelope[3+versionLength : 3+versionLength+nonceBytes]
	ciphertext := envelope[3+versionLength+nonceBytes:]
	key := append([]byte(nil), v.wrappingSecret...)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", ErrVaultInaccessible
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", ErrVaultInaccessible
	}
	aad, err := envelopeAADExact(v.installationID, scope, kind, componentVersion)
	if err != nil {
		return nil, "", err
	}
	defer clear(aad)
	plain, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, "", ErrVaultAuthentication
	}
	return plain, componentVersion, nil
}

func envelopeAADExact(installationID []byte, scope string, kind RecordKind, componentVersion string) ([]byte, error) {
	if len(installationID) != installationIDBytes || (kind != CredentialKind && kind != ProductIDKind) || len(componentVersion) == 0 || len(componentVersion) > 128 {
		return nil, ErrVaultInvalidInput
	}
	scopeBytes, err := hex.DecodeString(scope)
	if err != nil || len(scopeBytes) != 32 {
		clear(scopeBytes)
		return nil, ErrVaultInvalidInput
	}
	result := make([]byte, 0, len("tammy-sbr-vault-v1")+16+32+1+len(componentVersion))
	result = append(result, []byte("tammy-sbr-vault-v1")...)
	result = append(result, installationID...)
	result = append(result, scopeBytes...)
	result = append(result, byte(kind))
	result = append(result, componentVersion...)
	clear(scopeBytes)
	return result, nil
}

type pendingMutation struct {
	OperationID        string
	Action             PendingState
	Kind               RecordKind
	Account            string
	Envelope           []byte
	ExpectedDigest     string
	DesiredDigest      string
	ReservationAccount string
	ReservationDigest  string
	ReservationToken   string
	Phase              pendingPhase
	StoredDigest       string
}

func (v *Vault) pendingAccount(operationID string) string {
	return v.prefix() + "pending/" + operationID
}

func (v *Vault) stagePending(pending pendingMutation) error {
	existing, err := v.store.Read(v.pendingAccount(pending.OperationID))
	clear(existing)
	if err == nil {
		return ErrVaultCollision
	} else if !errors.Is(err, ErrVaultMissing) {
		return err
	}
	current, currentErr := v.store.Read(pending.Account)
	expectedDigest := "missing"
	if currentErr == nil {
		expectedDigest = hashValue(current)
	} else if !errors.Is(currentErr, ErrVaultMissing) {
		clear(current)
		return currentErr
	}
	clear(current)
	if pending.Action == PendingCreate && expectedDigest != "missing" {
		return ErrVaultCollision
	}
	if pending.Action != PendingCreate && expectedDigest == "missing" {
		return ErrVaultMissing
	}
	pending.ExpectedDigest = expectedDigest
	pending.Phase = pendingPrepared
	if pending.Action == PendingDelete {
		pending.DesiredDigest = "missing"
	} else {
		pending.DesiredDigest = hashValue(pending.Envelope)
	}
	pending.ReservationAccount = v.reservationAccount(pending.Account)
	ownerToken := make([]byte, 32)
	if _, err := io.ReadFull(v.random, ownerToken); err != nil {
		clear(ownerToken)
		return ErrVaultInaccessible
	}
	pending.ReservationToken = hex.EncodeToString(ownerToken)
	clear(ownerToken)
	reservation := v.reservationValue(pending)
	pending.ReservationDigest = hashValue(reservation)
	defer clear(reservation)
	plain := encodePending(pending)
	defer clear(plain)
	pendingScope := v.pendingScope(pending.OperationID)
	envelope, err := v.seal(pendingScope, pending.Kind, "pending-v1", plain)
	if err != nil {
		return err
	}
	defer clear(envelope)
	pendingAccount := v.pendingAccount(pending.OperationID)
	if err := v.store.Create(pendingAccount, envelope, v.policy); err != nil {
		return err
	}
	if err := v.createReservation(pending.ReservationAccount, reservation, v.policy); err != nil {
		if errors.Is(err, ErrVaultPending) {
			if cleanupErr := v.removePendingRecord(pending.OperationID); cleanupErr != nil {
				return cleanupErr
			}
		}
		return err
	}
	check, checkErr := v.store.Read(pending.Account)
	actualDigest := "missing"
	if checkErr == nil {
		actualDigest = hashValue(check)
	} else if !errors.Is(checkErr, ErrVaultMissing) {
		clear(check)
		return checkErr
	}
	clear(check)
	if actualDigest != expectedDigest {
		if err := v.releaseReservation(pending); err != nil {
			return err
		}
		if err := v.removePendingRecord(pending.OperationID); err != nil {
			return err
		}
		return ErrVaultCASConflict
	}
	return nil
}

func (v *Vault) createReservation(account string, value []byte, policy AccessPolicy) error {
	err := v.store.Create(account, value, policy)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrVaultCollision) {
		return ErrVaultPending
	}
	return err
}

func (v *Vault) verifyReservation(pending pendingMutation) error {
	value, err := v.store.Read(pending.ReservationAccount)
	if err != nil {
		clear(value)
		return ErrVaultCASConflict
	}
	defer clear(value)
	if hashValue(value) != pending.ReservationDigest {
		return ErrVaultCASConflict
	}
	fields, err := decodeFields(value, 4)
	if err != nil || string(fields[0]) != pending.OperationID || string(fields[1]) != pending.ExpectedDigest || string(fields[2]) != pending.DesiredDigest || string(fields[3]) != pending.ReservationToken {
		return ErrVaultCASConflict
	}
	return nil
}

func (v *Vault) reservationValue(pending pendingMutation) []byte {
	return encodeFields([][]byte{[]byte(pending.OperationID), []byte(pending.ExpectedDigest), []byte(pending.DesiredDigest), []byte(pending.ReservationToken)})
}

func (v *Vault) ensureReservation(pending pendingMutation) error {
	value, err := v.store.Read(pending.ReservationAccount)
	if err == nil {
		clear(value)
		return v.verifyReservation(pending)
	}
	clear(value)
	if !errors.Is(err, ErrVaultMissing) {
		return err
	}
	reservation := v.reservationValue(pending)
	defer clear(reservation)
	err = v.store.Create(pending.ReservationAccount, reservation, v.policy)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrVaultCollision) {
		return v.verifyReservation(pending)
	}
	return err
}

func (v *Vault) releaseReservation(pending pendingMutation) error {
	err := v.store.CompareAndDelete(pending.ReservationAccount, pending.ReservationDigest)
	if errors.Is(err, ErrVaultMissing) || errors.Is(err, ErrVaultCASConflict) {
		return v.requireStoredDigest(pending.ReservationAccount, "missing")
	}
	return err
}

func (v *Vault) removePendingRecord(operationID string) error {
	account := v.pendingAccount(operationID)
	value, err := v.store.Read(account)
	if err != nil {
		clear(value)
		return err
	}
	digest := hashValue(value)
	clear(value)
	err = v.store.CompareAndDelete(account, digest)
	if errors.Is(err, ErrVaultMissing) || errors.Is(err, ErrVaultCASConflict) {
		return v.requireStoredDigest(account, "missing")
	}
	return err
}

func (v *Vault) markPendingApplied(pending pendingMutation) error {
	pending.Phase = pendingApplied
	plain := encodePending(pending)
	defer clear(plain)
	envelope, err := v.seal(v.pendingScope(pending.OperationID), pending.Kind, "pending-v1", plain)
	if err != nil {
		return err
	}
	defer clear(envelope)
	return v.store.CompareAndReplace(v.pendingAccount(pending.OperationID), pending.StoredDigest, envelope, v.policy)
}

func (v *Vault) reservationAccount(target string) string {
	digest := sha256.Sum256([]byte(target))
	return v.prefix() + "reservation/" + hex.EncodeToString(digest[:])
}

func hashValue(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func (v *Vault) readPending(operationID string) (pendingMutation, error) {
	if !validOperationID(operationID) {
		return pendingMutation{}, ErrVaultInvalidInput
	}
	envelope, err := v.store.Read(v.pendingAccount(operationID))
	if err != nil {
		return pendingMutation{}, err
	}
	defer clear(envelope)
	if len(envelope) < 3 {
		return pendingMutation{}, ErrVaultAuthentication
	}
	// Pending kind is authenticated in AAD; try only the closed kinds.
	for _, kind := range []RecordKind{CredentialKind, ProductIDKind} {
		plain, version, openErr := v.open(v.pendingScope(operationID), kind, envelope)
		if openErr != nil {
			continue
		}
		pending, decodeErr := decodePending(plain)
		clear(plain)
		if decodeErr == nil && version == "pending-v1" && pending.OperationID == operationID && pending.Kind == kind && v.validPendingTarget(pending) {
			pending.StoredDigest = hashValue(envelope)
			return pending, nil
		}
		clear(pending.Envelope)
	}
	return pendingMutation{}, ErrVaultAuthentication
}

func (v *Vault) pendingScope(operationID string) string {
	mac := hmac.New(sha256.New, v.installationKey)
	_, _ = mac.Write([]byte("pending"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(operationID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (v *Vault) receiptAccount(operationID string) string {
	return v.prefix() + "receipt/" + operationID
}

func (v *Vault) receiptScope(operationID string) string {
	mac := hmac.New(sha256.New, v.installationKey)
	_, _ = mac.Write([]byte("receipt"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(operationID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (v *Vault) receiptForPending(pending pendingMutation) (CommitReceipt, error) {
	receipt := CommitReceipt{OperationID: pending.OperationID, TargetAccount: pending.Account,
		ExpectedDigest: pending.ExpectedDigest, DesiredDigest: pending.DesiredDigest}
	switch {
	case pending.Kind == CredentialKind && pending.Action == PendingCreate:
		receipt.MutationKind = ImportCredentialMutation
	case pending.Kind == CredentialKind && pending.Action == PendingReplace:
		receipt.MutationKind = ReplaceCredentialMutation
	case pending.Kind == CredentialKind && pending.Action == PendingDelete:
		receipt.MutationKind = RemoveCredentialMutation
	case pending.Kind == ProductIDKind && pending.Action == PendingCreate:
		receipt.MutationKind = ImportProductIDMutation
	case pending.Kind == ProductIDKind && pending.Action == PendingDelete:
		receipt.MutationKind = RemoveProductIDMutation
	default:
		return CommitReceipt{}, ErrVaultAuthentication
	}
	if pending.Action == PendingDelete {
		receipt.ProductID.State = ProductIDMissing
		return receipt, nil
	}
	scopeDigest := strings.TrimPrefix(pending.Account, v.prefix()+"credential/")
	if pending.Kind == ProductIDKind {
		scopeDigest = strings.TrimPrefix(pending.Account, v.prefix()+"product-id/")
	}
	plain, version, err := v.open(scopeDigest, pending.Kind, pending.Envelope)
	if err != nil {
		return CommitReceipt{}, err
	}
	defer clear(plain)
	if pending.Kind == CredentialKind {
		record, decodeErr := decodeCredentialRecord(plain)
		if decodeErr != nil || record.Metadata.ComponentVersion != version {
			clear(record.Opaque)
			return CommitReceipt{}, ErrVaultAuthentication
		}
		clear(record.Opaque)
		receipt.Credential = record.Metadata
		return receipt, nil
	}
	fields, decodeErr := decodeFields(plain, 2)
	if decodeErr != nil || version != "product-id-v1" || len(fields[0]) == 0 || len(fields[0]) > maximumPassword ||
		!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(string(fields[1])) {
		return CommitReceipt{}, ErrVaultAuthentication
	}
	receipt.ProductID = ProductIDStatusResult{State: ProductIDPresent, Fingerprint: string(fields[1])}
	return receipt, nil
}

func (v *Vault) writeCommittedReceipt(pending pendingMutation) error {
	receipt, err := v.receiptForPending(pending)
	if err != nil {
		return err
	}
	plain := encodeCommitReceipt(receipt)
	defer clear(plain)
	if len(plain) == 0 || len(plain) > maximumReceipt {
		return ErrVaultInvalidInput
	}
	envelope, err := v.seal(v.receiptScope(receipt.OperationID), pending.Kind, "receipt-v1", plain)
	if err != nil {
		return err
	}
	defer clear(envelope)
	err = v.store.Create(v.receiptAccount(receipt.OperationID), envelope, v.policy)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrVaultCollision) {
		return err
	}
	existing, readErr := v.CommittedReceipt(receipt.OperationID)
	if readErr != nil || existing != receipt {
		return ErrVaultCASConflict
	}
	return nil
}

func (v *Vault) CommittedReceipt(operationID string) (CommitReceipt, error) {
	if v.closed {
		return CommitReceipt{}, ErrVaultInaccessible
	}
	if !validOperationID(operationID) {
		return CommitReceipt{}, ErrVaultInvalidInput
	}
	envelope, err := v.store.Read(v.receiptAccount(operationID))
	if err != nil {
		return CommitReceipt{}, err
	}
	defer clear(envelope)
	if len(envelope) == 0 || len(envelope) > maximumReceipt {
		return CommitReceipt{}, ErrVaultAuthentication
	}
	for _, kind := range []RecordKind{CredentialKind, ProductIDKind} {
		plain, version, openErr := v.open(v.receiptScope(operationID), kind, envelope)
		if openErr != nil {
			continue
		}
		receipt, decodeErr := decodeCommitReceipt(plain)
		clear(plain)
		if decodeErr == nil && version == "receipt-v1" && receipt.OperationID == operationID && v.validCommitReceipt(receipt, kind) {
			return receipt, nil
		}
	}
	return CommitReceipt{}, ErrVaultAuthentication
}

func (v *Vault) validCommitReceipt(receipt CommitReceipt, kind RecordKind) bool {
	wantPrefix := v.prefix() + "credential/"
	if kind == ProductIDKind {
		wantPrefix = v.prefix() + "product-id/"
	}
	digest := strings.TrimPrefix(receipt.TargetAccount, wantPrefix)
	if digest == receipt.TargetAccount || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(digest) ||
		!validReceiptDigest(receipt.ExpectedDigest) || !validReceiptDigest(receipt.DesiredDigest) {
		return false
	}
	switch receipt.MutationKind {
	case ImportCredentialMutation, ReplaceCredentialMutation:
		return kind == CredentialKind && validCredentialMetadata(receipt.Credential) && receipt.ProductID == (ProductIDStatusResult{})
	case RemoveCredentialMutation:
		return kind == CredentialKind && receipt.Credential == (CredentialMetadata{}) && receipt.ProductID.State == ProductIDMissing && receipt.ProductID.Fingerprint == ""
	case ImportProductIDMutation:
		return kind == ProductIDKind && receipt.Credential == (CredentialMetadata{}) && receipt.ProductID.State == ProductIDPresent &&
			regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(receipt.ProductID.Fingerprint)
	case RemoveProductIDMutation:
		return kind == ProductIDKind && receipt.Credential == (CredentialMetadata{}) && receipt.ProductID.State == ProductIDMissing && receipt.ProductID.Fingerprint == ""
	default:
		return false
	}
}

func validReceiptDigest(value string) bool {
	return value == "missing" || regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(value)
}

func validCredentialMetadata(metadata CredentialMetadata) bool {
	if metadata.Fingerprint == "" || len(metadata.Fingerprint) > 256 || metadata.ComponentVersion == "" || len(metadata.ComponentVersion) > 128 ||
		(metadata.CreatedUnixMillis != 0 || metadata.ExpiresUnixMillis != 0) &&
			(metadata.CreatedUnixMillis <= 0 || metadata.ExpiresUnixMillis <= metadata.CreatedUnixMillis) {
		return false
	}
	canonical, err := canonicalABN(metadata.CanonicalABN)
	return err == nil && canonical == metadata.CanonicalABN
}

func (v *Vault) validPendingTarget(pending pendingMutation) bool {
	var prefix string
	switch pending.Kind {
	case CredentialKind:
		prefix = v.prefix() + "credential/"
	case ProductIDKind:
		prefix = v.prefix() + "product-id/"
	default:
		return false
	}
	digest := strings.TrimPrefix(pending.Account, prefix)
	return digest != pending.Account && regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(digest) && pending.ReservationAccount == v.reservationAccount(pending.Account)
}

func validOperationID(value string) bool {
	return regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(value)
}

func validateCredentialRecord(record CredentialRecord) error {
	if len(record.Opaque) == 0 || len(record.Opaque) > maximumOpaqueRecord || len(record.Metadata.Fingerprint) == 0 || len(record.Metadata.Fingerprint) > 256 || len(record.Metadata.ComponentVersion) == 0 || len(record.Metadata.ComponentVersion) > 128 {
		return ErrVaultInvalidInput
	}
	if _, err := canonicalABN(record.Metadata.CanonicalABN); err != nil {
		return err
	}
	return nil
}

func encodeCredentialRecord(record CredentialRecord) []byte {
	return encodeFields([][]byte{record.Opaque, []byte(record.Metadata.Fingerprint), []byte(record.Metadata.CanonicalABN),
		int64Bytes(record.Metadata.CreatedUnixMillis), int64Bytes(record.Metadata.ExpiresUnixMillis), []byte(record.Metadata.ComponentVersion)})
}

func decodeCredentialRecord(value []byte) (CredentialRecord, error) {
	fields, err := decodeFields(value, 6)
	if err != nil {
		return CredentialRecord{}, err
	}
	if len(fields[3]) != 8 || len(fields[4]) != 8 {
		return CredentialRecord{}, ErrVaultAuthentication
	}
	record := CredentialRecord{Opaque: append([]byte(nil), fields[0]...), Metadata: CredentialMetadata{Fingerprint: string(fields[1]), CanonicalABN: string(fields[2]),
		CreatedUnixMillis: int64(binary.BigEndian.Uint64(fields[3])), ExpiresUnixMillis: int64(binary.BigEndian.Uint64(fields[4])), ComponentVersion: string(fields[5])}}
	if validateCredentialRecord(record) != nil {
		clear(record.Opaque)
		return CredentialRecord{}, ErrVaultAuthentication
	}
	return record, nil
}

func encodePending(p pendingMutation) []byte {
	return encodeFields([][]byte{[]byte(p.OperationID), {byte(p.Action)}, {byte(p.Kind)}, []byte(p.Account), p.Envelope, []byte(p.ExpectedDigest), []byte(p.DesiredDigest), []byte(p.ReservationAccount), []byte(p.ReservationDigest), []byte(p.ReservationToken), {byte(p.Phase)}})
}

func encodeCommitReceipt(receipt CommitReceipt) []byte {
	return encodeFields([][]byte{[]byte(receipt.OperationID), {byte(receipt.MutationKind)}, []byte(receipt.TargetAccount),
		[]byte(receipt.ExpectedDigest), []byte(receipt.DesiredDigest), []byte(receipt.Credential.Fingerprint),
		[]byte(receipt.Credential.CanonicalABN), int64Bytes(receipt.Credential.CreatedUnixMillis),
		int64Bytes(receipt.Credential.ExpiresUnixMillis), []byte(receipt.Credential.ComponentVersion),
		{byte(receipt.ProductID.State)}, []byte(receipt.ProductID.Fingerprint)})
}

func decodeCommitReceipt(value []byte) (CommitReceipt, error) {
	fields, err := decodeFields(value, 12)
	if err != nil || len(fields[1]) != 1 || len(fields[7]) != 8 || len(fields[8]) != 8 || len(fields[10]) != 1 {
		return CommitReceipt{}, ErrVaultAuthentication
	}
	receipt := CommitReceipt{OperationID: string(fields[0]), MutationKind: MutationKind(fields[1][0]), TargetAccount: string(fields[2]),
		ExpectedDigest: string(fields[3]), DesiredDigest: string(fields[4]), Credential: CredentialMetadata{
			Fingerprint: string(fields[5]), CanonicalABN: string(fields[6]), CreatedUnixMillis: int64(binary.BigEndian.Uint64(fields[7])),
			ExpiresUnixMillis: int64(binary.BigEndian.Uint64(fields[8])), ComponentVersion: string(fields[9])},
		ProductID: ProductIDStatusResult{State: ProductIDState(fields[10][0]), Fingerprint: string(fields[11])}}
	if !validOperationID(receipt.OperationID) || !receipt.MutationKind.valid() {
		return CommitReceipt{}, ErrVaultAuthentication
	}
	return receipt, nil
}

func decodePending(value []byte) (pendingMutation, error) {
	fields, err := decodeFields(value, 11)
	if err != nil || len(fields[1]) != 1 || len(fields[2]) != 1 || len(fields[10]) != 1 {
		return pendingMutation{}, ErrVaultAuthentication
	}
	pending := pendingMutation{OperationID: string(fields[0]), Action: PendingState(fields[1][0]), Kind: RecordKind(fields[2][0]), Account: string(fields[3]), Envelope: append([]byte(nil), fields[4]...), ExpectedDigest: string(fields[5]), DesiredDigest: string(fields[6]), ReservationAccount: string(fields[7]), ReservationDigest: string(fields[8]), ReservationToken: string(fields[9]), Phase: pendingPhase(fields[10][0])}
	validDigest := func(value string) bool {
		return value == "missing" || regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(value)
	}
	if !validOperationID(pending.OperationID) || (pending.Action != PendingCreate && pending.Action != PendingReplace && pending.Action != PendingDelete) || (pending.Kind != CredentialKind && pending.Kind != ProductIDKind) || (pending.Phase != pendingPrepared && pending.Phase != pendingApplied) || !strings.HasPrefix(pending.Account, "tammy.sbr.") || (pending.Action == PendingDelete && len(pending.Envelope) != 0) || (pending.Action != PendingDelete && len(pending.Envelope) == 0) || !validDigest(pending.ExpectedDigest) || !validDigest(pending.DesiredDigest) || !regexp.MustCompile(`^tammy\.sbr\.(production|development)/reservation/[0-9a-f]{64}$`).MatchString(pending.ReservationAccount) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(pending.ReservationDigest) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(pending.ReservationToken) {
		clear(pending.Envelope)
		return pendingMutation{}, ErrVaultAuthentication
	}
	return pending, nil
}

func encodeFields(fields [][]byte) []byte {
	result := make([]byte, 0)
	for _, field := range fields {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		result = append(result, size[:]...)
		result = append(result, field...)
	}
	return result
}

func decodeFields(value []byte, count int) ([][]byte, error) {
	fields := make([][]byte, 0, count)
	offset := 0
	for len(fields) < count {
		if len(value)-offset < 4 {
			return nil, ErrVaultAuthentication
		}
		size := int(binary.BigEndian.Uint32(value[offset : offset+4]))
		offset += 4
		if size < 0 || size > len(value)-offset {
			return nil, ErrVaultAuthentication
		}
		fields = append(fields, value[offset:offset+size])
		offset += size
	}
	if offset != len(value) {
		return nil, ErrVaultAuthentication
	}
	return fields, nil
}

func int64Bytes(value int64) []byte {
	result := make([]byte, 8)
	binary.BigEndian.PutUint64(result, uint64(value))
	return result
}
func clearNested(values [][]byte) {
	for _, value := range values {
		clear(value)
	}
}

// Close clears the helper process's in-memory master-key material. Durable
// Keychain items remain available to a separately authenticated helper launch.
func (v *Vault) Close() {
	if v == nil || v.closed {
		return
	}
	clear(v.installationKey)
	clear(v.wrappingSecret)
	clear(v.installationID)
	v.installationKey = nil
	v.wrappingSecret = nil
	v.installationID = nil
	v.closed = true
}
