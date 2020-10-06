package tools

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

func GetPortSerialTimeOut(config string) (portName string, baudRate uint, dataBits uint, parity string, stopBit uint, TimeOut time.Duration) {
	TimeOut = time.Second

	m := make(map[string]string)
	fields := strings.Fields(config)

	for _, field := range fields {
		// check for connection string and split it into fields
		// eg "RTU /dev/ttyS0,9600,8,N,1 DeviceId:1 Timeout:1"
		if regexp.MustCompile(`^[0-9A-Za-z:/.\-]*,[0-9]{1,5},[5678],[NEO],[12]$`).MatchString(field) {
			f := strings.Split(field, ",")
			portName = f[0]
			b, _ := strconv.Atoi(f[1])
			baudRate = uint(b)
			b, _ = strconv.Atoi(f[2])
			dataBits = uint(b)
			parity = f[3]
			b, _ = strconv.Atoi(f[4])
			stopBit = uint(b)
		}

		// split fields into a map, eg DeviceId:1 >> m[DeviceId]=1
		parts := strings.Split(field, ":")
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
			continue
		}
		m[parts[0]] = ""
	}

	for p, v := range m {
		i, _ := strconv.Atoi(v)
		switch p {
		case "Timeout":
			TimeOut = time.Duration(i) * time.Millisecond
		}
	}

	return
}

func GetConnectionDeviceIdTimeOut(config string) (connection string, DeviceId byte, TimeOut time.Duration, MaxRetries int) {
	DeviceId = 1
	TimeOut = time.Second

	m := make(map[string]string)
	fields := strings.Fields(config)

	for _, field := range fields {
		// check if connection string is valid
		// eg "192.0.2.10:502"
		if regexp.MustCompile(`^[\d]{1,3}\.[\d]{1,3}\.[\d]{1,3}\.[\d]{1,3}:[\d]{1,5}$`).MatchString(field) {
			connection = field
		}
		if regexp.MustCompile(`^https?://.*$`).MatchString(field) {
			connection = field
		}
		// split fields into a map, eg DeviceId:1 >> m[DeviceId]=1
		parts := strings.Split(field, ":")
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
			continue
		}
		m[parts[0]] = ""
	}

	for p, v := range m {
		i, _ := strconv.Atoi(v)
		switch p {
		case "DeviceId":
			if i > 0 && i < 248 {
				DeviceId = byte(i)
			}
		case "Timeout":
			TimeOut = time.Duration(i) * time.Millisecond
		case "MaxRetries":
			MaxRetries = i
		}
	}

	return
}
