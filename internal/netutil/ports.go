package netutil

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// PortAvailability represents the result of a port availability check
type PortAvailability struct {
	Address   string
	Available bool
	Error     error
}

// CheckPortAvailable tests if a port is available for listening
func CheckPortAvailable(addr string) *PortAvailability {
	result := &PortAvailability{
		Address:   addr,
		Available: false,
	}

	// Try to listen on the port
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		result.Error = err
		return result
	}

	// Port is available - close the listener immediately
	listener.Close()
	result.Available = true
	return result
}

// WaitForPortRelease waits for a port to become available with timeout
// Useful for graceful restarts where previous instance may still be shutting down
func WaitForPortRelease(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	retryInterval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		if result := CheckPortAvailable(addr); result.Available {
			return nil
		}
		time.Sleep(retryInterval)
	}

	return fmt.Errorf("port %s did not become available within %v", addr, timeout)
}

// FindAvailablePort finds an available port starting from the given port
// Increments port number until an available one is found, up to maxAttempts
func FindAvailablePort(baseAddr string, maxAttempts int) (string, error) {
	// Parse the base address to extract host and port
	host, port, err := net.SplitHostPort(baseAddr)
	if err != nil {
		return "", fmt.Errorf("invalid address %s: %w", baseAddr, err)
	}

	// Try to parse port as integer
	var basePort int
	_, err = fmt.Sscanf(port, "%d", &basePort)
	if err != nil {
		return "", fmt.Errorf("invalid port in address %s: %w", baseAddr, err)
	}

	// Try ports incrementally
	for i := 0; i < maxAttempts; i++ {
		tryPort := basePort + i
		tryAddr := net.JoinHostPort(host, fmt.Sprintf("%d", tryPort))

		if result := CheckPortAvailable(tryAddr); result.Available {
			return tryAddr, nil
		}
	}

	return "", fmt.Errorf("no available port found after %d attempts starting from %s", maxAttempts, baseAddr)
}

// GetPortConflictSuggestion provides a helpful error message for port conflicts
func GetPortConflictSuggestion(addr string, err error) string {
	if err == nil {
		return ""
	}

	var suggestions []string

	// Check if it's an address-in-use error
	if strings.Contains(err.Error(), "address already in use") {
		suggestions = append(suggestions, fmt.Sprintf("Port %s is already in use.", addr))
		suggestions = append(suggestions, "Possible solutions:")
		suggestions = append(suggestions, "  1. Stop the process using this port (use 'lsof -ti:PORT | xargs kill' on Unix)")
		suggestions = append(suggestions, "  2. Change the port in config.yaml")
		suggestions = append(suggestions, "  3. Wait a few seconds for the previous instance to fully shut down")
		suggestions = append(suggestions, "  4. Enable SO_REUSEADDR/SO_REUSEPORT for faster restarts")
	} else if strings.Contains(err.Error(), "permission denied") {
		suggestions = append(suggestions, fmt.Sprintf("Permission denied for port %s.", addr))
		suggestions = append(suggestions, "Possible solutions:")
		suggestions = append(suggestions, "  1. Use a port above 1024 (non-privileged)")
		suggestions = append(suggestions, "  2. Run with sudo/administrator privileges (not recommended)")
		suggestions = append(suggestions, "  3. Grant CAP_NET_BIND_SERVICE capability on Linux")
	} else {
		suggestions = append(suggestions, fmt.Sprintf("Error binding to %s: %v", addr, err))
	}

	return strings.Join(suggestions, "\n")
}

// PortChecker provides batch port availability checking
type PortChecker struct {
	results map[string]*PortAvailability
}

// NewPortChecker creates a new port checker
func NewPortChecker() *PortChecker {
	return &PortChecker{
		results: make(map[string]*PortAvailability),
	}
}

// Check tests a port and stores the result
func (pc *PortChecker) Check(name string, addr string) {
	pc.results[name] = CheckPortAvailable(addr)
}

// AllAvailable returns true if all checked ports are available
func (pc *PortChecker) AllAvailable() bool {
	for _, result := range pc.results {
		if !result.Available {
			return false
		}
	}
	return true
}

// GetResults returns all check results
func (pc *PortChecker) GetResults() map[string]*PortAvailability {
	return pc.results
}

// GetFailures returns only the failed checks
func (pc *PortChecker) GetFailures() map[string]*PortAvailability {
	failures := make(map[string]*PortAvailability)
	for name, result := range pc.results {
		if !result.Available {
			failures[name] = result
		}
	}
	return failures
}

// FormatReport returns a human-readable report of all port checks
func (pc *PortChecker) FormatReport() string {
	var lines []string
	lines = append(lines, "Port Availability Check:")

	for name, result := range pc.results {
		status := "✓ AVAILABLE"
		if !result.Available {
			status = "✗ IN USE"
		}
		lines = append(lines, fmt.Sprintf("  %s (%s): %s", name, result.Address, status))

		if !result.Available && result.Error != nil {
			suggestion := GetPortConflictSuggestion(result.Address, result.Error)
			if suggestion != "" {
				for _, line := range strings.Split(suggestion, "\n") {
					lines = append(lines, "    "+line)
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}
