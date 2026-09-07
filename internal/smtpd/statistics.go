package smtpd

// RegisterStatistics attaches the actual listener's resolver and greylist.
// Callbacks return snapshots; registry entries contain no mail or credentials.
func (qm *QueueManager) RegisterStatistics(listener string, snapshot func() map[string]interface{}) {
	qm.statsMu.Lock()
	defer qm.statsMu.Unlock()
	if qm.statsSources == nil {
		qm.statsSources = map[string]func() map[string]interface{}{}
	}
	qm.statsSources[listener] = snapshot
}
func (qm *QueueManager) countSecurity(kind string) {
	qm.statsMu.Lock()
	defer qm.statsMu.Unlock()
	if qm.securityCounts == nil {
		qm.securityCounts = map[string]uint64{}
	}
	qm.securityCounts[kind]++
}
func (qm *QueueManager) OperationalStatistics() map[string]interface{} {
	qm.statsMu.RLock()
	sources := map[string]func() map[string]interface{}{}
	for k, v := range qm.statsSources {
		sources[k] = v
	}
	counts := map[string]uint64{}
	for k, v := range qm.securityCounts {
		counts[k] = v
	}
	qm.statsMu.RUnlock()
	listeners := map[string]interface{}{}
	for k, v := range sources {
		listeners[k] = v()
	}
	return map[string]interface{}{"listeners": listeners, "security_events": counts, "scope": "process lifetime; resets on restart"}
}
