package meters

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mb "github.com/simonvetter/modbus"
	"github.com/womat/smartmeter/pkg/fronius"
)

const (
	defaultDeviceID     = 285
	defaultFirmwareCode = 117
	defaultSerialNumber = 99999999

	maxGridVoltage     = 253.0 // 230 V + 10 % (EN 50160)
	plausibilityMargin = 1.2   // short overload above the rated current
)

type registerReader interface {
	ReadHoldingRegisters(address, quantity uint16) ([]uint16, error)
	Close() error
}

type readBlock struct {
	Start    uint16
	Quantity uint16
}

type compiledMapping struct {
	Field      fronius.CanonicalField
	ConfigName string
	Config     MappingConfig
	Registers  uint16
}

type compiledDevice struct {
	Config           MeterConfig
	RegisterMappings []compiledMapping
	FixedMappings    []compiledMapping
	ExprMappings     []compiledMapping
	Blocks           []readBlock
	Reader           registerReader

	mu            sync.Mutex // guards the fields below, read by Status from the web server
	started       time.Time  // start of polling, the reference for staleTimeout before the first snapshot
	lastSuccess   time.Time  // last snapshot written to the Modbus server
	lastValues    map[string]float64
	latency       time.Duration // duration of the last successful read of the source
	polls         uint64        // successful polls, for the activity LED of the web page
	lastError     string        // last failed or discarded poll
	lastErrorTime time.Time
	discarded     uint64    // snapshots discarded by the plausibility check
	online        bool      // the unit IDs answer; false until the first snapshot and after staleTimeout
	offlineSince  time.Time // when the unit IDs went silent after staleTimeout
}

// MeterStatus is the diagnostic state of one meter, reported by /health.
type MeterStatus struct {
	UnitIds       []int      `json:"unitIds"`                 // Unit IDs the meter answers on (int: []uint8 would be base64 in JSON)
	Source        string     `json:"source"`                  // Upstream meter, e.g. "tcp 192.168.1.10:502 unit 1"
	Ready         bool       `json:"ready"`                   // Values are current, see staleAfter
	Online        bool       `json:"online"`                  // The unit IDs answer, see staleTimeout
	OfflineSince  *time.Time `json:"offlineSince,omitempty"`  // When the unit IDs went silent after staleTimeout
	LastSuccess   *time.Time `json:"lastSuccess,omitempty"`   // Last valid snapshot from the source
	AgeSeconds    *float64   `json:"ageSeconds,omitempty"`    // Age of the served values
	LatencyMs     *float64   `json:"latencyMs,omitempty"`     // Duration of the last successful read of the source
	Polls         uint64     `json:"polls"`                   // Successful polls since start
	LastError     string     `json:"lastError,omitempty"`     // Last failed or discarded poll
	LastErrorTime *time.Time `json:"lastErrorTime,omitempty"` // Time of LastError
	Discarded     uint64     `json:"discardedSnapshots"`      // Snapshots discarded as implausible

	PollIntervalSeconds float64      `json:"pollIntervalSeconds"` // Poll interval of the source
	StaleTimeoutSeconds float64      `json:"staleTimeoutSeconds"` // 0 = the unit IDs never go silent
	MaxCurrent          float64      `json:"maxCurrent"`          // A per phase, plausibility check
	Limits              LimitsConfig `json:"limits"`              // Scale of the web page

	// Values are the canonical fields of the last valid snapshot, as served on the unit IDs.
	Values map[string]float64 `json:"values,omitempty"`
}

// staleAfter is how many poll intervals the served values may be old before the meter
// counts as not ready.
const staleAfter = 3

type snapshotWriter interface {
	WriteSnapshot(unitId uint8, snapshot fronius.Snapshot) error
	SetOnline(unitId uint8, online bool) error
}

type ModbusService struct {
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	devices []*compiledDevice
}

// NewModbusService compiles the polling of every meter; Start begins polling.
func NewModbusService(meters map[string]MeterConfig) (*ModbusService, error) {
	if len(meters) == 0 {
		return nil, nil
	}

	if err := CheckUniqueUnitIds(meters); err != nil {
		return nil, err
	}

	service := &ModbusService{}
	for _, name := range Names(meters) {
		meter := meters[name]
		meter.Name = name
		device, err := compileDevice(meter)
		if err != nil {
			return nil, fmt.Errorf("meter %q: %w", name, err)
		}
		service.devices = append(service.devices, device)
	}

	return service, nil
}

func (s *ModbusService) Start(parent context.Context, writer snapshotWriter) error {
	if s == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel

	startedReaders := make([]registerReader, 0, len(s.devices))
	for _, device := range s.devices {
		reader, err := newRegisterReader(device.Config.Source)
		if err != nil {
			_ = s.closeReaders(startedReaders)
			cancel()
			return fmt.Errorf("device %q source setup failed: %w", device.Config.Name, err)
		}
		device.Reader = reader
		startedReaders = append(startedReaders, reader)

		s.wg.Add(1)
		go func(d *compiledDevice) {
			defer s.wg.Done()
			d.run(ctx, writer)
		}(device)
	}

	return nil
}

func (s *ModbusService) Close() error {
	if s == nil {
		return nil
	}

	if s.cancel != nil {
		s.cancel()
	}

	var errs error
	for _, device := range s.devices {
		if device.Reader != nil {
			errs = errors.Join(errs, device.Reader.Close())
		}
	}

	s.wg.Wait()
	return errs
}

// Status returns the diagnostic state of every meter, keyed by name.
func (s *ModbusService) Status(now time.Time) map[string]MeterStatus {
	status := make(map[string]MeterStatus)
	if s == nil {
		return status
	}
	for _, device := range s.devices {
		status[device.Config.Name] = device.status(now)
	}
	return status
}

// Ready reports an error while a meter has not delivered a valid snapshot within
// staleAfter poll intervals: the Modbus server then serves outdated values.
func (s *ModbusService) Ready(now time.Time) error {
	if s == nil {
		return nil
	}
	var stale []string
	for _, device := range s.devices {
		if !device.status(now).Ready {
			stale = append(stale, device.Config.Name)
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("no current values from meter %s", strings.Join(stale, ", "))
	}
	return nil
}

func (s *ModbusService) closeReaders(readers []registerReader) error {
	var errs error
	for _, reader := range readers {
		errs = errors.Join(errs, reader.Close())
	}
	return errs
}

func compileDevice(cfg MeterConfig) (*compiledDevice, error) {
	compiled := &compiledDevice{Config: cfg}

	fieldsSeen := make(map[fronius.CanonicalField]struct{}, len(cfg.Map))
	for fieldName, mapping := range cfg.Map {
		field, err := normalizeCanonicalField(fieldName)
		if err != nil {
			return nil, err
		}
		if _, exists := fieldsSeen[field]; exists {
			return nil, fmt.Errorf("field %q is configured more than once", field)
		}
		fieldsSeen[field] = struct{}{}

		compMapping := compiledMapping{
			Field:      field,
			ConfigName: fieldName,
			Config:     mapping,
			Registers:  0,
		}

		switch strings.ToLower(mapping.Type) {
		case "register":
			compMapping.Registers, err = registerCount(mapping.DType)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", fieldName, err)
			}
			compiled.RegisterMappings = append(compiled.RegisterMappings, compMapping)
		case "fixed":
			compiled.FixedMappings = append(compiled.FixedMappings, compMapping)
		case "expr":
			compiled.ExprMappings = append(compiled.ExprMappings, compMapping)
		default:
			return nil, fmt.Errorf("field %q: unsupported mapping type %q", fieldName, mapping.Type)
		}
	}

	compiled.Blocks = buildReadBlocks(compiled.RegisterMappings, cfg.Poll.MaxBlockGap, cfg.Poll.MaxBlockSize)
	return compiled, nil
}

func buildReadBlocks(mappings []compiledMapping, maxGap, maxSize int) []readBlock {
	if len(mappings) == 0 {
		return nil
	}

	sorted := slices.Clone(mappings)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Config.Address == sorted[j].Config.Address {
			return sorted[i].Registers < sorted[j].Registers
		}
		return sorted[i].Config.Address < sorted[j].Config.Address
	})

	blocks := []readBlock{{
		Start:    sorted[0].Config.Address,
		Quantity: sorted[0].Registers,
	}}

	for _, mapping := range sorted[1:] {
		current := &blocks[len(blocks)-1]
		currentEnd := int(current.Start) + int(current.Quantity) - 1
		nextStart := int(mapping.Config.Address)
		nextEnd := nextStart + int(mapping.Registers) - 1

		gap := nextStart - currentEnd - 1
		newQuantity := nextEnd - int(current.Start) + 1
		if gap <= maxGap && newQuantity <= maxSize {
			current.Quantity = uint16(newQuantity)
			continue
		}

		blocks = append(blocks, readBlock{
			Start:    mapping.Config.Address,
			Quantity: mapping.Registers,
		})
	}

	return blocks
}

func (d *compiledDevice) run(ctx context.Context, writer snapshotWriter) {
	d.mu.Lock()
	d.started = time.Now()
	d.mu.Unlock()

	report := d.pollReporter()
	report(d.pollAndUpdate(writer))

	ticker := time.NewTicker(d.Config.Poll.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report(d.pollAndUpdate(writer))
			d.checkStale(writer, time.Now())
		}
	}
}

// pollReporter returns a function that logs the result of each poll: the first failure, then
// one a minute while the source stays down, so an outage of hours does not fill the journal,
// and the recovery. /health reports every failure regardless.
func (d *compiledDevice) pollReporter() func(error) {
	every := max(1, int(time.Minute/d.Config.Poll.Interval))
	failures := 0
	return func(err error) {
		if err == nil {
			if failures > 0 {
				slog.Info("Source answers again", "meter", d.Config.Name, "failedPolls", failures)
			}
			failures = 0
			return
		}
		failures++
		if failures == 1 || failures%every == 0 {
			slog.Error("Modbus poll failed", "meter", d.Config.Name, "failedPolls", failures, "error", err)
		}
	}
}

// checkStale takes the unit IDs off the bus once the source has delivered no valid snapshot for
// staleTimeout, so the inverter sees a failed meter instead of acting on frozen values. The
// registers keep the last values; the next valid snapshot brings the unit IDs back.
func (d *compiledDevice) checkStale(writer snapshotWriter, now time.Time) {
	timeout := d.Config.Stale()
	d.mu.Lock()
	since := d.lastSuccess
	if since.IsZero() {
		since = d.started
	}
	expired := timeout > 0 && d.online && now.Sub(since) > timeout
	if expired {
		d.online = false
		d.offlineSince = now
	}
	d.mu.Unlock()
	if !expired {
		return
	}

	if err := d.setOnline(writer, false); err != nil {
		slog.Error("Failed to take the unit IDs off the bus", "meter", d.Config.Name, "error", err)
		return
	}
	slog.Warn("No valid values from the source, unit IDs no longer answer",
		"meter", d.Config.Name, "unitIds", d.unitIds(), "staleTimeout", timeout, "lastValid", d.lastSuccessTime())
}

func (d *compiledDevice) setOnline(writer snapshotWriter, online bool) error {
	var errs error
	for _, id := range d.Config.UnitIds {
		errs = errors.Join(errs, writer.SetOnline(id, online))
	}
	return errs
}

// unitIds returns the unit IDs as numbers for logging; slog prints []uint8 as bytes.
func (d *compiledDevice) unitIds() []int {
	ids := make([]int, 0, len(d.Config.UnitIds))
	for _, id := range d.Config.UnitIds {
		ids = append(ids, int(id))
	}
	return ids
}

func (d *compiledDevice) lastSuccessTime() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastSuccess
}

func (d *compiledDevice) pollAndUpdate(writer snapshotWriter) error {
	start := time.Now()
	snapshot, err := d.pollSnapshot()
	if err != nil {
		d.recordError(err, false)
		return err
	}
	latency := time.Since(start)

	// Discard spikes from the source; the last valid values stay in place.
	if err = checkPlausibility(snapshot, d.Config.MaxCurrent); err != nil {
		d.recordError(err, true)
		slog.Warn("Implausible Modbus snapshot discarded", "meter", d.Config.Name, "error", err)
		return nil
	}

	// The same snapshot is served on every unit ID; only the SunSpec
	// Modbus address (40068) differs per unit ID.
	for _, id := range d.Config.UnitIds {
		snapshot.Set(fronius.FieldModbusAddr, float64(id))
		if err = writer.WriteSnapshot(id, snapshot); err != nil {
			err = fmt.Errorf("unitId %d: %w", id, err)
			d.recordError(err, false)
			return err
		}
	}

	d.mu.Lock()
	d.lastSuccess = time.Now()
	d.lastValues = snapshotValues(snapshot)
	delete(d.lastValues, string(fronius.FieldModbusAddr)) // differs per unit ID, see the loop above
	d.latency = latency
	d.polls++
	wasOnline := d.online
	d.online = true
	d.offlineSince = time.Time{}
	d.mu.Unlock()

	// The registers hold current values now, so the unit IDs may answer: at start, or after
	// staleTimeout took them off the bus.
	if !wasOnline {
		if err = d.setOnline(writer, true); err != nil {
			return fmt.Errorf("bring unit IDs online: %w", err)
		}
		slog.Info("Valid values from the source, unit IDs answer", "meter", d.Config.Name, "unitIds", d.unitIds())
	}
	slog.Debug("Modbus snapshot updated", "meter", d.Config.Name, "unitIds", d.unitIds())
	return nil
}

func (d *compiledDevice) recordError(err error, discarded bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastError = err.Error()
	d.lastErrorTime = time.Now()
	if discarded {
		d.discarded++
	}
}

func (d *compiledDevice) status(now time.Time) MeterStatus {
	d.mu.Lock()
	defer d.mu.Unlock()

	st := MeterStatus{
		Source:              d.Config.Source.String(),
		Online:              d.online,
		Polls:               d.polls,
		LastError:           d.lastError,
		Discarded:           d.discarded,
		PollIntervalSeconds: d.Config.Poll.Interval.Seconds(),
		StaleTimeoutSeconds: d.Config.Stale().Seconds(),
		MaxCurrent:          d.Config.MaxCurrent,
		Limits:              d.Config.Limits,
		Values:              d.lastValues, // replaced as a whole on every poll, never modified
	}
	if !d.offlineSince.IsZero() {
		t := d.offlineSince
		st.OfflineSince = &t
	}
	st.UnitIds = d.unitIds()
	if !d.lastSuccess.IsZero() {
		last := d.lastSuccess
		age := now.Sub(last).Seconds()
		latency := float64(d.latency.Microseconds()) / 1000
		st.LastSuccess, st.AgeSeconds, st.LatencyMs = &last, &age, &latency
		st.Ready = now.Sub(last) <= staleAfter*d.Config.Poll.Interval
	}
	if !d.lastErrorTime.IsZero() {
		t := d.lastErrorTime
		st.LastErrorTime = &t
	}
	return st
}

func (d *compiledDevice) pollSnapshot() (fronius.Snapshot, error) {
	registers := make(map[uint16]uint16)
	for _, block := range d.Blocks {
		values, err := d.Reader.ReadHoldingRegisters(block.Start, block.Quantity)
		if err != nil {
			return fronius.Snapshot{}, fmt.Errorf("read block start=%d quantity=%d: %w", block.Start, block.Quantity, err)
		}
		if len(values) != int(block.Quantity) {
			return fronius.Snapshot{}, fmt.Errorf("unexpected response size %d for block start=%d quantity=%d", len(values), block.Start, block.Quantity)
		}
		for i, value := range values {
			registers[block.Start+uint16(i)] = value
		}
	}

	snapshot := fronius.NewSnapshot()
	for _, mapping := range d.FixedMappings {
		if err := applyFixedMapping(&snapshot, mapping); err != nil {
			return fronius.Snapshot{}, err
		}
	}

	for _, mapping := range d.RegisterMappings {
		value, err := decodeRegisterValue(registers, mapping)
		if err != nil {
			return fronius.Snapshot{}, fmt.Errorf("field %q: %w", mapping.ConfigName, err)
		}
		snapshot.Set(mapping.Field, value)
	}

	if err := applyExpressionMappings(&snapshot, d.ExprMappings); err != nil {
		return fronius.Snapshot{}, err
	}

	applyDerivedFields(&snapshot)
	return snapshot, nil
}

func applyFixedMapping(snapshot *fronius.Snapshot, mapping compiledMapping) error {
	if mapping.Field == fronius.FieldSerialStr {
		switch value := mapping.Config.Value.(type) {
		case string:
			snapshot.SetString(mapping.Field, value)
		case int, int64, float64, uint64:
			snapshot.SetString(mapping.Field, fmt.Sprint(value))
		case nil:
			snapshot.SetString(mapping.Field, "")
		default:
			return fmt.Errorf("field %q: unsupported fixed string value type %T", mapping.ConfigName, value)
		}
		return nil
	}

	number, err := numericValue(mapping.Config.Value)
	if err != nil {
		return fmt.Errorf("field %q: %w", mapping.ConfigName, err)
	}
	snapshot.Set(mapping.Field, number)
	return nil
}

func applyExpressionMappings(snapshot *fronius.Snapshot, mappings []compiledMapping) error {
	for _, mapping := range mappings {
		vars := make(map[string]float64)
		for field, value := range snapshotValues(*snapshot) {
			vars[field] = value
		}
		result, err := evalExpression(mapping.Config.Expr, vars)
		if err != nil {
			return fmt.Errorf("field %q: %w", mapping.ConfigName, err)
		}
		snapshot.Set(mapping.Field, result)
	}
	return nil
}

// checkPlausibility rejects values a meter rated for maxCurrent per phase cannot
// measure. Limits allow for the upper grid voltage tolerance (253 V) and a short
// overload margin before the breaker trips. maxCurrent 0 disables the check.
func checkPlausibility(snapshot fronius.Snapshot, maxCurrent float64) error {
	if maxCurrent <= 0 {
		return nil
	}

	currentLimit := maxCurrent * plausibilityMargin
	phasePowerLimit := maxGridVoltage * currentLimit
	limits := []struct {
		fields []fronius.CanonicalField
		limit  float64
	}{
		{[]fronius.CanonicalField{fronius.FieldCurrentL1, fronius.FieldCurrentL2, fronius.FieldCurrentL3}, currentLimit},
		{[]fronius.CanonicalField{fronius.FieldPowerL1, fronius.FieldPowerL2, fronius.FieldPowerL3}, phasePowerLimit},
		{[]fronius.CanonicalField{fronius.FieldPowerTotal}, 3 * phasePowerLimit},
	}

	for _, l := range limits {
		for _, field := range l.fields {
			if value := snapshot.Float64(field); math.Abs(value) > l.limit {
				return fmt.Errorf("%s = %g exceeds limit %g", field, value, l.limit)
			}
		}
	}
	return nil
}

func applyDerivedFields(snapshot *fronius.Snapshot) {
	if !snapshot.Has(fronius.FieldDeviceID) {
		snapshot.Set(fronius.FieldDeviceID, defaultDeviceID)
	}
	if !snapshot.Has(fronius.FieldFirmware) {
		snapshot.Set(fronius.FieldFirmware, defaultFirmwareCode)
	}
	if !snapshot.Has(fronius.FieldSerialNumber) {
		snapshot.Set(fronius.FieldSerialNumber, defaultSerialNumber)
	}
	if !snapshot.HasString(fronius.FieldSerialStr) {
		snapshot.SetString(fronius.FieldSerialStr, strconv.FormatInt(int64(math.Round(snapshot.Float64(fronius.FieldSerialNumber))), 10))
	}

	for _, field := range []fronius.CanonicalField{fronius.FieldPFL1, fronius.FieldPFL2, fronius.FieldPFL3} {
		if !snapshot.Has(field) {
			snapshot.Set(field, 1)
		}
	}

	currentTotal := snapshot.Float64(fronius.FieldCurrentL1) + snapshot.Float64(fronius.FieldCurrentL2) + snapshot.Float64(fronius.FieldCurrentL3)
	snapshot.Set(fronius.FieldCurrentTotal, currentTotal)

	v1 := snapshot.Float64(fronius.FieldVoltageL1)
	v2 := snapshot.Float64(fronius.FieldVoltageL2)
	v3 := snapshot.Float64(fronius.FieldVoltageL3)
	snapshot.Set(fronius.FieldVoltageAvgPN, (v1+v2+v3)/3)

	if !snapshot.Has(fronius.FieldVoltageL1L2) {
		snapshot.Set(fronius.FieldVoltageL1L2, v1*math.Sqrt(3))
	}
	if !snapshot.Has(fronius.FieldVoltageL2L3) {
		snapshot.Set(fronius.FieldVoltageL2L3, v2*math.Sqrt(3))
	}
	if !snapshot.Has(fronius.FieldVoltageL3L1) {
		snapshot.Set(fronius.FieldVoltageL3L1, v3*math.Sqrt(3))
	}

	snapshot.Set(
		fronius.FieldVoltageAvgPP,
		(snapshot.Float64(fronius.FieldVoltageL1L2)+snapshot.Float64(fronius.FieldVoltageL2L3)+snapshot.Float64(fronius.FieldVoltageL3L1))/3,
	)

	i1 := snapshot.Float64(fronius.FieldCurrentL1)
	i2 := snapshot.Float64(fronius.FieldCurrentL2)
	i3 := snapshot.Float64(fronius.FieldCurrentL3)
	p1 := snapshot.Float64(fronius.FieldPowerL1)
	p2 := snapshot.Float64(fronius.FieldPowerL2)
	p3 := snapshot.Float64(fronius.FieldPowerL3)

	s1 := math.Abs(v1 * i1)
	s2 := math.Abs(v2 * i2)
	s3 := math.Abs(v3 * i3)
	snapshot.Set(fronius.FieldApparentL1, s1)
	snapshot.Set(fronius.FieldApparentL2, s2)
	snapshot.Set(fronius.FieldApparentL3, s3)
	snapshot.Set(fronius.FieldApparentTotal, s1+s2+s3)

	snapshot.Set(fronius.FieldReactiveL1, signedReactivePower(p1, s1))
	snapshot.Set(fronius.FieldReactiveL2, signedReactivePower(p2, s2))
	snapshot.Set(fronius.FieldReactiveL3, signedReactivePower(p3, s3))
	snapshot.Set(
		fronius.FieldReactiveTotal,
		snapshot.Float64(fronius.FieldReactiveL1)+snapshot.Float64(fronius.FieldReactiveL2)+snapshot.Float64(fronius.FieldReactiveL3),
	)

	if apparentTotal := snapshot.Float64(fronius.FieldApparentTotal); apparentTotal > 0 {
		snapshot.Set(fronius.FieldPFTotal, snapshot.Float64(fronius.FieldPowerTotal)/apparentTotal)
	} else {
		snapshot.Set(fronius.FieldPFTotal, (snapshot.Float64(fronius.FieldPFL1)+snapshot.Float64(fronius.FieldPFL2)+snapshot.Float64(fronius.FieldPFL3))/3)
	}

	distributeEnergy(snapshot, fronius.FieldEnergyExport, fronius.FieldEnergyExpL1, fronius.FieldEnergyExpL2, fronius.FieldEnergyExpL3)
	distributeEnergy(snapshot, fronius.FieldEnergyImport, fronius.FieldEnergyImpL1, fronius.FieldEnergyImpL2, fronius.FieldEnergyImpL3)
}

func distributeEnergy(snapshot *fronius.Snapshot, totalField, l1Field, l2Field, l3Field fronius.CanonicalField) {
	total := snapshot.Float64(totalField)
	if !snapshot.Has(l1Field) {
		snapshot.Set(l1Field, total/3)
	}
	if !snapshot.Has(l2Field) {
		snapshot.Set(l2Field, total/3)
	}
	if !snapshot.Has(l3Field) {
		snapshot.Set(l3Field, total/3)
	}
}

func signedReactivePower(realPower, apparentPower float64) float64 {
	if apparentPower <= 0 {
		return 0
	}
	reactive := math.Sqrt(math.Max(0, apparentPower*apparentPower-realPower*realPower))
	if realPower < 0 {
		return -reactive
	}
	return reactive
}

func snapshotValues(snapshot fronius.Snapshot) map[string]float64 {
	fields := []fronius.CanonicalField{
		fronius.FieldDeviceID, fronius.FieldFirmware, fronius.FieldSerialNumber, fronius.FieldModbusAddr,
		fronius.FieldPowerTotal, fronius.FieldPowerL1, fronius.FieldPowerL2, fronius.FieldPowerL3,
		fronius.FieldVoltageL1, fronius.FieldVoltageL2, fronius.FieldVoltageL3,
		fronius.FieldCurrentL1, fronius.FieldCurrentL2, fronius.FieldCurrentL3,
		fronius.FieldEnergyImport, fronius.FieldEnergyExport, fronius.FieldFrequency,
		fronius.FieldPFL1, fronius.FieldPFL2, fronius.FieldPFL3,
		fronius.FieldCurrentTotal, fronius.FieldVoltageAvgPN, fronius.FieldVoltageAvgPP,
		fronius.FieldVoltageL1L2, fronius.FieldVoltageL2L3, fronius.FieldVoltageL3L1,
		fronius.FieldApparentTotal, fronius.FieldApparentL1, fronius.FieldApparentL2, fronius.FieldApparentL3,
		fronius.FieldReactiveTotal, fronius.FieldReactiveL1, fronius.FieldReactiveL2, fronius.FieldReactiveL3,
		fronius.FieldPFTotal, fronius.FieldEnergyExpL1, fronius.FieldEnergyExpL2, fronius.FieldEnergyExpL3,
		fronius.FieldEnergyImpL1, fronius.FieldEnergyImpL2, fronius.FieldEnergyImpL3,
	}

	values := make(map[string]float64, len(fields))
	for _, field := range fields {
		values[string(field)] = snapshot.Float64(field)
	}

	return values
}

func decodeRegisterValue(registers map[uint16]uint16, mapping compiledMapping) (float64, error) {
	rawWords := make([]uint16, mapping.Registers)
	for i := uint16(0); i < mapping.Registers; i++ {
		value, ok := registers[mapping.Config.Address+i]
		if !ok {
			return 0, fmt.Errorf("missing register %d", mapping.Config.Address+i)
		}
		rawWords[i] = value
	}

	if strings.EqualFold(mapping.Config.WordOrder, "little") {
		slices.Reverse(rawWords)
	}

	bytes := make([]byte, 0, len(rawWords)*2)
	for _, word := range rawWords {
		chunk := []byte{byte(word >> 8), byte(word)}
		if strings.EqualFold(mapping.Config.ByteOrder, "little") {
			chunk[0], chunk[1] = chunk[1], chunk[0]
		}
		bytes = append(bytes, chunk...)
	}

	var raw float64
	switch strings.ToLower(mapping.Config.DType) {
	case "uint16":
		raw = float64(binary.BigEndian.Uint16(bytes))
	case "int16":
		raw = float64(int16(binary.BigEndian.Uint16(bytes)))
	case "uint32":
		raw = float64(binary.BigEndian.Uint32(bytes))
	case "int32":
		raw = float64(int32(binary.BigEndian.Uint32(bytes)))
	case "uint64":
		raw = float64(binary.BigEndian.Uint64(bytes))
	case "int64":
		raw = float64(int64(binary.BigEndian.Uint64(bytes)))
	case "float32":
		raw = float64(math.Float32frombits(binary.BigEndian.Uint32(bytes)))
	default:
		return 0, fmt.Errorf("unsupported dtype %q", mapping.Config.DType)
	}

	return (raw + mapping.Config.Offset) * math.Pow10(mapping.Config.Scale), nil
}

func numericValue(value any) (float64, error) {
	switch v := value.(type) {
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("unsupported numeric value type %T", value)
	}
}

func registerCount(dtype string) (uint16, error) {
	switch strings.ToLower(dtype) {
	case "uint16", "int16":
		return 1, nil
	case "uint32", "int32", "float32":
		return 2, nil
	case "uint64", "int64":
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported dtype %q", dtype)
	}
}

// configurableFields are the canonical fields a meter's map may define; every other
// field is derived from them.
var configurableFields = []fronius.CanonicalField{
	fronius.FieldDeviceID, fronius.FieldFirmware, fronius.FieldSerialNumber, fronius.FieldSerialStr,
	fronius.FieldPowerTotal, fronius.FieldPowerL1, fronius.FieldPowerL2, fronius.FieldPowerL3,
	fronius.FieldVoltageL1, fronius.FieldVoltageL2, fronius.FieldVoltageL3,
	fronius.FieldVoltageL1L2, fronius.FieldVoltageL2L3, fronius.FieldVoltageL3L1,
	fronius.FieldCurrentL1, fronius.FieldCurrentL2, fronius.FieldCurrentL3,
	fronius.FieldEnergyImport, fronius.FieldEnergyExport, fronius.FieldFrequency,
	fronius.FieldPFL1, fronius.FieldPFL2, fronius.FieldPFL3,
}

func normalizeCanonicalField(name string) (fronius.CanonicalField, error) {
	field := fronius.CanonicalField(name)
	if !slices.Contains(configurableFields, field) {
		return "", fmt.Errorf("unsupported canonical field %q", name)
	}
	return field, nil
}

func parseModbusParity(value string) (uint, error) {
	switch strings.ToUpper(value) {
	case "N":
		return mb.PARITY_NONE, nil
	case "E":
		return mb.PARITY_EVEN, nil
	case "O":
		return mb.PARITY_ODD, nil
	default:
		return 0, fmt.Errorf("unsupported parity %q", value)
	}
}

// simonRegisterReader reads the upstream meter. After a failed read it closes the connection and
// opens it again before the next read: a TCP connection the source dropped ("broken pipe") or a
// late answer after a timeout would otherwise spoil every following read, and the meter would
// stay stale until a restart.
type simonRegisterReader struct {
	client *mb.ModbusClient
	reopen bool        // the last read failed: reconnect before the next one; used by the poll goroutine only
	closed atomic.Bool // Close was called: never reconnect
}

func newTCPRegisterReader(source SourceConfig) (*simonRegisterReader, error) {
	client, err := mb.NewClient(&mb.ClientConfiguration{
		URL:     "tcp://" + net.JoinHostPort(source.TCP.Host, strconv.Itoa(source.TCP.Port)),
		Timeout: source.Timeout,
	})
	if err != nil {
		return nil, err
	}
	if err = client.SetUnitId(source.UnitId); err != nil {
		return nil, err
	}
	if err = client.Open(); err != nil {
		return nil, err
	}
	return &simonRegisterReader{client: client}, nil
}

func (r *simonRegisterReader) ReadHoldingRegisters(address, quantity uint16) ([]uint16, error) {
	if r.reopen {
		if r.closed.Load() {
			return nil, errors.New("source connection closed")
		}
		_ = r.client.Close()
		if err := r.client.Open(); err != nil {
			return nil, fmt.Errorf("reconnect: %w", err)
		}
		r.reopen = false
		if r.closed.Load() { // Close came while reconnecting: do not leave the connection open
			_ = r.client.Close()
			return nil, errors.New("source connection closed")
		}
	}

	values, err := r.client.ReadRegisters(address, quantity, mb.HOLDING_REGISTER)
	if err != nil {
		r.reopen = true
	}
	return values, err
}

// Close closes the connection for good; a read in progress ends with the closed connection.
func (r *simonRegisterReader) Close() error {
	r.closed.Store(true)
	return r.client.Close()
}

func newRTURegisterReader(source SourceConfig) (*simonRegisterReader, error) {
	parity, err := parseModbusParity(source.RTU.Parity)
	if err != nil {
		return nil, err
	}
	client, err := mb.NewClient(&mb.ClientConfiguration{
		URL:      "rtu://" + source.RTU.Port,
		Speed:    uint(source.RTU.BaudRate),
		DataBits: uint(source.RTU.DataBits),
		Parity:   parity,
		StopBits: uint(source.RTU.StopBits),
		Timeout:  source.Timeout,
	})
	if err != nil {
		return nil, err
	}
	if err = client.SetUnitId(source.UnitId); err != nil {
		return nil, err
	}
	if err = client.Open(); err != nil {
		return nil, err
	}
	return &simonRegisterReader{client: client}, nil
}

func newRegisterReader(source SourceConfig) (registerReader, error) {
	switch strings.ToLower(source.Type) {
	case "tcp":
		return newTCPRegisterReader(source)
	case "rtu":
		return newRTURegisterReader(source)
	default:
		return nil, fmt.Errorf("unsupported source type %q", source.Type)
	}
}
