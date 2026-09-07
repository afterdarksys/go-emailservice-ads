package api

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
	"net"
	"testing"
)

func TestManagementRPCUsesRESTAuthorization(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	l := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	registerManagement(g, s)
	go g.Serve(l)
	defer g.Stop()
	c, err := grpc.NewClient("passthrough:///test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return l.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, _ := structpb.NewStruct(map[string]interface{}{"method": "GET", "path": "/api/v1/mailboxes"})
	for _, token := range []string{"", testAPIKey} {
		ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
		out := new(structpb.Struct)
		if err = c.Invoke(ctx, "/mailhub.management.v1.Management/Call", in, out); err != nil {
			t.Fatal(err)
		}
		want := 200.
		if token == "" {
			want = 401
		}
		if out.Fields["status"].GetNumberValue() != want {
			t.Fatal(out)
		}
	}
	in, _ = structpb.NewStruct(map[string]interface{}{"method": "GET", "path": "https://example.test/api/v1/mailboxes"})
	if err = c.Invoke(context.Background(), "/mailhub.management.v1.Management/Call", in, new(structpb.Struct)); err == nil {
		t.Fatal("accepted absolute URL")
	}
}
