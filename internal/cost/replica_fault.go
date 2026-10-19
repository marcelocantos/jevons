// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// controlFrameMax mirrors the broker link's frame bound. A larger length is
// a corrupt stream, and the proxy ends that connection.
const controlFrameMax = 64 << 20

// ControlFaultProxy sits on the wire between Replica.Link and the broker's
// control socket and disturbs the frames the broker sends: it can drop or
// duplicate the next n sqlpipe changesets. It never opens either database
// file. Each frame is the link's own framing, a 4-byte big-endian length
// then one serialized sqlpipe peer message. Frames from Jevons pass through
// unchanged.
type ControlFaultProxy struct {
	ln       net.Listener
	path     string
	upstream string

	mu         sync.Mutex
	drop       int
	duplicate  int
	dropped    int
	duplicated int
	conns      map[net.Conn]struct{}
	wg         sync.WaitGroup
}

// StartControlFaultProxy listens on the unix socket path and forwards each
// connection to upstream, the broker's control socket.
func StartControlFaultProxy(path, upstream string) (*ControlFaultProxy, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("control fault proxy: %w", err)
	}
	p := &ControlFaultProxy{ln: ln, path: path, upstream: upstream, conns: map[net.Conn]struct{}{}}
	p.wg.Add(1)
	go p.accept()
	return p, nil
}

// DropChangesets drops the next n non-empty changesets the broker sends.
func (p *ControlFaultProxy) DropChangesets(n int) {
	p.mu.Lock()
	p.drop += n
	p.mu.Unlock()
}

// DuplicateChangesets sends each of the next n non-empty broker changesets
// twice.
func (p *ControlFaultProxy) DuplicateChangesets(n int) {
	p.mu.Lock()
	p.duplicate += n
	p.mu.Unlock()
}

// Faults reports how many broker changesets were dropped and duplicated.
func (p *ControlFaultProxy) Faults() (dropped, duplicated int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dropped, p.duplicated
}

// Sever closes every live connection, as a broken transport would. The
// consumer's link ends and dials again through the proxy.
func (p *ControlFaultProxy) Sever() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		_ = c.Close()
	}
}

// Close stops listening, severs live connections and waits for them.
func (p *ControlFaultProxy) Close() error {
	err := p.ln.Close()
	p.Sever()
	p.wg.Wait()
	_ = os.Remove(p.path)
	return err
}

func (p *ControlFaultProxy) accept() {
	defer p.wg.Done()
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		broker, err := net.Dial("unix", p.upstream)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		p.conns[client], p.conns[broker] = struct{}{}, struct{}{}
		p.mu.Unlock()
		p.wg.Add(2)
		go p.pipe(broker, client, true)
		go p.pipe(client, broker, false)
	}
}

// pipe copies frames from src to dst until either side fails, then closes
// both. fromBroker frames are subject to the configured faults.
func (p *ControlFaultProxy) pipe(src, dst net.Conn, fromBroker bool) {
	defer p.wg.Done()
	defer func() {
		_ = src.Close()
		_ = dst.Close()
		p.mu.Lock()
		delete(p.conns, src)
		delete(p.conns, dst)
		p.mu.Unlock()
	}()
	for {
		frame, err := readControlFrame(src)
		if err != nil {
			return
		}
		copies := 1
		if fromBroker && isChangeset(frame[4:]) {
			p.mu.Lock()
			switch {
			case p.drop > 0:
				p.drop--
				p.dropped++
				copies = 0
			case p.duplicate > 0:
				p.duplicate--
				p.duplicated++
				copies = 2
			}
			p.mu.Unlock()
		}
		for range copies {
			if _, err := dst.Write(frame); err != nil {
				return
			}
		}
	}
}

// isChangeset reports whether body is a sqlpipe changeset that carries
// rows. An empty changeset carries no fact, so dropping one would be no
// fault at all.
func isChangeset(body []byte) bool {
	msg, err := sqlpipe.DeserializePeer(body)
	if err != nil {
		return false
	}
	cs, ok := msg.Payload.(sqlpipe.ChangesetMsg)
	return ok && len(cs.Data) > 0
}

// readControlFrame returns one whole frame, length header included.
func readControlFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > controlFrameMax {
		return nil, errors.New("control fault proxy: bad frame length")
	}
	frame := make([]byte, 4+n)
	copy(frame, hdr[:])
	if _, err := io.ReadFull(r, frame[4:]); err != nil {
		return nil, err
	}
	return frame, nil
}
