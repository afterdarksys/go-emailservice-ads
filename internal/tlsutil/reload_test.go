package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pair(t *testing.T, dir string, serial int64) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "cert"), filepath.Join(dir, "key")
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: raw}), 0600)
	return certPath, keyPath
}
func TestReloadAndMutualTLS(t *testing.T) {
	dir := t.TempDir()
	cert, key := pair(t, dir, 1)
	cfg, err := ServerConfig(cert, key, cert, true)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cfg.GetConfigForClient(nil)
	if err != nil || snapshot.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatal(err)
	}
	pair(t, dir, 2)
	snapshot, err = cfg.GetConfigForClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(snapshot.Certificates[0].Certificate[0])
	if leaf.SerialNumber.Int64() != 2 {
		t.Fatal("certificate not reloaded")
	}
	for _, withCert := range []bool{false, true} {
		server, client := net.Pipe()
		server.SetDeadline(time.Now().Add(time.Second))
		client.SetDeadline(time.Now().Add(time.Second))
		done := make(chan error, 1)
		go func() { c := tls.Server(server, cfg); done <- c.Handshake(); c.Close() }()
		roots := x509.NewCertPool()
		raw, _ := os.ReadFile(cert)
		roots.AppendCertsFromPEM(raw)
		cc := &tls.Config{ServerName: "localhost", RootCAs: roots, MinVersion: tls.VersionTLS12}
		if withCert {
			p, _ := tls.LoadX509KeyPair(cert, key)
			cc.Certificates = []tls.Certificate{p}
		}
		c := tls.Client(client, cc)
		c.Handshake()
		err := <-done
		c.Close()
		if (err == nil) != withCert {
			t.Fatalf("mTLS client=%v err=%v", withCert, err)
		}
	}
	os.WriteFile(key, []byte("broken rotation"), 0600)
	if _, err = cfg.GetConfigForClient(nil); err == nil {
		t.Fatal("invalid rotation accepted")
	}
}
