package ardop

import "testing"

func TestParseIDFrame(t *testing.T) {
	type test struct {
		dFrame
		call string
		grid string
	}
	tests := []test{
		{ // Format from early versions of ARDOP_Win
			dFrame{dataType: `IDF`, data: []byte(` ID LA5NTA:[JP20QE] `)},
			"LA5NTA", "JP20QE",
		},
		{ // Format from ardopc
			dFrame{dataType: `IDF`, data: []byte(` LA5NTA:[JP20QE] `)},
			"LA5NTA", "JP20QE",
		},
		{ // Format from HB9AK (BPQ32 and ARDOP_Win 1.0?)
			dFrame{dataType: `IDF`, data: []byte(`ID:HB9AK [JN36pv]:`)},
			"HB9AK", "JN36pv",
		},
		{ // Not actually seen
			dFrame{dataType: `IDF`, data: []byte(` LA1B:::[JP20QE] `)},
			"LA1B", "JP20QE",
		},
		{ // Not actually seen
			dFrame{dataType: `IDF`, data: []byte(`ABC1DEF[JP20QE]`)},
			"ABC1DEF", "JP20QE",
		},
	}

	for i, test := range tests {
		call, loc, err := parseIDFrame(test.dFrame)
		if err != nil {
			t.Errorf("%d, Unexpected parse error: %s", i, err)
		}
		if call != test.call {
			t.Errorf("Unexpected call: %s", call)
		}
		if loc != test.grid {
			t.Errorf("Unexpected locator: %s", loc)
		}
	}
}
