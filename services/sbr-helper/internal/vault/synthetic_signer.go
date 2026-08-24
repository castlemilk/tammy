package vault

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
)

const SyntheticComponentVersion = "tammy-sbr-simulator-v1"
const syntheticCredentialLifetimeMillis = int64(5 * 365 * 24 * 60 * 60 * 1000)

var syntheticCredentialMagic = []byte("TAMMY-SBR-SYNTHETIC-CREDENTIAL-V1\x00")

const (
	syntheticABNBytes       = 11
	syntheticExpiryBytes    = 8
	syntheticSaltBytes      = 16
	syntheticKeyBytes       = 32
	syntheticVerifierBytes  = sha256.Size
	syntheticSignatureBytes = ed25519.SignatureSize
)

// SyntheticCredentialComponent accepts only Tammy's fixed, signed simulator
// fixture format. It is intentionally not a RAM/ATO credential parser.
type SyntheticCredentialComponent struct{}

func (SyntheticCredentialComponent) Import(original, password []byte) (CredentialRecord, error) {
	parsed, err := parseSyntheticCredential(original, password)
	if err != nil {
		return CredentialRecord{}, err
	}
	clear(parsed.key)
	fingerprint := sha256.Sum256(original)
	return CredentialRecord{Opaque: bytes.Clone(original), Metadata: CredentialMetadata{
		Fingerprint:       hex.EncodeToString(fingerprint[:]),
		CanonicalABN:      parsed.abn,
		CreatedUnixMillis: parsed.expires - syntheticCredentialLifetimeMillis,
		ExpiresUnixMillis: parsed.expires,
		ComponentVersion:  SyntheticComponentVersion,
	}}, nil
}

func (SyntheticCredentialComponent) Unlock(opaque, password []byte) ([]byte, error) {
	parsed, err := parseSyntheticCredential(opaque, password)
	if err != nil {
		return nil, err
	}
	return parsed.key, nil
}

type syntheticCredential struct {
	abn     string
	expires int64
	key     []byte
}

func parseSyntheticCredential(value, password []byte) (syntheticCredential, error) {
	fixed := len(syntheticCredentialMagic) + syntheticABNBytes + syntheticExpiryBytes + syntheticSaltBytes +
		syntheticKeyBytes + syntheticVerifierBytes + syntheticSignatureBytes
	if len(value) != fixed || !bytes.Equal(value[:len(syntheticCredentialMagic)], syntheticCredentialMagic) {
		return syntheticCredential{}, ErrVaultAuthentication
	}
	signed := value[:len(value)-syntheticSignatureBytes]
	signature := value[len(value)-syntheticSignatureBytes:]
	seed := sha256.Sum256([]byte("tammy-sbr-synthetic-fixture-signing-seed-v1"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)
	verified := ed25519.Verify(publicKey, signed, signature)
	clear(privateKey)
	clear(publicKey)
	if !verified {
		return syntheticCredential{}, ErrVaultAuthentication
	}
	offset := len(syntheticCredentialMagic)
	abn := string(value[offset : offset+syntheticABNBytes])
	if canonical, err := canonicalABN(abn); err != nil || canonical != abn {
		return syntheticCredential{}, ErrVaultAuthentication
	}
	offset += syntheticABNBytes
	expires := int64(binary.BigEndian.Uint64(value[offset : offset+syntheticExpiryBytes]))
	if expires <= 0 {
		return syntheticCredential{}, ErrVaultAuthentication
	}
	offset += syntheticExpiryBytes
	offset += syntheticSaltBytes
	key := bytes.Clone(value[offset : offset+syntheticKeyBytes])
	offset += syntheticKeyBytes
	wantVerifier := value[offset : offset+syntheticVerifierBytes]
	mac := hmac.New(sha256.New, password)
	_, _ = mac.Write([]byte("tammy-sbr-synthetic-password-v1\x00"))
	_, _ = mac.Write(value[:offset])
	actualVerifier := mac.Sum(nil)
	validPassword := hmac.Equal(actualVerifier, wantVerifier)
	clear(actualVerifier)
	if !validPassword {
		clear(key)
		return syntheticCredential{}, ErrVaultAuthentication
	}
	return syntheticCredential{abn: abn, expires: expires, key: key}, nil
}

// SyntheticSigner is the simulator-only protocol adapter for the Task 5 vault.
// The development Keychain is opened lazily so fixture-only helper invocations
// do not create or inspect vault items.
type SyntheticSigner struct {
	once        sync.Once
	initializer func() (*Vault, error)
	vault       *Vault
	err         error
}

func NewDevelopmentSyntheticSigner() *SyntheticSigner {
	return &SyntheticSigner{initializer: func() (*Vault, error) {
		return newDevelopmentVault(SyntheticCredentialComponent{}, nil)
	}}
}

func newSyntheticSigner(v *Vault) *SyntheticSigner {
	return &SyntheticSigner{vault: v}
}

func (s *SyntheticSigner) open() (*Vault, error) {
	if s == nil {
		return nil, ErrVaultInaccessible
	}
	if s.vault != nil {
		return s.vault, nil
	}
	s.once.Do(func() {
		if s.initializer == nil {
			s.err = ErrVaultInaccessible
			return
		}
		s.vault, s.err = s.initializer()
	})
	return s.vault, s.err
}

func (s *SyntheticSigner) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	defer request.ClearSecrets()
	if ctx == nil || ctx.Err() != nil {
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorHelperUnavailable)
	}
	if request.Environment != protocol.EnvironmentSimulator {
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorComponentUnavailable)
	}
	v, err := s.open()
	if err != nil {
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorSecureStoreUnavailable)
	}
	scope := Scope{WorkspaceID: request.WorkspaceID, OrganisationID: request.OrganisationID, CanonicalABN: request.CanonicalABN}
	productScope := ProductScope{Product: request.ProductScope, Service: request.ServiceID}
	switch request.Operation {
	case protocol.OperationStatus:
		metadata, readErr := v.ReadMetadata(scope)
		if errors.Is(readErr, ErrVaultMissing) {
			return protocol.Response{RequestID: request.RequestID, Outcome: protocol.OutcomeOK, RedactedResult: protocol.ResultRegistrationRequired}
		}
		if readErr != nil {
			return syntheticVaultError(request.RequestID, readErr)
		}
		return syntheticCredentialResponse(request.RequestID, protocol.OutcomeOK, protocol.ResultCredentialLocked, "", metadata)
	case protocol.OperationUnlock:
		metadata, unlockErr := v.Unlock(scope, request.TransientPassword)
		if unlockErr != nil {
			return syntheticVaultError(request.RequestID, unlockErr)
		}
		return syntheticCredentialResponse(request.RequestID, protocol.OutcomeOK, protocol.ResultReady, "", metadata)
	case protocol.OperationPrepareMutation:
		return s.prepare(v, request, scope, productScope)
	case protocol.OperationCommitMutation:
		if promoteErr := v.Promote(request.OperationID); promoteErr != nil {
			return syntheticVaultError(request.RequestID, promoteErr)
		}
		receipt, receiptErr := v.CommittedReceipt(request.OperationID)
		if receiptErr != nil {
			return syntheticVaultError(request.RequestID, receiptErr)
		}
		return s.committed(v, request, scope, productScope, receipt)
	case protocol.OperationAbortMutation:
		if abortErr := v.Abort(request.OperationID); abortErr != nil && !errors.Is(abortErr, ErrVaultMissing) {
			return syntheticVaultError(request.RequestID, abortErr)
		}
		return protocol.Response{RequestID: request.RequestID, Outcome: protocol.OutcomeOK, RedactedResult: protocol.ResultMutationAborted}
	case protocol.OperationReconcileMutation:
		pending, pendingErr := v.PendingStatus(request.OperationID)
		if pendingErr != nil {
			return syntheticVaultError(request.RequestID, pendingErr)
		}
		if pending == PendingNone {
			return protocol.Response{RequestID: request.RequestID, Outcome: protocol.OutcomeOK, RedactedResult: protocol.ResultNotStarted}
		}
		if pending == PendingCommitted {
			receipt, receiptErr := v.CommittedReceipt(request.OperationID)
			if receiptErr != nil {
				return syntheticVaultError(request.RequestID, receiptErr)
			}
			return s.committed(v, request, scope, productScope, receipt)
		}
		return protocol.Response{RequestID: request.RequestID, Outcome: protocol.OutcomePending,
			RedactedResult: protocol.ResultRecoveryRequired, PendingItemID: request.OperationID}
	default:
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorHelperProtocol)
	}
}

func (s *SyntheticSigner) prepare(v *Vault, request protocol.Request, scope Scope, productScope ProductScope) protocol.Response {
	mutation := Mutation{Kind: protocolMutationKind(request.MutationKind), OperationID: request.OperationID,
		Scope: scope, ProductScope: productScope, SelectedPath: request.SelectedLocalPath,
		Password: request.TransientPassword, ProductID: request.TransientProductID}
	switch mutation.Kind {
	case ImportCredentialMutation, ImportProductIDMutation:
		result, err := v.StageCreate(mutation)
		if err != nil {
			return syntheticVaultError(request.RequestID, err)
		}
		return syntheticPreparedResponse(request, result)
	case ReplaceCredentialMutation:
		result, err := v.StageReplace(mutation)
		if err != nil {
			return syntheticVaultError(request.RequestID, err)
		}
		return syntheticPreparedResponse(request, result)
	case RemoveCredentialMutation, RemoveProductIDMutation:
		if err := v.StageDelete(mutation); err != nil {
			return syntheticVaultError(request.RequestID, err)
		}
		response := protocol.Response{RequestID: request.RequestID, Outcome: protocol.OutcomePending, PendingItemID: request.OperationID}
		if mutation.Kind == RemoveProductIDMutation {
			response.ProductState = protocol.ProductMissing
		}
		return response
	default:
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorHelperProtocol)
	}
}

func (s *SyntheticSigner) committed(v *Vault, request protocol.Request, scope Scope, productScope ProductScope, receipt CommitReceipt) protocol.Response {
	if receipt.MutationKind != protocolMutationKind(request.MutationKind) {
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorMutationConflict)
	}
	var target string
	var err error
	if receipt.MutationKind == ImportProductIDMutation || receipt.MutationKind == RemoveProductIDMutation {
		target, err = v.productAccount(productScope)
	} else {
		target, err = v.credentialAccount(scope)
	}
	if err != nil || target != receipt.TargetAccount {
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorMutationConflict)
	}
	response := protocol.Response{RequestID: request.RequestID, Outcome: protocol.OutcomeOK, RedactedResult: protocol.ResultMutationCommitted}
	switch request.MutationKind {
	case protocol.MutationImportCredential, protocol.MutationReplaceCredential:
		return syntheticCredentialResponse(request.RequestID, protocol.OutcomeOK, protocol.ResultMutationCommitted, "", receipt.Credential)
	case protocol.MutationImportProductID:
		applySyntheticProduct(&response, receipt.ProductID)
	case protocol.MutationRemoveProductID:
		response.ProductState = protocol.ProductMissing
	case protocol.MutationRemoveCredential:
	default:
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorHelperProtocol)
	}
	return response
}

func syntheticPreparedResponse(request protocol.Request, staged StagedResult) protocol.Response {
	response := protocol.Response{RequestID: request.RequestID, Outcome: protocol.OutcomePending, PendingItemID: request.OperationID}
	if staged.Kind == ImportCredentialMutation || staged.Kind == ReplaceCredentialMutation {
		return syntheticCredentialResponse(request.RequestID, protocol.OutcomePending, 0, request.OperationID, staged.Credential)
	}
	applySyntheticProduct(&response, staged.ProductID)
	return response
}

func syntheticCredentialResponse(requestID string, outcome protocol.Outcome, result protocol.Result, pendingID string, metadata CredentialMetadata) protocol.Response {
	fingerprint, err := hex.DecodeString(metadata.Fingerprint)
	if err != nil || len(fingerprint) != sha256.Size {
		clear(fingerprint)
		return protocol.NewErrorResponse(requestID, protocol.StableErrorCredentialIncompatible)
	}
	return protocol.Response{RequestID: requestID, Outcome: outcome, RedactedResult: result, PendingItemID: pendingID,
		CanonicalABN: metadata.CanonicalABN, CredentialFingerprint: fingerprint,
		CredentialCreatedMillis: metadata.CreatedUnixMillis, CredentialExpiresMillis: metadata.ExpiresUnixMillis,
		ComponentVersion: metadata.ComponentVersion}
}

func applySyntheticProduct(response *protocol.Response, status ProductIDStatusResult) {
	switch status.State {
	case ProductIDMissing:
		response.ProductState = protocol.ProductMissing
	case ProductIDPresent, ProductIDInaccessible:
		fingerprint, err := hex.DecodeString(status.Fingerprint)
		if err != nil || len(fingerprint) != sha256.Size {
			clear(fingerprint)
			response.Outcome = protocol.OutcomeError
			response.RedactedResult = 0
			response.PendingItemID = ""
			response.StableErrorCode = protocol.StableErrorProductIDInaccessible
			return
		}
		response.ProductFingerprint = fingerprint
		response.ProductState = protocol.ProductPresent
		if status.State == ProductIDInaccessible {
			response.ProductState = protocol.ProductInaccessible
		}
	}
}

func syntheticVaultError(requestID string, err error) protocol.Response {
	code := protocol.StableErrorSecureStoreUnavailable
	switch {
	case errors.Is(err, ErrVaultMissing):
		code = protocol.StableErrorCredentialMissing
	case errors.Is(err, ErrVaultCollision), errors.Is(err, ErrVaultPending), errors.Is(err, ErrVaultCASConflict):
		code = protocol.StableErrorMutationConflict
	case errors.Is(err, ErrVaultAuthentication), errors.Is(err, ErrVaultInvalidInput):
		code = protocol.StableErrorCredentialIncompatible
	case errors.Is(err, ErrVaultBindingMismatch):
		code = protocol.StableErrorCredentialOrganisationMismatch
	}
	return protocol.NewErrorResponse(requestID, code)
}

func protocolMutationKind(kind protocol.MutationKind) MutationKind {
	switch kind {
	case protocol.MutationImportCredential:
		return ImportCredentialMutation
	case protocol.MutationReplaceCredential:
		return ReplaceCredentialMutation
	case protocol.MutationRemoveCredential:
		return RemoveCredentialMutation
	case protocol.MutationImportProductID:
		return ImportProductIDMutation
	case protocol.MutationRemoveProductID:
		return RemoveProductIDMutation
	default:
		return 0
	}
}
