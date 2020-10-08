package tools

import (
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

func TestGetConnectionDeviceIdTimeOut(t *testing.T) {
	testPattern := []struct {
		pattern    string
		connection string
		deviceId   uint8
		timeOut    time.Duration
		maxRetries int
	}{
		{"TCP 192.0.2.10:502 DeviceId:2 Timeout:100 MaxRetries:2", "192.0.2.10:502", 2, 100 * time.Millisecond, 2},
		{"HTTP http://fritz.box Timeout:500", "http://fritz.box", 1, 500 * time.Millisecond, 0},
		{"HTTP https://fritz.box/abc?t=3 Timeout:500 MaxRetries:1", "https://fritz.box/abc?t=3", 1, 500 * time.Millisecond, 1},
	}

	for _, test := range testPattern {
		c, d, to, r := GetConnectionDeviceIdTimeOut(test.pattern)
		if !isEqual(test.connection, c) || !isEqual(test.deviceId, d) || !isEqual(test.timeOut, to) || !isEqual(test.maxRetries, r) {
			t.Errorf("expected %v %v %v %v, got %v %v %v %v", test.connection, test.deviceId, test.timeOut, test.maxRetries, c, d, to, r)
		}
	}
}

func TestPortSerialTimeOut(t *testing.T) {
	testPattern := []struct {
		pattern  string
		port     string
		baudRate uint
		dataBits uint
		parity   string
		stopBit  uint
		timeOut  time.Duration
	}{
		{"RTU com3,9600,8,O,1 Timeout:5000", "com3", 9600, 8, "O", 1, 5000 * time.Millisecond},
		{"/dev/ttyS0,19200,8,N,2 Timeout:1000", "/dev/ttyS0", 19200, 8, "N", 2, 1000 * time.Millisecond},
	}

	for _, test := range testPattern {
		port, b, d, p, s, to := GetPortSerialTimeOut(test.pattern)
		if !isEqual(test.port, port) || !isEqual(test.baudRate, b) || !isEqual(test.dataBits, d) || !isEqual(test.parity, p) || !isEqual(test.stopBit, s) || !isEqual(test.timeOut, to) {
			t.Errorf("expected %v %v %v %v %v %v, got %v %v %v %v %v %v", test.port, test.baudRate, test.dataBits, test.parity, test.stopBit, test.timeOut, port, b, d, p, s, to)
		}
	}
}
