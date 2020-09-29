package mbgw

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"
)

// ClientData stores receive data form modbus gateway
type ClientData struct {
	Timestamp time.Time
	Runtime   time.Duration
	Register  map[uint16]uint16
}

// Client structure contains all Properties of a connection
type Client struct {
	connectionString string
	ticker           time.Duration
	timeout          time.Duration
	// stop receiving data
	Stop chan bool
	// update get data immediately
	Update chan struct{ Register, Quantity uint16 }
	// Data contains the received data
	Data chan ClientData
}

// NewClient creates a new Client handler
func NewClient() (c *Client) {
	c = &Client{
		Stop:   make(chan bool, 1),
		Update: make(chan struct{ Register, Quantity uint16 }, 1),
		Data:   make(chan ClientData, 1),
	}
	return
}

//Listen starts the go function to receive data
func (c *Client) Listen(connection string, polling, timeout time.Duration) (err error) {
	c.connectionString = connection
	c.ticker = polling
	c.timeout = timeout

	go c.receiver()

	// TODO registers should be a parameter in the config file
	for _, v := range []struct{ Register, Quantity uint16 }{
		{Register: uint16(18), Quantity: uint16(1)},
		{Register: uint16(768), Quantity: uint16(1)},
		{Register: uint16(4176), Quantity: uint16(2)},
		{Register: uint16(4096), Quantity: uint16(59)},
	} {
		go func(x struct{ Register, Quantity uint16 }) {
			c.Update <- x
		}(v)
	}
	return
}

// receiver is the Modbus Gateway data receiver
func (c *Client) receiver() {
	ticker := time.NewTicker(c.ticker)
	defer ticker.Stop()

	for {
		var request struct{ Register, Quantity uint16 }

		select {
		case <-c.Stop:
			infolog.Println("modbus gateway go function is stopped...")
			return
		case <-ticker.C:
			debuglog.Println("get a ticker request")
			request.Register = 4096
			request.Quantity = 59
		case request = <-c.Update:
			debuglog.Printf("get an update request (register %v, quantity %v)\n", request.Register, request.Quantity)
		}
		startTime := time.Now()

		var err error
		register := make(map[uint16]uint16)

		// A function ensures that all channels and timers are ended after one run
		// a defer is only called at the end of a function and not after the end of a loop!
		func() {
			done := make(chan bool, 1)
			defer close(done)

			// fills register map with received values or set variable err with error information
			go func() {
				// ensures that data is sent to the channel when the function is terminated
				defer func() {
					// recover from panic caused by writing to a closed channel
					defer func() {
						if r := recover(); r != nil {
							errorlog.Printf("error write to closed channel: %v", r)
							return
						}
					}()
					done <- true
				}()

				// http://raspberryz:8080/readholdingregisters?Address=4096&Quantity=64
				connectionString := fmt.Sprintf("%v/readholdingregisters?Address=%v&Quantity=%v", c.connectionString, request.Register, request.Quantity)
				debuglog.Printf("performing http get: %v\n", connectionString)
				var resp *http.Response
				if resp, err = http.Get(connectionString); err != nil {
					return
				}

				bodyBytes, _ := ioutil.ReadAll(resp.Body)
				_ = resp.Body.Close()

				// Convert response body to result struct
				type body struct {
					Time       time.Time
					Duration   int
					Connection string
					Data       struct {
						Address, Quantity uint16
						Data              string
					}
				}

				var bodyStruct body
				if err = json.Unmarshal(bodyBytes, &bodyStruct); err != nil {
					return
				}
				tracelog.Printf("api response: %+v\n", bodyStruct)

				for i := 0; i < int(bodyStruct.Data.Quantity); i++ {
					var value uint64
					if value, err = strconv.ParseUint(bodyStruct.Data.Data[i*4:i*4+4], 16, 16); err != nil {
						return
					}
					register[bodyStruct.Data.Address+uint16(i)] = uint16(value)
				}
			}()

			// wait for API Data
			select {
			case <-done:
			case <-time.After(c.timeout):
				err = errors.New("timeout during receive data")
			}
		}()

		if err != nil {
			errorlog.Printf("error to receive client data: %v\n", err)
		}

		d := ClientData{
			Timestamp: time.Now(),
			Runtime:   time.Now().Sub(startTime),
			Register:  register,
		}
		tracelog.Printf("send data to client channel: %+v\n", d)
		c.Data <- d
	}
}

func (c *Client) Close() (err error) {
	infolog.Println("stop modbus gateway go function")
	c.Stop <- true

	close(c.Stop)
	close(c.Update)
	close(c.Data)
	return
}
