package mbclient

import (
	"encoding/json"
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

func TestGet(t *testing.T) {
	const connection = "TCP 192.0.2.10:502 DeviceId:1 Timeout:100 MaxRetries:1"
	const q = 39
	c := NewClient()
	if err := c.Listen(connection, 60*time.Second); err != nil {
		t.Error(err)
		t.FailNow()
	}

	data, err := c.get(41000-1, q)

	if err != nil {
		t.Error(err)
	}

	if l := len(data); l != q*2 {
		t.Errorf("expected %v, got %v\n", q*2, l)
	}
}
