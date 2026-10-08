package meters

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// ListenConfig configures the Modbus server. TCP and RTU can be active at the same time
// and serve the same unit IDs.
type ListenConfig struct {
	TCP ListenTCPConfig `yaml:"tcp"`
	RTU ListenRTUConfig `yaml:"rtu"`
}

// ListenTCPConfig configures the Modbus TCP server.
type ListenTCPConfig struct {
	Enabled bool   `yaml:"enabled"` // Start the Modbus TCP server
	Host    string `yaml:"host"`    // Listen address (0.0.0.0 = all interfaces)
	Port    int    `yaml:"port"`    // Listen port
}

// ListenRTUConfig configures the Modbus RTU server on a serial port.
type ListenRTUConfig struct {
	Enabled      bool `yaml:"enabled"` // Start the Modbus RTU server
	SerialConfig `yaml:",inline"`

	// InterFrameDelay is the silence that ends a request; 0 = t3.5 of the Modbus specification
	// (about 4 ms at 9600 baud). Raise it (e.g. 20-40ms) for USB adapters that split requests.
	InterFrameDelay time.Duration `yaml:"interFrameDelay"`
}

// SerialConfig holds the settings of a serial port.
type SerialConfig struct {
	Port     string `yaml:"port"`     // Device, e.g. /dev/ttyS0
	BaudRate int    `yaml:"baudRate"` // Baud rate, e.g. 9600
	DataBits int    `yaml:"dataBits"` // Data bits: 5-8
	Parity   string `yaml:"parity"`   // Parity: N | E | O
	StopBits int    `yaml:"stopBits"` // Stop bits: 1 | 2
}

// MeterConfig describes one emulated meter: where its values come from and on which
// unit IDs it answers.
type MeterConfig struct {
	Name       string                   `yaml:"-"`          // Key in the meter map, set by Validate
	UnitIDs    []uint8                  `yaml:"unitIDs"`    // Unit IDs the emulated meter answers on
	MaxCurrent float64                  `yaml:"maxCurrent"` // Rated current per phase in A, 0 = no plausibility check
	Source     SourceConfig             `yaml:"source"`     // Upstream meter
	Poll       PollConfig               `yaml:"poll"`       // Polling of the upstream meter
	Map        map[string]MappingConfig `yaml:"map"`        // Canonical field name -> mapping
}

// SourceConfig describes the connection to the upstream meter.
type SourceConfig struct {
	Type    string          `yaml:"type"`    // tcp | rtu
	UnitID  uint8           `yaml:"unitID"`  // Unit ID of the upstream meter
	Timeout time.Duration   `yaml:"timeout"` // Request timeout as Go duration string (e.g. 300ms)
	TCP     TCPSourceConfig `yaml:"tcp"`     // Used with type tcp
	RTU     SerialConfig    `yaml:"rtu"`     // Used with type rtu
}

// TCPSourceConfig holds the address of a Modbus TCP upstream meter.
type TCPSourceConfig struct {
	Host string `yaml:"host"` // Host name or IP address
	Port int    `yaml:"port"` // Port
}

// PollConfig controls how the upstream meter is read.
type PollConfig struct {
	Interval     time.Duration `yaml:"interval"`     // Poll interval as Go duration string (e.g. 1s)
	MaxBlockGap  int           `yaml:"maxBlockGap"`  // Unused registers read along to merge two fields into one request
	MaxBlockSize int           `yaml:"maxBlockSize"` // Registers per request, at most 125
}

// MappingConfig maps one canonical field to its source.
type MappingConfig struct {
	Type      string  `yaml:"type"`      // register | fixed | expr
	Address   uint16  `yaml:"address"`   // register: 0-based start address
	DType     string  `yaml:"dtype"`     // register: uint16 | uint32 | uint64 | int16 | int32 | int64 | float32
	ByteOrder string  `yaml:"byteOrder"` // register: big | little
	WordOrder string  `yaml:"wordOrder"` // register: big | little (32/64 bit only)
	Scale     int     `yaml:"scale"`     // register: value = (raw + offset) * 10^scale
	Offset    float64 `yaml:"offset"`    // register: added to the raw value before scaling
	Value     any     `yaml:"value"`     // fixed: the value
	Expr      string  `yaml:"expr"`      // expr: e.g. "{power_l1} + {power_l2}"
}

// Names returns the meter names in a fixed order, so validation errors and start-up
// do not depend on map order.
func Names(meters map[string]MeterConfig) []string {
	names := make([]string, 0, len(meters))
	for name := range meters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CheckUniqueUnitIDs ensures that every unit ID is served by exactly one meter.
func CheckUniqueUnitIDs(meters map[string]MeterConfig) error {
	owner := make(map[uint8]string)
	for _, name := range Names(meters) {
		for _, id := range meters[name].UnitIDs {
			if other, exists := owner[id]; exists {
				return fmt.Errorf("unitID %d is used by meter %q and meter %q", id, other, name)
			}
			owner[id] = name
		}
	}
	return nil
}

// ApplyDefaults fills unset optional values.
func (m *MeterConfig) ApplyDefaults() {
	if m.Source.Timeout == 0 {
		m.Source.Timeout = 2 * time.Second
	}
	if m.Poll.Interval == 0 {
		m.Poll.Interval = time.Second
	}
	if m.Poll.MaxBlockGap == 0 {
		m.Poll.MaxBlockGap = 4
	}
	if m.Poll.MaxBlockSize == 0 {
		m.Poll.MaxBlockSize = 125
	}

	switch strings.ToLower(m.Source.Type) {
	case "tcp":
		if m.Source.TCP.Port == 0 {
			m.Source.TCP.Port = 502
		}
	case "rtu":
		if m.Source.RTU.BaudRate == 0 {
			m.Source.RTU.BaudRate = 9600
		}
		if m.Source.RTU.DataBits == 0 {
			m.Source.RTU.DataBits = 8
		}
		if m.Source.RTU.Parity == "" {
			m.Source.RTU.Parity = "N"
		}
		if m.Source.RTU.StopBits == 0 {
			m.Source.RTU.StopBits = 1
		}
	}

	for field, mapping := range m.Map {
		if mapping.ByteOrder == "" {
			mapping.ByteOrder = "big"
		}
		if mapping.WordOrder == "" {
			mapping.WordOrder = "big"
		}
		m.Map[field] = mapping
	}
}

// Validate checks the meter for invalid or missing values.
func (m MeterConfig) Validate() error {
	if len(m.UnitIDs) == 0 {
		return errors.New("unitIDs requires at least one entry")
	}
	for _, id := range m.UnitIDs {
		if id == 0 || id > 247 {
			return fmt.Errorf("invalid unitID %d, must be 1-247", id)
		}
	}
	if m.MaxCurrent < 0 {
		return fmt.Errorf("invalid maxCurrent %g", m.MaxCurrent)
	}
	if m.Source.UnitID == 0 || m.Source.UnitID > 247 {
		return fmt.Errorf("invalid source.unitID %d, must be 1-247", m.Source.UnitID)
	}
	if m.Source.Timeout <= 0 {
		return fmt.Errorf("invalid source.timeout %s", m.Source.Timeout)
	}
	if m.Poll.Interval <= 0 {
		return fmt.Errorf("invalid poll.interval %s", m.Poll.Interval)
	}
	if m.Poll.MaxBlockGap < 0 {
		return fmt.Errorf("invalid poll.maxBlockGap %d", m.Poll.MaxBlockGap)
	}
	if m.Poll.MaxBlockSize < 1 || m.Poll.MaxBlockSize > 125 {
		return fmt.Errorf("invalid poll.maxBlockSize %d, must be 1-125", m.Poll.MaxBlockSize)
	}
	if len(m.Map) == 0 {
		return errors.New("map requires at least one entry")
	}

	switch strings.ToLower(m.Source.Type) {
	case "tcp":
		if m.Source.TCP.Host == "" {
			return errors.New("source.tcp.host is required")
		}
		if m.Source.TCP.Port < 1 || m.Source.TCP.Port > 65535 {
			return fmt.Errorf("invalid source.tcp.port %d", m.Source.TCP.Port)
		}
	case "rtu":
		if err := m.Source.RTU.Validate(); err != nil {
			return fmt.Errorf("invalid source.rtu: %w", err)
		}
	default:
		return fmt.Errorf("invalid source.type %q, must be tcp or rtu", m.Source.Type)
	}

	validMappingTypes := []string{"register", "fixed", "expr"}
	validDTypes := []string{"uint16", "uint32", "uint64", "int16", "int32", "int64", "float32"}
	validByteOrders := []string{"big", "little"}

	for field, mapping := range m.Map {
		if !slices.Contains(validMappingTypes, strings.ToLower(mapping.Type)) {
			return fmt.Errorf("field %q has invalid type %q", field, mapping.Type)
		}

		switch strings.ToLower(mapping.Type) {
		case "register":
			if !slices.Contains(validDTypes, strings.ToLower(mapping.DType)) {
				return fmt.Errorf("field %q has invalid dtype %q", field, mapping.DType)
			}
			if !slices.Contains(validByteOrders, strings.ToLower(mapping.ByteOrder)) {
				return fmt.Errorf("field %q has invalid byteOrder %q", field, mapping.ByteOrder)
			}
			if !slices.Contains(validByteOrders, strings.ToLower(mapping.WordOrder)) {
				return fmt.Errorf("field %q has invalid wordOrder %q", field, mapping.WordOrder)
			}
		case "fixed":
			if mapping.Value == nil {
				return fmt.Errorf("field %q requires value for fixed mapping", field)
			}
		case "expr":
			if strings.TrimSpace(mapping.Expr) == "" {
				return fmt.Errorf("field %q requires expr for expression mapping", field)
			}
		}
	}

	return nil
}

// Validate checks the serial port settings.
func (s SerialConfig) Validate() error {
	if s.Port == "" {
		return errors.New("port is required")
	}
	if s.BaudRate <= 0 {
		return fmt.Errorf("invalid baudRate %d", s.BaudRate)
	}
	if s.DataBits < 5 || s.DataBits > 8 {
		return fmt.Errorf("invalid dataBits %d, must be 5-8", s.DataBits)
	}
	if !slices.Contains([]string{"N", "E", "O"}, strings.ToUpper(s.Parity)) {
		return fmt.Errorf("invalid parity %q, must be N, E or O", s.Parity)
	}
	if s.StopBits != 1 && s.StopBits != 2 {
		return fmt.Errorf("invalid stopBits %d, must be 1 or 2", s.StopBits)
	}
	return nil
}
