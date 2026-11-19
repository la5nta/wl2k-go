package mailbox

import (
	"testing"

	"github.com/la5nta/wl2k-go/fbb"
)

func TestOutboundHasNoPrivateHeaders(t *testing.T) {
	h := NewDirHandler(t.TempDir(), false)
	if err := h.Prepare(); err != nil {
		t.Fatal(err)
	}
	msg := fbb.NewMessage(fbb.Private, "N0AAA")
	msg.AddTo("N0BBB")
	msg.SetSubject("Test")
	msg.SetBody("Hello")
	msg.Header.Set("X-P2POnly", "true")
	if err := h.AddOut(msg); err != nil {
		t.Fatal(err)
	}

	for name, fws := range map[string][]fbb.Address{
		"P2P": {fbb.AddressFromString("N0BBB")},
		"CMS": nil,
	} {
		for _, m := range h.GetOutbound(fws...) {
			for _, key := range []string{"X-FilePath", "X-Unread", "X-P2POnly"} {
				if v := m.Header.Get(key); v != "" {
					t.Errorf("%s: outbound message carries %s: %s", name, key, v)
				}
			}
		}
	}
	if n := len(h.GetOutbound(fbb.AddressFromString("N0BBB"))); n != 1 {
		t.Errorf("P2P: got %d outbound messages, want 1", n)
	}
}
