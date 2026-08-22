package vault

import (
	"errors"
	"strings"
	"testing"
)

func TestProductIDStagesSeparateScopedEnvelopeAndReturnsOnlyStatusAndFingerprint(t *testing.T) {
	store := newMemoryStore()
	v := newTestVault(t, DevelopmentNamespace, store)
	value := []byte("synthetic-product-id")
	scope := ProductScope{Product: "SBR", Service: "EVTE.BAS"}
	op := "018f0000-0000-7000-8000-000000000041"
	metadata, err := v.StageCreate(Mutation{Kind: ImportProductIDMutation, OperationID: op, ProductScope: scope, ProductID: value})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ProductID.State != ProductIDPresent || metadata.ProductID.Fingerprint == "" || strings.Contains(metadata.ProductID.Fingerprint, "synthetic") {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !allZero(value) {
		t.Fatal("Product ID input not cleared")
	}
	if err := v.Promote(op); err != nil {
		t.Fatal(err)
	}
	status, err := v.ProductIDStatus(scope)
	if err != nil || status.State != ProductIDPresent || status.Fingerprint != metadata.ProductID.Fingerprint {
		t.Fatalf("status = %+v, %v", status, err)
	}
	other, err := v.ProductIDStatus(ProductScope{Product: "SBR", Service: "EVTE.PAYROLL"})
	if !errors.Is(err, ErrVaultMissing) || other.State != ProductIDMissing {
		t.Fatalf("cross-service status = %+v, %v", other, err)
	}
	for account, stored := range store.items {
		if strings.Contains(string(stored), "synthetic-product-id") || strings.Contains(account, "synthetic-product-id") {
			t.Fatalf("raw product ID leaked via %q", account)
		}
	}
}

func TestProductIDReplaceDeleteAreDeferredAndInaccessibleIsRedacted(t *testing.T) {
	store := newMemoryStore()
	v := newTestVault(t, DevelopmentNamespace, store)
	scope := ProductScope{Product: "SBR", Service: "EVTE.BAS"}
	create := "018f0000-0000-7000-8000-000000000051"
	if _, err := v.StageCreate(Mutation{Kind: ImportProductIDMutation, OperationID: create, ProductScope: scope, ProductID: []byte("one")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(create); err != nil {
		t.Fatal(err)
	}
	if _, err := v.StageReplace(Mutation{Kind: ImportProductIDMutation, OperationID: "018f0000-0000-7000-8000-000000000052", ProductScope: scope, ProductID: []byte("two")}); !errors.Is(err, ErrVaultInvalidInput) {
		t.Fatalf("Product ID replacement accepted: %v", err)
	}
	remove := "018f0000-0000-7000-8000-000000000053"
	if err := v.StageDelete(Mutation{Kind: RemoveProductIDMutation, OperationID: remove, ProductScope: scope}); err != nil {
		t.Fatal(err)
	}
	if status, _ := v.ProductIDStatus(scope); status.State != ProductIDPresent {
		t.Fatal("delete visible before commit")
	}
	if err := v.Promote(remove); err != nil {
		t.Fatal(err)
	}
	if status, err := v.ProductIDStatus(scope); !errors.Is(err, ErrVaultMissing) || status.State != ProductIDMissing {
		t.Fatalf("deleted status = %+v, %v", status, err)
	}
	store.inaccessible = true
	if status, err := v.ProductIDStatus(scope); !errors.Is(err, ErrVaultInaccessible) || status.State != ProductIDInaccessible {
		t.Fatalf("inaccessible = %+v, %v", status, err)
	}
}
