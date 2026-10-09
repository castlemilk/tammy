//go:build darwin && cgo

package vault

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type recordingKeychainNative struct {
	service, group, account, digest string
	policy                          AccessPolicy
	value                           []byte
	readValue                       []byte
	readErr                         error
}

func (n *recordingKeychainNative) Read(service, group, account string) ([]byte, error) {
	n.service, n.group, n.account = service, group, account
	return append([]byte(nil), n.readValue...), n.readErr
}
func (n *recordingKeychainNative) Create(service, group, account string, value []byte, digest string, policy AccessPolicy) error {
	n.service, n.group, n.account, n.digest, n.policy = service, group, account, digest, policy
	n.value = append([]byte(nil), value...)
	return nil
}
func (n *recordingKeychainNative) CompareAndReplace(service, group, account, expected string, value []byte, digest string, policy AccessPolicy) error {
	return n.Create(service, group, account, value, digest, policy)
}
func (n *recordingKeychainNative) Delete(service, group, account, expected string) error {
	n.service, n.group, n.account, n.digest = service, group, account, expected
	return nil
}

func TestKeychainPolicyUsesExactHelperCodeRequirement(t *testing.T) {
	policy, _ := productionChannel("TEAM123456")
	requirement, err := policy.policy.CodeRequirement()
	if err != nil {
		t.Fatal(err)
	}
	want := `identifier "com.tammy.desktop.sbr-helper" and anchor apple generic and certificate leaf[subject.OU] = "TEAM123456"`
	if requirement != want {
		t.Fatalf("requirement = %q", requirement)
	}
	for _, invalid := range []AccessPolicy{
		{Namespace: ProductionNamespace, Identifier: "bad\" or true", TeamID: "TEAM123456", AccessGroup: "TEAM123456.com.tammy.desktop.sbr"},
		{Namespace: ProductionNamespace, Identifier: "com.tammy.desktop.sbr-helper", TeamID: "bad or true", AccessGroup: "bad.com.tammy.desktop.sbr"},
		{Namespace: Namespace("other"), Identifier: "com.tammy.desktop.sbr-helper", TeamID: "TEAM123456", AccessGroup: "TEAM123456.com.tammy.desktop.sbr"},
	} {
		if _, err := invalid.CodeRequirement(); err == nil {
			t.Fatalf("invalid policy accepted: %+v", invalid)
		}
	}
}

func TestKeychainServiceNameIsNamespaceSeparated(t *testing.T) {
	production, err := newProductionKeychainStore("TEAM123456")
	if err != nil {
		t.Fatal(err)
	}
	development, err := newDevelopmentKeychainStore("unit")
	if err != nil {
		t.Fatal(err)
	}
	if production.service == development.service || !strings.Contains(production.service, ".production") || !strings.Contains(development.service, ".development.") || production.policy.AccessGroup == development.policy.AccessGroup {
		t.Fatalf("stores = %+v %+v", production, development)
	}
	if _, err := development.Read("tammy.sbr.production/installation-generation"); !errors.Is(err, ErrVaultInvalidInput) {
		t.Fatalf("development opened production account: %v", err)
	}
}

func TestSimulatorVaultUsesVersionedDevelopmentKeychainService(t *testing.T) {
	identity, err := currentSimulatorCodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	suffix, err := simulatorServiceSuffix(identity)
	if err != nil {
		t.Fatal(err)
	}
	store, err := newSimulatorDevelopmentKeychainStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.service != "com.tammy.sbr.development."+suffix {
		t.Fatalf("simulator service = %q", store.service)
	}
	if store.policy != developmentChannel().policy {
		t.Fatalf("simulator policy = %+v", store.policy)
	}
}

func TestSimulatorServiceIsBoundToExactCodeIdentity(t *testing.T) {
	first := bytes.Repeat([]byte{0x11}, 20)
	second := bytes.Repeat([]byte{0x22}, 20)
	a, err := simulatorServiceSuffix(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := simulatorServiceSuffix(second)
	if err != nil || a == b || a != "simulator-v2."+strings.Repeat("11", 20) {
		t.Fatalf("code-bound simulator services = %q %q, error=%v", a, b, err)
	}
	again, err := simulatorServiceSuffix(bytes.Clone(first))
	if err != nil || again != a {
		t.Fatal("the same signed helper changed its simulator service")
	}
	for _, invalid := range [][]byte{nil, make([]byte, 20), bytes.Repeat([]byte{1}, 19), bytes.Repeat([]byte{1}, 33)} {
		if _, err := simulatorServiceSuffix(invalid); !errors.Is(err, ErrVaultInaccessible) {
			t.Fatalf("invalid code identity admitted: length=%d error=%v", len(invalid), err)
		}
	}
}

func TestKeychainStorePassesExactRequirementThroughNarrowAdapter(t *testing.T) {
	channel, _ := productionChannel("TEAM123456")
	policy := channel.policy
	native := &recordingKeychainNative{readValue: []byte("owned-copy")}
	store := &KeychainStore{service: "isolated-service", policy: policy, native: native}
	value := []byte("ciphertext")
	if err := store.Create("tammy.sbr.production/credential/abc", value, policy); err != nil {
		t.Fatal(err)
	}
	if native.policy != policy || native.group != "TEAM123456.com.tammy.desktop.sbr" || native.service != "isolated-service" || !bytes.Equal(native.value, value) {
		t.Fatalf("native call = %+v", native)
	}
	clear(value)
	if string(native.value) != "ciphertext" {
		t.Fatal("native adapter retained caller slice")
	}
	read, err := store.Read("tammy.sbr.production/credential/abc")
	if err != nil {
		t.Fatal(err)
	}
	clear(read)
	if string(native.readValue) != "owned-copy" {
		t.Fatal("store aliased adapter-owned read bytes")
	}
	wrong := policy
	wrong.Namespace = DevelopmentNamespace
	if err := store.Replace("tammy.sbr.production/credential/abc", []byte("value"), wrong); !errors.Is(err, ErrVaultInvalidInput) {
		t.Fatalf("wrong policy = %v", err)
	}
}
