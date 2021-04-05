package tools

import "encoding/json"

func IsEqual(a interface{}, b interface{}) bool {
	expect, _ := json.Marshal(a)
	got, _ := json.Marshal(b)

	return string(expect) == string(got)
}
