package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestEmbeddedMigrationsAreOrderedAndContentAuthenticated(t *testing.T) {
	steps, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("migration count = %d, want 3", len(steps))
	}
	wantNames := []string{"0001_platform.sql", "0002_ledger.sql", "0003_audit_idempotency.sql"}
	for index, step := range steps {
		wantVersion := uint32(index + 1)
		if step.Version != wantVersion || step.Name != wantNames[index] {
			t.Fatalf("migration %d = version:%d name:%q", index, step.Version, step.Name)
		}
		if len(step.SQL) == 0 {
			t.Fatalf("migration %d SQL is empty", step.Version)
		}
		digest := sha256.Sum256(step.SQL)
		if got := hex.EncodeToString(digest[:]); got != step.SHA256 {
			t.Fatalf("migration %d checksum = %q, want %q", step.Version, step.SHA256, got)
		}
	}
}

func TestPrefixReturnsAnIndependentOrderedCopy(t *testing.T) {
	prefix, err := Prefix(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefix) != 1 || prefix[0].Version != 1 {
		t.Fatalf("prefix = %#v", prefix)
	}
	prefix[0].Name = "mutated"
	again, err := Prefix(1)
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Name != "0001_platform.sql" {
		t.Fatalf("embedded migration was mutable: %#v", again[0])
	}
	for _, target := range []uint32{0, 4} {
		if _, err := Prefix(target); err == nil {
			t.Fatalf("Prefix(%d) succeeded", target)
		}
	}
}
