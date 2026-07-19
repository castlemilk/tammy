package transport

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
)

const (
	// ReadinessProtocol identifies the versioned parent-child readiness record.
	ReadinessProtocol = "tammy-core-ready-v1"

	// MaxReadinessRecordBytes is the maximum encoded record size, including its
	// terminating newline.
	MaxReadinessRecordBytes = 64 << 10
)

// ReadinessRecord is the single JSON record emitted when the local API is
// ready to accept authenticated requests.
type ReadinessRecord struct {
	Protocol   string `json:"protocol"`
	Port       int    `json:"port"`
	CAPEM      string `json:"ca_pem"`
	Capability string `json:"capability"`
}

// WriteReadiness validates and emits one bounded newline-terminated record
// with one call to writer.Write.
func WriteReadiness(writer io.Writer, record ReadinessRecord) error {
	if err := validateReadiness(record); err != nil {
		return err
	}

	encoded, err := json.Marshal(record)
	if err != nil {
		return errors.New("readiness: could not encode record")
	}
	encoded = append(encoded, '\n')
	if len(encoded) > MaxReadinessRecordBytes {
		return errors.New("readiness: record exceeds 64 KiB")
	}

	written, err := writer.Write(encoded)
	if written != len(encoded) {
		return io.ErrShortWrite
	}
	if err != nil {
		return errors.New("readiness: could not write record")
	}
	return nil
}

// ReadReadiness reads and validates one bounded newline-terminated record. It
// reads no bytes after the terminating newline.
func ReadReadiness(reader io.Reader) (ReadinessRecord, error) {
	encoded := make([]byte, 0, MaxReadinessRecordBytes)
	var one [1]byte
	for {
		read, err := reader.Read(one[:])
		if read > 0 {
			if len(encoded) == MaxReadinessRecordBytes {
				return ReadinessRecord{}, errors.New("readiness: record exceeds 64 KiB")
			}
			encoded = append(encoded, one[0])
			if one[0] == '\n' {
				break
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ReadinessRecord{}, errors.New("readiness: record is not newline terminated")
			}
			return ReadinessRecord{}, errors.New("readiness: could not read record")
		}
		if read == 0 {
			return ReadinessRecord{}, errors.New("readiness: could not read record")
		}
	}

	payload := encoded[:len(encoded)-1]
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()

	var record ReadinessRecord
	if err := decoder.Decode(&record); err != nil {
		return ReadinessRecord{}, errors.New("readiness: invalid JSON record")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ReadinessRecord{}, errors.New("readiness: trailing data")
	}
	if err := validateReadiness(record); err != nil {
		return ReadinessRecord{}, err
	}
	return record, nil
}

func validateReadiness(record ReadinessRecord) error {
	if record.Protocol != ReadinessProtocol {
		return errors.New("readiness: invalid protocol")
	}
	if record.Port < 1 || record.Port > 65535 {
		return errors.New("readiness: invalid port")
	}

	block, rest := pem.Decode([]byte(record.CAPEM))
	if block == nil ||
		block.Type != "CERTIFICATE" ||
		len(bytes.TrimSpace(rest)) != 0 {
		return errors.New("readiness: invalid CA certificate")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return errors.New("readiness: invalid CA certificate")
	}

	decoded, err := capabilityEncoding.DecodeString(record.Capability)
	validCapability := err == nil &&
		len(decoded) == capabilityLength &&
		base64.RawURLEncoding.EncodeToString(decoded) == record.Capability
	clear(decoded)
	if !validCapability {
		return errors.New("readiness: invalid capability")
	}
	return nil
}
