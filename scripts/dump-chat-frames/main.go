package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://127.0.0.1:13705/ws/chat", nil)
	if err != nil {
		panic(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	frames := make(chan []byte, 256)
	go func() {
		for {
			_, d, err := conn.Read(ctx)
			if err != nil {
				close(frames)
				return
			}
			frames <- d
		}
	}()
	for {
		select {
		case <-frames:
		case <-time.After(800 * time.Millisecond):
			goto sent
		}
	}
sent:
	_ = conn.Write(ctx, websocket.MessageText, []byte("Reply with exactly: hello"))
	n := 0
	for n < 40 {
		select {
		case d, ok := <-frames:
			if !ok {
				return
			}
			n++
			s := string(d)
			if len(s) > 400 {
				s = s[:400] + "..."
			}
			fmt.Printf("%d %s\n", n, s)
			if strings.Contains(string(d), "end_turn") {
				return
			}
		case <-time.After(40 * time.Second):
			fmt.Println("timeout")
			return
		}
	}
}
