package mbserver

import (
	"net"
	"sync"
	"testing"
	"time"

	mb "github.com/simonvetter/modbus"
)

// freeAddress returns a local TCP address that is free right now.
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func startServer(t *testing.T) (*Server, *mb.ModbusClient) {
	t.Helper()
	s := NewServer()
	if err := s.NewDevice(200); err != nil {
		t.Fatal(err)
	}
	address := freeAddress(t)
	if err := s.ListenTCP(address); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	client, err := mb.NewClient(&mb.ClientConfiguration{URL: "tcp://" + address, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return s, client
}

func TestUnitIDsHaveOwnRegisters(t *testing.T) {
	s, client := startServer(t)

	s.Lock()
	for id, value := range map[uint8]uint16{1: 11, 200: 22} {
		if err := s.SetHoldingRegister(id, 4096, value); err != nil {
			t.Fatal(err)
		}
	}
	s.Unlock()

	for id, want := range map[uint8]uint16{1: 11, 200: 22} {
		_ = client.SetUnitId(id)
		got, err := client.ReadRegister(4096, mb.HOLDING_REGISTER)
		if err != nil || got != want {
			t.Errorf("unit %d register 4096 = %d, %v; want %d", id, got, err, want)
		}
	}

	if err := s.SetHoldingRegister(7, 0, 1); err == nil {
		t.Error("SetHoldingRegister on an unknown unit ID succeeded")
	}
}

// TestNoTornReads updates a 32-bit value continuously while a client reads it; both words
// always carry the same counter, so a read mixing an old and a new word is detected.
func TestNoTornReads(t *testing.T) {
	s, client := startServer(t)
	_ = client.SetUnitId(1)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := uint16(0); ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			s.Lock()
			_ = s.SetHoldingRegister(1, 4124, i)
			_ = s.SetHoldingRegister(1, 4125, i)
			s.Unlock()
		}
	})

	for range 300 {
		regs, err := client.ReadRegisters(4124, 2, mb.HOLDING_REGISTER)
		if err != nil {
			t.Fatal(err)
		}
		if regs[0] != regs[1] {
			t.Fatalf("torn read: high word %d, low word %d", regs[0], regs[1])
		}
	}
	close(stop)
	wg.Wait()
}

func TestWritesAreRefused(t *testing.T) {
	_, client := startServer(t)
	_ = client.SetUnitId(1)

	if err := client.WriteRegister(4096, 1); err == nil {
		t.Error("write single register succeeded, want IllegalFunction")
	}
	if err := client.WriteRegisters(4096, []uint16{1, 2}); err == nil {
		t.Error("write multiple registers succeeded, want IllegalFunction")
	}
}
