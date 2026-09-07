package dns

import (
	"context"
	"go.uber.org/zap"
	"net"
	"sync/atomic"
	"testing"
)

type missingResolver struct {
	*net.Resolver
	calls atomic.Uint64
}

func (r *missingResolver) LookupTXT(context.Context, string) ([]string, error) {
	r.calls.Add(1)
	return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
}
func BenchmarkRepeatedNXDOMAIN(b *testing.B) {
	r := NewResolver(zap.NewNop())
	backend := &missingResolver{Resolver: net.DefaultResolver}
	r.resolver = backend
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.LookupTXT(context.Background(), "missing.example.test")
	}
	b.ReportMetric(float64(backend.calls.Load())/float64(b.N), "upstream_queries/op")
}
