package meters

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/womat/mbserver"
	"github.com/womat/smartmeter/pkg/fronius"
)

// writeFunctions are the Modbus write function codes, refused like a real meter does:
// write single coil, write single register, write multiple coils, write multiple registers.
var writeFunctions = []uint8{5, 6, 15, 16}

// ModbusServerService is the read-only Modbus server facing the inverter (RTU) and other
// clients (TCP), with one register memory per unit ID.
type ModbusServerService struct {
	server *mbserver.Server
	cancel context.CancelFunc
	listen ListenConfig // set by Start, reported by Activity
}

// NewModbusServerService creates the Modbus server with one device per unit ID of every meter.
func NewModbusServerService(meters map[string]MeterConfig) (*ModbusServerService, error) {
	if len(meters) == 0 {
		return nil, nil
	}

	if err := CheckUniqueUnitIDs(meters); err != nil {
		return nil, err
	}

	server := mbserver.NewServer(slog.Default().With("component", "mbserver"))
	for _, function := range writeFunctions {
		server.RegisterFunctionHandler(function, nil) // answered with IllegalFunction
	}

	for _, name := range Names(meters) {
		for _, id := range meters[name].UnitIDs {
			// unit ID 1 is created by mbserver.NewServer
			if id != 1 {
				if err := server.NewDevice(id); err != nil {
					return nil, fmt.Errorf("create Modbus device %d: %w", id, err)
				}
			}
			// Silent until the first valid snapshot: a client never reads empty registers.
			if err := server.SetOnline(id, false); err != nil {
				return nil, fmt.Errorf("Modbus device %d: %w", id, err)
			}
		}
	}

	return &ModbusServerService{server: server}, nil
}

// Start opens the configured TCP and RTU listeners. On an error, Close releases what was
// opened so far.
func (s *ModbusServerService) Start(listen ListenConfig) error {
	if s == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.listen = listen
	if err := s.server.Start(ctx); err != nil {
		return fmt.Errorf("start Modbus server: %w", err)
	}

	if listen.TCP.Enabled {
		address := net.JoinHostPort(listen.TCP.Host, strconv.Itoa(listen.TCP.Port))
		if err := s.server.ListenTCP(ctx, address); err != nil {
			return fmt.Errorf("listen tcp %s: %w", address, err)
		}
		slog.Info("Modbus TCP listener started", "address", address)
	}

	if listen.RTU.Enabled {
		config, err := serialConfig(listen.RTU.SerialConfig)
		if err != nil {
			return err
		}
		config.InterFrameDelay = listen.RTU.InterFrameDelay
		// mbserver detects the end of a request by the inter-frame delay and reopens the
		// port after a failure, e.g. an unplugged USB adapter.
		if err = s.server.ListenRTU(ctx, listen.RTU.Port, config); err != nil {
			return fmt.Errorf("listen rtu %s: %w", listen.RTU.Port, err)
		}
		slog.Info("Modbus RTU listener started", "port", listen.RTU.Port)
	}

	return nil
}

// SetOnline takes unitID off the bus (online false) or back on it; its registers stay as they
// are. An offline unit ID is answered like an unknown one: silence over RTU, exception 11 over TCP.
func (s *ModbusServerService) SetOnline(unitID uint8, online bool) error {
	return s.server.SetOnline(unitID, online)
}

// Activity is the state of the listeners for the web page: where they listen and how many
// requests they answered since start.
type Activity struct {
	TCP *TCPActivity `json:"tcp,omitempty"` // nil without TCP listener
	RTU *RTUActivity `json:"rtu,omitempty"` // nil without RTU listener
}

// TCPActivity describes the Modbus TCP listener.
type TCPActivity struct {
	Address  string `json:"address"`  // host:port
	Requests uint64 `json:"requests"` // answered since start
	Clients  int    `json:"clients"`  // connected right now
}

// RTUActivity describes the Modbus RTU listener.
type RTUActivity struct {
	Port     string `json:"port"`     // e.g. /dev/ttyS0
	Line     string `json:"line"`     // e.g. 9600 8N1
	Requests uint64 `json:"requests"` // answered since start
}

// Activity returns the listeners and their request counters, or an empty value before Start.
func (s *ModbusServerService) Activity() Activity {
	var a Activity
	if s == nil {
		return a
	}
	st := s.server.Stats()
	if s.listen.TCP.Enabled {
		a.TCP = &TCPActivity{
			Address:  net.JoinHostPort(s.listen.TCP.Host, strconv.Itoa(s.listen.TCP.Port)),
			Requests: st.TCPRequests,
			Clients:  st.TCPClients,
		}
	}
	if s.listen.RTU.Enabled {
		c := s.listen.RTU.SerialConfig
		a.RTU = &RTUActivity{
			Port:     c.Port,
			Line:     fmt.Sprintf("%d %d%s%d", c.BaudRate, c.DataBits, strings.ToUpper(c.Parity), c.StopBits),
			Requests: st.RTURequests,
		}
	}
	return a
}

// SerialStatus is the state of the RTU port, reported by /health.
type SerialStatus struct {
	Port      string    `json:"port"`
	Connected bool      `json:"connected"`
	Error     string    `json:"error,omitempty"`
	Since     time.Time `json:"since"`
}

// Status returns the state of the RTU port, or nil without RTU listener.
func (s *ModbusServerService) Status() []SerialStatus {
	if s == nil {
		return nil
	}
	var status []SerialStatus
	for _, st := range s.server.SerialStatus() {
		entry := SerialStatus{Port: st.Port, Connected: st.Connected, Since: st.Since}
		if st.Err != nil {
			entry.Error = st.Err.Error()
		}
		status = append(status, entry)
	}
	return status
}

// Ready reports an error while the RTU port is not connected: the inverter then gets no
// values at all.
func (s *ModbusServerService) Ready() error {
	return serialReady(s.Status())
}

func serialReady(status []SerialStatus) error {
	for _, st := range status {
		if !st.Connected {
			return fmt.Errorf("serial port %s not available since %s: %s", st.Port, st.Since.Format(time.RFC3339), st.Error)
		}
	}
	return nil
}

// Close stops the listeners and disconnects all clients, so the next server can bind the
// same port and serial line.
func (s *ModbusServerService) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.server.Close()
	return nil
}

// WriteSnapshot encodes snapshot into the Fronius register maps of unitID as one consistent
// update: a client reads either the previous or the new snapshot, never a mix.
func (s *ModbusServerService) WriteSnapshot(unitID uint8, snapshot fronius.Snapshot) error {
	var bank fronius.RegisterBank
	if err := bank.WriteSnapshot(snapshot); err != nil {
		return err
	}

	return s.server.UpdateHoldingRegisters(unitID, func(registers []uint16) error {
		for _, entry := range fronius.FroniusRegisterMap {
			for offset := range entry.Width() {
				registers[entry.Addr+offset] = bank.Uint16(entry.Addr + offset)
			}
		}
		return nil
	})
}

// RegisterRow is one entry of a register map as a client reads it.
type RegisterRow struct {
	Addr  int      `json:"addr"`
	Field string   `json:"field,omitempty"` // canonical field, empty for a fixed value
	Type  string   `json:"type"`            // uint16 | int16 | uint32 | int32 | float32 | string
	SF    int      `json:"sf"`              // value = raw * 10^sf
	Raw   []uint16 `json:"raw"`             // registers, high word first
	Value float64  `json:"value"`           // decoded value, 0 for strings
	Text  string   `json:"text,omitempty"`  // decoded string
}

// RegisterDump holds both register maps of one unit ID.
type RegisterDump struct {
	UnitID      uint8         `json:"unitID"`
	Online      bool          `json:"online"`      // false: the unit ID does not answer, see staleTimeout
	Proprietary []RegisterRow `json:"proprietary"` // Fronius RS485 map, read by the inverter
	SunSpec     []RegisterRow `json:"sunspec"`     // SunSpec models 1 and 203, read by wallboxes and evcc
}

// ErrUnknownUnit is returned by Registers for a unit ID no meter answers on.
var ErrUnknownUnit = errors.New("unknown unit ID")

// Registers returns both register maps of unitID as a client would read them, from one
// consistent copy: no mix of two snapshots.
func (s *ModbusServerService) Registers(unitID uint8) (RegisterDump, error) {
	dump := RegisterDump{UnitID: unitID, Online: s.server.Online(unitID)}
	var regs []uint16
	// UpdateHoldingRegisters holds the lock while the copy is made; nothing is changed.
	err := s.server.UpdateHoldingRegisters(unitID, func(r []uint16) error {
		regs = slices.Clone(r)
		return nil
	})
	if err != nil {
		return dump, fmt.Errorf("%w %d", ErrUnknownUnit, unitID)
	}

	rows := func(entries []fronius.RegisterEntry) []RegisterRow {
		out := make([]RegisterRow, 0, len(entries))
		for _, e := range entries {
			raw := slices.Clone(regs[e.Addr : e.Addr+e.Width()])
			value, text := fronius.DecodeEntry(e, raw)
			out = append(out, RegisterRow{Addr: e.Addr, Field: string(e.Field), Type: e.Type.String(), SF: e.SF, Raw: raw, Value: value, Text: text})
		}
		slices.SortFunc(out, func(a, b RegisterRow) int { return a.Addr - b.Addr })
		return out
	}
	dump.Proprietary = rows(fronius.ProprietaryRegisterMap)
	dump.SunSpec = rows(fronius.SunSpecRegisterMap)
	return dump, nil
}

func serialConfig(c SerialConfig) (mbserver.SerialConfig, error) {
	config := mbserver.SerialConfig{BaudRate: c.BaudRate, DataBits: c.DataBits}

	switch strings.ToUpper(c.Parity) {
	case "N":
		config.Parity = mbserver.NoParity
	case "E":
		config.Parity = mbserver.EvenParity
	case "O":
		config.Parity = mbserver.OddParity
	default:
		return config, fmt.Errorf("unsupported parity %q", c.Parity)
	}

	switch c.StopBits {
	case 1:
		config.StopBits = mbserver.OneStopBit
	case 2:
		config.StopBits = mbserver.TwoStopBits
	default:
		return config, fmt.Errorf("unsupported stop bits %d", c.StopBits)
	}
	return config, nil
}
