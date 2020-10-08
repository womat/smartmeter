package main

import (
	mbSlave "github.com/goburrow/modbus"
	modbusServer "github.com/womat/mbserver"

	"SmartmeterEmu/global"
	"SmartmeterEmu/pkg/debug"
	"SmartmeterEmu/pkg/mbclient"
	"SmartmeterEmu/pkg/mbgw"
	"SmartmeterEmu/pkg/mbserver"
	"github.com/womat/framereader"

	"encoding/json"
	"testing"
	"time"
)

func isEqual(a interface{}, b interface{}) bool {
	expect, _ := json.Marshal(a)
	got, _ := json.Marshal(b)
	if string(expect) != string(got) {
		return false
	}
	return true
}

func TestMbServer(t *testing.T) {

	testPattern1 := []byte{0, 0, 0, 0, 1, 25, 242, 53, 0, 0, 0, 0, 0, 55, 213, 165, 0, 0, 0, 0, 0, 37, 74, 18, 0, 0, 2, 218, 0, 0, 0, 5, 0, 0, 3, 218, 255, 255, 248, 65, 255, 255, 253, 180, 255, 255, 252, 222, 255, 255, 253, 175, 9, 9, 9, 62, 9, 51, 0, 0, 10, 222, 0, 0, 13, 63, 0, 0, 10, 194, 224, 155, 38, 47, 247, 240, 99, 99}

	debug.SetDebug(global.Config.Debug.File, 0)
	framereader.SetDebug(global.Config.Debug.File, 0)
	modbusServer.SetDebug(global.Config.Debug.File, 0)
	mbserver.SetDebug(global.Config.Debug.File, 0)
	mbclient.SetDebug(global.Config.Debug.File, 0)
	mbgw.SetDebug(global.Config.Debug.File, 0)

	/*
		debug.SetDebug(global.Config.Debug.File, debug.Warning|debug.Info|debug.Error|debug.Fatal|debug.Debug)
			framereader.SetDebug(global.Config.Debug.File, debug.Full)
			modbusserver.SetDebug(global.Config.Debug.File, debug.Standard)
			mbserver.SetDebug(global.Config.Debug.File, debug.Warning|debug.Info|debug.Error|debug.Fatal|debug.Debug)
			mbclient.SetDebug(global.Config.Debug.File, debug.Warning|debug.Info|debug.Error|debug.Fatal|debug.Debug)
			mbgw.SetDebug(global.Config.Debug.File, debug.Full)

	*/
	s := modbusServer.NewServer()
	err := s.ListenTCP("127.0.0.1:3333")
	if err != nil {
		t.Fatalf("failed to listen, got %v\n", err)
	}
	defer s.Close()
	// Allow the server to start and to avoid a connection refused on the client
	time.Sleep(1 * time.Millisecond)

	// Client
	testSourceHandler := mbSlave.NewTCPClientHandler("127.0.0.1:3333")
	// Connect manually so that multiple requests are handled in one connection session
	err = testSourceHandler.Connect()
	if err != nil {
		t.Errorf("failed to connect, got %v\n", err)
		t.FailNow()
	}
	defer testSourceHandler.Close()
	testSourceHandler.SlaveId = 1
	testSource := mbSlave.NewClient(testSourceHandler)

	_, err = testSource.WriteMultipleRegisters(41000-1, uint16(len(testPattern1)/2), testPattern1)

	if err != nil {
		t.Errorf("expected nil, got %v\n", err)
		t.FailNow()
	}

	results, err := testSource.ReadHoldingRegisters(41000-1, uint16(len(testPattern1)/2))
	if err != nil {
		t.Errorf("expected nil, got %v\n", err)
		t.FailNow()
	}
	expect := testPattern1
	got := results
	if !isEqual(expect, got) {
		t.Errorf("expected %v, got %v", expect, got)
	}
	testSourceHandler.Close()

	ModBusServer := mbserver.NewServer()
	defer ModBusServer.Close()
	ModBusServer.SetTimeOut(time.Second * 5)

	if err := ModBusServer.SetRegisterFunctionHandler(3); err != nil {
		t.Fatalf("error to set register function handler: %v\n", err)
		t.FailNow()
		return
	}

	c := clientHandler{client: mbclient.NewClient(), deviceId: 1, mode: global.Request}
	defer c.client.Close()
	if err := c.client.Listen("127.0.0.1:3333 Timeout:1000", time.Second*300); err != nil {
		t.Fatalf("error to start modbus client 127.0.0.1:3333: %v\n", err)
		t.FailNow()

	}
	go c.handler(ModBusServer)

	time.Sleep(1000 * time.Millisecond)

	if err := ModBusServer.ListenTCP("127.0.0.1:502"); err != nil {
		debug.Errorlog.Printf("error to listen tcp port %v: %v\n", "", err)
		t.FailNow()
		return
	}

	// Client
	testPattern2 := []byte{0, 0, 0, 0, 1, 25, 242, 53, 0, 0, 0, 0, 0, 55, 213, 165, 0, 0, 0, 0, 0, 37, 74, 18, 0, 0, 2, 218, 0, 0, 0, 5, 0, 0, 3, 218, 255, 255, 248, 65, 255, 255, 253, 180, 255, 255, 252, 222, 255, 255, 253, 175, 9, 9, 9, 62, 9, 51, 0, 0, 10, 222, 0, 0, 13, 63, 0, 0, 10, 194, 224, 155, 38, 47, 247, 240, 19, 136}

	testSourceHandler = mbSlave.NewTCPClientHandler("127.0.0.1:3333")
	// Connect manually so that multiple requests are handled in one connection session
	err = testSourceHandler.Connect()
	if err != nil {
		t.Errorf("failed to connect, got %v\n", err)
		t.FailNow()
	}
	defer testSourceHandler.Close()
	testSourceHandler.SlaveId = 1
	testSource = mbSlave.NewClient(testSourceHandler)

	_, err = testSource.WriteMultipleRegisters(41000-1, uint16(len(testPattern1)/2), testPattern2)

	// Client
	handler := mbSlave.NewTCPClientHandler("127.0.0.1:502")
	// Connect manually so that multiple requests are handled in one connection session
	if err := handler.Connect(); err != nil {
		t.Errorf("failed to connect, got %v\n", err)
		t.FailNow()
	}
	defer handler.Close()
	handler.SlaveId = 1
	client := mbSlave.NewClient(handler)

	results, err = client.ReadHoldingRegisters(4134, 1)
	expect = []byte{1, 244}
	got = results
	if !isEqual(expect, got) {
		t.Errorf("expected %v, got %v", expect, got)
	}
	testSourceHandler.Close()

}

func TestMbServerPolling(t *testing.T) {
	testPattern1 := []byte{0, 0, 0, 0, 1, 25, 242, 53, 0, 0, 0, 0, 0, 55, 213, 165, 0, 0, 0, 0, 0, 37, 74, 18, 0, 0, 2, 218, 0, 0, 0, 5, 0, 0, 3, 218, 255, 255, 248, 65, 255, 255, 253, 180, 255, 255, 252, 222, 255, 255, 253, 175, 9, 9, 9, 62, 9, 51, 0, 0, 10, 222, 0, 0, 13, 63, 0, 0, 10, 194, 224, 155, 38, 47, 247, 240, 19, 136}

	debug.SetDebug(global.Config.Debug.File, 0)
	framereader.SetDebug(global.Config.Debug.File, 0)
	modbusServer.SetDebug(global.Config.Debug.File, 0)
	mbserver.SetDebug(global.Config.Debug.File, 0)
	mbclient.SetDebug(global.Config.Debug.File, 0)
	mbgw.SetDebug(global.Config.Debug.File, 0)

	s := modbusServer.NewServer()
	err := s.ListenTCP("127.0.0.1:3333")
	if err != nil {
		t.Fatalf("failed to listen, got %v\n", err)
	}
	defer s.Close()
	// Allow the server to start and to avoid a connection refused on the client
	time.Sleep(1 * time.Millisecond)

	// Client
	testSourceHandler := mbSlave.NewTCPClientHandler("127.0.0.1:3333")
	// Connect manually so that multiple requests are handled in one connection session
	err = testSourceHandler.Connect()
	if err != nil {
		t.Errorf("failed to connect, got %v\n", err)
		t.FailNow()
	}
	defer testSourceHandler.Close()
	testSourceHandler.SlaveId = 1
	testSource := mbSlave.NewClient(testSourceHandler)

	_, err = testSource.WriteMultipleRegisters(41000-1, uint16(len(testPattern1)/2), testPattern1)

	if err != nil {
		t.Errorf("expected nil, got %v\n", err)
		t.FailNow()
	}

	results, err := testSource.ReadHoldingRegisters(41000-1, uint16(len(testPattern1)/2))
	if err != nil {
		t.Errorf("expected nil, got %v\n", err)
		t.FailNow()
	}
	expect := testPattern1
	got := results
	if !isEqual(expect, got) {
		t.Errorf("expected %v, got %v", expect, got)
	}
	testSourceHandler.Close()

	ModBusServer := mbserver.NewServer()
	defer ModBusServer.Close()
	ModBusServer.SetTimeOut(time.Second * 5)

	if err := ModBusServer.SetRegisterFunctionHandler(3); err != nil {
		t.Fatalf("error to set register function handler: %v\n", err)
		t.FailNow()
		return
	}

	c := clientHandler{client: mbclient.NewClient(), deviceId: 1, mode: global.Polling}
	defer c.client.Close()
	if err := c.client.Listen("127.0.0.1:3333 Timeout:1000", time.Second*300); err != nil {
		t.Fatalf("error to start modbus client 127.0.0.1:3333: %v\n", err)
		t.FailNow()

	}
	go c.handler(ModBusServer)

	time.Sleep(1000 * time.Millisecond)

	if err := ModBusServer.ListenTCP("127.0.0.1:502"); err != nil {
		debug.Errorlog.Printf("error to listen tcp port %v: %v\n", "", err)
		t.FailNow()
		return
	}

	// Client
	testPattern2 := []byte{0, 0, 0, 0, 1, 25, 242, 53, 0, 0, 0, 0, 0, 55, 213, 165, 0, 0, 0, 0, 0, 37, 74, 18, 0, 0, 2, 218, 0, 0, 0, 5, 0, 0, 3, 218, 255, 255, 248, 65, 255, 255, 253, 180, 255, 255, 252, 222, 255, 255, 253, 175, 9, 9, 9, 62, 9, 51, 0, 0, 10, 222, 0, 0, 13, 63, 0, 0, 10, 194, 224, 155, 38, 47, 247, 240, 99, 99}

	testSourceHandler = mbSlave.NewTCPClientHandler("127.0.0.1:3333")
	// Connect manually so that multiple requests are handled in one connection session
	err = testSourceHandler.Connect()
	if err != nil {
		t.Errorf("failed to connect, got %v\n", err)
		t.FailNow()
	}
	defer testSourceHandler.Close()
	testSourceHandler.SlaveId = 1
	testSource = mbSlave.NewClient(testSourceHandler)

	_, err = testSource.WriteMultipleRegisters(41000-1, uint16(len(testPattern1)/2), testPattern2)

	// Client
	handler := mbSlave.NewTCPClientHandler("127.0.0.1:502")
	// Connect manually so that multiple requests are handled in one connection session
	if err := handler.Connect(); err != nil {
		t.Errorf("failed to connect, got %v\n", err)
		t.FailNow()
	}
	defer handler.Close()
	handler.SlaveId = 1
	client := mbSlave.NewClient(handler)

	results, err = client.ReadHoldingRegisters(4134, 1)
	expect = []byte{1, 244}
	got = results
	if !isEqual(expect, got) {
		t.Errorf("expected %v, got %v", expect, got)
	}
	testSourceHandler.Close()

}
