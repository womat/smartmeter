package config

import (
	"encoding/json"
	"testing"
)

func isEqual(a interface{}, b interface{}) bool {
	expect, _ := json.Marshal(a)
	got, _ := json.Marshal(b)
	if string(expect) != string(got) {
		return false
	}
	return true
}

func TestInit(t *testing.T) {

}
