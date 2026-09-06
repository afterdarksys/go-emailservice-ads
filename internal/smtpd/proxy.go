package smtpd

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

type proxyConn struct {
	net.Conn
	reader   *bufio.Reader
	original net.Addr
}

func (c *proxyConn) Read(b []byte) (int, error) { return c.reader.Read(b) }
func (c *proxyConn) RemoteAddr() net.Addr       { return c.original }
func (c *proxyConn) PeerAddr() net.Addr         { return c.Conn.RemoteAddr() }
func matchesNetworks(ip string, networks []string) bool {
	addr := net.ParseIP(ip)
	for _, n := range networks {
		_, network, e := net.ParseCIDR(n)
		if e == nil && network.Contains(addr) {
			return true
		}
	}
	return false
}
func peerIP(c net.Conn) string {
	for {
		if t, ok := c.(interface{ NetConn() net.Conn }); ok {
			c = t.NetConn()
			continue
		}
		break
	}
	addr := c.RemoteAddr()
	if p, ok := c.(interface{ PeerAddr() net.Addr }); ok {
		addr = p.PeerAddr()
	}
	host, _, _ := net.SplitHostPort(addr.String())
	return host
}

// proxyListener accepts mandatory HAProxy PROXY v1 from configured TCP peers.
// Parsing is deferred to the connection goroutine, never the accept loop.
type proxyListener struct {
	net.Listener
	config config.ProxyProtocolConfig
}

func (l *proxyListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &lazyProxyConn{Conn: c, config: l.config}, nil
}

type lazyProxyConn struct {
	net.Conn
	config config.ProxyProtocolConfig
	parsed *proxyConn
	err    error
	done   bool
}

func (c *lazyProxyConn) parse() {
	if c.done {
		return
	}
	c.done = true
	if !matchesNetworks(peerIP(c.Conn), c.config.Networks) {
		c.err = fmt.Errorf("untrusted PROXY peer")
		c.Conn.Close()
		return
	}
	c.Conn.SetReadDeadline(time.Now().Add(configuredDuration(c.config.Timeout, 5*time.Second)))
	r := bufio.NewReaderSize(c.Conn, 256)
	line, e := r.ReadSlice('\n')
	if e != nil || len(line) > 108 {
		c.err = fmt.Errorf("invalid PROXY header")
		c.Conn.Close()
		return
	}
	f := strings.Fields(string(line))
	if len(f) != 6 || f[0] != "PROXY" || (f[1] != "TCP4" && f[1] != "TCP6") {
		c.err = fmt.Errorf("invalid PROXY v1 header")
		c.Conn.Close()
		return
	}
	ip := net.ParseIP(f[2])
	port, e := strconv.Atoi(f[4])
	if ip == nil || e != nil || port < 1 || port > 65535 || (f[1] == "TCP4" && ip.To4() == nil) || (f[1] == "TCP6" && ip.To4() != nil) {
		c.err = fmt.Errorf("invalid PROXY source")
		c.Conn.Close()
		return
	}
	c.parsed = &proxyConn{Conn: c.Conn, reader: r, original: &net.TCPAddr{IP: ip, Port: port}}
	c.Conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
}
func (c *lazyProxyConn) Read(b []byte) (int, error) {
	c.parse()
	if c.err != nil {
		return 0, c.err
	}
	return c.parsed.Read(b)
}
func (c *lazyProxyConn) RemoteAddr() net.Addr {
	if c.parsed != nil {
		return c.parsed.original
	}
	return c.Conn.RemoteAddr()
}
func (c *lazyProxyConn) PeerAddr() net.Addr { return c.Conn.RemoteAddr() }
