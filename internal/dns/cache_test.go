package dns

import (
	"context"
	"go.uber.org/zap"
	"net"
	"testing"
)

func TestNegativeCacheCanonicalNamesAndIsolation(t *testing.T) {
	r := NewResolver(zap.NewNop())
	source := &missingResolver{Resolver: net.DefaultResolver}
	r.resolver = source
	_, err := r.LookupTXT(context.Background(), "MISSING.example.test.")
	if err == nil {
		t.Fatal("missing error")
	}
	err.(*net.DNSError).IsNotFound = false
	_, err = r.LookupTXT(context.Background(), "missing.example.test")
	if err == nil || !err.(*net.DNSError).IsNotFound || source.calls.Load() != 1 {
		t.Fatal(err, source.calls.Load())
	}
	r.ClearCache()
	r.LookupTXT(context.Background(), "missing.example.test")
	if source.calls.Load() != 2 {
		t.Fatal("clear failed")
	}
}
