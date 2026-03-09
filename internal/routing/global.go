package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/cluster/state"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
)

// HealthStatus represents the health of a region
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// RegionInfo contains information about a region
type RegionInfo struct {
	Name          string        `json:"name"`
	Endpoints     []string      `json:"endpoints"`
	Load          float64       `json:"load"`           // 0.0 - 1.0
	HealthStatus  HealthStatus  `json:"health_status"`
	Latency       time.Duration `json:"latency"`
	Capacity      int           `json:"capacity"`
	CurrentConns  int           `json:"current_conns"`
	QueueDepth    int           `json:"queue_depth"`
	LastHeartbeat time.Time     `json:"last_heartbeat"`
	Weight        int           `json:"weight"` // For weighted routing
}

// RoutingTable contains routing configuration for all regions
type RoutingTable struct {
	Version    int64                  `json:"version"`
	UpdatedAt  time.Time              `json:"updated_at"`
	Regions    map[string]*RegionInfo `json:"regions"`
	DomainRules map[string]string     `json:"domain_rules"` // domain -> preferred region
}

// RoutingDecision represents a routing decision
type RoutingDecision struct {
	Region     *RegionInfo
	Reason     string
	Latency    time.Duration
	Cost       float64
	Confidence float64 // 0.0 - 1.0
}

// GlobalRouter handles cross-region routing decisions
type GlobalRouter struct {
	currentRegion string
	stateStore    state.Store
	healthChecker *HealthChecker
	latencyMap    *LatencyMap
	geoIP         *GeoIPResolver
	logger        *zap.Logger

	routingTable *RoutingTable
	tableMu      sync.RWMutex
}

// NewGlobalRouter creates a new global routing engine
func NewGlobalRouter(currentRegion string, stateStore state.Store, logger *zap.Logger) *GlobalRouter {
	return &GlobalRouter{
		currentRegion: currentRegion,
		stateStore:    stateStore,
		healthChecker: NewHealthChecker(stateStore, logger),
		latencyMap:    NewLatencyMap(),
		geoIP:         NewGeoIPResolver(),
		logger:        logger,
		routingTable: &RoutingTable{
			Regions:     make(map[string]*RegionInfo),
			DomainRules: make(map[string]string),
		},
	}
}

// SelectRegion selects the best region for routing based on multiple factors
func (r *GlobalRouter) SelectRegion(ctx context.Context, emailCtx *policy.EmailContext) (*RegionInfo, error) {
	r.tableMu.RLock()
	defer r.tableMu.RUnlock()

	// 1. Get all healthy regions
	healthy := r.getHealthyRegions()
	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy regions available")
	}

	// 2. Check for same-region optimization (fastest, no transfer cost)
	if region, ok := r.routingTable.Regions[r.currentRegion]; ok && region.HealthStatus == HealthStatusHealthy {
		// Check if recipient domain is local to this region
		if r.isLocalDomain(emailCtx.To[0]) {
			r.logger.Debug("Using same-region routing (local domain)",
				zap.String("region", r.currentRegion))
			return region, nil
		}
	}

	// 3. Check for domain-specific routing rules
	if emailCtx.To != nil && len(emailCtx.To) > 0 {
		domain := extractDomain(emailCtx.To[0])
		if preferredRegion, ok := r.routingTable.DomainRules[domain]; ok {
			if region, ok := r.routingTable.Regions[preferredRegion]; ok && region.HealthStatus == HealthStatusHealthy {
				r.logger.Debug("Using domain-specific routing",
					zap.String("domain", domain),
					zap.String("region", preferredRegion))
				return region, nil
			}
		}
	}

	// 4. Geolocate sender and recipient
	senderRegion := r.geolocateIP(emailCtx.RemoteIP)
	recipientRegion := ""
	if len(emailCtx.To) > 0 {
		recipientRegion = r.lookupMailboxRegion(emailCtx.To[0])
	}

	// 5. Score all healthy regions
	decisions := make([]*RoutingDecision, 0, len(healthy))
	for _, region := range healthy {
		decision := r.scoreRegion(region, senderRegion, recipientRegion)
		decisions = append(decisions, decision)
	}

	// 6. Sort by score (confidence * -cost)
	sort.Slice(decisions, func(i, j int) bool {
		scoreI := decisions[i].Confidence * (1.0 / (1.0 + decisions[i].Cost))
		scoreJ := decisions[j].Confidence * (1.0 / (1.0 + decisions[j].Cost))
		return scoreI > scoreJ
	})

	best := decisions[0]
	r.logger.Info("Selected region for routing",
		zap.String("region", best.Region.Name),
		zap.String("reason", best.Reason),
		zap.Float64("confidence", best.Confidence),
		zap.Float64("cost", best.Cost))

	return best.Region, nil
}

// SelectRegionWeighted selects a region using weighted random selection
func (r *GlobalRouter) SelectRegionWeighted(ctx context.Context) (*RegionInfo, error) {
	r.tableMu.RLock()
	defer r.tableMu.RUnlock()

	healthy := r.getHealthyRegions()
	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy regions available")
	}

	// Calculate total weight
	totalWeight := 0
	for _, region := range healthy {
		totalWeight += region.Weight
	}

	if totalWeight == 0 {
		// Equal weight distribution
		return healthy[rand.Intn(len(healthy))], nil
	}

	// Weighted random selection
	random := rand.Intn(totalWeight)
	current := 0
	for _, region := range healthy {
		current += region.Weight
		if random < current {
			return region, nil
		}
	}

	return healthy[0], nil
}

// scoreRegion scores a region for routing
func (r *GlobalRouter) scoreRegion(region *RegionInfo, senderRegion, recipientRegion string) *RoutingDecision {
	decision := &RoutingDecision{
		Region:     region,
		Confidence: 1.0,
	}

	// Factor 1: Load (prefer less loaded regions)
	loadFactor := 1.0 - region.Load
	decision.Confidence *= loadFactor

	// Factor 2: Latency (prefer lower latency)
	if region.Latency > 0 {
		latencyFactor := 1.0 / (1.0 + float64(region.Latency.Milliseconds())/100.0)
		decision.Confidence *= latencyFactor
		decision.Latency = region.Latency
	}

	// Factor 3: Geographic proximity
	if senderRegion == region.Name {
		decision.Confidence *= 1.2 // Boost for sender proximity
		decision.Reason = "sender proximity"
	}
	if recipientRegion == region.Name {
		decision.Confidence *= 1.5 // Stronger boost for recipient location
		decision.Reason = "recipient location"
	}

	// Factor 4: Cost (same-region is free, cross-region has cost)
	if region.Name == r.currentRegion {
		decision.Cost = 0.0
		if decision.Reason == "" {
			decision.Reason = "same region (no transfer cost)"
		}
	} else {
		decision.Cost = r.calculateTransferCost(r.currentRegion, region.Name)
	}

	// Factor 5: Queue depth (avoid congested regions)
	if region.QueueDepth > 1000 {
		decision.Confidence *= 0.8
	}

	return decision
}

// getHealthyRegions returns all healthy regions
func (r *GlobalRouter) getHealthyRegions() []*RegionInfo {
	var healthy []*RegionInfo
	for _, region := range r.routingTable.Regions {
		if region.HealthStatus == HealthStatusHealthy {
			healthy = append(healthy, region)
		}
	}
	return healthy
}

// RegisterRegion registers a region in the routing table
func (r *GlobalRouter) RegisterRegion(ctx context.Context, region *RegionInfo) error {
	r.tableMu.Lock()
	r.routingTable.Regions[region.Name] = region
	r.routingTable.Version++
	r.routingTable.UpdatedAt = time.Now()
	r.tableMu.Unlock()

	// Persist to state store
	if r.stateStore != nil {
		key := fmt.Sprintf("/routing/regions/%s", region.Name)
		data, _ := json.Marshal(region)
		return r.stateStore.PutWithTTL(ctx, key, data, 60*time.Second)
	}

	return nil
}

// UpdateRegionHealth updates the health status of a region
func (r *GlobalRouter) UpdateRegionHealth(regionName string, status HealthStatus) {
	r.tableMu.Lock()
	defer r.tableMu.Unlock()

	if region, ok := r.routingTable.Regions[regionName]; ok {
		region.HealthStatus = status
		region.LastHeartbeat = time.Now()
		r.logger.Info("Updated region health",
			zap.String("region", regionName),
			zap.String("status", string(status)))
	}
}

// UpdateRegionLoad updates the load metrics for a region
func (r *GlobalRouter) UpdateRegionLoad(regionName string, load float64, queueDepth, currentConns int) {
	r.tableMu.Lock()
	defer r.tableMu.Unlock()

	if region, ok := r.routingTable.Regions[regionName]; ok {
		region.Load = load
		region.QueueDepth = queueDepth
		region.CurrentConns = currentConns
	}
}

// AddDomainRule adds a domain-specific routing rule
func (r *GlobalRouter) AddDomainRule(domain, region string) {
	r.tableMu.Lock()
	defer r.tableMu.Unlock()

	r.routingTable.DomainRules[domain] = region
	r.logger.Info("Added domain routing rule",
		zap.String("domain", domain),
		zap.String("region", region))
}

// WatchRoutingTable watches for routing table changes in the state store
func (r *GlobalRouter) WatchRoutingTable(ctx context.Context) error {
	if r.stateStore == nil {
		return fmt.Errorf("state store not configured")
	}

	events, err := r.stateStore.Watch(ctx, "/routing")
	if err != nil {
		return fmt.Errorf("failed to watch routing table: %w", err)
	}

	go func() {
		for event := range events {
			if event.Type == state.WatchEventPut {
				r.handleRoutingUpdate(event.Key, event.Value)
			}
		}
	}()

	return nil
}

// handleRoutingUpdate processes routing table updates
func (r *GlobalRouter) handleRoutingUpdate(key string, value []byte) {
	if strings.HasPrefix(key, "/routing/regions/") {
		var region RegionInfo
		if err := json.Unmarshal(value, &region); err != nil {
			r.logger.Error("Failed to unmarshal region info", zap.Error(err))
			return
		}

		r.tableMu.Lock()
		r.routingTable.Regions[region.Name] = &region
		r.tableMu.Unlock()

		r.logger.Debug("Updated region from state store",
			zap.String("region", region.Name))
	}
}

// Helper functions

func (r *GlobalRouter) isLocalDomain(email string) bool {
	// TODO: Check if domain is hosted locally
	return false
}

func (r *GlobalRouter) geolocateIP(ip string) string {
	if r.geoIP != nil {
		return r.geoIP.GetRegion(ip)
	}
	return ""
}

func (r *GlobalRouter) lookupMailboxRegion(email string) string {
	// TODO: Query mailbox location service
	return ""
}

func (r *GlobalRouter) calculateTransferCost(fromRegion, toRegion string) float64 {
	// Simplified cost model
	// Same region: 0.0
	// Same continent: 0.1
	// Different continent: 1.0
	if fromRegion == toRegion {
		return 0.0
	}

	// Parse region names (e.g., "us-east-1", "eu-west-1")
	fromContinent := strings.Split(fromRegion, "-")[0]
	toContinent := strings.Split(toRegion, "-")[0]

	if fromContinent == toContinent {
		return 0.1
	}

	return 1.0
}

func extractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// HealthChecker monitors region health
type HealthChecker struct {
	stateStore state.Store
	logger     *zap.Logger
	healthMu   sync.RWMutex
	health     map[string]HealthStatus
}

func NewHealthChecker(stateStore state.Store, logger *zap.Logger) *HealthChecker {
	return &HealthChecker{
		stateStore: stateStore,
		logger:     logger,
		health:     make(map[string]HealthStatus),
	}
}

func (hc *HealthChecker) IsHealthy(regionName string) bool {
	hc.healthMu.RLock()
	defer hc.healthMu.RUnlock()
	status, ok := hc.health[regionName]
	return ok && status == HealthStatusHealthy
}

func (hc *HealthChecker) UpdateHealth(regionName string, status HealthStatus) {
	hc.healthMu.Lock()
	defer hc.healthMu.Unlock()
	hc.health[regionName] = status
}

// LatencyMap tracks inter-region latency
type LatencyMap struct {
	latencies map[string]map[string]time.Duration
	mu        sync.RWMutex
}

func NewLatencyMap() *LatencyMap {
	return &LatencyMap{
		latencies: make(map[string]map[string]time.Duration),
	}
}

func (lm *LatencyMap) GetLatency(from, to string) time.Duration {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if targets, ok := lm.latencies[from]; ok {
		return targets[to]
	}
	return 0
}

func (lm *LatencyMap) UpdateLatency(from, to string, latency time.Duration) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lm.latencies[from] == nil {
		lm.latencies[from] = make(map[string]time.Duration)
	}
	lm.latencies[from][to] = latency
}

// GeoIPResolver resolves IP addresses to regions
type GeoIPResolver struct {
	// TODO: Integrate with MaxMind GeoIP or similar
}

func NewGeoIPResolver() *GeoIPResolver {
	return &GeoIPResolver{}
}

func (g *GeoIPResolver) GetRegion(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ""
	}

	// Simplified region detection based on IP ranges
	// In production, use MaxMind GeoIP2 or similar
	if ip.IsLoopback() || ip.IsPrivate() {
		return "local"
	}

	// TODO: Real GeoIP lookup
	return "unknown"
}
