package agwpe

import (
	"bytes"
)

type kind byte

const (
	kindLogin                    kind = 'P'
	kindRegister                 kind = 'X'
	kindUnregister               kind = 'x'
	kindVersionNumber            kind = 'R'
	kindOutstandingFramesForPort kind = 'y' // Direwolf >= 1.2
	kindPortCapabilities         kind = 'g'

	kindConnect                  kind = 'C'
	kindConnectVia               kind = 'v'
	kindDisconnect               kind = 'd'
	kindConnectedData            kind = 'D'
	kindOutstandingFramesForConn kind = 'Y' // Direwolf >= 1.4
	kindUnprotoInformation       kind = 'M'
)

func versionNumberFrame() frame {
	return frame{header: header{DataKind: kindVersionNumber}}
}

func portCapabilitiesFrame(port uint8) frame {
	return frame{
		header: header{
			Port:     port,
			DataKind: kindPortCapabilities,
		},
	}
}

func connectedDataFrame(port uint8, from, to string, data []byte) frame {
	return frame{
		header: header{
			Port:     port,
			DataKind: kindConnectedData,
			PID:      0xf0,
			From:     callsignFromString(from),
			To:       callsignFromString(to),
			DataLen:  uint32(len(data)),
		},
		Data: data,
	}
}

func outstandingFramesForConnFrame(port uint8, from, to string) frame {
	return frame{
		header: header{
			Port:     port,
			DataKind: kindOutstandingFramesForConn,
			From:     callsignFromString(from),
			To:       callsignFromString(to),
		},
	}
}

func outstandingFramesForPortFrame(port uint8) frame {
	return frame{
		header: header{
			Port:     port,
			DataKind: kindOutstandingFramesForPort,
		},
	}
}

func loginFrame(username, password string) frame {
	data := make([]byte, 255+255)
	copy(data[:255], username)
	copy(data[255:], password)
	return frame{
		header: header{DataKind: kindLogin},
		Data:   data,
	}
}

func registerCallsignFrame(callsign string, port uint8) frame {
	h := header{DataKind: kindRegister, Port: port}
	copy(h.From[:], callsign)
	return frame{header: h}
}

func unregisterCallsignFrame(callsign string, port uint8) frame {
	h := header{DataKind: kindUnregister}
	copy(h.From[:], callsign)
	return frame{header: h}
}

func connectFrame(from, to string, port uint8, digis []string) frame {
	if len(digis) > 0 {
		return connectViaFrame(from, to, port, digis)
	}
	return frame{header: header{
		DataKind: kindConnect,
		From:     callsignFromString(from),
		To:       callsignFromString(to),
	}}
}

func connectViaFrame(from, to string, port uint8, digis []string) frame {
	h := header{
		DataKind: kindConnectVia,
		From:     callsignFromString(from),
		To:       callsignFromString(to),
	}
	var buf bytes.Buffer
	buf.WriteByte(uint8(len(digis)))
	for _, str := range digis {
		callsign := callsignFromString(str)
		buf.Write([]byte(callsign[:]))
	}
	return frame{header: h, Data: buf.Bytes()}
}

func unprotoInformationFrame(from, to string, port uint8, data []byte) frame {
	h := header{
		DataKind: kindUnprotoInformation,
		From:     callsignFromString(from),
		To:       callsignFromString(to),
	}
	return frame{header: h, Data: data}
}

func disconnectFrame(from, to string, port uint8) frame {
	h := header{DataKind: kindDisconnect}
	copy(h.From[:], from)
	copy(h.To[:], to)
	return frame{header: h}
}
