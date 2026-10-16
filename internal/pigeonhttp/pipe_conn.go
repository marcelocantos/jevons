// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package pigeonhttp

import (
	"context"
	"io"
	"sync"
)

// PipePair is a bidirectional in-memory Conn pair for hermetic tests.
type PipePair struct {
	A, B Conn
}

// NewPipePair returns two Conns that talk to each other.
func NewPipePair() PipePair {
	ab := make(chan []byte, 32)
	ba := make(chan []byte, 32)
	return PipePair{
		A: &pipeConn{out: ab, in: ba},
		B: &pipeConn{out: ba, in: ab},
	}
}

type pipeConn struct {
	out    chan []byte
	in     chan []byte
	mu     sync.Mutex
	closed bool
}

func (p *pipeConn) Send(ctx context.Context, data []byte) error {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return io.ErrClosedPipe
	}
	cp := append([]byte(nil), data...)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.out <- cp:
		return nil
	}
}

func (p *pipeConn) Recv(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case data, ok := <-p.in:
		if !ok {
			return nil, io.EOF
		}
		return data, nil
	}
}

func (p *pipeConn) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	close(p.out)
	return nil
}
