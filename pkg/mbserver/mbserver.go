// Package mbserver wraps github.com/womat/mbserver as a read-only Modbus server whose
// holding registers can be updated while clients are reading them.
//
// The library answers requests in its own goroutine and reads the register memory
// without locking. Here function code 3 is served under the server's read lock, and
// callers update registers under the write lock (Lock/Unlock), so a client never sees
// a 32-bit value whose high word is new and whose low word is old.
package mbserver

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	modbusServer "github.com/womat/mbserver"
)

// writeFunctions are the Modbus write function codes, refused like a real meter does:
// write single coil, write single register, write multiple coils, write multiple registers.
var writeFunctions = []uint8{5, 6, 15, 16}

// Server is a read-only Modbus server with one register memory per unit ID.
// Lock it while updating holding registers with SetHoldingRegister.
type Server struct {
	sync.RWMutex
	handler *modbusServer.Server
}

// NewServer creates a new Modbus server. Unit ID 1 exists from the start.
func NewServer() *Server {
	s := &Server{handler: modbusServer.NewServer()}

	s.handler.RegisterFunctionHandler(3, s.readHoldingRegisters)
	for _, function := range writeFunctions {
		s.handler.RegisterFunctionHandler(function, nil) // answered with IllegalFunction
	}
	return s
}

// ListenRTU serves requests arriving on a serial port.
func (s *Server) ListenRTU(port io.ReadWriteCloser) error {
	return s.handler.ListenRTU(port)
}

// ListenTCP serves requests arriving on a TCP address, e.g. "0.0.0.0:502".
func (s *Server) ListenTCP(address string) error {
	return s.handler.ListenTCP(address)
}

// Close stops the listeners.
func (s *Server) Close() error {
	s.handler.Close()
	return nil
}

// NewDevice adds a unit ID with its own register memory.
func (s *Server) NewDevice(id uint8) error {
	s.Lock()
	defer s.Unlock()
	return s.handler.NewDevice(id)
}

// SetHoldingRegister sets one holding register of unit ID id. The caller must hold the
// write lock, so that a multi-register value is published as a whole.
func (s *Server) SetHoldingRegister(id uint8, register, value uint16) error {
	device, ok := s.handler.Devices[id]
	if !ok {
		return fmt.Errorf("unit ID %d does not exist", id)
	}
	device.HoldingRegisters[register] = value
	return nil
}

// GetHoldingRegister returns one holding register of unit ID id.
func (s *Server) GetHoldingRegister(id uint8, register uint16) (uint16, error) {
	s.RLock()
	defer s.RUnlock()

	device, ok := s.handler.Devices[id]
	if !ok {
		return 0, fmt.Errorf("unit ID %d does not exist", id)
	}
	return device.HoldingRegisters[register], nil
}

// readHoldingRegisters answers function code 3 under the read lock.
func (s *Server) readHoldingRegisters(mb *modbusServer.Server, frame modbusServer.Framer) ([]byte, modbusServer.Exception) {
	data := frame.GetData()
	if len(data) < 4 {
		return []byte{}, modbusServer.IllegalDataValue
	}
	register := int(binary.BigEndian.Uint16(data[0:2]))
	quantity := int(binary.BigEndian.Uint16(data[2:4]))
	if quantity < 1 || quantity > 125 {
		return []byte{}, modbusServer.IllegalDataValue
	}
	end := register + quantity
	if end > 65536 {
		return []byte{}, modbusServer.IllegalDataAddress
	}

	s.RLock()
	defer s.RUnlock()

	device, ok := mb.Devices[frame.GetDevice()]
	if !ok {
		return []byte{}, modbusServer.IllegalDataAddress
	}
	return append([]byte{byte(quantity * 2)}, modbusServer.Uint16ToBytes(device.HoldingRegisters[register:end])...), modbusServer.Success
}
