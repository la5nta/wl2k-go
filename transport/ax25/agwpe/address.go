package agwpe

import "strings"

type addr struct {
	dest  string
	digis []string
}

func (a addr) Network() string { return "AX.25" }

func (a addr) String() string {
	if len(a.digis) == 0 {
		return a.dest
	}
	return a.dest + " via " + strings.Join(a.digis, " ")
}
