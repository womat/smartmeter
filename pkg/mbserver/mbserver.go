package mbserver

import (
	"SmartmeterEmu/pkg/debug"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	mbmaster "github.com/womat/mbserver"
)

// Server is a Modbus slave with allocated memory for discrete inputs, coils, etc.

type DeviceChannels struct {
	//	Update chan bool
	Update chan struct{ Register, Quantity uint16 }
	Done   map[*chan bool]chan bool
}

type Server struct {
	sync.RWMutex
	handler *mbmaster.Server
	timeout time.Duration
	Devices map[uint8]DeviceChannels
}

// NewServer creates a new Modbus server (slave).
func NewServer() *Server {
	// Allocate Modbus memory maps.
	s := Server{
		handler: mbmaster.NewServer(),
		timeout: time.Second,
		Devices: map[uint8]DeviceChannels{},
	}

	// deviceid will be created automatically when  mbmaster.NewServer()
	s.Devices[1] = DeviceChannels{
		Update: make(chan struct{ Register, Quantity uint16 }, 1),
		Done:   map[*chan bool]chan bool{},
	}

	return &s
}

func (s *Server) SetTimeOut(t time.Duration) {
	s.timeout = t
	debug.Debuglog.Printf("timeout value %v\n", s.timeout)

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
	s.Devices[id] = DeviceChannels{
		Update: make(chan struct{ Register, Quantity uint16 }, 1),
		Done:   map[*chan bool]chan bool{},
	}
	return
}

func (s *Server) RemoveDevice(id uint8) (err error) {
	if err = s.handler.RemoveDevice(id); err != nil {
		return
	}

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

func registerAddressAndNumber(frame mbmaster.Framer) (register int, numRegs int, endRegister int) {
	data := frame.GetData()
	register = int(binary.BigEndian.Uint16(data[0:2]))
	numRegs = int(binary.BigEndian.Uint16(data[2:4]))
	endRegister = register + numRegs
	return register, numRegs, endRegister
}

func (s *Server) SetRegisterFunctionHandler(function uint8) error {
	var registerhandlerfunction func(*mbmaster.Server, mbmaster.Framer) ([]byte, *mbmaster.Exception)

	switch function {
	case 3:
		registerhandlerfunction = func(mb *mbmaster.Server, frame mbmaster.Framer) ([]byte, *mbmaster.Exception) {
			register, numRegs, endRegister := registerAddressAndNumber(frame)
			device := frame.GetDevice()

			if endRegister > 65536 {
				warninglog.Printf("ReadHoldingRegisters from Device %v, Address %v, quantity %v >> Exception: IllegalDataAddress, Registeraddress: %v\n", device, register, numRegs, endRegister)
				return []byte{}, &mbmaster.IllegalDataAddress
			}

			if _, ok := mb.Devices[device]; !ok {
				warninglog.Printf("ReadHoldingRegisters from Device %v, Address %v, quantity %v >> Exception: SlaveDeviceFailure, Invalid DeviceId: %v\n", device, register, numRegs, device)
				return []byte{}, &mbmaster.SlaveDeviceFailure
			}

			debuglog.Printf("ReadHoldingRegisters from Device %v, Address %v, quantity %v\n", device, register, numRegs)
			done := make(chan bool)
			defer func() {
				close(done)
				debug.Debuglog.Printf("close server channel: server.Devices[%v]Done\n", device)

			}()

			uid := &done
			s.Devices[device].Done[uid] = done
			debug.Debuglog.Printf("create server channel: server.Devices[%v].[%v]Done\n", device, uid)

			s.Devices[device].Update <- struct{ Register, Quantity uint16 }{Register: uint16(register), Quantity: uint16(numRegs)}
			debug.Debuglog.Printf("timeout value %v\n", s.timeout)

			select {
			case <-done:
				debug.Debuglog.Printf("get done signal")
			case <-time.After(s.timeout):
				debug.Errorlog.Println("timeout during receive data")

			}
			delete(s.Devices[device].Done, uid)
			debug.Debuglog.Printf("delete server channel: server.Devices[%v].[%v]Done\n", device, uid)

			s.RLock()
			defer s.RUnlock()

			r := append([]byte{byte(numRegs * 2)}, mbmaster.Uint16ToBytes(mb.Devices[device].HoldingRegisters[register:endRegister])...)
			tracelog.Printf("response %v\n", hex.EncodeToString(r))
			return r, &mbmaster.Success
		}
	}

	if registerhandlerfunction == nil {
		return fmt.Errorf("functioncode %v is not supported", function)
	}
	s.handler.RegisterFunctionHandler(function, registerhandlerfunction)
	return nil
}
