package sharing

import (
	"errors"
	"io"
	"net"
	"testing"
)

func TestTailcatServerOnlyExposesVirtualPort(t *testing.T) {
	listener := newConnectionListener()
	defer listener.Close()
	server := newTailcatServer(listener)
	if len(server.ServedTCPPorts) != 1 || server.ServedTCPPorts[0].First != TailcatVirtualPort || server.ServedTCPPorts[0].Last != TailcatVirtualPort {
		t.Fatal("Tailcat packet filter exposed an unexpected port", server.ServedTCPPorts)
	}
	if server.OnTCP(TailcatVirtualPort) == nil || server.OnTCP(TailcatVirtualPort+1) != nil {
		t.Fatal("Tailcat callback gate did not match the virtual HTTPS port")
	}
}

func TestConnectionListenerAcceptAndClose(t *testing.T) {
	listener := newConnectionListener()
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
		_ = listener.Close()
	})
	listener.Handle(server)
	accepted, err := listener.Accept()
	if err != nil || accepted != server {
		t.Fatal("Tailcat stream was not accepted", err)
	}
	go func() { _, _ = client.Write([]byte("ok")) }()
	buffer := make([]byte, 2)
	if _, err = io.ReadFull(accepted, buffer); err != nil || string(buffer) != "ok" {
		t.Fatal("accepted stream did not remain usable", string(buffer), err)
	}
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatal("closed listener still accepted streams", err)
	}
}

func TestConnectionListenerRejectsLateStream(t *testing.T) {
	listener := newConnectionListener()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer client.Close()
	listener.Handle(server)
	buffer := make([]byte, 1)
	if _, err := client.Read(buffer); !errors.Is(err, io.EOF) {
		t.Fatal("late Tailcat stream was not closed", err)
	}
}
