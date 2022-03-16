package ardop

import (
	"reflect"
	"testing"
)

func TestParseBandwidth(t *testing.T) {
	tests := []struct {
		in      string
		want    Bandwidth
		wantErr bool
	}{
		{
			in:      "200",
			want:    Bandwidth200Max,
			wantErr: false,
		}, {
			in:      "500",
			want:    Bandwidth500Max,
			wantErr: false,
		}, {
			in:      "1000",
			want:    Bandwidth1000Max,
			wantErr: false,
		}, {
			in:      "2000",
			want:    Bandwidth2000Max,
			wantErr: false,
		}, {
			in:      "200MAX",
			want:    Bandwidth200Max,
			wantErr: false,
		}, {
			in:      "500MAX",
			want:    Bandwidth500Max,
			wantErr: false,
		}, {
			in:      "1000MAX",
			want:    Bandwidth1000Max,
			wantErr: false,
		}, {
			in:      "2000MAX",
			want:    Bandwidth2000Max,
			wantErr: false,
		}, {
			in:      "200FORCED",
			want:    Bandwidth200Forced,
			wantErr: false,
		}, {
			in:      "500FORCED",
			want:    Bandwidth500Forced,
			wantErr: false,
		}, {
			in:      "1000FORCED",
			want:    Bandwidth1000Forced,
			wantErr: false,
		}, {
			in:      "2000FORCED",
			want:    Bandwidth2000Forced,
			wantErr: false,
		}, {
			in:      "2000INVALID",
			want:    Bandwidth{},
			wantErr: true,
		}, {
			in:      "5000",
			want:    Bandwidth{},
			wantErr: true,
		}, {
			in:      "FORCED",
			want:    Bandwidth{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := StrToBandwidth(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("StrToBandwidth() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StrToBandwidth() got = %v, want %v", got, tt.want)
			}
		})
	}
}
