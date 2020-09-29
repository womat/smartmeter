package global

import (
	"io"
	"time"

	"github.com/jacobsa/go-serial/serial"
)

// VERSION holds the version information with the following logic in mind
//  1 ... fixed
//  0 ... year 2020, 1->year 2021, etc.
//  7 ... month of year (7=July)
//  the date format after the + is always the first of the month
//
// VERSION differs from semantic versioning as described in https://semver.org/
// but we keep the correct syntax.
const VERSION = "1.0.11+20200929"

const (
	Polling = iota
	Request
)

type Register struct {
	Address uint16
	Format  int
	SF      int
	Mul     float64
	Value   interface{}
}

type RegisterMap struct {
	Client    string
	Server    string
	ServerReg Register
	ClientReg Register
}

type ClientConfig struct {
	Type        string
	Connection  string
	Mode        int
	TimeOut     time.Duration
	PollingRate time.Duration
	DeviceId    uint8
	Register    map[string]RegisterMap
}

type ModbusServer struct {
	Connection string
	TimeOut    time.Duration
	Options    serial.OpenOptions
}

type Configuration struct {
	Debug struct {
		File    io.WriteCloser
		Flag    int
		Package map[string]int
	}
	Clients      map[string]ClientConfig
	ModbusServer ModbusServer
	Register     map[string]RegisterMap
}

// Config holds the global configuration
var Config Configuration

func init() {
	Config = Configuration{
		Debug: struct {
			File    io.WriteCloser
			Flag    int
			Package map[string]int
		}{
			Package: map[string]int{},
		},
		Clients:      map[string]ClientConfig{},
		ModbusServer: ModbusServer{},
		Register:     map[string]RegisterMap{},
	}
}
