// Package sbrhelper defines the core-owned side of the bounded private helper protocol.
package sbrhelper

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

// ReadFrame reads one complete bounded frame and requires EOF afterwards.
// The caller owns a successful payload and must overwrite it after decoding.
func ReadFrame(reader io.Reader) ([]byte, error) {
	return readFrame(reader, nil)
}

func readFrame(reader io.Reader, observeCleanup func([]byte)) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, protocolError("FRAME_SHORT")
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > MaxPayloadSize {
		return nil, protocolError("FRAME_SIZE_INVALID")
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(reader, payload); err != nil {
		zeroBytes(payload)
		if observeCleanup != nil {
			observeCleanup(payload)
		}
		return nil, protocolError("FRAME_SHORT")
	}
	var trailing [1]byte
	n, err := reader.Read(trailing[:])
	if n != 0 || err == nil {
		zeroBytes(payload)
		if observeCleanup != nil {
			observeCleanup(payload)
		}
		return nil, protocolError("FRAME_TRAILING")
	}
	if err != io.EOF {
		zeroBytes(payload)
		if observeCleanup != nil {
			observeCleanup(payload)
		}
		return nil, protocolError("FRAME_READ")
	}
	return payload, nil
}

// WriteFrame writes the header and payload completely, including to short writers.
func WriteFrame(writer io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxPayloadSize {
		return protocolError("FRAME_SIZE_INVALID")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeAll(writer, header[:]); err != nil {
		return err
	}
	return writeAll(writer, payload)
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil || n <= 0 || n > len(data) {
			return protocolError("FRAME_WRITE")
		}
		data = data[n:]
	}
	return nil
}

const (
	ProtocolVersion         uint32 = 1
	MaxPayloadSize                 = 1 << 20
	maxDeadlineHorizon             = 5 * time.Minute
	maxPathBytes                   = 4 << 10
	maxBookmarkBytes               = 64 << 10
	maxSecretBytes                 = 1 << 10
	maxEndpointProfileBytes        = 256 << 10
)

// Request field numbers are permanently fixed: version=1, request_id=2,
// operation=3, deadline=4, environment=5, workspace=6, organisation=7,
// ABN=8, opaque_scope=9, operation_id=10, mutation_kind=11, selected_path=12,
// bookmark=13, password=14, Product ID=15, product_scope=16, service_id=17,
// endpoint_profile=18, simulator_case=19.
const (
	requestFieldVersion protowire.Number = 1 + iota
	requestFieldRequestID
	requestFieldOperation
	requestFieldDeadline
	requestFieldEnvironment
	requestFieldWorkspaceID
	requestFieldOrganisationID
	requestFieldABN
	requestFieldOpaqueScope
	requestFieldOperationID
	requestFieldMutationKind
	requestFieldSelectedPath
	requestFieldBookmark
	requestFieldPassword
	requestFieldProductID
	requestFieldProductScope
	requestFieldServiceID
	requestFieldEndpointProfile
	requestFieldSimulatorCase
)

const (
	responseFieldRequestID protowire.Number = 1 + iota
	responseFieldOutcome
	responseFieldRedactedResult
	responseFieldStableErrorCode
	responseFieldPendingItemID
)

type Operation uint32

const (
	OperationStatus            Operation = 1
	OperationUnlock            Operation = 2
	OperationFixture           Operation = 3
	OperationPrepareMutation   Operation = 4
	OperationCommitMutation    Operation = 5
	OperationAbortMutation     Operation = 6
	OperationReconcileMutation Operation = 7
)

type MutationKind uint32

const (
	MutationImportCredential  MutationKind = 1
	MutationReplaceCredential MutationKind = 2
	MutationRemoveCredential  MutationKind = 3
	MutationImportProductID   MutationKind = 4
	MutationRemoveProductID   MutationKind = 5
)

type Environment uint32

const (
	EnvironmentSimulator Environment = 1
	EnvironmentEVTE      Environment = 2
)

type SimulatorCase uint32

const (
	SimulatorAccepted          SimulatorCase = 1
	SimulatorNotStarted        SimulatorCase = 2
	SimulatorMaybeSent         SimulatorCase = 3
	SimulatorMalformedResponse SimulatorCase = 4
	SimulatorHelperDeath       SimulatorCase = 5
	SimulatorTimeout           SimulatorCase = 6
	SimulatorUnknown           SimulatorCase = 7 // forbidden in requests; recovery is ResultRecoveryRequired
)

type Outcome uint32

const (
	OutcomeOK      Outcome = 1
	OutcomeError   Outcome = 2
	OutcomePending Outcome = 3
)

type Result uint32

const (
	ResultReady                Result = 1
	ResultCredentialLocked     Result = 2
	ResultRegistrationRequired Result = 3
	ResultMutationCommitted    Result = 4
	ResultMutationAborted      Result = 5
	ResultRecoveryRequired     Result = 6
	ResultFixtureSelected      Result = 7
	ResultNotStarted           Result = 8
)

type StableErrorCode string

const (
	StableErrorUnsupportedTarget              StableErrorCode = "UNSUPPORTED_SBR_TARGET"
	StableErrorProfileMissing                 StableErrorCode = "SBR_PROFILE_MISSING"
	StableErrorProfileInvalid                 StableErrorCode = "SBR_PROFILE_INVALID"
	StableErrorProfileUntrusted               StableErrorCode = "SBR_PROFILE_UNTRUSTED"
	StableErrorProfileExpired                 StableErrorCode = "SBR_PROFILE_EXPIRED"
	StableErrorHelperUntrusted                StableErrorCode = "SBR_HELPER_UNTRUSTED"
	StableErrorHelperUnavailable              StableErrorCode = "SBR_HELPER_UNAVAILABLE"
	StableErrorHelperSandboxUnavailable       StableErrorCode = "SBR_HELPER_SANDBOX_UNAVAILABLE"
	StableErrorHelperProtocol                 StableErrorCode = "SBR_HELPER_PROTOCOL_ERROR"
	StableErrorComponentMissing               StableErrorCode = "SBR_COMPONENT_MISSING"
	StableErrorComponentUntrusted             StableErrorCode = "SBR_COMPONENT_UNTRUSTED"
	StableErrorComponentUnavailable           StableErrorCode = "SBR_COMPONENT_UNAVAILABLE"
	StableErrorComponentLicenceNotApproved    StableErrorCode = "SBR_COMPONENT_LICENCE_NOT_APPROVED"
	StableErrorRegistrationManifestMissing    StableErrorCode = "SBR_REGISTRATION_MANIFEST_MISSING"
	StableErrorRegistrationManifestInvalid    StableErrorCode = "SBR_REGISTRATION_MANIFEST_INVALID"
	StableErrorRegistrationManifestUntrusted  StableErrorCode = "SBR_REGISTRATION_MANIFEST_UNTRUSTED"
	StableErrorRegistrationManifestExpired    StableErrorCode = "SBR_REGISTRATION_MANIFEST_EXPIRED"
	StableErrorDSPRegistrationNotApproved     StableErrorCode = "SBR_DSP_REGISTRATION_NOT_APPROVED"
	StableErrorProductRegistrationNotApproved StableErrorCode = "SBR_PRODUCT_REGISTRATION_NOT_APPROVED"
	StableErrorOSFAssessmentNotApproved       StableErrorCode = "SBR_OSF_ASSESSMENT_NOT_APPROVED"
	StableErrorEVTEAccessNotApproved          StableErrorCode = "SBR_EVTE_ACCESS_NOT_APPROVED"
	StableErrorEndpointProfileMissing         StableErrorCode = "SBR_ENDPOINT_PROFILE_MISSING"
	StableErrorEndpointProfileUntrusted       StableErrorCode = "SBR_ENDPOINT_PROFILE_UNTRUSTED"
	StableErrorEndpointProfileExpired         StableErrorCode = "SBR_ENDPOINT_PROFILE_EXPIRED"
	StableErrorServiceEnrolmentNotApproved    StableErrorCode = "SBR_SERVICE_ENROLMENT_NOT_APPROVED"
	StableErrorServiceConformanceNotPassed    StableErrorCode = "SBR_SERVICE_CONFORMANCE_NOT_PASSED"
	StableErrorProductIDMissing               StableErrorCode = "SBR_PRODUCT_ID_MISSING"
	StableErrorProductIDInaccessible          StableErrorCode = "SBR_PRODUCT_ID_INACCESSIBLE"
	StableErrorSecureStoreUnavailable         StableErrorCode = "SBR_SECURE_STORE_UNAVAILABLE"
	StableErrorCredentialLocked               StableErrorCode = "SBR_CREDENTIAL_LOCKED"
	StableErrorCredentialMissing              StableErrorCode = "SBR_CREDENTIAL_MISSING"
	StableErrorCredentialInaccessible         StableErrorCode = "SBR_CREDENTIAL_INACCESSIBLE"
	StableErrorCredentialIncompatible         StableErrorCode = "SBR_CREDENTIAL_INCOMPATIBLE"
	StableErrorCredentialRevoked              StableErrorCode = "SBR_CREDENTIAL_REVOKED"
	StableErrorCredentialExpired              StableErrorCode = "SBR_CREDENTIAL_EXPIRED"
	StableErrorCredentialOrganisationMismatch StableErrorCode = "SBR_CREDENTIAL_ORGANISATION_MISMATCH"
	StableErrorMutationConflict               StableErrorCode = "SBR_MUTATION_CONFLICT"
	StableErrorRecoveryRequired               StableErrorCode = "SBR_RECOVERY_REQUIRED"
	StableErrorDeadlineExpired                StableErrorCode = "SBR_DEADLINE_EXPIRED"
)

type Request struct {
	ProtocolVersion    uint32
	RequestID          string
	Operation          Operation
	DeadlineMillis     int64
	Environment        Environment
	WorkspaceID        string
	OrganisationID     string
	CanonicalABN       string
	OpaqueScope        []byte
	OperationID        string
	MutationKind       MutationKind
	SelectedLocalPath  string
	Bookmark           []byte
	TransientPassword  []byte
	TransientProductID []byte
	ProductScope       string
	ServiceID          string
	EndpointProfile    []byte
	SimulatorCase      SimulatorCase
}

// ClearSecrets overwrites and releases request-owned sensitive buffers. Callers
// must invoke it as soon as a successfully decoded request has been consumed.
func (r *Request) ClearSecrets() {
	for _, buffer := range r.sensitiveBuffers() {
		zeroBytes(buffer)
	}
	r.OpaqueScope = nil
	r.Bookmark = nil
	r.TransientPassword = nil
	r.TransientProductID = nil
	r.EndpointProfile = nil
	r.SelectedLocalPath = ""
}

func (r *Request) sensitiveBuffers() [][]byte {
	return [][]byte{r.OpaqueScope, r.Bookmark, r.TransientPassword, r.TransientProductID, r.EndpointProfile}
}

// Response field numbers are fixed: request_id=1, outcome=2,
// redacted_result=3, stable_error_code=4, pending_item_id=5.
type Response struct {
	RequestID       string
	Outcome         Outcome
	RedactedResult  Result
	StableErrorCode StableErrorCode
	PendingItemID   string
}

// Error deliberately exposes only a stable code.
type Error struct{ code string }

func (e *Error) Error() string        { return e.code }
func (e *Error) Code() string         { return e.code }
func protocolError(code string) error { return &Error{code: code} }

func EncodeRequest(request Request, now time.Time) ([]byte, error) {
	if !validRequest(request, now) {
		return nil, protocolError("REQUEST_INVALID")
	}
	return appendRequest(nil, request), nil
}

func DecodeRequest(data []byte, now time.Time) (Request, error) {
	return decodeRequest(data, now, nil)
}

func decodeRequest(data []byte, now time.Time, observeCleanup func([][]byte, []byte)) (request Request, err error) {
	var canonical []byte
	defer func() {
		zeroBytes(canonical)
		if err != nil {
			buffers := request.sensitiveBuffers()
			request.ClearSecrets()
			if observeCleanup != nil {
				observeCleanup(buffers, canonical)
			}
			request = Request{}
		} else if observeCleanup != nil {
			observeCleanup(nil, canonical)
		}
	}()
	if len(data) == 0 || len(data) > MaxPayloadSize {
		err = protocolError("REQUEST_INVALID")
		return
	}
	remaining := data
	var last protowire.Number
	for len(remaining) > 0 {
		number, wireType, n := protowire.ConsumeTag(remaining)
		if n < 0 || number <= last || requestWireType(number) != wireType {
			err = protocolError("REQUEST_INVALID")
			return
		}
		remaining = remaining[n:]
		last = number
		var ok bool
		switch number {
		case requestFieldVersion:
			var v uint64
			v, remaining, ok = consumeVarint(remaining)
			if ok && v <= math.MaxUint32 {
				request.ProtocolVersion = uint32(v)
			} else {
				ok = false
			}
		case requestFieldRequestID:
			request.RequestID, remaining, ok = consumeOwnedString(remaining)
		case requestFieldOperation:
			var v uint64
			v, remaining, ok = consumeVarint(remaining)
			if ok && v <= math.MaxUint32 {
				request.Operation = Operation(v)
			} else {
				ok = false
			}
		case requestFieldDeadline:
			var v uint64
			v, remaining, ok = consumeVarint(remaining)
			if ok && v <= math.MaxInt64 {
				request.DeadlineMillis = int64(v)
			} else {
				ok = false
			}
		case requestFieldEnvironment:
			var v uint64
			v, remaining, ok = consumeVarint(remaining)
			if ok && v <= math.MaxUint32 {
				request.Environment = Environment(v)
			} else {
				ok = false
			}
		case requestFieldWorkspaceID:
			request.WorkspaceID, remaining, ok = consumeOwnedString(remaining)
		case requestFieldOrganisationID:
			request.OrganisationID, remaining, ok = consumeOwnedString(remaining)
		case requestFieldABN:
			request.CanonicalABN, remaining, ok = consumeOwnedString(remaining)
		case requestFieldOpaqueScope:
			request.OpaqueScope, remaining, ok = consumeOwnedBytes(remaining, 32)
		case requestFieldOperationID:
			request.OperationID, remaining, ok = consumeOwnedString(remaining)
		case requestFieldMutationKind:
			var v uint64
			v, remaining, ok = consumeVarint(remaining)
			if ok && v <= math.MaxUint32 {
				request.MutationKind = MutationKind(v)
			} else {
				ok = false
			}
		case requestFieldSelectedPath:
			request.SelectedLocalPath, remaining, ok = consumeOwnedString(remaining)
		case requestFieldBookmark:
			request.Bookmark, remaining, ok = consumeOwnedBytes(remaining, maxBookmarkBytes)
		case requestFieldPassword:
			request.TransientPassword, remaining, ok = consumeOwnedBytesAllowEmpty(remaining, maxSecretBytes)
		case requestFieldProductID:
			request.TransientProductID, remaining, ok = consumeOwnedBytes(remaining, maxSecretBytes)
		case requestFieldProductScope:
			request.ProductScope, remaining, ok = consumeOwnedString(remaining)
		case requestFieldServiceID:
			request.ServiceID, remaining, ok = consumeOwnedString(remaining)
		case requestFieldEndpointProfile:
			request.EndpointProfile, remaining, ok = consumeOwnedBytes(remaining, maxEndpointProfileBytes)
		case requestFieldSimulatorCase:
			var v uint64
			v, remaining, ok = consumeVarint(remaining)
			if ok && v <= math.MaxUint32 {
				request.SimulatorCase = SimulatorCase(v)
			} else {
				ok = false
			}
		default:
			err = protocolError("REQUEST_INVALID")
			return
		}
		if !ok {
			err = protocolError("REQUEST_INVALID")
			return
		}
	}
	canonical = appendRequest(nil, request)
	if !validRequest(request, now) || !bytes.Equal(canonical, data) {
		err = protocolError("REQUEST_INVALID")
		return
	}
	return
}

func appendRequest(dst []byte, r Request) []byte {
	dst = appendVarintField(dst, requestFieldVersion, uint64(r.ProtocolVersion))
	dst = appendStringField(dst, requestFieldRequestID, r.RequestID)
	dst = appendVarintField(dst, requestFieldOperation, uint64(r.Operation))
	dst = appendVarintField(dst, requestFieldDeadline, uint64(r.DeadlineMillis))
	dst = appendVarintField(dst, requestFieldEnvironment, uint64(r.Environment))
	if r.WorkspaceID != "" {
		dst = appendStringField(dst, requestFieldWorkspaceID, r.WorkspaceID)
	}
	if r.OrganisationID != "" {
		dst = appendStringField(dst, requestFieldOrganisationID, r.OrganisationID)
	}
	if r.CanonicalABN != "" {
		dst = appendStringField(dst, requestFieldABN, r.CanonicalABN)
	}
	if r.OpaqueScope != nil {
		dst = appendBytesField(dst, requestFieldOpaqueScope, r.OpaqueScope)
	}
	if r.OperationID != "" {
		dst = appendStringField(dst, requestFieldOperationID, r.OperationID)
	}
	if r.MutationKind != 0 {
		dst = appendVarintField(dst, requestFieldMutationKind, uint64(r.MutationKind))
	}
	if r.SelectedLocalPath != "" {
		dst = appendStringField(dst, requestFieldSelectedPath, r.SelectedLocalPath)
	}
	if r.Bookmark != nil {
		dst = appendBytesField(dst, requestFieldBookmark, r.Bookmark)
	}
	if r.TransientPassword != nil {
		dst = appendBytesField(dst, requestFieldPassword, r.TransientPassword)
	}
	if r.TransientProductID != nil {
		dst = appendBytesField(dst, requestFieldProductID, r.TransientProductID)
	}
	if r.ProductScope != "" {
		dst = appendStringField(dst, requestFieldProductScope, r.ProductScope)
	}
	if r.ServiceID != "" {
		dst = appendStringField(dst, requestFieldServiceID, r.ServiceID)
	}
	if r.EndpointProfile != nil {
		dst = appendBytesField(dst, requestFieldEndpointProfile, r.EndpointProfile)
	}
	if r.SimulatorCase != 0 {
		dst = appendVarintField(dst, requestFieldSimulatorCase, uint64(r.SimulatorCase))
	}
	return dst
}

func validRequest(r Request, now time.Time) bool {
	if r.ProtocolVersion != ProtocolVersion || !validUUIDv7(r.RequestID) || !validOperation(r.Operation) ||
		(r.Environment != EnvironmentSimulator && r.Environment != EnvironmentEVTE) ||
		r.DeadlineMillis <= now.UnixMilli() || r.DeadlineMillis > now.Add(maxDeadlineHorizon).UnixMilli() {
		return false
	}
	scoped := r.Operation != OperationFixture
	if scoped != (r.WorkspaceID != "" && r.OrganisationID != "" && r.CanonicalABN != "" && r.OpaqueScope != nil) {
		return false
	}
	if scoped && (!validUUIDv7(r.WorkspaceID) || !validUUIDv7(r.OrganisationID) || !validABN(r.CanonicalABN) || len(r.OpaqueScope) != 32) {
		return false
	}
	if r.Operation == OperationFixture {
		return r.Environment == EnvironmentSimulator && r.SimulatorCase >= SimulatorAccepted && r.SimulatorCase <= SimulatorTimeout &&
			r.WorkspaceID == "" && r.OrganisationID == "" && r.CanonicalABN == "" && r.OpaqueScope == nil &&
			r.OperationID == "" && r.MutationKind == 0 && noMutationInputs(r) && r.EndpointProfile == nil
	}
	if r.SimulatorCase != 0 {
		return false
	}
	if r.Environment == EnvironmentSimulator && r.EndpointProfile != nil {
		return false
	}
	if r.Operation == OperationStatus {
		return r.OperationID == "" && r.MutationKind == 0 && noMutationInputs(r) &&
			((r.Environment == EnvironmentEVTE && len(r.EndpointProfile) >= 1 && len(r.EndpointProfile) <= maxEndpointProfileBytes) || (r.Environment == EnvironmentSimulator && r.EndpointProfile == nil))
	}
	if r.Operation == OperationUnlock {
		return r.OperationID == "" && r.MutationKind == 0 && noMutationInputs(r) && r.EndpointProfile == nil
	}
	if !validUUIDv7(r.OperationID) || !validMutation(r.MutationKind) || r.EndpointProfile != nil {
		return false
	}
	return validMutationInputs(r)
}

func noMutationInputs(r Request) bool {
	return r.SelectedLocalPath == "" && r.Bookmark == nil && r.TransientPassword == nil && r.TransientProductID == nil && r.ProductScope == "" && r.ServiceID == ""
}

func validMutationInputs(r Request) bool {
	isPrepare := r.Operation == OperationPrepareMutation
	credentialImport := r.MutationKind == MutationImportCredential || r.MutationKind == MutationReplaceCredential
	productMutation := r.MutationKind == MutationImportProductID || r.MutationKind == MutationRemoveProductID
	if credentialImport && isPrepare {
		return validPath(r.SelectedLocalPath) && validOptionalBytes(r.Bookmark, 1, maxBookmarkBytes) && validOptionalBytes(r.TransientPassword, 0, maxSecretBytes) &&
			r.TransientProductID == nil && r.ProductScope == "" && r.ServiceID == ""
	}
	if productMutation {
		if !validProductIdentifier(r.ProductScope) || !validProductIdentifier(r.ServiceID) || r.SelectedLocalPath != "" || r.Bookmark != nil || r.TransientPassword != nil {
			return false
		}
		if r.MutationKind == MutationImportProductID && isPrepare {
			return len(r.TransientProductID) >= 1 && len(r.TransientProductID) <= maxSecretBytes
		}
		return r.TransientProductID == nil
	}
	return noMutationInputs(r)
}

func validOptionalBytes(value []byte, minimum, maximum int) bool {
	return value == nil || (len(value) >= minimum && len(value) <= maximum)
}
func validPath(value string) bool {
	return len(value) >= 1 && len(value) <= maxPathBytes && utf8.ValidString(value) && !hasControl(value) && filepath.IsAbs(value)
}

func EncodeResponse(response Response) ([]byte, error) {
	if !validResponse(response) {
		return nil, protocolError("RESPONSE_INVALID")
	}
	return appendResponse(nil, response), nil
}

func DecodeResponse(data []byte) (Response, error) {
	if len(data) == 0 || len(data) > MaxPayloadSize {
		return Response{}, protocolError("RESPONSE_INVALID")
	}
	var response Response
	remaining := data
	var last protowire.Number
	for len(remaining) > 0 {
		number, wireType, n := protowire.ConsumeTag(remaining)
		if n < 0 || number <= last || responseWireType(number) != wireType {
			return Response{}, protocolError("RESPONSE_INVALID")
		}
		remaining = remaining[n:]
		last = number
		var ok bool
		switch number {
		case responseFieldRequestID:
			response.RequestID, remaining, ok = consumeOwnedString(remaining)
		case responseFieldOutcome:
			var v uint64
			v, remaining, ok = consumeVarint(remaining)
			if ok && v <= math.MaxUint32 {
				response.Outcome = Outcome(v)
			} else {
				ok = false
			}
		case responseFieldRedactedResult:
			var v uint64
			v, remaining, ok = consumeVarint(remaining)
			if ok && v <= math.MaxUint32 {
				response.RedactedResult = Result(v)
			} else {
				ok = false
			}
		case responseFieldStableErrorCode:
			response.StableErrorCode, remaining, ok = consumeStableErrorCode(remaining)
		case responseFieldPendingItemID:
			response.PendingItemID, remaining, ok = consumeOwnedString(remaining)
		default:
			return Response{}, protocolError("RESPONSE_INVALID")
		}
		if !ok {
			return Response{}, protocolError("RESPONSE_INVALID")
		}
	}
	canonical := appendResponse(nil, response)
	valid := validResponse(response) && bytes.Equal(canonical, data)
	zeroBytes(canonical)
	if !valid {
		return Response{}, protocolError("RESPONSE_INVALID")
	}
	return response, nil
}

func appendResponse(dst []byte, r Response) []byte {
	dst = appendStringField(dst, responseFieldRequestID, r.RequestID)
	dst = appendVarintField(dst, responseFieldOutcome, uint64(r.Outcome))
	if r.RedactedResult != 0 {
		dst = appendVarintField(dst, responseFieldRedactedResult, uint64(r.RedactedResult))
	}
	if r.StableErrorCode != "" {
		dst = appendStringField(dst, responseFieldStableErrorCode, string(r.StableErrorCode))
	}
	if r.PendingItemID != "" {
		dst = appendStringField(dst, responseFieldPendingItemID, r.PendingItemID)
	}
	return dst
}

func validResponse(r Response) bool {
	if !validUUIDv7(r.RequestID) {
		return false
	}
	switch r.Outcome {
	case OutcomeOK:
		return validResult(r.RedactedResult) && r.StableErrorCode == "" && r.PendingItemID == ""
	case OutcomeError:
		return r.RedactedResult == 0 && validStableErrorCode(r.StableErrorCode) && r.PendingItemID == ""
	case OutcomePending:
		return r.RedactedResult == 0 && r.StableErrorCode == "" && validUUIDv7(r.PendingItemID)
	default:
		return false
	}
}

// NewErrorResponse maps non-allowlisted values to a generic non-secret code.
func NewErrorResponse(requestID string, code StableErrorCode) Response {
	if !validStableErrorCode(code) {
		code = StableErrorHelperProtocol
	}
	return Response{RequestID: strings.Clone(requestID), Outcome: OutcomeError, StableErrorCode: code}
}

type pendingRequest struct {
	requestID      string
	operation      Operation
	deadlineMillis int64
}

type Session struct {
	mu      sync.Mutex
	pending *pendingRequest
}

func (s *Session) Begin(request Request, now time.Time) error {
	if !validRequest(request, now) {
		return protocolError("REQUEST_INVALID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil {
		return protocolError("SESSION_BUSY")
	}
	s.pending = &pendingRequest{requestID: strings.Clone(request.RequestID), operation: request.Operation, deadlineMillis: request.DeadlineMillis}
	return nil
}

// Complete uses a trusted numeric wall clock for wire-deadline enforcement. The
// runner must additionally own a monotonic context timeout when it is added.
func (s *Session) Complete(response Response, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		return protocolError("SESSION_IDLE")
	}
	nowMillis := now.UnixMilli()
	minimumPlausible := s.pending.deadlineMillis - int64(maxDeadlineHorizon/time.Millisecond)
	if nowMillis <= 0 || nowMillis < minimumPlausible {
		s.pending = nil
		return protocolError("SESSION_CLOCK_INVALID")
	}
	if nowMillis >= s.pending.deadlineMillis {
		s.pending = nil
		return protocolError("SESSION_DEADLINE_EXPIRED")
	}
	if !validResponse(response) {
		return protocolError("SESSION_RESPONSE_INVALID")
	}
	if response.RequestID != s.pending.requestID {
		return protocolError("SESSION_MISMATCH")
	}
	if !responseMatchesOperation(s.pending.operation, response) {
		return protocolError("SESSION_RESPONSE_INVALID")
	}
	s.pending = nil
	return nil
}

func responseMatchesOperation(operation Operation, response Response) bool {
	if response.Outcome == OutcomeError {
		return true
	}
	switch operation {
	case OperationStatus:
		return response.Outcome == OutcomeOK && (response.RedactedResult == ResultReady || response.RedactedResult == ResultCredentialLocked || response.RedactedResult == ResultRegistrationRequired)
	case OperationUnlock:
		return response.Outcome == OutcomeOK && (response.RedactedResult == ResultReady || response.RedactedResult == ResultCredentialLocked)
	case OperationFixture:
		return response.Outcome == OutcomeOK && (response.RedactedResult == ResultFixtureSelected || response.RedactedResult == ResultRecoveryRequired || response.RedactedResult == ResultNotStarted)
	case OperationPrepareMutation:
		return response.Outcome == OutcomePending
	case OperationCommitMutation:
		return response.Outcome == OutcomeOK && response.RedactedResult == ResultMutationCommitted
	case OperationAbortMutation:
		return response.Outcome == OutcomeOK && response.RedactedResult == ResultMutationAborted
	case OperationReconcileMutation:
		return response.Outcome == OutcomeOK && (response.RedactedResult == ResultMutationCommitted || response.RedactedResult == ResultMutationAborted || response.RedactedResult == ResultRecoveryRequired)
	default:
		return false
	}
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	owned := make([]byte, len(value))
	copy(owned, value)
	return owned
}
func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
func consumeOwnedBytes(data []byte, maximum int) ([]byte, []byte, bool) {
	value, rest, ok := consumeBytes(data)
	if !ok || len(value) < 1 || len(value) > maximum {
		return nil, nil, false
	}
	return cloneBytes(value), rest, true
}
func consumeOwnedBytesAllowEmpty(data []byte, maximum int) ([]byte, []byte, bool) {
	value, rest, ok := consumeBytes(data)
	if !ok || len(value) > maximum {
		return nil, nil, false
	}
	return cloneBytes(value), rest, true
}
func consumeOwnedString(data []byte) (string, []byte, bool) {
	value, rest, ok := consumeBytes(data)
	if !ok || !utf8.Valid(value) {
		return "", nil, false
	}
	return string(cloneBytes(value)), rest, true
}
func consumeBytes(data []byte) ([]byte, []byte, bool) {
	value, n := protowire.ConsumeBytes(data)
	if n < 0 {
		return nil, nil, false
	}
	return value, data[n:], true
}
func consumeVarint(data []byte) (uint64, []byte, bool) {
	value, n := protowire.ConsumeVarint(data)
	if n < 0 {
		return 0, nil, false
	}
	return value, data[n:], true
}
func appendVarintField(dst []byte, field protowire.Number, value uint64) []byte {
	dst = protowire.AppendTag(dst, field, protowire.VarintType)
	return protowire.AppendVarint(dst, value)
}
func appendStringField(dst []byte, field protowire.Number, value string) []byte {
	dst = protowire.AppendTag(dst, field, protowire.BytesType)
	return protowire.AppendString(dst, value)
}
func appendBytesField(dst []byte, field protowire.Number, value []byte) []byte {
	dst = protowire.AppendTag(dst, field, protowire.BytesType)
	return protowire.AppendBytes(dst, value)
}
func requestWireType(number protowire.Number) protowire.Type {
	switch number {
	case 1, 3, 4, 5, 11, 19:
		return protowire.VarintType
	case 2, 6, 7, 8, 9, 10, 12, 13, 14, 15, 16, 17, 18:
		return protowire.BytesType
	default:
		return -1
	}
}
func responseWireType(number protowire.Number) protowire.Type {
	switch number {
	case responseFieldOutcome, responseFieldRedactedResult:
		return protowire.VarintType
	case responseFieldRequestID, responseFieldStableErrorCode, responseFieldPendingItemID:
		return protowire.BytesType
	default:
		return -1
	}
}
func validOperation(v Operation) bool { return v >= OperationStatus && v <= OperationReconcileMutation }
func validMutation(v MutationKind) bool {
	return v >= MutationImportCredential && v <= MutationRemoveProductID
}
func validResult(v Result) bool { return v >= ResultReady && v <= ResultNotStarted }

func validProductIdentifier(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= 1 && utf8.RuneCountInString(value) <= 128 && !hasControl(value)
}

var stableErrorCodes = [...]StableErrorCode{
	StableErrorUnsupportedTarget, StableErrorProfileMissing, StableErrorProfileInvalid, StableErrorProfileUntrusted,
	StableErrorProfileExpired, StableErrorHelperUntrusted, StableErrorHelperUnavailable, StableErrorHelperSandboxUnavailable,
	StableErrorHelperProtocol, StableErrorComponentMissing, StableErrorComponentUntrusted, StableErrorComponentUnavailable,
	StableErrorComponentLicenceNotApproved, StableErrorRegistrationManifestMissing, StableErrorRegistrationManifestInvalid,
	StableErrorRegistrationManifestUntrusted, StableErrorRegistrationManifestExpired, StableErrorDSPRegistrationNotApproved,
	StableErrorProductRegistrationNotApproved, StableErrorOSFAssessmentNotApproved, StableErrorEVTEAccessNotApproved,
	StableErrorEndpointProfileMissing, StableErrorEndpointProfileUntrusted, StableErrorEndpointProfileExpired,
	StableErrorServiceEnrolmentNotApproved, StableErrorServiceConformanceNotPassed, StableErrorProductIDMissing,
	StableErrorProductIDInaccessible, StableErrorSecureStoreUnavailable, StableErrorCredentialLocked,
	StableErrorCredentialMissing, StableErrorCredentialInaccessible, StableErrorCredentialIncompatible,
	StableErrorCredentialRevoked, StableErrorCredentialExpired, StableErrorCredentialOrganisationMismatch,
	StableErrorMutationConflict, StableErrorRecoveryRequired, StableErrorDeadlineExpired,
}

func validStableErrorCode(value StableErrorCode) bool {
	for _, candidate := range stableErrorCodes {
		if value == candidate {
			return true
		}
	}
	return false
}

func consumeStableErrorCode(data []byte) (StableErrorCode, []byte, bool) {
	value, rest, ok := consumeOwnedBytes(data, 96)
	if !ok {
		return "", nil, false
	}
	defer zeroBytes(value)
	for _, candidate := range stableErrorCodes {
		if bytes.Equal(value, []byte(candidate)) {
			return candidate, rest, true
		}
	}
	return "", nil, false
}

func validUUIDv7(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value[14] != '7' {
		return false
	}
	if value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b' {
		return false
	}
	for i := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := value[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func validABN(value string) bool {
	if len(value) != 11 {
		return false
	}
	weights := [...]int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	sum := 0
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
		digit := int(value[i] - '0')
		if i == 0 {
			digit -= 1
		}
		sum += digit * weights[i]
	}
	return sum%89 == 0
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
