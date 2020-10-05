package tools

import "encoding/json"

func IsEqual(a interface{}, b interface{}) bool {
	expect, _ := json.Marshal(a)
	got, _ := json.Marshal(b)
	if string(expect) != string(got) {
		return false
	}
	return true
}
