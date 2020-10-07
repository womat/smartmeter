package mbclient

import (
	"errors"
	"time"

	mbslave "github.com/goburrow/modbus"

	"SmartmeterEmu/pkg/tools"
)

// ClientData stores receive data form modbus client
type ClientData struct {
	Timestamp time.Time
	Runtime   time.Duration
	Data      []byte
}

// Client structure contains all Properties of a connection
type Client struct {
	connectionString string
	ticker           time.Duration
	timeout          time.Duration
	deviceId         uint8
	maxRetries       int
	// stop receiving data
	Stop chan bool
	// update get data immediately
	Update chan bool
	// Data contains the received data
	Data chan ClientData
}

func NewClient() (c *Client) {
	c = &Client{
		Stop:   make(chan bool, 1),
		Update: make(chan bool, 1),
		Data:   make(chan ClientData, 1),
	}
	return
}

// Listen starts the go function to receive data
func (c *Client) Listen(connectionstring string, polling time.Duration) (err error) {
	c.connectionString, c.deviceId, c.timeout, c.maxRetries = tools.GetConnectionDeviceIdTimeOut(connectionstring)
	c.ticker = polling

	go c.receiver()
	c.Update <- true
	return
}

// receiver is the Modbus Client data receiver
func (c *Client) receiver() {
	var retryCounter int
	//retry := make(chan bool)
	ticker := time.NewTicker(c.ticker)
	defer ticker.Stop()
	retry := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	retry.Stop()

	for {
		select {
		case <-c.Stop:
			infolog.Println("modbus client go function is stopped...")
			close(c.Stop)
			close(c.Update)
			close(c.Data)
			return
		case <-ticker.C:
			debuglog.Println("get a ticker request")
			retryCounter = 0
		case <-c.Update:
			debuglog.Println("get an update request")
			retryCounter = 0
		case <-retry.C:
		}

		retry.Stop()
		start := time.Now()

		data, err := c.get(41000-1, 39)

		if err != nil {
			errorlog.Printf("error to receive client data: %v\n", err)
			if retryCounter < c.maxRetries {
				debuglog.Println("send a retry request")
				retryCounter++
				retry.Reset(10 * time.Millisecond)
			}
			continue
		}

		d := ClientData{
			Timestamp: time.Now(),
			Runtime:   time.Now().Sub(start),
			Data:      data,
		}
		tracelog.Printf("send data to client channel: %+v\n", d)
		c.Data <- d
	}
}

func (c *Client) get(address, quantity uint16) (data []byte, err error) {
	done := make(chan bool, 1)

	//  fills register map with received values or set variable err with error information
	go func() {
		// ensures that data is sent to the channel when the function is terminated
		defer func() {
			select {
			case done <- true:
			default:
			}
			close(done)
		}()

		clientHandler := mbslave.NewTCPClientHandler(c.connectionString)
		clientHandler.SlaveId = c.deviceId

		if err = clientHandler.Connect(); err != nil {
			return
		}
		defer clientHandler.Close()

		client := mbslave.NewClient(clientHandler)
		// TODO registers should be a parameter in the config file
		data, err = client.ReadHoldingRegisters(address, quantity)
	}()

	// wait for Modbus Data
	select {
	case <-done:
	case <-time.After(c.timeout):
		err = errors.New("timeout during receive data")
	}

	return
}

func (c *Client) Close() (err error) {
	infolog.Println("stop modbus client go function")
	c.Stop <- true
	return
}
