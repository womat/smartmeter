package debug

import (
	"bytes"
	"encoding/json"
	"io"
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

type testString = struct {
	pattern string
	expect  []byte
	got     []byte
}

type dataSourceWrite struct {
	count    int
	testCase []testString
}

func (ds *dataSourceWrite) Write(data []byte) (int, error) {
	if len(ds.testCase) < ds.count {
		return 0, io.ErrClosedPipe
	}

	ds.testCase[ds.count].got = make([]byte, 255)
	n := copy(ds.testCase[ds.count].got, data[:len(data)])
	ds.testCase[ds.count].got = ds.testCase[ds.count].got[:n]
	ds.count++
	return n, nil
}

func (ds *dataSourceWrite) Close() error {
	return nil
}

func (ds *dataSourceWrite) Reset() error {
	ds.count = 0
	ds.testCase = []testString{}
	return nil
}

func (ds *dataSourceWrite) AddTestCase(p, e string) {
	ds.testCase = append(ds.testCase, testString{pattern: p, expect: []byte(e)})
}

func TestError(t *testing.T) {
	writer := &dataSourceWrite{}
	writer.AddTestCase("Hello world!", "Hello world!")
	writer.AddTestCase("Hello wolfgang!", "Hello wolfgang!")
	SetDebug(writer, Error)

	for _, tc := range writer.testCase {
		Errorlog.Print(tc.pattern)
	}

	for c, tc := range writer.testCase {
		if !bytes.Contains(tc.got, tc.expect) {
			t.Errorf("test case %v: expected %v, got %v", c, string(tc.expect), string(tc.got))
		}
	}
}

func TestErrorDisabled(t *testing.T) {
	writer := &dataSourceWrite{}
	writer.AddTestCase("Hello world!", "Hello world!")
	SetDebug(writer, Info)

	for _, tc := range writer.testCase {
		Errorlog.Print(tc.pattern)
	}

	for c, tc := range writer.testCase {
		if string(tc.got) != "" {
			t.Errorf("test case %v: expected %v, got %v", c, string(tc.expect), string(tc.got))
		}
	}
}

func TestStandard(t *testing.T) {
	writer := &dataSourceWrite{}
	writer.AddTestCase("Hello world!", "Hello world!")
	SetDebug(writer, Standard)
	Infolog.Print(writer.testCase[0].pattern)
	if !bytes.Contains(writer.testCase[0].got, writer.testCase[0].expect) {
		t.Errorf("test case infolog: expected %v, got %v", string(writer.testCase[0].expect), string(writer.testCase[0].got))
	}

	writer.Reset()
	writer.AddTestCase("Hello world!", "Hello world!")
	Warninglog.Print(writer.testCase[0].pattern)
	if !bytes.Contains(writer.testCase[0].got, writer.testCase[0].expect) {
		t.Errorf("test case warninglog: expected %v, got %v", string(writer.testCase[0].expect), string(writer.testCase[0].got))
	}

	writer.Reset()
	writer.AddTestCase("Hello world!", "Hello world!")
	Errorlog.Print(writer.testCase[0].pattern)
	if !bytes.Contains(writer.testCase[0].got, writer.testCase[0].expect) {
		t.Errorf("test case errorlog: expected %v, got %v", string(writer.testCase[0].expect), string(writer.testCase[0].got))
	}

	writer.Reset()
	writer.AddTestCase("Hello world!", "Hello world!")
	Debuglog.Print(writer.testCase[0].pattern)
	if string(writer.testCase[0].got) != "" {
		t.Errorf("test case debuglog: expected %v, got %v", "", string(writer.testCase[0].got))
	}

	writer.Reset()
	writer.AddTestCase("Hello world!", "Hello world!")
	Tracelog.Print(writer.testCase[0].pattern)
	if string(writer.testCase[0].got) != "" {
		t.Errorf("test case tracelog: expected %v, got %v", "", string(writer.testCase[0].got))
	}
}

func TestFull(t *testing.T) {
	writer := &dataSourceWrite{}
	writer.AddTestCase("Hello world!", "Hello world!")
	SetDebug(writer, Full)
	Infolog.Print(writer.testCase[0].pattern)
	if !bytes.Contains(writer.testCase[0].got, writer.testCase[0].expect) {
		t.Errorf("test case infolog: expected %v, got %v", string(writer.testCase[0].expect), string(writer.testCase[0].got))
	}

	writer.Reset()
	writer.AddTestCase("Hello world!", "Hello world!")
	Warninglog.Print(writer.testCase[0].pattern)
	if !bytes.Contains(writer.testCase[0].got, writer.testCase[0].expect) {
		t.Errorf("test case warninglog: expected %v, got %v", string(writer.testCase[0].expect), string(writer.testCase[0].got))
	}

	writer.Reset()
	writer.AddTestCase("Hello world!", "Hello world!")
	Errorlog.Print(writer.testCase[0].pattern)
	if !bytes.Contains(writer.testCase[0].got, writer.testCase[0].expect) {
		t.Errorf("test case errorlog: expected %v, got %v", string(writer.testCase[0].expect), string(writer.testCase[0].got))
	}

	writer.Reset()
	writer.AddTestCase("Hello world!", "Hello world!")
	Debuglog.Print(writer.testCase[0].pattern)
	if !bytes.Contains(writer.testCase[0].got, writer.testCase[0].expect) {
		t.Errorf("test case debuglog: expected %v, got %v", string(writer.testCase[0].expect), string(writer.testCase[0].got))
	}

	writer.Reset()
	writer.AddTestCase("Hello world!", "Hello world!")
	Tracelog.Print(writer.testCase[0].pattern)
	if !bytes.Contains(writer.testCase[0].got, writer.testCase[0].expect) {
		t.Errorf("test case tracelog: expected %v, got %v", string(writer.testCase[0].expect), string(writer.testCase[0].got))
	}
}
