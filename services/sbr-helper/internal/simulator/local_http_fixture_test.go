package simulator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
)

func TestLocalHTTPFixtureMatchesNetworkDisabledAdapter(t *testing.T) {
	data, err := os.ReadFile("../../../../test/fixtures/sbr/local-http-cases-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Schema    int    `json:"schema_version"`
		Contract  string `json:"contract"`
		Mode      string `json:"mode"`
		FixtureID string `json:"fixture_id"`
		ServiceID string `json:"service_id"`
		SHA256    string `json:"fixture_sha256"`
		Cases     []struct {
			Case      string                   `json:"case"`
			Outcome   protocol.Outcome         `json:"helper_outcome"`
			State     protocol.SimulatorState  `json:"simulator_state"`
			Result    protocol.Result          `json:"redacted_result"`
			Error     protocol.StableErrorCode `json:"stable_error_code"`
			Fatal     bool                     `json:"fatal"`
			Malformed string                   `json:"malformed_payload_hex"`
		} `json:"cases"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	canonical, err := ParseCanonicalFixture(CanonicalFixtureBytes())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(CanonicalFixtureBytes())
	if fixture.Schema != 1 || fixture.Contract != "TAMMY_SBR_READINESS_SIMULATOR_V1" || fixture.Mode != "LOCAL_SIMULATOR_NOT_FOR_ATO_LODGMENT" || fixture.FixtureID != canonical.FixtureID || fixture.ServiceID != canonical.ServiceID || fixture.SHA256 != hex.EncodeToString(sum[:]) || len(fixture.Cases) != 6 {
		t.Fatal("local mock fixture metadata changed")
	}
	adapter, err := NewAdapter(DenyDialer{})
	if err != nil {
		t.Fatal(err)
	}
	for index, row := range fixture.Cases {
		got, err := adapter.Select(context.Background(), "018bcfe5-6800-7000-8000-000000000088", protocol.SimulatorCase(index+1))
		if err != nil {
			t.Fatal(err)
		}
		if string(got.SemanticOutcome) != row.Case || got.Response.Outcome != row.Outcome || got.Response.SimulatorState != row.State || got.Response.RedactedResult != row.Result || got.Response.StableErrorCode != row.Error || got.Fatal != row.Fatal || hex.EncodeToString(got.MalformedPayload) != row.Malformed {
			t.Fatalf("local HTTP fixture drift in case %d", index+1)
		}
	}
}
