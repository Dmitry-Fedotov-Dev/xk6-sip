package engine

import "testing"

func TestMessageKey(t *testing.T) {
	invite := "INVITE sip:702@pbx SIP/2.0\r\nVia: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-a1;rport\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n"
	compact := "INVITE sip:702@pbx SIP/2.0\r\nv: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-a1\r\nCSeq: 1 INVITE\r\n\r\n"
	other := "INVITE sip:702@pbx SIP/2.0\r\nVia: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-b2\r\nCSeq: 2 INVITE\r\n\r\n"
	ok200 := "SIP/2.0 200 OK\r\nVia: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-a1\r\nCSeq: 1 INVITE\r\n\r\n"
	ringing := "SIP/2.0 180 Ringing\r\nVia: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-a1\r\nCSeq: 1 INVITE\r\n\r\n"

	k1, m1, s1, ok := messageKey([]byte(invite))
	if !ok || m1 != "INVITE" || s1 != 0 {
		t.Fatalf("invite: %q %q %d %v", k1, m1, s1, ok)
	}
	if k2, _, _, _ := messageKey([]byte(compact)); k2 != k1 {
		t.Errorf("compact Via gives another key: %q vs %q", k2, k1)
	}
	if k3, _, _, _ := messageKey([]byte(other)); k3 == k1 {
		t.Error("different transaction, same key")
	}
	k4, m4, s4, ok := messageKey([]byte(ok200))
	if !ok || m4 != "INVITE" || s4 != 200 || k4 == k1 {
		t.Errorf("200 OK: %q %q %d %v", k4, m4, s4, ok)
	}
	if _, _, _, ok := messageKey([]byte(ringing)); ok {
		t.Error("provisional responses must be ignored")
	}
	if _, _, _, ok := messageKey([]byte("\r\n\r\n")); ok {
		t.Error("keepalive must be ignored")
	}
}
