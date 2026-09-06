package api

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestLifecycleBindsAndStops(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	s.config.API.RESTAddr = "127.0.0.1:0"
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	addr := s.listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		c.Close()
		t.Fatal("listener remains open")
	}
	if err := s.Start(); err == nil {
		t.Fatal("restarted stopped instance")
	}
}
func TestStartupReportsBindFailure(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s, _ := newMailboxTestServer(t, false)
	s.config.API.RESTAddr = l.Addr().String()
	if err = s.Start(); err == nil {
		t.Fatal("bind failure hidden")
	}
	if err = s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
