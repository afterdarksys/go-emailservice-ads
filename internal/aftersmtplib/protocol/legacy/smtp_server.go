package legacy

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"net"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/amp"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/telemetry"
	"go.uber.org/zap"
)

// SMTPServer is a basic listener for legacy port 25/587 traffic.
type SMTPServer struct {
	addr      string
	bridge    *Bridge
	tlsConfig *tls.Config
	banner    string // SMTP 220 greeting banner
	// Delivery callback for the resulting AMP message
	OnMessageDelivered func(msg *amp.AMPMessage) error
}

func NewSMTPServer(addr string, b *Bridge, banner string) *SMTPServer {
	// Load the server certificates for STARTTLS
	cert, err := tls.LoadX509KeyPair("certs/cert.pem", "certs/key.pem")
	var tlsConfig *tls.Config
	if err == nil {
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	} else {
		telemetry.Log.Warn("Failed to load TLS certificates for legacy SMTP. STARTTLS degraded.", zap.Error(err))
	}

	// Use default banner if not provided
	if banner == "" {
		banner = "aftersmtp.msgs.global ESMTP AMP-Bridge"
	}

	return &SMTPServer{
		addr:      addr,
		bridge:    b,
		tlsConfig: tlsConfig,
		banner:    banner,
	}
}

func (s *SMTPServer) ListenAndServe() error {
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	telemetry.Log.Info("Legacy SMTP Ingress listening", zap.String("address", s.addr))

	for {
		conn, err := l.Accept()
		if err != nil {
			telemetry.Log.Error("SMTPServer Accept error", zap.Error(err))
			continue
		}
		go s.handleConnection(conn)
	}
}

type smtpSession struct {
	conn          net.Conn
	reader        *bufio.Reader
	isTLS         bool
	authenticated bool
	sender        string
	recipient     string
}

func (s *SMTPServer) handleConnection(rawConn net.Conn) {
	defer rawConn.Close()

	// Defensive timeout: Maximum 5 minutes to complete an SMTP transaction
	rawConn.SetDeadline(time.Now().Add(5 * time.Minute))

	sess := &smtpSession{
		conn:   rawConn,
		reader: bufio.NewReader(rawConn),
	}

	// Send Greeting
	sess.conn.Write([]byte("220 " + s.banner + "\r\n"))

	var inData bool
	var dataBytes []byte

	for {
		// Reset idle read deadline for the next line (helps prevent slowloris)
		sess.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		line, err := sess.reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		if inData {
			if line == "." {
				// End of DATA phase
				inData = false

				// Translate to AMP
				ampMsg, err := s.bridge.ConvertToAMP(sess.sender, sess.recipient, dataBytes)
				if err != nil {
					telemetry.Log.Error("Bridge translation failed", zap.Error(err))
					sess.conn.Write([]byte("554 Transaction failed, AMP translation error\r\n"))
					return
				}

				// Enqueue into new core
				if s.OnMessageDelivered != nil {
					err = s.OnMessageDelivered(ampMsg)
					if err != nil {
						sess.conn.Write([]byte("451 Requested action aborted: local error in processing\r\n"))
						return
					}
				}

				sess.conn.Write([]byte("250 2.0.0 Ok: queued as AMP\r\n"))
				dataBytes = nil
				sess.sender = ""
				sess.recipient = ""
			} else {
				dataBytes = append(dataBytes, []byte(line+"\n")...)
			}
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToUpper(parts[0])
		args := ""
		if len(parts) > 1 {
			args = parts[1]
		}

		switch cmd {
		case "HELO", "EHLO":
			// Advertise STARTTLS and AUTH
			features := []string{
				"250-aftersmtp.msgs.global",
				"250-8BITMIME",
			}
			if s.tlsConfig != nil && !sess.isTLS {
				features = append(features, "250-STARTTLS")
			}
			if sess.isTLS { // Only offer AUTH over TLS for security
				features = append(features, "250-AUTH PLAIN")
			}
			features = append(features, "250 Ok")
			sess.conn.Write([]byte(strings.Join(features, "\r\n") + "\r\n"))

		case "STARTTLS":
			if s.tlsConfig == nil {
				sess.conn.Write([]byte("454 TLS not available due to temporary reason\r\n"))
				continue
			}
			sess.conn.Write([]byte("220 Ready to start TLS\r\n"))

			tlsConn := tls.Server(sess.conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				telemetry.Log.Error("TLS Handshake failed", zap.Error(err))
				return
			}

			sess.conn = tlsConn
			sess.reader = bufio.NewReader(tlsConn)
			sess.isTLS = true

		case "AUTH":
			if !sess.isTLS {
				sess.conn.Write([]byte("530 Must issue a STARTTLS command first\r\n"))
				continue
			}

			// Weak/Mock auth check - Production would use the ledger or central directory
			authParts := strings.Split(args, " ")
			if len(authParts) != 2 || strings.ToUpper(authParts[0]) != "PLAIN" {
				sess.conn.Write([]byte("504 Unrecognized authentication type\r\n"))
				continue
			}

			decoded, err := base64.StdEncoding.DecodeString(authParts[1])
			if err != nil {
				sess.conn.Write([]byte("501 Invalid base64\r\n"))
				continue
			}

			// PLAIN auth format: \x00user\x00password
			credentials := strings.Split(string(decoded), "\x00")
			if len(credentials) == 3 && credentials[1] == "ryan" && credentials[2] == "securepassword" {
				sess.authenticated = true
				sess.conn.Write([]byte("235 2.7.0 Authentication successful\r\n"))
			} else {
				sess.conn.Write([]byte("535 5.7.8 Authentication credentials invalid\r\n"))
			}

		case "MAIL":
			if !sess.authenticated {
				sess.conn.Write([]byte("530 5.7.0 Authentication required\r\n"))
				continue
			}
			sess.sender = parseAddress(line)
			sess.conn.Write([]byte("250 Ok\r\n"))

		case "RCPT":
			if !sess.authenticated {
				sess.conn.Write([]byte("530 5.7.0 Authentication required\r\n"))
				continue
			}
			sess.recipient = parseAddress(line)
			sess.conn.Write([]byte("250 Ok\r\n"))

		case "DATA":
			if !sess.authenticated {
				sess.conn.Write([]byte("530 5.7.0 Authentication required\r\n"))
				continue
			}
			inData = true
			sess.conn.Write([]byte("354 End data with <CR><LF>.<CR><LF>\r\n"))

		case "QUIT":
			sess.conn.Write([]byte("221 Bye\r\n"))
			return
		default:
			sess.conn.Write([]byte("500 unrecognized command\r\n"))
		}
	}
}

func parseAddress(cmd string) string {
	start := strings.Index(cmd, "<")
	end := strings.Index(cmd, ">")
	if start != -1 && end != -1 && end > start {
		return cmd[start+1 : end]
	}
	return "unknown"
}
