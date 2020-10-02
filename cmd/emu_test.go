package main

import (
	"SmartmeterEmu/global"
	"SmartmeterEmu/pkg/debug"
	"SmartmeterEmu/pkg/mbclient"
	"SmartmeterEmu/pkg/mbgw"
	"fmt"
	"github.com/goburrow/modbus"
	"github.com/womat/framereader"
	modbusserver "github.com/womat/mbserver"
	"testing"
	"time"

	"SmartmeterEmu/pkg/mbserver"
)

func TestMbserver(t *testing.T) {

	debug.SetDebug(global.Config.Debug.File, debug.Trace)
	framereader.SetDebug(global.Config.Debug.File, debug.Trace)
	modbusserver.SetDebug(global.Config.Debug.File, debug.Trace)
	mbserver.SetDebug(global.Config.Debug.File, debug.Trace)
	mbclient.SetDebug(global.Config.Debug.File, debug.Trace)
	mbgw.SetDebug(global.Config.Debug.File, debug.Trace)
	debug.SetDebug(global.Config.Debug.File, debug.Trace)

	ModBusServer := mbserver.NewServer()
	defer ModBusServer.Close()
	ModBusServer.SetTimeOut(global.Config.ModbusServer.TimeOut)

	if err := ModBusServer.SetRegisterFunctionHandler(3); err != nil {
		t.Fatalf("error to set register function handler: %v\n", err)
		t.FailNow()
		return
	}

	c := clientHandler{client: mbclient.NewClient(), deviceId: 1, mode: global.Request}
	defer c.client.Close()
	if err := c.client.Listen("192.0.2.10:502", time.Second*300, time.Second); err != nil {
		t.Fatalf("error to start modbus client 192.0.2.10:502: %v\n", err)
		t.FailNow()

	}
	go c.handler(ModBusServer)

	time.Sleep(time.Second)
	if err := ModBusServer.ListenTCP("127.0.0.1:502"); err != nil {
		debug.Errorlog.Printf("error to listen tcp port %v: %v\n", "", err)
		t.FailNow()
		return
	}
	time.Sleep(100 * time.Millisecond)

	// Client
	handler := modbus.NewTCPClientHandler("127.0.0.1:502")
	// Connect manually so that multiple requests are handled in one connection session
	if err := handler.Connect(); err != nil {
		t.Errorf("failed to connect, got %v\n", err)
		t.FailNow()
	}
	defer handler.Close()
	handler.SlaveId = 1
	client := modbus.NewClient(handler)

	results, err := client.ReadHoldingRegisters(18, 1)
	if err != nil {
		t.Errorf("expected nil, got %v\n", err)
		t.FailNow()
	}
	fmt.Printf("%v", results)
}
