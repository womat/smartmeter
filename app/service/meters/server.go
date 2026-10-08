package meters

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
			if id == 1 {
				continue
			}
			if err := server.NewDevice(id); err != nil {
				return nil, fmt.Errorf("create Modbus device %d: %w", id, err)
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
			for offset := range registerEntryWidth(entry) {
				registers[entry.Addr+offset] = bank.Uint16(entry.Addr + offset)
			}
		}
		return nil
	})
}

func registerEntryWidth(entry fronius.RegisterEntry) int {
	switch entry.Type {
	case fronius.RegString:
		return entry.NRegs
	case fronius.RegUint16, fronius.RegInt16:
		return 1
	case fronius.RegUint32, fronius.RegInt32, fronius.RegFloat32:
		return 2
	default:
		return 1
	}
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
