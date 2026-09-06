package delivery

import (
	"context"
	"testing"
)

func TestTLSReportSuppressionSurvivesDerivedContexts(t *testing.T) {
	ctx := context.Background()
	if !reportsEnabled(ctx) {
		t.Fatal("ordinary delivery suppressed")
	}
	child, cancel := context.WithCancel(WithoutTLSReports(ctx))
	defer cancel()
	if reportsEnabled(child) {
		t.Fatal("report delivery feeds reporting loop")
	}
	if !reportsEnabled(ctx) {
		t.Fatal("suppression leaked to unrelated delivery")
	}
}
