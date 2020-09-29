package mbgw

import (
	"encoding/json"
	"errors"
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
	init             bool
	// stop receiving data
	Stop chan bool
	// update get data immediately
	Update chan bool
	// Data contains the received data
	Data chan ClientData
}

// NewClient creates a new Client handler
func NewClient() (c *Client) {
	c = &Client{
		Stop:   make(chan bool, 1),
		Update: make(chan bool, 1),
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
	c.Update <- true
	return
}

// receiver is the Modbus Gateway data receiver
func (c *Client) receiver() {
	retryTime := c.ticker / 10
	if retryTime < time.Second {
		retryTime = time.Second
	}

	// initialize timer for repetition in case of error
	retry := time.NewTimer(retryTime)
	defer retry.Stop()
	retry.Stop() // is only activated in the event of an error!

	ticker := time.NewTicker(c.ticker)
	defer ticker.Stop()

	for {
		select {
		case <-c.Stop:
			infolog.Println("modbus gateway go function is stopped...")
			return
		case <-retry.C:
			debuglog.Println("get a retry request")
		case <-ticker.C:
			debuglog.Println("get a ticker request")
		case <-c.Update:
			debuglog.Println("get an update request")
		}
		retry.Stop()
		startTime := time.Now()

		var err error
		register := make(map[uint16]uint16)

		// A function ensures that all channels and timers are ended after one run
		// a defer is only called at the end of a function and not after the end of a loop!
		func() {
			done := make(chan bool, 1)
			defer close(done)

			timerOutTimer := time.NewTimer(c.timeout)
			defer timerOutTimer.Stop()

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
				request := []string{
					c.connectionString + "/readholdingregisters?Address=18&Quantity=1",
					c.connectionString + "/readholdingregisters?Address=768&Quantity=1",
					c.connectionString + "/readholdingregisters?Address=4176&Quantity=2",
					c.connectionString + "/readholdingregisters?Address=4096&Quantity=59"}
				for i, connectionString := range request {
					if c.init && i < 3 {
						continue
					}
					debuglog.Printf("performing http get: %v\n", connectionString)
					resp, err := http.Get(connectionString)
					if err != nil {
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
						value, err := strconv.ParseUint(bodyStruct.Data.Data[i*4:i*4+4], 16, 16)
						if err != nil {
							return
						}
						register[bodyStruct.Data.Address+uint16(i)] = uint16(value)
					}
				}
			}()

			// wait for API Data
			select {
			case <-done:
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
			Runtime:   time.Now().Sub(startTime),
			Register:  register,
		}
		tracelog.Printf("send data to client channel: %+v\n", d)
		c.Data <- d
		c.init = true
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
