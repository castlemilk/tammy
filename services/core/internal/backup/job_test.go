package backup

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestBackupPassphraseCapabilitiesAreCopiedOneUseExpiredAndZeroed(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	original := []byte("secret passphrase")
	provider, err := NewInMemoryPassphraseCapabilities([]PassphraseCapability{{
		ID: "018f0000-0000-7000-8000-0000000000f1", Passphrase: original, ExpiresAt: now.Add(time.Minute),
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	original[0] = 'X'
	var callbackBytes []byte
	err = provider.WithPassphrase(context.Background(), "018f0000-0000-7000-8000-0000000000f1", func(passphrase []byte) error {
		if string(passphrase) != "secret passphrase" {
			t.Fatalf("constructor aliased caller secret: %q", passphrase)
		}
		callbackBytes = passphrase
		return context.Canceled
	})
	if !errors.Is(err, ErrBackupJob) || !errors.Is(err, context.Canceled) {
		t.Fatalf("callback cancellation error=%v", err)
	}
	if !bytes.Equal(callbackBytes, make([]byte, len(callbackBytes))) {
		t.Fatalf("callback secret not zeroed after return: %x", callbackBytes)
	}
	if err := provider.WithPassphrase(context.Background(), "018f0000-0000-7000-8000-0000000000f1",
		func([]byte) error { t.Fatal("one-use callback repeated"); return nil }); !errors.Is(err, ErrBackupJob) {
		t.Fatalf("second capability use error=%v", err)
	}

	expiring, err := NewInMemoryPassphraseCapabilities([]PassphraseCapability{{
		ID: "018f0000-0000-7000-8000-0000000000f2", Passphrase: []byte("expires"), ExpiresAt: now.Add(time.Second),
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := expiring.WithPassphrase(context.Background(), "018f0000-0000-7000-8000-0000000000f2",
		func([]byte) error { t.Fatal("expired callback ran"); return nil }); !errors.Is(err, ErrBackupJob) {
		t.Fatalf("expired capability error=%v", err)
	}
	expiring.Close()

	cancelled, err := NewInMemoryPassphraseCapabilities([]PassphraseCapability{{
		ID: "018f0000-0000-7000-8000-0000000000f3", Passphrase: []byte("cancelled"), ExpiresAt: now.Add(time.Minute),
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cancelled.WithPassphrase(cancelledContext, "018f0000-0000-7000-8000-0000000000f3",
		func([]byte) error { t.Fatal("cancelled callback ran"); return nil }); !errors.Is(err, ErrBackupJob) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capability error=%v", err)
	}
	cancelled.Close()
}

type cancelledBackupJobTransactions struct{}

type unitPassphraseProviderFunc func(context.Context, string, func([]byte) error) error

func (function unitPassphraseProviderFunc) WithPassphrase(
	ctx context.Context,
	capabilityID string,
	callback func([]byte) error,
) error {
	return function(ctx, capabilityID, callback)
}

func (cancelledBackupJobTransactions) Read(ctx context.Context, _ func(SQLExecutor) error) error {
	return ctx.Err()
}

func (cancelledBackupJobTransactions) Mutate(ctx context.Context, _ func(SQLExecutor) error) error {
	return ctx.Err()
}

func TestBackupJobWorkerJoinsCancelledClaimContext(t *testing.T) {
	worker, err := NewBackupJobWorker(BackupJobWorkerConfig{Transactions: cancelledBackupJobTransactions{},
		Backups: &Service{}, Passphrases: unitPassphraseProviderFunc(func(context.Context, string, func([]byte) error) error {
			return errors.New("unexpected passphrase request")
		}), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = worker.Run(ctx, "018f0000-0000-7000-8000-000000000111")
	if !errors.Is(err, ErrBackupJob) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled worker error=%v", err)
	}
}
