// Package simulator provides Tammy's deterministic, network-disabled SBR fixture.
package simulator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
)

const canonicalFixture = `{
  "fixture_id": "SIM-SBR-READINESS-V1",
  "organisation_name": "Wattle & Co Test Pty Ltd",
  "abn": "11 000 000 560",
  "service_id": "SIM.READINESS.0001",
  "clock": "2026-06-30T00:00:00Z",
  "message_id": "SIM.MSG.0001",
  "conversation_id": "SIM.CONV.0001",
  "receipt": "SIM-READY-0001"
}
`

var (
	ErrFixtureInvalid       = errors.New("SBR_SIMULATOR_FIXTURE_INVALID")
	ErrSimulatorCaseInvalid = errors.New("SBR_SIMULATOR_CASE_INVALID")
	ErrNetworkPolicyInvalid = errors.New("SBR_SIMULATOR_NETWORK_POLICY_INVALID")
	ErrNetworkForbidden     = &NetworkForbiddenError{}
)

const (
	selfCheckPolicy = "SIMULATOR_DENY_ONLY_SELF_CHECK"
	selfCheckTarget = "INTERNAL_POLICY_PROBE"
)

// Dialer deliberately has no net package types. Its sole use in the simulator
// is the fixed composition self-check below; fixture selection has no dial path.
type Dialer interface {
	Dial(context.Context, string, string) error
}

type NetworkForbiddenError struct{}

func (*NetworkForbiddenError) Error() string { return "SBR_SIMULATOR_NETWORK_FORBIDDEN" }
func (*NetworkForbiddenError) Code() string  { return "SBR_SIMULATOR_NETWORK_FORBIDDEN" }

type DenyDialer struct{}

func (DenyDialer) Dial(context.Context, string, string) error { return ErrNetworkForbidden }

type Fixture struct {
	FixtureID        string `json:"fixture_id"`
	OrganisationName string `json:"organisation_name"`
	ABN              string `json:"abn"`
	ServiceID        string `json:"service_id"`
	Clock            string `json:"clock"`
	MessageID        string `json:"message_id"`
	ConversationID   string `json:"conversation_id"`
	Receipt          string `json:"receipt"`
}

func CanonicalFixtureBytes() []byte { return []byte(canonicalFixture) }

func ParseCanonicalFixture(data []byte) (Fixture, error) {
	if !bytes.Equal(data, []byte(canonicalFixture)) {
		return Fixture{}, ErrFixtureInvalid
	}
	var fixture Fixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		return Fixture{}, ErrFixtureInvalid
	}
	return fixture, nil
}

type SemanticOutcome string

const (
	SemanticAccepted          SemanticOutcome = "ACCEPTED"
	SemanticNotStarted        SemanticOutcome = "NOT_STARTED"
	SemanticMaybeSent         SemanticOutcome = "MAYBE_SENT"
	SemanticMalformedResponse SemanticOutcome = "MALFORMED_RESPONSE"
	SemanticHelperDeath       SemanticOutcome = "HELPER_DEATH"
	SemanticTimeout           SemanticOutcome = "TIMEOUT"
	SemanticUnknown           SemanticOutcome = "UNKNOWN"
)

type Selection struct {
	SemanticOutcome  SemanticOutcome
	FixtureBytes     []byte
	FixtureSHA256    string
	MalformedPayload []byte
	Response         protocol.Response
	Fatal            bool
}

// Adapter is deliberately stateless. Durable idempotency and replay recovery
// belong to core; the one-request helper has no send path or replay cache.
type Adapter struct{}

func NewAdapter(dialer Dialer) (*Adapter, error) {
	if dialer == nil || dialer.Dial(context.Background(), selfCheckPolicy, selfCheckTarget) != ErrNetworkForbidden {
		return nil, ErrNetworkPolicyInvalid
	}
	return &Adapter{}, nil
}

func (a *Adapter) Select(ctx context.Context, requestID string, caseID protocol.SimulatorCase) (Selection, error) {
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	if caseID < protocol.SimulatorAccepted || caseID > protocol.SimulatorTimeout {
		return Selection{}, ErrSimulatorCaseInvalid
	}
	digest := sha256.Sum256([]byte(canonicalFixture))
	selection := Selection{
		FixtureBytes:  []byte(canonicalFixture),
		FixtureSHA256: hex.EncodeToString(digest[:]),
		Response:      protocol.Response{RequestID: requestID, SimulatorCase: caseID},
	}
	switch caseID {
	case protocol.SimulatorAccepted:
		selection.SemanticOutcome = SemanticAccepted
		selection.Response.Outcome = protocol.OutcomeOK
		selection.Response.RedactedResult = protocol.ResultFixtureSelected
		selection.Response.SimulatorState = protocol.SimulatorStateAccepted
	case protocol.SimulatorNotStarted:
		selection.SemanticOutcome = SemanticNotStarted
		selection.Response.Outcome = protocol.OutcomeOK
		selection.Response.RedactedResult = protocol.ResultNotStarted
		selection.Response.SimulatorState = protocol.SimulatorStateNotStarted
	case protocol.SimulatorMaybeSent:
		selection.SemanticOutcome = SemanticMaybeSent
		selection.Response.Outcome = protocol.OutcomeOK
		selection.Response.RedactedResult = protocol.ResultRecoveryRequired
		selection.Response.SimulatorState = protocol.SimulatorStateMaybeSent
	case protocol.SimulatorMalformedResponse:
		selection.SemanticOutcome = SemanticMalformedResponse
		selection.MalformedPayload = []byte{0x0a, 0x01, 'x'}
	case protocol.SimulatorHelperDeath:
		selection.SemanticOutcome = SemanticHelperDeath
		selection.Fatal = true
	case protocol.SimulatorTimeout:
		selection.SemanticOutcome = SemanticTimeout
		selection.Response.Outcome = protocol.OutcomeError
		selection.Response.StableErrorCode = protocol.StableErrorDeadlineExpired
		selection.Response.SimulatorState = protocol.SimulatorStateMaybeSent
	}
	return selection, nil
}

// RecoverUnknown produces only a recovery result. It never re-enters a send path.
func (a *Adapter) RecoverUnknown(requestID string) Selection {
	digest := sha256.Sum256([]byte(canonicalFixture))
	return Selection{
		SemanticOutcome: SemanticUnknown,
		FixtureBytes:    []byte(canonicalFixture),
		FixtureSHA256:   hex.EncodeToString(digest[:]),
		Response: protocol.Response{
			RequestID:      requestID,
			Outcome:        protocol.OutcomeOK,
			RedactedResult: protocol.ResultRecoveryRequired,
			SimulatorCase:  protocol.SimulatorUnknown,
			SimulatorState: protocol.SimulatorStateUnknown,
		},
	}
}
