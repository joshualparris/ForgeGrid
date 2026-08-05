package main

import (
	"forgegrid/internal/coordinator"
	"forgegrid/internal/store"
	"net"
	"testing"
)

func TestCoordinatorListener(t *testing.T) {
	s, _ := store.NewStore(t.TempDir())
	c := coordinator.New(s, true)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.Listener = l
	addr := l.Addr().(*net.TCPAddr)
	if !addr.IP.IsLoopback() {
		t.Fatalf("Expected loopback listener, got %v", addr.IP)
	}
}
