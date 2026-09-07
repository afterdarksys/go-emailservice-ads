# Measured listing, storage and DNS improvements

Measured 7 September 2026 on macOS amd64, Intel i9-9880H, Go 1.27.1. Three
200 ms benchmark samples per case; values below are representative medians.
These are local microbenchmarks, not a production capacity certification.

| Workload | Before | After | Practical change |
| --- | --- | --- | --- |
| List 1,000 emails with 64 KiB bodies, page of 50 | 27.0 ms; 85.2 MB allocated; 1,000 body reads | 4.89 ms; 1.67–1.74 MB allocated; zero body reads | Metadata filters/page listing avoid body fetches |
| Read a thread identity for a 64 KiB message | 31.2 µs; 80.3 KB allocated | 59.9 ns; zero allocation after warmup | Storage reads immutable headers once, caches derived ID |
| Repeated NXDOMAIN, deterministic in-process upstream | 734.5 ns; 544 B; one upstream query/op | 176.6 ns; 112 B; one upstream query for the whole run | Negative results are cached for 30 seconds |

The DNS fixture has no network latency, so its timing understates avoided network
cost and must not be interpreted as internet DNS latency. Cache misses and
negative-cache hits are visible through the statistics API.

Reproduce with:

```sh
go test ./internal/jmap -run '^$' -bench BenchmarkMailboxListing -benchmem -benchtime=200ms -count=3
go test ./internal/storage -run '^$' -bench BenchmarkThreadMetadata -benchmem -benchtime=200ms -count=3
go test ./internal/dns -run '^$' -bench BenchmarkRepeatedNXDOMAIN -benchmem -benchtime=200ms -count=3
```

Listing baseline was measured before the metadata-only branch in this commit.
The storage benchmark retains both implementations for direct comparison. DNS
baseline used the same deterministic fixture before negative caching was enabled.

The thread cache is derived from immutable headers, bounded at 100,000 IDs, and
rebuilds after restart. No on-disk format migration is required. Full-text/header
filters still read applicable message content; mailbox/keyword filters run first.
The implementation still materializes account metadata and sorts query results;
it does not claim streaming search over arbitrarily large mailboxes.

MX/TXT cache keys normalize case and the trailing root dot. Positive caching keeps
the existing five-minute TTL. Only NXDOMAIN/no-record errors are negatively cached;
timeouts, temporary errors and cancellation are not. Each record cache is bounded
at 4,096 entries. Concurrent misses are coalesced. Returned records and DNS errors
are copied so callers cannot corrupt the cache. Cache clear forces new lookups.

Compression and speculative DNS prefetch are not enabled: this measured change
removes body copies and repeated DNS work without changing stored format or
creating background queries. Revisit them with representative storage/traffic
measurements if disk footprint or cold-cache latency becomes the limiting factor.
