package invoices

import (
	"context"
	"errors"
	"testing"
)

func TestRepositoryWithoutEncryptedExecutorFailsClosed(t *testing.T) {
	if _, err := NewRepository(nil); !errors.Is(err, ErrRepository) {
		t.Fatal("nil executor accepted")
	}
	repository := &Repository{}
	if _, err := repository.Get(context.Background(), "018f0000-0000-7000-8000-000000000020", "018f0000-0000-7000-8000-000000000030"); !errors.Is(err, ErrRepository) {
		t.Fatalf("zero repository read=%v", err)
	}
	if _, err := repository.List(context.Background(), "018f0000-0000-7000-8000-000000000020", 50); !errors.Is(err, ErrRepository) {
		t.Fatalf("zero repository list=%v", err)
	}
}
