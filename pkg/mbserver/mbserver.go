package mbserver

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	modbusserver "github.com/womat/mbserver"
)

// Server is a Modbus slave with allocated memory for discrete inputs, coils, etc.

type Request struct {
	Register uint16
	Quantity uint16
	Done     chan bool
}

type Device struct {
	Update chan Request
}

type Server struct {
	sync.RWMutex
	handler *modbusserver.Server
	timeout time.Duration
	Devices map[uint8]Device
}

// NewServer creates a new Modbus server (slave).
func NewServer() *Server {
	// Allocate Modbus memory maps.
	s := Server{
		handler: modbusserver.NewServer(),
		timeout: time.Second,
		Devices: map[uint8]Device{},
	}

	// deviceid will be created automatically when  mbmaster.NewServer()
	s.Devices[1] = Device{
		Update: make(chan Request, 1),
	}

	return &s
}

func (s *Server) SetTimeOut(t time.Duration) {
	s.timeout = t
}

func (s *Server) ListenRTU(port io.ReadWriteCloser) error {
	return s.handler.ListenRTU(port)
}

func (s *Server) ListenTCP(port string) error {
	return s.handler.ListenTCP(port)
}

//Close closes the server and closes the connect
func (s *Server) Close() error {
	s.handler.Close()
	return nil
}

func (s *Server) NewDevice(id uint8) (err error) {
	if err = s.handler.NewDevice(id); err != nil {
		return
	}

	s.Devices[id] = Device{Update: make(chan Request)}
	return
}

func (s *Server) RemoveDevice(id uint8) (err error) {
	if err = s.handler.RemoveDevice(id); err != nil {
		return
	}
	close(s.Devices[id].Update)
	delete(s.Devices, id)
	return
}

func (s *Server) SetHoldingRegister(id uint8, register, value uint16) error {
	if _, ok := s.handler.Devices[id]; !ok {
		return fmt.Errorf("deviceid %v doesn't exists", id)
	}

	s.handler.Devices[id].HoldingRegisters[register] = value
	return nil
}

func (s *Server) GetHoldingRegister(id uint8, register uint16) (uint16, error) {
	if _, ok := s.handler.Devices[id]; !ok {
		return 0, fmt.Errorf("deviceid %v doesn't exists", id)
	}

	return s.handler.Devices[id].HoldingRegisters[register], nil
}

func registerAddressAndNumber(frame modbusserver.Framer) (register int, numRegs int, endRegister int) {
	data := frame.GetData()
	register = int(binary.BigEndian.Uint16(data[0:2]))
	numRegs = int(binary.BigEndian.Uint16(data[2:4]))
	endRegister = register + numRegs
	return register, numRegs, endRegister
}

func (s *Server) SetRegisterFunctionHandler(function uint8) error {
	var registerhandlerfunction func(*modbusserver.Server, modbusserver.Framer) ([]byte, modbusserver.Exception)

	switch function {
	case 3:
		registerhandlerfunction = func(mb *modbusserver.Server, frame modbusserver.Framer) ([]byte, modbusserver.Exception) {
			register, numRegs, endRegister := registerAddressAndNumber(frame)
			device := frame.GetDevice()

			if endRegister > 65536 {
				warninglog.Printf("ReadHoldingRegisters from Device %v, Address %v, quantity %v >> Exception: IllegalDataAddress, Registeraddress: %v\n", device, register, numRegs, endRegister)
				return []byte{}, modbusserver.IllegalDataAddress
			}

			debuglog.Printf("ReadHoldingRegisters from Device %v, Address %v, quantity %v\n", device, register, numRegs)

			done := make(chan bool)
			s.Devices[device].Update <- Request{Register: uint16(register), Quantity: uint16(numRegs), Done: done}

			select {
			case <-done:
				debuglog.Printf("get done signal\n")
			case <-time.After(s.timeout):
				errorlog.Println("timeout during receive data")
			}

			s.RLock()
			defer s.RUnlock()

			r := append([]byte{byte(numRegs * 2)}, modbusserver.Uint16ToBytes(mb.Devices[device].HoldingRegisters[register:endRegister])...)
			tracelog.Printf("response %v\n", hex.EncodeToString(r))
			return r, modbusserver.Success
		}
	}

	if registerhandlerfunction == nil {
		return fmt.Errorf("functioncode %v is not supported", function)
	}
	s.handler.RegisterFunctionHandler(function, registerhandlerfunction)
	return nil
}
