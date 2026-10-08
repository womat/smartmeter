// Package health provides basic system and application health information.
//
// It collects metrics such as memory usage, goroutine count, uptime, host info,
// Go runtime version, and application version. The module is intended to be
// lightweight and easily serializable to JSON for monitoring or diagnostic purposes.
package health

import (
	"os"
	"runtime"
	"time"

	"github.com/womat/smartmeter/app/service/meters"
)

// Model holds the main system and runtime health information.
type Model struct {
	App            string  `json:"app"`            // Application name (MODULE)
	AppVersion     string  `json:"appVersion"`     // Current version of the application
	GoVersion      string  `json:"goVersion"`      // Go runtime version
	Hostname       string  `json:"hostname"`       // Machine name where the app runs
	OS             string  `json:"os"`             // Operating system name
	UptimeSeconds  float64 `json:"uptimeSeconds"`  // Application uptime in seconds
	NumGoroutines  int     `json:"numGoroutines"`  // Current number of active goroutines
	HeapAllocBytes uint64  `json:"heapAllocBytes"` // Allocated heap memory in bytes
	SysMemoryBytes uint64  `json:"sysMemoryBytes"` // Total memory obtained from the OS
	Timestamp      string  `json:"timestamp"`      // UTC timestamp when health info was collected (RFC3339)

	// Meters holds the diagnostic state per meter name: unit IDs, the last valid snapshot,
	// the last error and the snapshots discarded as implausible. Filled in by the caller.
	Meters map[string]meters.MeterStatus `json:"meters"`

	// RTU is the state of the serial port towards the inverter, empty without RTU listener.
	// Filled in by the caller.
	RTU []meters.SerialStatus `json:"rtu,omitempty"`

	// Modbus describes the listeners towards the inverter and the TCP clients and counts the
	// requests they answered. Filled in by the caller.
	Modbus meters.Activity `json:"modbus"`
}

var startTime = time.Now() // Tracks application start time

// GetCurrentHealth returns the current system and application health data.
func GetCurrentHealth(module, version string) Model {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return Model{
		App:            module,
		AppVersion:     version,
		GoVersion:      runtime.Version(),
		Hostname:       host,
		OS:             runtime.GOOS,
		UptimeSeconds:  time.Since(startTime).Seconds(),
		NumGoroutines:  runtime.NumGoroutine(),
		HeapAllocBytes: mem.Alloc,
		SysMemoryBytes: mem.Sys,
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}
}
