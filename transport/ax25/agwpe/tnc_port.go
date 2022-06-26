package agwpe

// TNCPort representw a TNC connection with a single registered port.
type TNCPort struct {
	TNC
	Port
}

// Close closes both the port and TNC.
func (tp TNCPort) Close() error { tp.Port.Close(); return tp.TNC.Close() }

// OpenPortTCP opens a connection to the TNC and registers a single port.
//
// The returned TNCPort is a reference to the combined Port and TNC.
func OpenPortTCP(addr string, port int, callsign string) (*TNCPort, error) {
	t, err := OpenTCP(addr)
	if err != nil {
		return nil, err
	}
	p, err := t.RegisterPort(port, callsign)
	if err != nil {
		t.Close()
		return nil, err
	}
	return &TNCPort{*t, *p}, nil
}
