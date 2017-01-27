package ardop

import "testing"

func TestParseIDFrame(t *testing.T) {
	frame := dFrame{
		dataType: `IDF`,
		data:     []byte(` ID LA5NTA:[JP20QE] `),
	}

	call, loc, err := parseIDFrame(frame)
	if err != nil {
		t.Fatalf("Unexpected parse error: %s", err)
	}
	if call != "LA5NTA" {
		t.Errorf("Unexpected call: %s", call)
	}
	if loc != "JP20QE" {
		t.Errorf("Unexpected locator: %s", loc)
	}
}
