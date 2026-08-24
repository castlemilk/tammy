package simulator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
)

const fixtureSHA256 = "d4b7b2991b21eff234e272015d39eeed8d332afeb95104dc2117a2c4484ef416"

type policyDialer struct {
	calls  int
	policy string
	target string
	result error
}

func (d *policyDialer) Dial(_ context.Context, policy, target string) error {
	d.calls++
	d.policy = policy
	d.target = target
	return d.result
}

func TestCanonicalFixtureExactBytesHashAndFields(t *testing.T) {
	want, err := os.ReadFile("../../../../test/fixtures/sbr/simulator-readiness-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	got := CanonicalFixtureBytes()
	if !bytes.Equal(got, want) {
		t.Fatalf("embedded fixture differs from retained fixture\ngot  %q\nwant %q", got, want)
	}
	digest := sha256.Sum256(got)
	if hex.EncodeToString(digest[:]) != fixtureSHA256 {
		t.Fatalf("fixture sha256 = %s, want %s", hex.EncodeToString(digest[:]), fixtureSHA256)
	}
	fixture, err := ParseCanonicalFixture(got)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.FixtureID != "SIM-SBR-READINESS-V1" || fixture.OrganisationName != "Wattle & Co Test Pty Ltd" ||
		fixture.ABN != "11 000 000 560" || fixture.ServiceID != "SIM.READINESS.0001" ||
		fixture.Clock != "2026-06-30T00:00:00Z" || fixture.MessageID != "SIM.MSG.0001" ||
		fixture.ConversationID != "SIM.CONV.0001" || fixture.Receipt != "SIM-READY-0001" {
		t.Fatalf("unexpected fixture: %#v", fixture)
	}
	got[0] ^= 0xff
	if bytes.Equal(got, CanonicalFixtureBytes()) {
		t.Fatal("fixture bytes alias package storage")
	}
}

func TestCanonicalFixtureRejectsAnyEditOrExtraKey(t *testing.T) {
	original := CanonicalFixtureBytes()
	edited := bytes.Clone(original)
	edited[20] ^= 1
	extra := bytes.Replace(original, []byte("}\n"), []byte(",\"extra\":true}\n"), 1)
	for name, input := range map[string][]byte{"edited": edited, "extra": extra, "missing newline": bytes.TrimSuffix(original, []byte("\n"))} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCanonicalFixture(input); !errors.Is(err, ErrFixtureInvalid) {
				t.Fatalf("error = %v, want ErrFixtureInvalid", err)
			}
		})
	}
}

func TestSimulatorCaseMappingIsStatelessAndHasNoSendPath(t *testing.T) {
	dialer := &policyDialer{result: ErrNetworkForbidden}
	adapter, err := NewAdapter(dialer)
	if err != nil {
		t.Fatal(err)
	}
	if dialer.calls != 1 || dialer.policy != selfCheckPolicy || dialer.target != selfCheckTarget {
		t.Fatalf("policy self-check calls=%d policy=%q target=%q", dialer.calls, dialer.policy, dialer.target)
	}
	cases := []struct {
		caseID protocol.SimulatorCase
		kind   SemanticOutcome
		wire   protocol.Response
		fatal  bool
	}{
		{protocol.SimulatorAccepted, SemanticAccepted, protocol.Response{Outcome: protocol.OutcomeOK, RedactedResult: protocol.ResultFixtureSelected}, false},
		{protocol.SimulatorNotStarted, SemanticNotStarted, protocol.Response{Outcome: protocol.OutcomeOK, RedactedResult: protocol.ResultNotStarted}, false},
		{protocol.SimulatorMaybeSent, SemanticMaybeSent, protocol.Response{Outcome: protocol.OutcomeOK, RedactedResult: protocol.ResultRecoveryRequired}, false},
		{protocol.SimulatorMalformedResponse, SemanticMalformedResponse, protocol.Response{}, false},
		{protocol.SimulatorHelperDeath, SemanticHelperDeath, protocol.Response{}, true},
		{protocol.SimulatorTimeout, SemanticTimeout, protocol.Response{Outcome: protocol.OutcomeError, StableErrorCode: protocol.StableErrorDeadlineExpired}, false},
	}
	for index, tt := range cases {
		requestID := "018bcfe5-6800-7000-8000-00000000000" + string(rune('1'+index))
		first, err := adapter.Select(context.Background(), requestID, tt.caseID)
		if err != nil {
			t.Fatalf("case %d: %v", tt.caseID, err)
		}
		second, err := adapter.Select(context.Background(), requestID, tt.caseID)
		if err != nil {
			t.Fatalf("replay case %d: %v", tt.caseID, err)
		}
		if first.SemanticOutcome != tt.kind || first.Fatal != tt.fatal || first.Response.Outcome != tt.wire.Outcome ||
			first.Response.RedactedResult != tt.wire.RedactedResult || first.Response.StableErrorCode != tt.wire.StableErrorCode {
			t.Fatalf("case %d selection = %#v", tt.caseID, first)
		}
		if first.Response.RequestID != requestID || !reflect.DeepEqual(second.Response, first.Response) || second.SemanticOutcome != first.SemanticOutcome ||
			!bytes.Equal(second.FixtureBytes, first.FixtureBytes) || !bytes.Equal(second.MalformedPayload, first.MalformedPayload) || second.FixtureSHA256 != first.FixtureSHA256 {
			t.Fatalf("case %d replay changed semantics: first=%#v second=%#v", tt.caseID, first, second)
		}
		if !bytes.Equal(first.FixtureBytes, CanonicalFixtureBytes()) || first.FixtureSHA256 != fixtureSHA256 {
			t.Fatalf("case %d missing semantic fixture identity", tt.caseID)
		}
	}
	malformed, err := adapter.Select(context.Background(), "018bcfe5-6800-7000-8000-000000000088", protocol.SimulatorMalformedResponse)
	if err != nil || !bytes.Equal(malformed.MalformedPayload, []byte{0x0a, 0x01, 'x'}) {
		t.Fatalf("malformed simulator payload = %x, %v", malformed.MalformedPayload, err)
	}
	if _, err := adapter.Select(context.Background(), "018bcfe5-6800-7000-8000-000000000099", protocol.SimulatorUnknown); !errors.Is(err, ErrSimulatorCaseInvalid) {
		t.Fatalf("UNKNOWN request error = %v", err)
	}
	unknown := adapter.RecoverUnknown("018bcfe5-6800-7000-8000-000000000099")
	if unknown.SemanticOutcome != SemanticUnknown || unknown.Response.RedactedResult != protocol.ResultRecoveryRequired || unknown.Response.Outcome != protocol.OutcomeOK ||
		unknown.FixtureSHA256 != fixtureSHA256 || !bytes.Equal(unknown.FixtureBytes, CanonicalFixtureBytes()) {
		t.Fatalf("UNKNOWN recovery output = %#v", unknown)
	}
	changed, err := adapter.Select(context.Background(), "018bcfe5-6800-7000-8000-000000000001", protocol.SimulatorNotStarted)
	if err != nil || changed.SemanticOutcome != SemanticNotStarted {
		t.Fatalf("one-shot adapter retained misleading replay state: %#v, %v", changed, err)
	}
	if dialer.calls != 1 {
		t.Fatalf("fixture selection invoked dialer after policy self-check: %d", dialer.calls)
	}
}

func TestSimulatorHonorsCancelledContextBeforeSelection(t *testing.T) {
	adapter, err := NewAdapter(DenyDialer{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Select(ctx, "018bcfe5-6800-7000-8000-000000000001", protocol.SimulatorAccepted); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled selection error=%v", err)
	}
}

func TestDenyDialerReturnsExactTypedNetworkForbiddenCode(t *testing.T) {
	err := (DenyDialer{}).Dial(context.Background(), selfCheckPolicy, selfCheckTarget)
	if err != ErrNetworkForbidden || err.Error() != "SBR_SIMULATOR_NETWORK_FORBIDDEN" {
		t.Fatalf("deny error=%#v", err)
	}
	coded, ok := err.(interface{ Code() string })
	if !ok || coded.Code() != "SBR_SIMULATOR_NETWORK_FORBIDDEN" {
		t.Fatalf("deny error has no exact code: %#v", err)
	}
}

func TestSimulatorCompositionRejectsAllowingAndWrongErrorDialers(t *testing.T) {
	if adapter, err := NewAdapter(nil); adapter != nil || !errors.Is(err, ErrNetworkPolicyInvalid) {
		t.Fatalf("nil dialer adapter=%#v error=%v", adapter, err)
	}
	for name, result := range map[string]error{
		"allowed":              nil,
		"same text wrong type": errors.New("SBR_SIMULATOR_NETWORK_FORBIDDEN"),
		"wrong error":          errors.New("WRONG"),
	} {
		t.Run(name, func(t *testing.T) {
			dialer := &policyDialer{result: result}
			adapter, err := NewAdapter(dialer)
			if adapter != nil || !errors.Is(err, ErrNetworkPolicyInvalid) || dialer.calls != 1 {
				t.Fatalf("adapter=%#v error=%v calls=%d", adapter, err, dialer.calls)
			}
		})
	}
}
