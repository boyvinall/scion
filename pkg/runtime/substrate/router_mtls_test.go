package substrate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func selfSignedBundle(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "broker"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
}

func TestNewMTLSRouterClient_PresentsClientCertAndVerifiesServer(t *testing.T) {
	var gotActor string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) != 1 || r.TLS.PeerCertificates[0].Subject.CommonName != "broker" {
			http.Error(w, "no client cert", http.StatusForbidden)
			return
		}
		gotActor = r.Header.Get(TargetActorHeader)
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	bundleFile := filepath.Join(dir, "credential-bundle.pem")
	if err := os.WriteFile(bundleFile, selfSignedBundle(t), 0o600); err != nil {
		t.Fatal(err)
	}

	client, err := NewMTLSRouterClient(server.URL, caFile, bundleFile)
	if err != nil {
		t.Fatalf("NewMTLSRouterClient() error = %v", err)
	}
	resp, err := client.Do(context.Background(), "space", "actor", http.MethodGet, "/scion/v1/healthz", nil, nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Do() status = %d, want 200", resp.StatusCode)
	}
	if gotActor != "space/actor" {
		t.Errorf("%s = %q, want %q", TargetActorHeader, gotActor, "space/actor")
	}
}

func TestNewMTLSRouterClient_RejectsCAFileWithoutCertificates(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMTLSRouterClient("https://router", caFile, "unused"); err == nil {
		t.Fatal("NewMTLSRouterClient() error = nil, want an error for a CA file with no certificates")
	}
}
