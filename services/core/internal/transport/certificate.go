package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"time"
)

type ephemeralCredentials struct {
	certificate tls.Certificate
	caPEM       string
	capability  string
}

func generateEphemeralCredentials(
	random io.Reader,
	now time.Time,
) (ephemeralCredentials, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), random)
	if err != nil {
		return ephemeralCredentials{}, errors.New("could not generate local CA key")
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), random)
	if err != nil {
		return ephemeralCredentials{}, errors.New("could not generate local server key")
	}

	caSerial, err := randomSerial(random)
	if err != nil {
		return ephemeralCredentials{}, errors.New("could not generate local CA serial")
	}
	leafSerial, err := randomSerial(random)
	if err != nil {
		return ephemeralCredentials{}, errors.New("could not generate local server serial")
	}

	notBefore := now.Add(-time.Minute)
	notAfter := now.Add(30 * time.Minute)
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "Tammy Ephemeral Local CA"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(
		random,
		caTemplate,
		caTemplate,
		&caKey.PublicKey,
		caKey,
	)
	if err != nil {
		return ephemeralCredentials{}, errors.New("could not create local CA certificate")
	}

	leafTemplate := &x509.Certificate{
		SerialNumber:          leafSerial,
		Subject:               pkix.Name{CommonName: "Tammy Ephemeral Local API"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1).To4()},
	}
	leafDER, err := x509.CreateCertificate(
		random,
		leafTemplate,
		caTemplate,
		&leafKey.PublicKey,
		caKey,
	)
	if err != nil {
		return ephemeralCredentials{}, errors.New("could not create local server certificate")
	}

	capabilityBytes := make([]byte, capabilityLength)
	if _, err := io.ReadFull(random, capabilityBytes); err != nil {
		return ephemeralCredentials{}, errors.New("could not generate local capability")
	}
	capability := capabilityEncoding.EncodeToString(capabilityBytes)
	clear(capabilityBytes)

	return ephemeralCredentials{
		certificate: tls.Certificate{
			Certificate: [][]byte{leafDER, caDER},
			PrivateKey:  leafKey,
		},
		caPEM: string(pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: caDER,
		})),
		capability: capability,
	}, nil
}

func randomSerial(random io.Reader) (*big.Int, error) {
	var bytes [16]byte
	if _, err := io.ReadFull(random, bytes[:]); err != nil {
		return nil, err
	}
	bytes[0] |= 0x80
	return new(big.Int).SetBytes(bytes[:]), nil
}
