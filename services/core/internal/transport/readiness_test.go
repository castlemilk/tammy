package transport

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestReadinessRecordWritesBoundedNewlineTerminatedStrictJSON(t *testing.T) {
	t.Parallel()

	record := fixedReadinessRecord(t)
	writer := &recordingWriter{}
	if err := WriteReadiness(writer, record); err != nil {
		t.Fatalf("WriteReadiness() error = %v", err)
	}

	if writer.calls != 1 {
		t.Fatalf("WriteReadiness() writes = %d, want 1", writer.calls)
	}
	if got := len(writer.data); got > MaxReadinessRecordBytes {
		t.Fatalf("readiness bytes = %d, want at most %d", got, MaxReadinessRecordBytes)
	}
	if !bytes.HasSuffix(writer.data, []byte{'\n'}) || bytes.HasSuffix(writer.data, []byte("\n\n")) {
		t.Fatalf("readiness output must end in exactly one newline")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSuffix(writer.data, []byte{'\n'}), &fields); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	wantFields := []string{"protocol", "port", "ca_pem", "capability"}
	if len(fields) != len(wantFields) {
		t.Fatalf("JSON field count = %d, want %d", len(fields), len(wantFields))
	}
	for _, field := range wantFields {
		if _, ok := fields[field]; !ok {
			t.Fatalf("JSON field %q missing", field)
		}
	}

	roundTrip, err := ReadReadiness(bytes.NewReader(writer.data))
	if err != nil {
		t.Fatalf("ReadReadiness() error = %v", err)
	}
	if roundTrip != record {
		t.Fatalf("ReadReadiness() changed non-secret readiness fields")
	}
}

func TestReadinessRecordRejectsInvalidValuesWithoutLeakingCapability(t *testing.T) {
	t.Parallel()

	valid := fixedReadinessRecord(t)
	tests := []struct {
		name    string
		mutate  func(*ReadinessRecord)
		wantErr string
	}{
		{
			name: "protocol",
			mutate: func(record *ReadinessRecord) {
				record.Protocol = "tammy-core-ready-v2"
			},
			wantErr: "readiness: invalid protocol",
		},
		{
			name: "zero port",
			mutate: func(record *ReadinessRecord) {
				record.Port = 0
			},
			wantErr: "readiness: invalid port",
		},
		{
			name: "large port",
			mutate: func(record *ReadinessRecord) {
				record.Port = 65536
			},
			wantErr: "readiness: invalid port",
		},
		{
			name: "malformed PEM",
			mutate: func(record *ReadinessRecord) {
				record.CAPEM = "not a certificate"
			},
			wantErr: "readiness: invalid CA certificate",
		},
		{
			name: "padded Base64",
			mutate: func(record *ReadinessRecord) {
				record.Capability += "="
			},
			wantErr: "readiness: invalid capability",
		},
		{
			name: "non-32-byte capability",
			mutate: func(record *ReadinessRecord) {
				record.Capability = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 31))
			},
			wantErr: "readiness: invalid capability",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := valid
			tt.mutate(&record)

			var stderr bytes.Buffer
			var output bytes.Buffer
			err := WriteReadiness(&output, record)
			if err == nil {
				t.Fatal("WriteReadiness() error = nil, want rejection")
			}
			if got := fmt.Sprint(err); got != tt.wantErr {
				t.Fatalf("formatted error = %q, want %q", got, tt.wantErr)
			}
			if output.Len() != 0 {
				t.Fatalf("WriteReadiness() wrote %d bytes for invalid record", output.Len())
			}
			assertReadinessSecretAbsent(t, fmt.Sprint(err), record.Capability)
			assertReadinessSecretAbsent(t, stderr.String(), record.Capability)
		})
	}
}

func TestReadinessReaderRejectsOversizeUnknownAndTrailingInput(t *testing.T) {
	t.Parallel()

	valid := fixedReadinessRecord(t)
	validJSON, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	tests := []struct {
		name    string
		input   []byte
		wantErr string
	}{
		{
			name:    "over 64 KiB",
			input:   append(bytes.Repeat([]byte{'x'}, MaxReadinessRecordBytes), '\n'),
			wantErr: "readiness: record exceeds 64 KiB",
		},
		{
			name: "unknown field",
			input: []byte(strings.TrimSuffix(string(validJSON), "}") +
				`, "unexpected": true}` + "\n"),
			wantErr: "readiness: invalid JSON record",
		},
		{
			name:    "trailing JSON",
			input:   append(append([]byte(nil), validJSON...), []byte(" {}\n")...),
			wantErr: "readiness: trailing data",
		},
		{
			name:    "missing newline",
			input:   validJSON,
			wantErr: "readiness: record is not newline terminated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			record, gotErr := ReadReadiness(bytes.NewReader(tt.input))
			if gotErr == nil {
				t.Fatal("ReadReadiness() error = nil, want rejection")
			}
			if got := fmt.Sprint(gotErr); got != tt.wantErr {
				t.Fatalf("formatted error = %q, want %q", got, tt.wantErr)
			}
			assertReadinessSecretAbsent(t, fmt.Sprint(gotErr), valid.Capability)
			assertReadinessSecretAbsent(t, stderr.String(), valid.Capability)
			if record != (ReadinessRecord{}) {
				t.Fatal("ReadReadiness() returned a partial record")
			}
		})
	}
}

func TestReadinessWriterHandlesShortWriteWithOneCall(t *testing.T) {
	t.Parallel()

	record := fixedReadinessRecord(t)
	writer := &shortWriter{}
	err := WriteReadiness(writer, record)
	if err != io.ErrShortWrite {
		t.Fatalf("WriteReadiness() error = %v, want %v", err, io.ErrShortWrite)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteReadiness() writes = %d, want 1", writer.calls)
	}
	assertReadinessSecretAbsent(t, fmt.Sprint(err), record.Capability)
}

type recordingWriter struct {
	calls int
	data  []byte
}

func (w *recordingWriter) Write(data []byte) (int, error) {
	w.calls++
	w.data = append(w.data, data...)
	return len(data), nil
}

type shortWriter struct {
	calls int
}

func (w *shortWriter) Write(data []byte) (int, error) {
	w.calls++
	return len(data) - 1, nil
}

func fixedReadinessRecord(t *testing.T) ReadinessRecord {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Tammy readiness test CA"},
		NotBefore:             time.Unix(1_700_000_000, 0),
		NotAfter:              time.Unix(1_700_003_600, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("x509.CreateCertificate() error = %v", err)
	}

	return ReadinessRecord{
		Protocol:   ReadinessProtocol,
		Port:       43123,
		CAPEM:      string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		Capability: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, 32)),
	}
}

func assertReadinessSecretAbsent(t *testing.T, output, secret string) {
	t.Helper()
	if secret != "" && strings.Contains(output, secret) {
		t.Fatal("formatted public output contained capability material")
	}
}
