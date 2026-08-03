package network

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

func GenerateSelfSignedCert() (certPEM, keyPEM []byte, fingerprint string, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, "", err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, "", err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"ForgeGrid Cluster"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, "", err
	}

	certOut := new(bytes.Buffer)
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certPEM = certOut.Bytes()

	keyOut := new(bytes.Buffer)
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, "", err
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	keyPEM = keyOut.Bytes()

	h := sha256.Sum256(derBytes)
	fingerprint = hex.EncodeToString(h[:])

	return certPEM, keyPEM, fingerprint, nil
}

func FingerprintFromPEM(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", errors.New("failed to parse certificate PEM")
	}
	h := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(h[:]), nil
}

// InsecureTLSConfig creates a client TLS config that accepts a specific certificate fingerprint.
func PinTLSConfig(fingerprint string) *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("no certificates presented")
			}
			h := sha256.Sum256(cs.PeerCertificates[0].Raw)
			fp := hex.EncodeToString(h[:])
			if fp != fingerprint {
				return fmt.Errorf("certificate fingerprint mismatch! expected: %s got: %s", fingerprint, fp)
			}
			return nil
		},
	}
}
