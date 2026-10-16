// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package pigeonhttp

import "context"

// Conn is the pigeon primary-stream surface the gateway needs.
// Satisfied by *pigeon.Conn (Send/Recv) and by test fakes.
type Conn interface {
	Send(ctx context.Context, data []byte) error
	Recv(ctx context.Context) ([]byte, error)
}

// SendFrame encodes and sends one frame.
func SendFrame(ctx context.Context, c Conn, f Frame) error {
	data, err := Encode(f)
	if err != nil {
		return err
	}
	return c.Send(ctx, data)
}

// RecvFrame reads and decodes one frame.
func RecvFrame(ctx context.Context, c Conn) (Frame, error) {
	data, err := c.Recv(ctx)
	if err != nil {
		return Frame{}, err
	}
	return Decode(data)
}
