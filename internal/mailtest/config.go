package mailtest

// Config holds the configuration for mail protocol testing
type Config struct {
	Host     string
	Port     int
	TLS      bool
	Insecure bool
	Timeout  int
	Verbose  bool
	Username string
	Password string

	// Remote testing
	Remote   bool
	APIURL   string
	GRPCAddr string
	APIKey   string
}

// GetPort returns the appropriate port for the protocol
func (c *Config) GetPort(protocol string, defaultPort int) int {
	if c.Port != 0 {
		return c.Port
	}

	// Auto-detect port based on protocol and TLS
	ports := map[string]int{
		"smtp":     25,
		"smtp-tls": 465,
		"smtp-sub": 587,
		"imap":     143,
		"imap-tls": 993,
		"pop3":     110,
		"pop3-tls": 995,
	}

	if defaultPort != 0 {
		return defaultPort
	}

	key := protocol
	if c.TLS {
		key = protocol + "-tls"
	}

	if port, ok := ports[key]; ok {
		return port
	}

	return 25 // Default fallback
}
