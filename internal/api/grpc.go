package api

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/afterdarksys/go-emailservice-ads/internal/tlsutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// Management transports the versioned REST contract without bypassing its
// authorization or status codes. See api/management.proto for the wire contract.
type managementService interface {
	Call(context.Context, *structpb.Struct) (*structpb.Struct, error)
}

func registerManagement(g *grpc.Server, s managementService) {
	g.RegisterService(&grpc.ServiceDesc{ServiceName: "mailhub.management.v1.Management", HandlerType: (*managementService)(nil), Methods: []grpc.MethodDesc{{MethodName: "Call", Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
		in := new(structpb.Struct)
		if err := dec(in); err != nil {
			return nil, err
		}
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return srv.(managementService).Call(ctx, req.(*structpb.Struct))
		}
		if interceptor == nil {
			return handler(ctx, in)
		}
		return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/mailhub.management.v1.Management/Call"}, handler)
	}}}}, s)
}
func (s *Server) startGRPC() error {
	if !s.config.API.GRPCEnabled {
		return nil
	}
	if s.config.API.TLS == nil {
		return fmt.Errorf("management gRPC requires TLS")
	}
	c := s.config.API.TLS
	tc, err := tlsutil.ServerConfig(c.Cert, c.Key, c.ClientCAFile, c.RequireClientCert)
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", s.config.API.GRPCAddr)
	if err != nil {
		return err
	}
	g := grpc.NewServer(grpc.Creds(credentials.NewTLS(tc)), grpc.MaxRecvMsgSize(2<<20), grpc.MaxSendMsgSize(4<<20), grpc.MaxConcurrentStreams(32))
	registerManagement(g, s)
	s.grpcServer = g
	s.wg.Add(1)
	go func() { defer s.wg.Done(); g.Serve(l) }()
	return nil
}

type rpcResponse struct {
	header   http.Header
	code     int
	body     bytes.Buffer
	overflow bool
}

func (w *rpcResponse) Header() http.Header { return w.header }
func (w *rpcResponse) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *rpcResponse) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	if w.body.Len()+len(b) > 3<<20 {
		w.overflow = true
		return 0, fmt.Errorf("response too large")
	}
	return w.body.Write(b)
}
func (s *Server) Call(ctx context.Context, in *structpb.Struct) (*structpb.Struct, error) {
	for key := range in.GetFields() {
		if key != "method" && key != "path" && key != "body" {
			return nil, status.Error(codes.InvalidArgument, "unknown field")
		}
	}
	method := in.GetFields()["method"].GetStringValue()
	path := in.GetFields()["path"].GetStringValue()
	body := in.GetFields()["body"].GetStringValue()
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid method")
	}
	u, err := url.ParseRequestURI(path)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/api/v1/") || strings.Contains(u.Path, "..") || len(body) > 1<<20 {
		return nil, status.Error(codes.InvalidArgument, "invalid management request")
	}
	r, err := http.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	if values := md.Get("authorization"); len(values) == 1 {
		r.Header.Set("Authorization", values[0])
	}
	r.Header.Set("Content-Type", "application/json")
	if p, ok := peer.FromContext(ctx); ok {
		r.RemoteAddr = p.Addr.String()
		if ti, ok := p.AuthInfo.(credentials.TLSInfo); ok {
			r.TLS = &ti.State
		}
	}
	w := &rpcResponse{header: make(http.Header)}
	s.buildMux().ServeHTTP(w, r)
	if w.overflow {
		return nil, status.Error(codes.ResourceExhausted, "response exceeds management RPC limit; use REST")
	}
	if w.code == 0 {
		w.code = 200
	}
	return structpb.NewStruct(map[string]interface{}{"status": w.code, "body": w.body.String(), "content_type": w.header.Get("Content-Type"), "location": w.header.Get("Location")})
}
