package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureTLSCert returns the certificate/key pair for the daemon's https
// listener, generating a self-signed one on first use. If both files
// already exist in dir they are used as-is — this is also the override
// point for a proper locally-trusted pair (e.g. produced by mkcert:
// `mkcert -cert-file cert.pem -key-file key.pem localhost 127.0.0.1 ::1`).
//
// The generated certificate is ECDSA P-256, valid 10 years, with SANs for
// localhost, 127.0.0.1, ::1 and extraHost (the config's host, when it is a
// distinct IP — e.g. a LAN address for the mobile app). Browsers will warn
// about it until the user trusts it once (Keychain on macOS); the daemon
// logs a hint with the file path at startup.
func EnsureTLSCert(dir, extraHost string) (certFile, keyFile string, created bool, err error) {
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	_, certErr := os.Stat(certFile)
	_, keyErr := os.Stat(keyFile)
	if certErr == nil && keyErr == nil {
		return certFile, keyFile, false, nil
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", false, fmt.Errorf("create tls dir: %w", err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", false, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", false, fmt.Errorf("generate serial: %w", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "rocketd", Organization: []string{"rocket"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if extraHost != "" && extraHost != "127.0.0.1" && extraHost != "localhost" {
		if ip := net.ParseIP(extraHost); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, extraHost)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", false, fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", false, fmt.Errorf("marshal key: %w", err)
	}

	if err := writePEMFile(certFile, "CERTIFICATE", der); err != nil {
		return "", "", false, err
	}
	if err := writePEMFile(keyFile, "EC PRIVATE KEY", keyDER); err != nil {
		return "", "", false, err
	}
	return certFile, keyFile, true, nil
}

func writePEMFile(path, blockType string, der []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return nil
}
