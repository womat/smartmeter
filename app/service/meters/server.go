package meters

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/jacobsa/go-serial/serial"
	"github.com/womat/smartmeter/pkg/fronius"
	"github.com/womat/smartmeter/pkg/mbserver"
)

type ModbusServerService struct {
	server      *mbserver.Server
	rtuListener io.ReadWriteCloser
}

// NewModbusServerService creates the Modbus server with one device per unit ID of every meter.
func NewModbusServerService(meters map[string]MeterConfig) (*ModbusServerService, error) {
	if len(meters) == 0 {
		return nil, nil
	}

	if err := CheckUniqueUnitIDs(meters); err != nil {
		return nil, err
	}

	service := &ModbusServerService{server: mbserver.NewServer()}
	for _, name := range Names(meters) {
		for _, id := range meters[name].UnitIDs {
			// unit ID 1 is created by mbserver.NewServer
			if id == 1 {
				continue
			}
			if err := service.server.NewDevice(id); err != nil {
				return nil, fmt.Errorf("create Modbus device %d: %w", id, err)
			}
		}
	}

	return service, nil
}

func (s *ModbusServerService) Start(listen ListenConfig) error {
	if s == nil {
		return nil
	}

	if listen.TCP.Enabled {
		address := net.JoinHostPort(listen.TCP.Host, strconv.Itoa(listen.TCP.Port))
		if err := s.server.ListenTCP(address); err != nil {
			return fmt.Errorf("listen tcp %s: %w", address, err)
		}
		slog.Info("Modbus TCP listener started", "address", address)
	}

	if listen.RTU.Enabled {
		parity, err := parseSerialParity(listen.RTU.Parity)
		if err != nil {
			return err
		}

		port, err := serial.Open(serial.OpenOptions{
			PortName:              listen.RTU.Port,
			BaudRate:              uint(listen.RTU.BaudRate),
			DataBits:              uint(listen.RTU.DataBits),
			StopBits:              uint(listen.RTU.StopBits),
			ParityMode:            parity,
			InterCharacterTimeout: 100,
			MinimumReadSize:       1,
		})
		if err != nil {
			return fmt.Errorf("open RTU listener %s: %w", listen.RTU.Port, err)
		}

		if err = s.server.ListenRTU(port); err != nil {
			_ = port.Close()
			return fmt.Errorf("listen rtu %s: %w", listen.RTU.Port, err)
		}
		s.rtuListener = port
		slog.Info("Modbus RTU listener started", "port", listen.RTU.Port)
	}

	return nil
}

func (s *ModbusServerService) Close() error {
	if s == nil {
		return nil
	}

	var errs error
	if s.rtuListener != nil {
		errs = joinErrors(errs, s.rtuListener.Close())
	}
	errs = joinErrors(errs, s.server.Close())
	return errs
}

func (s *ModbusServerService) WriteSnapshot(unitID uint8, snapshot fronius.Snapshot) error {
	var bank fronius.RegisterBank
	if err := bank.WriteSnapshot(snapshot); err != nil {
		return err
	}

	s.server.Lock()
	defer s.server.Unlock()

	for _, entry := range fronius.FroniusRegisterMap {
		count := registerEntryWidth(entry)
		for offset := 0; offset < count; offset++ {
			if err := s.server.SetHoldingRegister(unitID, uint16(entry.Addr+offset), bank.Uint16(entry.Addr+offset)); err != nil {
				return err
			}
		}
	}

	return nil
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

func parseSerialParity(value string) (serial.ParityMode, error) {
	switch strings.ToUpper(value) {
	case "N":
		return serial.PARITY_NONE, nil
	case "E":
		return serial.PARITY_EVEN, nil
	case "O":
		return serial.PARITY_ODD, nil
	default:
		return serial.PARITY_NONE, fmt.Errorf("unsupported parity %q", value)
	}
}

func joinErrors(left, right error) error {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return fmt.Errorf("%w; %v", left, right)
}
