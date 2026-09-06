// Package tlsutil loads certificate and trust material on new TLS handshakes.
package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// ServerConfig validates startup material and reloads an entire snapshot on each
// new handshake. Invalid rotations fail the handshake instead of weakening TLS.
// Deploy a cert/key pair by atomically switching a versioned secret directory.
func ServerConfig(certFile, keyFile, clientCA string, requireClient bool) (*tls.Config, error) {
	load := func() (*tls.Config, error) {
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		c := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
		if requireClient {
			if clientCA == "" {
				return nil, fmt.Errorf("mTLS requires client_ca_file")
			}
			raw, err := os.ReadFile(clientCA)
			if err != nil {
				return nil, err
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(raw) {
				return nil, fmt.Errorf("invalid client CA")
			}
			c.ClientCAs = pool
			c.ClientAuth = tls.RequireAndVerifyClientCert
		}
		return c, nil
	}
	initial, err := load()
	if err != nil {
		return nil, err
	}
	initial.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) { return load() }
	return initial, nil
}
