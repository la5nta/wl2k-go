// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ardop

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := map[string]ctrlMsg{
		"NEWSTATE DISC":                     {cmdNewState, Disconnected},
		"PTT True":                          {cmdPTT, true},
		"PTT False":                         {cmdPTT, false},
		"PTT trUE":                          {cmdPTT, true},
		"CODEC True":                        {cmdCodec, true},
		"foobar baz":                        {command("FOOBAR"), nil},
		"DISCONNECTED":                      {cmdDisconnected, nil},
		"FAULT 5/Error in the application.": {cmdFault, "5/Error in the application."},
		"BUFFER 300":                        {cmdBuffer, 300},
		"MYCALL LA5NTA":                     {cmdMyCall, "LA5NTA"},
		"GRIDSQUARE JP20QH":                 {cmdGridSquare, "JP20QH"},
		"MYAUX LA5NTA,LE3OF":                {cmdMyAux, []string{"LA5NTA", "LE3OF"}},
		"MYAUX LA5NTA, LE3OF":               {cmdMyAux, []string{"LA5NTA", "LE3OF"}},
		"VERSION 1.4.7.0":                   {cmdVersion, "1.4.7.0"},
		"FREQUENCY 14096400":                {cmdFrequency, 14096400},
		"ARQBW 200MAX":                      {cmdARQBW, "200MAX"},
	}
	for input, expected := range tests {
		got := parseCtrlMsg(input)
		if got.cmd != expected.cmd {
			t.Errorf("Got %#v expected %#v when parsing '%s'", got.cmd, expected.cmd, input)
		}
		if !reflect.DeepEqual(got.value, expected.value) {
			t.Errorf("Got %#v expected %#v when parsing '%s'", got.value, expected.value, input)
		}
	}
}
