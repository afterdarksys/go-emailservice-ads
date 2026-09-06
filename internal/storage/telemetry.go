package storage

import (
	"golang.org/x/sys/unix"
	"time"
)

type Telemetry struct {
	States        map[string]int
	QueueBytes    int64
	OldestSeconds float64
	FreeBytes     uint64
}

func (s *MessageStore) Telemetry() Telemetry {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	out := Telemetry{States: map[string]int{}}
	for _, e := range s.index {
		out.States[e.Status]++
		if e.Tier != "mailbox" {
			out.QueueBytes += int64(len(e.Data))
		}
		if e.Status == "pending" || e.Status == "queued" || e.Status == "processing" {
			age := time.Since(e.CreatedAt).Seconds()
			if age > out.OldestSeconds {
				out.OldestSeconds = age
			}
		}
	}
	var fs unix.Statfs_t
	if unix.Statfs(s.basePath, &fs) == nil {
		out.FreeBytes = uint64(fs.Bavail) * uint64(fs.Bsize)
	}
	return out
}
