// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import "testing"

// timedOutBroker models the loaded host of 🎯T796.1: claudia.BrokerAvailable
// returns false because its 2s round trip timed out, while a socket exists.
func timedOutBroker(t *testing.T) {
	t.Helper()
	pa, ps := brokerAvailable, brokerSocketProbe
	t.Cleanup(func() { brokerAvailable, brokerSocketProbe = pa, ps })
	brokerAvailable = func() bool { return false }
	brokerSocketProbe = func() brokerSocketState { return socketMaybe }
}

// 🎯T883: a socket that exists is enough to say a broker may own the seats.
// The broker itself is not asked, so one that accepts and never answers
// costs nothing here; with no socket, an answering broker is still a yes and
// a silent one is the positive-evidence no.
func TestT883SocketAloneMeansBrokerMayOwnSeats(t *testing.T) {
	timedOutBroker(t)
	calls := 0
	brokerAvailable = func() bool { calls++; return false }
	if !brokerMayOwnSeats() {
		t.Fatal("an existing broker socket was ruled out")
	}
	if calls != 0 {
		t.Fatalf("asked the broker %d times though its socket exists", calls)
	}
	brokerSocketProbe = func() brokerSocketState { return socketNone }
	if brokerMayOwnSeats() {
		t.Fatal("no socket and no answer should rule the broker out")
	}
	brokerAvailable = func() bool { return true }
	if !brokerMayOwnSeats() {
		t.Fatal("a broker that answers was ruled out because the socket probe missed it")
	}
}
