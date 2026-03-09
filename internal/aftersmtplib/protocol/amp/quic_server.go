package amp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/crypto"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/ledger"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/quic-go/quic-go"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var (
	activeQuicStreams = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "aftersmtp_quic_active_streams",
		Help: "The current number of active QUIC streams",
	})
)

// QUICServer implements multiplexed UDP/HTTP3 native encrypted AMP streams
type QUICServer struct {
	addr      string
	tlsConfig *tls.Config
	quicConf  *quic.Config

	Ledger             ledger.Ledger
	OnMessageDelivered func(msg *AMPMessage) error
}

func NewQUICServer(addr string, l ledger.Ledger, certPath string, keyPath string) (*QUICServer, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificates for QUIC: %w", err)
	}

	return &QUICServer{
		addr: addr,
		tlsConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"aftersmtp-amp-v1"},
		},
		quicConf: &quic.Config{
			MaxIdleTimeout:         0,       // Keep connection alive
			MaxStreamReceiveWindow: 5 << 20, // 5MB Stream window
		},
		Ledger: l,
	}, nil
}

func (q *QUICServer) ListenAndServe(ctx context.Context) error {
	listener, err := quic.ListenAddr(q.addr, q.tlsConfig, q.quicConf)
	if err != nil {
		return err
	}
	telemetry.Log.Info("AMP Native QUIC Transport listening", zap.String("address", q.addr))

	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			telemetry.Log.Error("QUIC Accept error", zap.Error(err))
			return err
		}

		go q.handleConnection(ctx, conn)
	}
}

func (q *QUICServer) handleConnection(ctx context.Context, conn *quic.Conn) {
	// A single QUIC connection allows multi-stream bidirectional data delivery.
	// We bound the multiplexer to prevent OOM under DDOS.
	telemetry.Log.Info("Received QUIC link", zap.String("remote_addr", conn.RemoteAddr().String()))

	// Bounded worker pool of 1000 concurrent streams per connection
	streamLimit := make(chan struct{}, 1000)

	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			// Normally triggered on connection closing
			telemetry.Log.Debug("Stream ending", zap.Error(err))
			return
		}

		streamLimit <- struct{}{}
		activeQuicStreams.Inc()
		go func(s *quic.Stream) {
			defer func() {
				<-streamLimit
				activeQuicStreams.Dec()
			}()
			q.handleStream(s)
		}(stream)
	}
}

func (q *QUICServer) handleStream(stream *quic.Stream) {
	defer stream.Close()

	// Read arriving payload into memory (in prod this streams directly to disk)
	payloadBytes, err := io.ReadAll(stream)
	if err != nil {
		telemetry.Log.Error("Failed to read QUIC stream", zap.Error(err))
		return
	}

	var msg AMPMessage
	if err := proto.Unmarshal(payloadBytes, &msg); err != nil {
		telemetry.Log.Error("Invalid Payload Protobuf shape over QUIC", zap.Error(err))
		return
	}

	if msg.Headers == nil {
		return
	}

	telemetry.Log.Info("QUIC receiving AMP message", zap.String("from", msg.Headers.SenderDid), zap.String("to", msg.Headers.RecipientDid))

	// 1. Resolve Sender Keys to Verify Signature
	senderRecord, err := q.Ledger.ResolveDID(msg.Headers.SenderDid)
	if err != nil {
		telemetry.Log.Warn("QUIC Rejecting message: Sender Identity Not Found", zap.Error(err))
		return
	}

	if senderRecord.Revoked {
		telemetry.Log.Warn("QUIC Rejecting message: Sender Identity Revoked")
		return
	}

	verificationPayload := append([]byte(msg.Headers.SenderDid+msg.Headers.RecipientDid+msg.Headers.MessageId), msg.EncryptedPayload...)
	if !crypto.Verify(senderRecord.SigningPublicKey, verificationPayload, msg.Signature) {
		telemetry.Log.Warn("QUIC Rejecting message: Invalid Signature Matrix")
		return // Connection stream drops them
	}

	// 2. Deliver via application logic
	if q.OnMessageDelivered != nil {
		if err := q.OnMessageDelivered(&msg); err != nil {
			telemetry.Log.Error("Delivery Callback Failed", zap.Error(err))
			return
		}
	}

	// Issue success receipt hash explicitly (simulated via empty protobuf response)
	_, _ = stream.Write([]byte("OK"))
}
