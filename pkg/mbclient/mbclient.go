package mbclient

import (
	"errors"
	"time"

	mbslave "github.com/goburrow/modbus"
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
func (c *Client) Listen(ipaddress string, polling, timeout time.Duration) (err error) {
	c.connectionString = ipaddress
	c.ticker = polling
	c.timeout = timeout

	go c.receiver()
	c.Update <- true
	return
}

// receiver is the Modbus Client data receiver
func (c *Client) receiver() {
	retryTime := c.ticker / 10
	if retryTime < time.Second {
		retryTime = time.Second
	}

	// initialize timer for repetition in case of error
	retry := time.NewTimer(retryTime)
	defer retry.Stop()
	retry.Stop() // initialize timer for repetition in case of error

	ticker := time.NewTicker(c.ticker)
	defer ticker.Stop()

	for {
		select {
		case <-c.Stop:
			infolog.Println("modbus client go function is stopped...")
			return
		case <-retry.C:
			debuglog.Println("get a retry request")
		case <-ticker.C:
			debuglog.Println("get a ticker request")
		case <-c.Update:
			debuglog.Println("get an update request")
		}

		retry.Stop()
		start := time.Now()

		var data []byte
		var err error

		// A function ensures that all channels and timers are ended after one run
		// a defer is only called at the end of a function and not after the end of a loop!
		func() {
			finish := make(chan bool, 1)
			defer close(finish)

			timerOutTimer := time.NewTimer(c.timeout)
			defer timerOutTimer.Stop()

			// fills register map with received values or set variable err with error information
			go func() {
				defer func() {
					// ensures that data is sent to the channel when the function is terminated
					defer func() {
						// recover from panic caused by writing to a closed channel
						if r := recover(); r != nil {
							errorlog.Printf("error write to closed channel: %v", r)
							return
						}
					}()
					finish <- true
				}()

				clientHandler := mbslave.NewTCPClientHandler(c.connectionString)
				if err = clientHandler.Connect(); err != nil {
					return
				}
				defer clientHandler.Close()

				client := mbslave.NewClient(clientHandler)
				data, err = client.ReadHoldingRegisters(41000-1, 39)
			}()

			// wait for Modbus Data
			select {
			case <-finish:
			case <-timerOutTimer.C:
				err = errors.New("timeout during receive data")
			}
		}()

		if err != nil {
			errorlog.Printf("error to receive client data: %v\n", err)
			tracelog.Println("start retry timer")
			retry.Reset(retryTime)
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

func (c *Client) Close() (err error) {
	infolog.Println("stop modbus client go function")
	c.Stop <- true

	close(c.Stop)
	close(c.Update)
	close(c.Data)
	return
}
