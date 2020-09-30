package config

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jacobsa/go-serial/serial"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"SmartmeterEmu/global"
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

func init() {
	type yamlStruct struct {
		Debug struct {
			File    string
			Flag    string
			Package map[string]string
		}
		Clients map[string]struct {
			Type        string
			Connection  string
			Mode        string
			PollingRate int
			DeviceId    uint8
			Register    map[string]global.RegisterMap
		}
		ModbusServer global.ModbusServer
		Register     map[string]global.RegisterMap
	}

	var configFile yamlStruct

	flag.Bool("version", false, "print version and exit")
	flag.String("debug.file", "stderr", "log file eg. /tmp/emu.log")
	flag.String("debug.flag", "", "enable debug information (standard | trace | debug)")
	flag.String("config", "", "Config File eg. /opt/womat/config.yaml")

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	_ = viper.BindPFlags(pflag.CommandLine)

	if viper.GetBool("version") {
		fmt.Printf("Version: %v\n", global.VERSION)
		os.Exit(0)
	}

	if f := viper.GetString("config"); f != "" {
		viper.SetConfigFile(f)
	} else {
		viper.SetConfigName("config")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/opt/womat/")
	}

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config file, %s", err)
	}
	err := viper.Unmarshal(&configFile)
	if err != nil {
		log.Fatalf("unable to decode into struct, %v", err)
	}

	// split the config string into a register structure
	for name, register := range configFile.Register {
		//TODO registers should not be global, each client needs register config
		register.ClientReg = getRegisterConfig(register.ClientReg, register.Client)
		register.ServerReg = getRegisterConfig(register.ServerReg, register.Server)
		configFile.Register[name] = register
	}
	{
		// split the connection string into am modbus client/server structure
		var p string
		parity := map[string]serial.ParityMode{
			"N": serial.PARITY_NONE,
			"O": serial.PARITY_ODD,
			"E": serial.PARITY_EVEN,
		}

		configFile.ModbusServer.Options.PortName,
			configFile.ModbusServer.Options.BaudRate,
			configFile.ModbusServer.Options.DataBits,
			p,
			configFile.ModbusServer.Options.StopBits,
			configFile.ModbusServer.TimeOut = getServerConnection(configFile.ModbusServer.Connection)
		configFile.ModbusServer.Options.ParityMode = parity[p]
	}

	getDebugFlag := func(flag string) int {
		switch flag {
		case "trace":
			return Full
		case "debug":
			return Warning | Info | Error | Fatal | Debug
		case "standard":
			return Standard
		}
		return 0
	}

	global.Config.Debug.Flag = getDebugFlag(configFile.Debug.Flag)
	for n, p := range configFile.Debug.Package {
		global.Config.Debug.Package[n] = getDebugFlag(p)
	}

	switch file := configFile.Debug.File; file {
	case "stderr":
		global.Config.Debug.File = os.Stderr
	case "stdeout":
		global.Config.Debug.File = os.Stdout
	default:
		if !FileExists(file) {
			_ = CreateFile(file)
		}
		if global.Config.Debug.File, err = os.Open(file); err != nil {
			fatallog.Println(err)
			os.Exit(0)
		}
	}

	for n, c := range configFile.Clients {
		connection, _, timeOut := getClientConnection(c.Connection)
		polling := 60 * time.Second
		mode := global.Polling
		if p := time.Duration(c.PollingRate); p > 0 {
			polling = p * time.Second
		}

		if c.Mode == "request" {
			mode = global.Request
		}
		global.Config.Clients[n] = global.ClientConfig{
			Type:        c.Type,
			Connection:  connection,
			TimeOut:     timeOut,
			Mode:        mode,
			PollingRate: polling,
			DeviceId:    c.DeviceId,
			Register:    c.Register,
		}
	}
	global.Config.Register = configFile.Register
	global.Config.ModbusServer = configFile.ModbusServer
	return
}

func getServerConnection(config string) (portName string, baudRate uint, dataBits uint, parity string, stopBit uint, TimeOut time.Duration) {
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

func getClientConnection(config string) (IpAddress string, DeviceId byte, TimeOut time.Duration) {
	DeviceId = 1
	TimeOut = time.Second

	m := make(map[string]string)
	fields := strings.Fields(config)

	for _, field := range fields {
		// check if connection string is valid
		// eg "192.0.2.10:502"
		if regexp.MustCompile(`^[\d]{1,3}\.[\d]{1,3}\.[\d]{1,3}\.[\d]{1,3}:[\d]{1,5}$`).MatchString(field) {
			IpAddress = field
		}
		if regexp.MustCompile(`^https?:\/\/.*$`).MatchString(field) {
			//TODO redundant character escape '\/' in regexp
			IpAddress = field
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
		}
	}

	return
}

func getRegisterConfig(reg global.Register, config string) global.Register {
	reg.Format = _uint16
	reg.Mul = 1

	m := make(map[string]string)
	fields := strings.Fields(config)

	// split fields into a map, eg Value:99 >> m[Value]=99
	for _, field := range fields {
		parts := strings.Split(field, ":")
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
			continue
		}
		m[parts[0]] = ""
	}

	for p, v := range m {
		switch p {
		case "sint16":
			reg.Format = _sint16
		case "sint32":
			reg.Format = _sint32
		case "sint64":
			reg.Format = _sint64
		case "uint16":
			reg.Format = _uint16
		case "uint32":
			reg.Format = _uint32
		case "uint64":
			reg.Format = _uint64
		case "Addr":
			i, _ := strconv.Atoi(v)
			reg.Address = uint16(i)
		case "SF":
			reg.SF, _ = strconv.Atoi(v)
		case "Mul":
			reg.Mul, _ = strconv.ParseFloat(v, 64)
		case "Value":
			x, _ := strconv.ParseInt(v, 10, 64)
			// since value is of type interface {}, the correct type must be determined
			for format := range m {
				switch format {
				case "sint16":
					reg.Value = int16(x)
				case "sint32":
					reg.Value = int32(x)
				case "sint64":
					reg.Value = x
				case "uint16":
					reg.Value = uint16(x)
				case "uint32":
					reg.Value = uint32(x)
				case "uint64":
					reg.Value = uint64(x)
				}
			}
			if reg.Value == nil {
				//if no type was defined, UINT16 is used (corresponds to modbus register)
				reg.Value = uint16(x)
			}
		}
	}

	return reg
}

/*
	configuration.ModbusServer.Name = "Fronius Smartmeter"
	configuration.ModbusServer.Connection = "RTU /dev/ttyS0,9600,8,N,1 DeviceID 1"
	configuration.ModbusServer.Options = OpenOptions{
		RTSCTSFlowControl:       false,
		InterCharacterTimeout:   100,
		MinimumReadSize:         4,
		Rs485Enable:             false,
		Rs485RtsHighDuringSend:  false,
		Rs485RtsHighAfterSend:   false,
		Rs485RxDuringTx:         false,
		Rs485DelayRtsBeforeSend: 0,
		Rs485DelayRtsAfterSend:  0,
	}


	configuration.ModbusClient.Connection = "TCP 192.0.2.10:502  DeviceID 1 Timeout 1"
	configuration.Register = map[string]RegisterMap{
		"unknown": {
			MasterReg: Register{Address: 0x0012, Value: uint16(0x011d)}},
		"Device Identifier": {
			MasterReg: Register{Address: 0x0300, Value: uint16(0x75)}},
		"SerialNumber": {
			MasterReg: Register{Address: 0x1050, Value: uint32(99999999)}},
		"Energy into grid": {
			ClientReg:      Register{Address: 8, Format: _uint64, SF: 0},
			MasterReg: Register{Address: 0x1020, Format: _uint32, SF: 0, Mul: 1}},
		"Energy Smartfox": {
			ClientReg:      Register{Address: 16, Format: _uint64, SF: 0, Mul: 1},
			MasterReg: Register{Address: 0, Format: _nil, SF: 0, Mul: 1}},
		"Day Energy from grid": {
			ClientReg:      Register{Address: 24, Format: _uint32, SF: 0},
			MasterReg: Register{Address: 0, Format: _nil, SF: 0, Mul: 1}},
		"Day Energy into grid": {
			ClientReg:      Register{Address: 28, Format: _uint32, SF: 0},
			MasterReg: Register{Address: 0, Format: _nil, SF: 0, Mul: 1}},
		"Day Energy Smartfox": {
			ClientReg:      Register{Address: 32, Format: _uint32, SF: 0},
			MasterReg: Register{Address: 0, Format: _nil, SF: 0, Mul: 1}},
		"Power total": {
			ClientReg:      Register{Address: 36, Format: _uint32, SF: 0},
			MasterReg: Register{Address: 0x1014, Format: _uint32, SF: 2, Mul: 1}},
		"Power L1": {
			ClientReg:      Register{Address: 40, Format: _uint32, SF: 0},
			MasterReg: Register{Address: 0x102c, Format: _uint32, SF: 2, Mul: 1}},
		"Power L2": {
			ClientReg:      Register{Address: 44, Format: _uint32, SF: 0},
			MasterReg: Register{Address: 0x102e, Format: _uint32, SF: 2, Mul: 1}},
		"Power L3": {
			ClientReg:      Register{Address: 48, Format: _uint32, SF: 0},
			MasterReg: Register{Address: 0x1030, Format: _uint32, SF: 2, Mul: 1}},
		"Voltage L1-N": {
			ClientReg:      Register{Address: 52, Format: _uint16, SF: -1},
			MasterReg: Register{Address: 0x1000, Format: _uint32, SF: 3, Mul: 1}},
		"Voltage L2-N": {
			ClientReg:      Register{Address: 54, Format: _uint16, SF: -1},
			MasterReg: Register{Address: 0x1002, Format: _uint32, SF: 3, Mul: 1}},
		"Voltage L3-N": {
			ClientReg:      Register{Address: 56, Format: _uint16, SF: -1},
			MasterReg: Register{Address: 0x1004, Format: _uint32, SF: 3, Mul: 1}},
		"Current L1": {
			ClientReg:      Register{Address: 58, Format: _uint32, SF: -3},
			MasterReg: Register{Address: 0x1006, Format: _uint32, SF: 3, Mul: 1}},
		"Current L2": {
			ClientReg:      Register{Address: 62, Format: _uint32, SF: -3},
			MasterReg: Register{Address: 0x1008, Format: _uint32, SF: 3, Mul: 1}},
		"Current L3": {
			ClientReg:      Register{Address: 66, Format: _uint32, SF: -3},
			MasterReg: Register{Address: 0x100a, Format: _uint32, SF: 3, Mul: 1}},
		"Voltage L1-L2": {
			ClientReg:      Register{Address: 52, Format: _uint16, SF: -1},
			MasterReg: Register{Address: 0x100e, Format: _uint32, SF: 3, Mul: 1.732050807568877}},
		"Voltage L2-L3": {
			ClientReg:      Register{Address: 54, Format: _uint16, SF: -1},
			MasterReg: Register{Address: 0x1010, Format: _uint32, SF: 3, Mul: 1.732050807568877}},
		"Voltage L3-L1": {
			ClientReg:      Register{Address: 56, Format: _uint16, SF: -1},
			MasterReg: Register{Address: 0x1012, Format: _uint32, SF: 3, Mul: 1.732050807568877}},
		"Powerfactor L1": {
			ClientReg:      Register{Address: 70, Format: _uint16, SF: -4},
			MasterReg: Register{Address: 0x1044, Format: _uint16, SF: 3, Mul: 1}},
		"Powerfactor L2": {
			ClientReg:      Register{Address: 72, Format: _uint16, SF: -4},
			MasterReg: Register{Address: 0x1045, Format: _uint16, SF: 3, Mul: 1}},
		"Powerfactor L3": {
			ClientReg:      Register{Address: 74, Format: _uint16, SF: -4},
			MasterReg: Register{Address: 0x1046, Format: _uint16, SF: 3, Mul: 1}},
		"Frequeny": {
			ClientReg:      Register{Address: 76, Format: _uint16, SF: -2},
			MasterReg: Register{Address: 0x1026, Format: _uint16, SF: 1, Mul: 1}},
	}

	for n, x := range configuration.Register {
		x.Client = fmt.Sprintf("Address: %v Format: %v SF: %v Mul: %v Value: %v", x.ClientReg.Address, x.ClientReg.Format, x.ClientReg.SF, x.ClientReg.Mul, x.ClientReg.Value)
		x.Server = fmt.Sprintf("Address: %v Format: %v SF: %v Mul: %v Value: %v", x.MasterReg.Address, x.MasterReg.Format, x.MasterReg.SF, x.MasterReg.Mul, x.MasterReg.Value)
		configuration.Register[n] = x
	}

	x, err := yaml.Marshal(configuration)
	if err != nil {
		log.Println(err)
	}

	log.Println(x)
*/

func FileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func CreateFile(name string) error {
	fo, err := os.Create(name)
	if err != nil {
		return err
	}
	defer func() {
		fo.Close()
	}()
	return nil
}
