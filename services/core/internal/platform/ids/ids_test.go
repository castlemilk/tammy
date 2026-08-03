package ids_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

type countingEntropy struct {
	next byte
}

func (entropy *countingEntropy) Read(destination []byte) (int, error) {
	for index := range destination {
		destination[index] = entropy.next
		entropy.next++
	}
	return len(destination), nil
}

func TestGeneratorBuildsCanonicalUUIDv7FromInjectedTimeAndEntropy(t *testing.T) {
	source := clock.NewFixed(time.UnixMilli(0x0123456789ab))
	entropy := bytes.NewReader([]byte{0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15})
	generator, err := ids.NewGenerator(source, entropy)
	if err != nil {
		t.Fatal(err)
	}

	got, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	want := "01234567-89ab-7c0d-8e0f-101112131415"
	if got != want {
		t.Fatalf("UUID = %q, want %q", got, want)
	}
	if !ids.IsCanonicalV7(got) {
		t.Fatalf("generated UUID is not canonical v7: %q", got)
	}
}

func TestCanonicalUUIDv7ValidationRejectsOtherRepresentations(t *testing.T) {
	for _, invalid := range []string{
		"01890f3c-7b2e-4cc4-98c4-dc0c0c07398f",
		"01890F3C-7B2E-7CC4-98C4-DC0C0C07398F",
		"01890f3c-7b2e-7cc4-c8c4-dc0c0c07398f",
		"01890f3c7b2e7cc498c4dc0c0c07398f",
	} {
		if ids.IsCanonicalV7(invalid) {
			t.Fatalf("accepted invalid UUID %q", invalid)
		}
	}
}

func TestGeneratorRejectsInvalidSourcesTimeAndEntropy(t *testing.T) {
	if _, err := ids.NewGenerator(nil, bytes.NewReader(make([]byte, 10))); !errors.Is(err, ids.ErrInvalidSource) {
		t.Fatalf("nil clock error = %v, want %v", err, ids.ErrInvalidSource)
	}
	if _, err := ids.NewGenerator(clock.NewFixed(time.Unix(0, 0)), nil); !errors.Is(err, ids.ErrInvalidSource) {
		t.Fatalf("nil entropy error = %v, want %v", err, ids.ErrInvalidSource)
	}

	beforeEpoch, err := ids.NewGenerator(clock.NewFixed(time.UnixMilli(-1)), bytes.NewReader(make([]byte, 10)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beforeEpoch.New(); !errors.Is(err, ids.ErrInvalidTime) {
		t.Fatalf("pre-epoch error = %v, want %v", err, ids.ErrInvalidTime)
	}
	afterCeiling, err := ids.NewGenerator(clock.NewFixed(time.UnixMilli(1<<48)), bytes.NewReader(make([]byte, 10)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := afterCeiling.New(); !errors.Is(err, ids.ErrInvalidTime) {
		t.Fatalf("above-ceiling error = %v, want %v", err, ids.ErrInvalidTime)
	}

	exhausted, err := ids.NewGenerator(clock.NewFixed(time.UnixMilli(1)), bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exhausted.New(); !errors.Is(err, ids.ErrEntropy) || err.Error() != ids.ErrEntropy.Error() {
		t.Fatalf("entropy error = %v, want stable %v", err, ids.ErrEntropy)
	}
}

func TestGeneratorRejectsTypedNilSources(t *testing.T) {
	var source clock.Func
	if _, err := ids.NewGenerator(source, bytes.NewReader(make([]byte, 10))); !errors.Is(err, ids.ErrInvalidSource) {
		t.Fatalf("typed-nil clock error = %v, want %v", err, ids.ErrInvalidSource)
	}
	var entropy *bytes.Reader
	if _, err := ids.NewGenerator(clock.NewFixed(time.UnixMilli(1)), entropy); !errors.Is(err, ids.ErrInvalidSource) {
		t.Fatalf("typed-nil entropy error = %v, want %v", err, ids.ErrInvalidSource)
	}
}

func TestGeneratorSerializesConcurrentEntropyReads(t *testing.T) {
	entropy := &countingEntropy{}
	generator, err := ids.NewGenerator(clock.NewFixed(time.UnixMilli(1)), entropy)
	if err != nil {
		t.Fatal(err)
	}
	const count = 16
	start := make(chan struct{})
	identifiers := make(chan string, count)
	errorsChannel := make(chan error, count)
	var group sync.WaitGroup
	for range count {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			identifier, err := generator.New()
			identifiers <- identifier
			errorsChannel <- err
		}()
	}
	close(start)
	group.Wait()
	close(identifiers)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := make(map[string]struct{}, count)
	for identifier := range identifiers {
		if !ids.IsCanonicalV7(identifier) {
			t.Fatalf("noncanonical UUID %q", identifier)
		}
		seen[identifier] = struct{}{}
	}
	if len(seen) != count {
		t.Fatalf("unique UUIDs = %d, want %d", len(seen), count)
	}
}
