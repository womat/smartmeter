package mbgw

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func isEqual(a interface{}, b interface{}) bool {
	expect, _ := json.Marshal(a)
	got, _ := json.Marshal(b)
	if string(expect) != string(got) {
		return false
	}
	return true
}

type testsequenz = struct {
	address  uint16
	quantity int
	register []uint16
	reg      map[uint16]uint16
}

func TestGet(t *testing.T) {
	const connection = "HTTP http://raspberryz:8080 Timeout:500 MaxRetries:1"
	testSequenz := []testsequenz{{address: 18, quantity: 1, register: []uint16{285}},
		{address: 768, quantity: 1, register: []uint16{117}},
		{address: 4176, quantity: 2, register: []uint16{276, 52082}},
		{address: 4096, quantity: 59, register: []uint16{}}}

	for i, test := range testSequenz {
		test.reg = make(map[uint16]uint16)

		for i, v := range test.register {
			test.reg[test.address+uint16(i)] = v
		}
		testSequenz[i].reg = test.reg
	}

	c := NewClient()
	if err := c.Listen(connection, 60*time.Second); err != nil {
		t.Error(err)
		t.FailNow()
	}

	for _, test := range testSequenz {
		connectionString := fmt.Sprintf("%v/readholdingregisters?Address=%v&Quantity=%v", c.connectionString, test.address, test.quantity)
		register, err := c.get(connectionString)
		if err != nil {
			t.Error(err)
		}

		if l := len(register); l != test.quantity {
			t.Errorf("expected %v, got %v\n", test.quantity, l)
		}

		if !isEqual(register, test.reg) && len(register) <= 2 {
			t.Errorf("expected %v, got %v\n", register, test.reg)
		}
	}

}
