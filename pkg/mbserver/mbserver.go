package mbserver

import (
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
	Update   chan bool
	RChannel map[*chan bool]chan bool
}

type Server struct {
	sync.RWMutex
	handler *mbmaster.Server
	Devices map[uint8]DeviceChannels
	D       map[uint8]struct {
		Update   chan bool
		RChannel map[*chan bool]chan bool
	}
}

// NewServer creates a new Modbus server (slave).
func NewServer() *Server {
	// Allocate Modbus memory maps.

	s := Server{
		handler: mbmaster.NewServer(),
		Devices: map[uint8]DeviceChannels{},
	}

	s.Devices[1] = DeviceChannels{
		Update:   make(chan bool, 1),
		RChannel: map[*chan bool]chan bool{},
	}

	return &s
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
		Update:   make(chan bool, 1),
		RChannel: map[*chan bool]chan bool{},
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

func (s *Server) SetHoldingRegisters(id uint8, register, value uint16) error {
	if _, ok := s.handler.Devices[id]; !ok {
		return fmt.Errorf("deviceid %v doesn't exists", id)
	}

	s.handler.Devices[id].HoldingRegisters[register] = value
	return nil
}

func (s *Server) GetHoldingRegisters(id uint8, register uint16) (uint16, error) {
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

func (s *Server) SetNewFunction3Handler() {
	ReadHoldingRegisters := func(mb *mbmaster.Server, frame mbmaster.Framer) ([]byte, *mbmaster.Exception) {
		register, numRegs, endRegister := registerAddressAndNumber(frame)
		device := frame.GetDevice()

		if endRegister > 65536 {
			errorlog.Printf("ReadHoldingRegisters from Device %v, Address %v, quantity %v >> Exception: IllegalDataAddress, Registeraddress: %v\n", device, register, numRegs, endRegister)
			return []byte{}, &mbmaster.IllegalDataAddress
		}

		if _, ok := mb.Devices[device]; !ok {
			errorlog.Printf("ReadHoldingRegisters from Device %v, Address %v, quantity %v >> Exception: SlaveDeviceFailure, Invalid DeviceId: %v\n", device, register, numRegs, device)
			return []byte{}, &mbmaster.SlaveDeviceFailure
		}

		debuglog.Printf("ReadHoldingRegisters from Device %v, Address %v, quantity %v\n", device, register, numRegs)

		ready := make(chan bool)
		defer close(ready)
		uid := &ready
		s.Devices[device].RChannel[uid] = ready

		s.Devices[device].Update <- true

		select {
		case <-ready:
		case <-time.After(5 * time.Second):
		}
		delete(s.Devices[device].RChannel, uid)

		s.RLock()
		defer s.RUnlock()

		r := append([]byte{byte(numRegs * 2)}, mbmaster.Uint16ToBytes(mb.Devices[device].HoldingRegisters[register:endRegister])...)
		tracelog.Printf("response %v\n", hex.EncodeToString(r))
		return r, &mbmaster.Success
	}

	s.handler.RegisterFunctionHandler(2, ReadHoldingRegisters)
}
