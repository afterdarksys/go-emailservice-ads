package smtpd

import (
	"context"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/filtering"
	"io"
	"time"
)

func (q *QueueManager) WriteOperationalMetrics(ctx context.Context, w io.Writer) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ready := 0
	if q.Ready(ctx) {
		ready = 1
	}
	fmt.Fprintf(w, "mailhub_dependencies_ready %d\n", ready)
	q.metricsMu.RLock()
	count, seconds := q.metrics.DeliveryCount, q.metrics.DeliverySeconds
	q.metricsMu.RUnlock()
	fmt.Fprintf(w, "mailhub_delivery_duration_seconds_count %d\nmailhub_delivery_duration_seconds_sum %g\n", count, seconds)
	if q.StormGuard != nil {
		s := q.StormGuard.Status()
		fmt.Fprintf(w, "mailhub_mailstorm_deferred_total %d\nmailhub_mailstorm_active_circuits %d\n", s.Deferred, len(s.Circuits))
	}
	active, paused := 0, 0
	if q.destinations != nil {
		for _, s := range q.destinations.Status() {
			active += s.Active
			if s.Until.After(time.Now()) {
				paused++
			}
		}
	}
	fmt.Fprintf(w, "mailhub_destination_active_deliveries %d\nmailhub_destination_backoffs %d\n", active, paused)
	if q.platform.ClamAVAddress != "" {
		age := -1.0
		if updated, err := filtering.ClamAVDatabaseTime(ctx, q.platform.ClamAVAddress); err == nil {
			age = time.Since(updated).Seconds()
		}
		fmt.Fprintf(w, "mailhub_antivirus_database_age_seconds %g\n", age)
	}
}
