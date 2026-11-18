package agent

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPumpTCPCarriesBytesBothWays(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() { // an echo that adds a prefix
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		b := make([]byte, 64)
		for {
			n, err := c.Read(b)
			if err != nil {
				return
			}
			_, _ = c.Write(append([]byte("echo:"), b[:n]...))
		}
	}()
	in := make(chan []byte, 4)
	var mu sync.Mutex
	var got strings.Builder
	sio := &StreamIO{In: in, Out: func(b []byte) error { mu.Lock(); got.Write(b); mu.Unlock(); return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pumpTCP(ctx, ln.Addr().String(), sio) }()
	in <- []byte("hello")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		s := got.String()
		mu.Unlock()
		if s == "echo:hello" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	if got.String() != "echo:hello" {
		t.Fatalf("got %q", got.String())
	}
	mu.Unlock()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPumpTCPEndsWhenTheServiceCloses(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = io.WriteString(c, "bye")
			_ = c.Close()
		}
	}()
	in := make(chan []byte)
	sio := &StreamIO{In: in, Out: func([]byte) error { return nil }}
	done := make(chan error, 1)
	go func() { done <- pumpTCP(context.Background(), ln.Addr().String(), sio) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream must end with the connection")
	}
}

func TestPumpTCPReportsAnUnreachableService(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()
	err := pumpTCP(context.Background(), addr, &StreamIO{In: make(chan []byte), Out: func([]byte) error { return nil }})
	if err == nil || !strings.Contains(err.Error(), "remote desktop service") {
		t.Fatalf("got %v", err)
	}
}
