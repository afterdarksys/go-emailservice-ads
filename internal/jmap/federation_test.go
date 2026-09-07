package jmap

import (
	"crypto/rand"
	"crypto/rsa"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestBrokerTokenAudienceIssuerAndExpiry(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.JMAP.JWTIssuer = "https://broker.test/realm"
	cfg.JMAP.JWTAudience = "mailhub-jmap"
	j := &JMAPServer{config: cfg, jwtPublicKey: &key.PublicKey, logger: zap.NewNop()}
	for _, tc := range []struct {
		aud, iss string
		exp      int64
		want     bool
	}{
		{"mailhub-jmap", cfg.JMAP.JWTIssuer, time.Now().Add(time.Minute).Unix(), true},
		{"other-service", cfg.JMAP.JWTIssuer, time.Now().Add(time.Minute).Unix(), false},
		{"mailhub-jmap", "https://untrusted.test", time.Now().Add(time.Minute).Unix(), false},
		{"mailhub-jmap", cfg.JMAP.JWTIssuer, time.Now().Add(-time.Minute).Unix(), false},
	} {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "alice", "iss": tc.iss, "aud": tc.aud, "exp": tc.exp})
		signed, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		subject, ok := j.bearerSubject("Bearer " + signed)
		if ok != tc.want || (ok && subject != "alice") {
			t.Fatal(tc, subject, ok)
		}
	}
}
