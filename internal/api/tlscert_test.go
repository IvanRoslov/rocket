package api

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureTLSCert_GeneratesAndReuses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")

	certFile, keyFile, created, err := EnsureTLSCert(dir, "192.168.1.50")
	if err != nil {
		t.Fatalf("EnsureTLSCert: %v", err)
	}
	if !created {
		t.Fatalf("created = false on first call, want true")
	}

	raw, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("cert is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if len(cert.DNSNames) == 0 || cert.DNSNames[0] != "localhost" {
		t.Errorf("DNSNames = %v, want localhost", cert.DNSNames)
	}
	wantIPs := map[string]bool{"127.0.0.1": false, "::1": false, "192.168.1.50": false}
	for _, ip := range cert.IPAddresses {
		wantIPs[ip.String()] = true
	}
	for ip, ok := range wantIPs {
		if !ok {
			t.Errorf("IP SAN %s missing: %v", ip, cert.IPAddresses)
		}
	}
	if time.Until(cert.NotAfter) < 9*365*24*time.Hour {
		t.Errorf("NotAfter = %v, want ~10y validity", cert.NotAfter)
	}

	// Key must be private (0600).
	if info, err := os.Stat(keyFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("key mode = %v (err %v), want 0600", info.Mode(), err)
	}

	// Second call reuses without regenerating.
	certFile2, _, created2, err := EnsureTLSCert(dir, "192.168.1.50")
	if err != nil || created2 || certFile2 != certFile {
		t.Errorf("second call: created=%v err=%v, want reuse", created2, err)
	}
}

// TestTLSListenerServesHTTP2 boots the generated cert into a real
// http.Server the same way Serve does (ServeTLS) and verifies a client
// negotiates HTTP/2 — the whole point of the tls_port listener.
func TestTLSListenerServesHTTP2(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	certFile, keyFile, _, err := EnsureTLSCert(dir, "")
	if err != nil {
		t.Fatalf("EnsureTLSCert: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "proto=%s", r.Proto)
	})}
	go srv.ServeTLS(ln, certFile, keyFile)
	defer srv.Close()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		ForceAttemptHTTP2: true,
	}}
	resp, err := client.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Errorf("ProtoMajor = %d (%s), want 2 — ALPN h2 must be negotiated", resp.ProtoMajor, resp.Proto)
	}
}
