# Worker Architecture Deep Dive

## Overview

The email service uses **1,050 concurrent goroutine workers** distributed across 5 queue tiers to process millions of messages per day.

## Worker Pool Configuration

**File:** `internal/smtpd/queue.go:106-113`

```go
func (qm *QueueManager) startWorkers() {
    qm.spawnWorkers("emergency", qm.emergency, 50)   // 50 workers
    qm.spawnWorkers("msa", qm.msa, 200)              // 200 workers
    qm.spawnWorkers("int", qm.intQ, 500)             // 500 workers (HIGHEST)
    qm.spawnWorkers("out", qm.out, 200)              // 200 workers
    qm.spawnWorkers("bulk", qm.bulk, 100)            // 100 workers
}
```

### Why These Numbers?

| Tier | Workers | Rationale |
|------|---------|-----------|
| **Emergency** | 50 | Small count, high priority. Used for critical recovery messages that must be delivered immediately. |
| **MSA** | 200 | Mail Submission Agent - handles interactive user emails. Moderate volume, fast response needed. |
| **Internal** | 500 | **HIGHEST WORKER COUNT** - handles internal routing between systems. This is where millions of messages/day will flow. |
| **Outbound** | 200 | External delivery requires DNS lookups, SMTP handshakes. Medium worker count for I/O-bound work. |
| **Bulk** | 100 | Newsletters, notifications. Intentionally throttled to prevent overwhelming recipients. |

## How Workers Work

### 1. Worker Goroutine Structure

**File:** `internal/smtpd/queue.go:115-133`

```go
func (qm *QueueManager) spawnWorkers(name string, ch <-chan *Message, count int) {
    for i := 0; i < count; i++ {
        qm.wg.Add(1)
        workerID := i
        go func() {
            defer qm.wg.Done()
            for {
                select {
                case <-qm.ctx.Done():
                    // Graceful shutdown
                    qm.logger.Debug("Worker stopped", ...)
                    return
                case msg := <-ch:
                    // Process message
                    qm.processMessage(name, msg)
                }
            }
        }()
    }
}
```

**Each worker:**
1. Runs in its own goroutine (lightweight thread)
2. Blocks waiting for messages on its tier's channel
3. When message arrives, processes it
4. Returns to waiting (infinite loop)
5. Exits gracefully on shutdown signal

### 2. Message Processing Flow

**File:** `internal/smtpd/queue.go:135-164`

```go
func (qm *QueueManager) processMessage(queueName string, msg *Message) {
    // Step 1: Rate limiting
    limiter := qm.getLimiter(msg.Tier)
    if err := limiter.Wait(qm.ctx); err != nil {
        return
    }

    // Step 2: Log processing
    qm.logger.Debug("Processing message", ...)

    // Step 3: SIMULATE delivery (TODO: Real delivery!)
    time.Sleep(10 * time.Millisecond)

    // Step 4: Update storage
    if err := qm.store.UpdateStatus(msg.ID, "delivered", ""); err != nil {
        qm.updateMetrics(msg.Tier, "failed")
        qm.store.UpdateStatus(msg.ID, "pending", err.Error())
        return
    }

    // Step 5: Update metrics
    qm.updateMetrics(msg.Tier, "processed")
}
```

## Current State: SIMULATION ONLY

### What Workers Do NOW ❌

```
1. Pull message from channel          ✓
2. Apply rate limiting                 ✓
3. Sleep for 10ms (simulates work)     ← FAKE!
4. Mark message as "delivered"         ← FAKE!
5. Update metrics                      ✓
```

### What Workers SHOULD Do ✅

```
1. Pull message from channel
2. Apply rate limiting
3. Look up destination (MX records)
4. Establish SMTP connection
5. Perform SMTP handshake (EHLO, MAIL FROM, RCPT TO, DATA)
6. Transmit message
7. Handle response (2xx success, 4xx retry, 5xx fail)
8. Update storage with actual delivery status
9. Update metrics
```

## Rate Limiting Per Tier

**File:** `internal/smtpd/queue.go:85-90`

```go
// Rate limiters (messages per second, burst capacity)
emergencyLimiter: rate.NewLimiter(rate.Inf, 0),        // Unlimited
msaLimiter:       rate.NewLimiter(1000, 2000),         // 1000/s, burst 2000
intLimiter:       rate.NewLimiter(5000, 10000),        // 5000/s, burst 10000
outLimiter:       rate.NewLimiter(500, 1000),          // 500/s, burst 1000
bulkLimiter:      rate.NewLimiter(100, 500),           // 100/s, burst 500
```

**How it works:**
- Workers call `limiter.Wait()` before processing
- If rate exceeded, worker blocks until token available
- Prevents overwhelming downstream systems
- Burst capacity allows temporary spikes

### Example: Internal Tier

- **Sustained Rate:** 5,000 messages/second
- **Burst Capacity:** 10,000 messages
- **Workers:** 500 goroutines

If 10,000 messages arrive at once:
1. First 10,000 process immediately (burst)
2. After burst depleted, processes at 5,000/s
3. 500 workers share the 5,000/s quota

## Message Flow Through Workers

### Enqueue Path

```
SMTP Server
    ↓
msg.Tier = TierInt
    ↓
QueueManager.Enqueue(msg)
    ↓
Store in persistent storage (journal)
    ↓
Put in channel: qm.intQ <- msg
    ↓
[Channel Buffer: 100,000 slots]
    ↓
Worker goroutine wakes up: msg := <-ch
    ↓
processMessage(msg)
```

### Processing Path

```
Worker receives message
    ↓
Apply rate limiter (may block here)
    ↓
Log processing start
    ↓
TODO: Actual delivery (currently just sleep 10ms)
    ↓
Update status in storage
    ↓
Update metrics
    ↓
Worker returns to waiting for next message
```

## Channel Buffers

**File:** `internal/smtpd/queue.go:79-83`

```go
emergency: make(chan *Message, 10000),    // 10K buffer
msa:       make(chan *Message, 50000),    // 50K buffer
intQ:      make(chan *Message, 100000),   // 100K buffer (largest!)
out:       make(chan *Message, 50000),    // 50K buffer
bulk:      make(chan *Message, 100000),   // 100K buffer
```

**Why buffered channels?**

1. **Decouple acceptance from processing**
   - SMTP server can accept messages quickly
   - Workers process at their own pace
   - Prevents blocking SMTP sessions

2. **Handle burst traffic**
   - 100K internal messages can queue
   - Workers drain at 5000/s (20 seconds to drain full buffer)

3. **Memory vs blocking tradeoff**
   - Each buffered slot = ~1KB (message metadata)
   - 100K buffer = ~100MB RAM
   - Alternative is blocking SMTP connections (bad UX)

## Worker Concurrency Model

### Go's Goroutines

- **Lightweight:** ~2KB stack per goroutine
- **1,050 workers = ~2MB RAM** (very efficient!)
- Go runtime multiplexes onto OS threads
- Typical setup: 1,050 goroutines on 8-16 OS threads

### Why 1,050 Workers Can Handle Millions/Day

**Math:**
- Internal tier: 500 workers, 5000 msg/s = 10 msg/s per worker
- At 10ms per message = 100 msg/s per worker (underutilized!)
- Real SMTP delivery: 100-500ms per message
- With real delivery: 500 workers × 2-10 msg/s = 1,000-5,000 msg/s
- **Daily capacity:** 1,000 msg/s × 86,400s = 86.4 million/day

## What Needs to Be Implemented

### Priority 1: SMTP Delivery Agent

**Replace this:**
```go
// TODO: Integrate actual delivery/routing logic here
time.Sleep(10 * time.Millisecond)
```

**With this:**
```go
// Lookup MX records
mxRecords, err := net.LookupMX(recipientDomain)

// Try each MX in priority order
for _, mx := range sortMXByPriority(mxRecords) {
    // Connect to SMTP server
    conn, err := smtp.Dial(mx.Host + ":25")

    // SMTP handshake
    conn.Hello(ourDomain)
    conn.Mail(msg.From)
    conn.Rcpt(msg.To)

    // Send message
    w, _ := conn.Data()
    w.Write(msg.Data)
    w.Close()

    // Check response
    if success {
        return nil  // Delivered!
    }
}
```

### Priority 2: Error Handling

```go
// Classify SMTP responses
switch smtpCode {
case 250:  // Success
    return success
case 421, 450, 451, 452:  // Temporary failure
    return retry
case 550, 551, 552, 553, 554:  // Permanent failure
    return fail
}
```

### Priority 3: Connection Pooling

```go
// Reuse SMTP connections to same destination
type ConnectionPool struct {
    pools map[string]*smtp.Client
    mu    sync.Mutex
}

func (p *ConnectionPool) Get(domain string) *smtp.Client {
    // Reuse existing connection or create new
}
```

## Monitoring Workers

### Current Metrics

```go
type QueueMetrics struct {
    Enqueued   map[QueueTier]int64  // Messages added to queue
    Processed  map[QueueTier]int64  // Messages processed (fake delivery)
    Failed     map[QueueTier]int64  // Messages that failed
    Duplicates int64                 // Deduplicated messages
    LastUpdate time.Time
}
```

### View Metrics

```bash
./bin/mailctl --username admin --password changeme queue stats
```

### Metrics Exposed

```
Queue Statistics:
─────────────────────────────────────
TIER       ENQUEUED  PROCESSED  FAILED
emergency  0         0          0
msa        0         0          0
int        12        12         0      ← 12 messages "delivered"
out        0         0          0
bulk       0         0          0

Storage Statistics:
  Pending:    0
  Processing: 0
  DLQ:        0
  Total:      0
```

## Performance Characteristics

### Current (Simulated)

- **Throughput:** ~10,000 msg/s (limited by 10ms sleep)
- **Latency:** 10ms per message
- **CPU:** Near zero (just sleeping)
- **Memory:** ~50MB (workers + channels)

### Expected (Real Delivery)

- **Throughput:** 1,000-5,000 msg/s (network limited)
- **Latency:** 100-500ms per message (DNS + SMTP handshake)
- **CPU:** 20-50% (TLS, parsing, etc.)
- **Memory:** 200-500MB (connections, buffers)

## Graceful Shutdown

When service stops:

```go
1. qm.cancel()               // Signal context cancellation
2. All workers receive signal on qm.ctx.Done()
3. Workers finish current message
4. Workers exit gracefully
5. qm.wg.Wait()             // Wait for all workers
6. Service logs "QueueManager stopped gracefully"
```

**This is why you see 1,050 "Worker stopped" messages in logs!**

## Worker Lifecycle

```
Startup:
  startWorkers()
    → spawnWorkers("emergency", 50)
      → 50 goroutines created
      → Each starts infinite loop
      → Each blocks on channel receive
    → spawnWorkers("msa", 200)
      → 200 more goroutines...
    [repeat for all tiers]

Running:
  Worker loop:
    1. Wait for message on channel (blocking)
    2. Message arrives
    3. processMessage()
    4. Update metrics
    5. Go back to step 1

Shutdown:
  1. cancel() called
  2. ctx.Done() fires
  3. Each worker:
     - Sees ctx.Done()
     - Logs "Worker stopped"
     - Returns from goroutine
  4. wg.Wait() blocks until all 1,050 workers exit
  5. QueueManager shutdown complete
```

## Summary

**What workers do:**
- ✅ Pull messages from their tier's queue
- ✅ Apply rate limiting
- ✅ Update persistent storage
- ✅ Track metrics
- ❌ Actually deliver email (TODO!)

**Why 1,050 workers:**
- Spread across 5 tiers based on expected volume
- Internal tier (500 workers) handles highest load
- Each worker is only ~2KB of memory
- Can handle millions of messages per day

**Next step:**
Implement real SMTP delivery in `processMessage()` - that's the main missing piece!
