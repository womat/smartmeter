package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jacobsa/go-serial/serial"

	"github.com/womat/framereader"
	mbmaster "github.com/womat/mbserver"

	"SmartmeterEmu/global"
	_ "SmartmeterEmu/pkg/config"
	"SmartmeterEmu/pkg/debug"
	"SmartmeterEmu/pkg/mbclient"
	"SmartmeterEmu/pkg/mbgw"
	"SmartmeterEmu/pkg/mbserver"
)

const (
	_nil = iota
	_sint16
	_sint32
	_sint64
	_uint16
	_uint32
	_uint64
)

type clientHandler struct {
	client *mbclient.Client
	// DeviceId represents the deviceId of the modbusserver
	DeviceId byte
}

type mbgwClientHandler struct {
	client *mbgw.Client
	// DeviceId represents the deviceId of the modbusserver
	DeviceId byte
}

func main() {
	defer global.Config.Debug.File.Close()
	// TODO: get debugging for each package from config file
	debug.SetDebug(global.Config.Debug.File, global.Config.Debug.Flag)
	framereader.SetDebug(global.Config.Debug.File, framereader.Default)
	mbmaster.SetDebug(global.Config.Debug.File, global.Config.Debug.Flag)
	mbserver.SetDebug(global.Config.Debug.File, global.Config.Debug.Flag)
	mbclient.SetDebug(global.Config.Debug.File, framereader.Default)
	mbgw.SetDebug(global.Config.Debug.File, global.Config.Debug.Flag)

	// initialize modbus server
	port, err := serial.Open(global.Config.ModbusServer.Options)
	if err != nil {
		debug.Errorlog.Printf("error to open serial port %v: %v\n", global.Config.ModbusServer.Options.PortName, err)
		return
	}

	serialReadWriteCloser := framereader.NewReadWriteCloser(port, time.Second, 10*time.Millisecond)
	defer serialReadWriteCloser.Close()

	ModBusServer := mbserver.NewServer()
	defer ModBusServer.Close()
	//TODO real time and polling mode: w/o new function3 it'S polling mode >> config in config file is missing
	ModBusServer.SetNewFunction3Handler()

	//TODO support multiple connection strings from config file
	err = ModBusServer.ListenTCP("127.0.0.1:502")
	if err != nil {
		debug.Errorlog.Printf("error to listen tcp port %v: %v\n", "", err)
		return
	}

	err = ModBusServer.ListenRTU(serialReadWriteCloser)
	if err != nil {
		debug.Errorlog.Printf("error to listen serial port %v: %v\n", "", err)
		return
	}

	for _, client := range global.Config.Clients {
		// TODO define client handler as an interface

		if client.DeviceId != 1 {
			if err := ModBusServer.NewDevice(client.DeviceId); err != nil {
				debug.Errorlog.Printf("error to create a new modbus device %v: %v\n", client.DeviceId, err)
				return
			}
		}

		switch t := client.Type; t {
		case "mbclient":
			c := clientHandler{client: mbclient.NewClient(), DeviceId: client.DeviceId}
			defer c.client.Close()
			if err := c.client.Listen(client.Connection, client.Polling, client.TimeOut); err != nil {
				debug.Errorlog.Printf("error to start modbus client %v: %v", client.Connection, err)
				return
			}
			go c.handler(ModBusServer)
		case "mbgateway":
			c := mbgwClientHandler{client: mbgw.NewClient(), DeviceId: client.DeviceId}
			defer c.client.Close()
			if err := c.client.Listen(client.Connection, client.Polling, client.TimeOut); err != nil {
				debug.Errorlog.Printf("error to start modbus gateway client %v: %v", client.Connection, err)
			}
			go c.handler(ModBusServer)
		case "fritz!powerline":
			debug.Warninglog.Printf("client type %v is not supported", t)
		default:
			debug.Warninglog.Printf("client type %v is not supported", t)
		}
	}

	select {}
}

// handler receives row data from receiver and save ist to modbus register
func (handler *clientHandler) handler(server *mbserver.Server) {
	client := handler.client

	for {
		select {
		case <-server.Devices[handler.DeviceId].Update:
			debug.Debuglog.Println("get an update request from modbus server >> send an update request to modbus client receiver")
			client.Update <- true
		case stream := <-client.Data:
			debug.Debuglog.Printf("receive client data from modbus client receiver (%v bytes)", len(stream.Data))
			debug.Tracelog.Printf("receive client data: %+v\n", stream)

			server.Lock()
			for n, r := range global.Config.Register {
				var value interface{}

				if r.ServerReg.Value != nil {
					value = r.ServerReg.Value
				} else {
					if r.ServerReg.Format != _nil {
						var f float64

						endAddress := r.ClientReg.Address + sizeOf(r.ClientReg.Format)
						if int(endAddress) > len(stream.Data) {
							debug.Warninglog.Printf("endaddress (%v) exceeds received data range (%v), register %v will be ignored\n", endAddress, len(stream.Data), n)
							continue
						}

						switch r.ClientReg.Format {
						case _sint16:
							f = float64(int16(binary.BigEndian.Uint16(stream.Data[r.ClientReg.Address:endAddress])))
						case _sint32:
							f = float64(int32(binary.BigEndian.Uint32(stream.Data[r.ClientReg.Address:endAddress])))
						case _sint64:
							f = float64(int64(binary.BigEndian.Uint64(stream.Data[r.ClientReg.Address:endAddress])))
						case _uint16:
							f = float64(binary.BigEndian.Uint16(stream.Data[r.ClientReg.Address:endAddress]))
						case _uint32:
							f = float64(binary.BigEndian.Uint32(stream.Data[r.ClientReg.Address:endAddress]))
						case _uint64:
							f = float64(binary.BigEndian.Uint64(stream.Data[r.ClientReg.Address:endAddress]))
						}

						f = f * math.Pow10(r.ClientReg.SF) * math.Pow10(r.ServerReg.SF) * r.ServerReg.Mul
						switch r.ServerReg.Format {
						case _sint16:
							value = int16(f)
						case _sint32:
							value = int32(f)
						case _sint64:
							value = int64(f)
						case _uint16:
							value = uint16(f)
						case _uint32:
							value = uint32(f)
						case _uint64:
							value = uint64(f)
						}
					}
				}

				setHoldingRegister(server, handler.DeviceId, r.ServerReg.Address, value)

				switch value.(type) {
				case uint16, int16:
					v, _ := server.GetHoldingRegisters(handler.DeviceId, r.ServerReg.Address)
					debug.Tracelog.Printf("%v: %v", n, v)
				case uint32, int32:
					v, _ := server.GetHoldingRegisters(handler.DeviceId, r.ServerReg.Address)
					v1, _ := server.GetHoldingRegisters(handler.DeviceId, r.ServerReg.Address+1)
					debug.Tracelog.Printf("%v: %v", n, uint32(v1)|uint32(v)<<16)
				case uint64, int64:
					v, _ := server.GetHoldingRegisters(handler.DeviceId, r.ServerReg.Address)
					v1, _ := server.GetHoldingRegisters(handler.DeviceId, r.ServerReg.Address+1)
					v2, _ := server.GetHoldingRegisters(handler.DeviceId, r.ServerReg.Address+2)
					v3, _ := server.GetHoldingRegisters(handler.DeviceId, r.ServerReg.Address+3)
					debug.Tracelog.Printf("%v: %v", n, uint64(v3)|uint64(v2)<<16|uint64(v1)<<32|uint64(v)<<48)
				}
			}
			server.Unlock()

			var waitGroup sync.WaitGroup
			for n, c := range server.Devices[handler.DeviceId].Done {
				waitGroup.Add(1)

				go func(name *chan bool, channel chan bool) {
					defer func() {
						// recover from panic caused by writing to a closed channel
						if r := recover(); r != nil {
							err := fmt.Errorf("%v", r)
							debug.Errorlog.Printf("error write to closed channel server.Devices[%v].[%v]Done: %v\n", handler.DeviceId, name, err)
							delete(server.Devices[handler.DeviceId].Done, name)
							return
						}
					}()

					debug.Tracelog.Printf("send completion of update to server channel: server.Devices[%v].[%v]Done\n", handler.DeviceId, name)
					channel <- true
					waitGroup.Done()
				}(n, c)
			}
			waitGroup.Wait()
		}
	}
}

// handler receives row data from receiver and save ist to modbus register
func (handler *mbgwClientHandler) handler(server *mbserver.Server) {
	client := handler.client

	for {
		select {
		case <-server.Devices[handler.DeviceId].Update:
			debug.Debuglog.Println("get an update request from modbus server >> send an update request to modbus gateway receiver")
			client.Update <- true
		case stream := <-client.Data:
			debug.Debuglog.Printf("receive client data from modbus gateway receiver (%v Registers)", len(stream.Register))
			debug.Tracelog.Printf("receive client data: %+v\n", stream)

			server.Lock()
			for a, v := range stream.Register {
				if err := server.SetHoldingRegisters(handler.DeviceId, a, v); err != nil {
					debug.Errorlog.Printf("error write registers: %v\n", err)
				}
			}
			server.Unlock()

			var waitGroup sync.WaitGroup
			for n, c := range server.Devices[handler.DeviceId].Done {
				waitGroup.Add(1)

				go func(name *chan bool, channel chan bool) {
					defer func() {
						// recover from panic caused by writing to a closed channel
						if r := recover(); r != nil {
							err := fmt.Errorf("%v", r)
							debug.Errorlog.Printf("error write to closed channel server.Devices[%v].[%v]Done: %v\n", handler.DeviceId, name, err)
							delete(server.Devices[handler.DeviceId].Done, name)
							return
						}
					}()

					debug.Tracelog.Printf("send completion of update to server channel: server.Devices[%v].[%v]Done\n", handler.DeviceId, name)
					channel <- true
					waitGroup.Done()
				}(n, c)
			}
			waitGroup.Wait()
		}
	}
}

func sizeOf(t int) uint16 {
	switch t {
	case _sint16, _uint16:
		return 2
	case _sint32, _uint32:
		return 4
	case _sint64, _uint64:
		return 8
	}
	return 0
}

func udWord(HoldingRegisters []uint16, address uint16) (value uint32) {
	value = uint32(HoldingRegisters[address+1]) | uint32(HoldingRegisters[address])<<16
	return
}

func setHoldingRegister(server *mbserver.Server, id uint8, address uint16, value interface{}) (quantity int) {
	switch v := value.(type) {
	case int8:
		_ = server.SetHoldingRegisters(id, address, uint16(v))
		return 1
	case int16:
		_ = server.SetHoldingRegisters(id, address, uint16(v))
		return 1
	case int32:
		_ = server.SetHoldingRegisters(id, address, uint16(v>>16))
		_ = server.SetHoldingRegisters(id, address+1, uint16(v&0x0000ffff))
		return 2
	case int64:
		_ = server.SetHoldingRegisters(id, address, uint16(v>>48))
		_ = server.SetHoldingRegisters(id, address+1, uint16((v>>32)&0x0000ffff))
		_ = server.SetHoldingRegisters(id, address+2, uint16((v>>16)&0x0000ffff))
		_ = server.SetHoldingRegisters(id, address+3, uint16(v&0x0000ffff))
		return 4
	case uint8:
		_ = server.SetHoldingRegisters(id, address, uint16(v))
		return 1
	case uint16:
		_ = server.SetHoldingRegisters(id, address, v)
		return 1
	case uint32:
		_ = server.SetHoldingRegisters(id, address, uint16(v>>16))
		_ = server.SetHoldingRegisters(id, address+1, uint16(v&0x0000ffff))
		return 2
	case uint64:
		_ = server.SetHoldingRegisters(id, address, uint16(v>>48))
		_ = server.SetHoldingRegisters(id, address+1, uint16((v>>32)&0x0000ffff))
		_ = server.SetHoldingRegisters(id, address+2, uint16((v>>16)&0x0000ffff))
		_ = server.SetHoldingRegisters(id, address+3, uint16(v&0x0000ffff))
		return 4
	}

	return 0
}

/*
func setHoldingRegister(HoldingRegisters []uint16, address uint16, value interface{}) (quantity int) {
	switch v := value.(type) {
	case int8:
		_ = HoldingRegisters[address] // bounds check hint to compiler; see golang.org/issue/14808
		HoldingRegisters[address] = uint16(v)
		return 1
	case int16:
		_ = HoldingRegisters[address] // bounds check hint to compiler; see golang.org/issue/14808
		HoldingRegisters[address] = uint16(v)
		return 1
	case int32:
		_ = HoldingRegisters[address+1] // bounds check hint to compiler; see golang.org/issue/14808
		c := uint16(v >> 16)
		HoldingRegisters[address] = c
		y := uint16(v & 0x0000ffff)
		HoldingRegisters[address+1] = y
		return 2
	case int64:
		_ = HoldingRegisters[address+3] // bounds check hint to compiler; see golang.org/issue/14808
		HoldingRegisters[address] = uint16(v >> 48)
		HoldingRegisters[address+1] = uint16((v >> 32) & 0x0000ffff)
		HoldingRegisters[address+2] = uint16((v >> 16) & 0x0000ffff)
		HoldingRegisters[address+3] = uint16(v & 0x0000ffff)
		return 4
	case uint8:
		_ = HoldingRegisters[address] // bounds check hint to compiler; see golang.org/issue/14808
		HoldingRegisters[address] = uint16(v)
		return 1
	case uint16:
		_ = HoldingRegisters[address] // bounds check hint to compiler; see golang.org/issue/14808
		HoldingRegisters[address] = v
		return 1
	case uint32:
		_ = HoldingRegisters[address+1] // bounds check hint to compiler; see golang.org/issue/14808
		c := uint16(v >> 16)
		HoldingRegisters[address] = c
		y := uint16(v & 0x0000ffff)
		HoldingRegisters[address+1] = y
		return 2
	case uint64:
		_ = HoldingRegisters[address+3] // bounds check hint to compiler; see golang.org/issue/14808
		HoldingRegisters[address] = uint16(v >> 48)
		HoldingRegisters[address+1] = uint16((v >> 32) & 0x0000ffff)
		HoldingRegisters[address+2] = uint16((v >> 16) & 0x0000ffff)
		HoldingRegisters[address+3] = uint16(v & 0x0000ffff)
		return 4
	}

	return 0
}

*/
