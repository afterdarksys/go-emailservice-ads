package delivery

import "context"

type reportSuppressionKey struct{}

// WithoutTLSReports prevents locally generated TLS reports from feeding back
// into reporting. Never derive this capability from untrusted message headers.
func WithoutTLSReports(ctx context.Context) context.Context {
	return context.WithValue(ctx, reportSuppressionKey{}, true)
}

func reportsEnabled(ctx context.Context) bool {
	suppressed, _ := ctx.Value(reportSuppressionKey{}).(bool)
	return !suppressed
}
