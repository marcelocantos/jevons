// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// voicelab is a throwaway desktop CLI for iterating on Grok Realtime
// voice interaction quality. The loop core lives in
// internal/voicelab — this main wires it to a live audio device.
//
// On macOS the device is the system's VoiceProcessingIO audio unit
// (the same one FaceTime uses), which means AEC, noise suppression,
// and AGC come from the OS for free — including on the always-on
// --continuous path.
//
// Default mode is push-to-talk anyway, because the AEC reference
// signal still needs your speaker to be playing the unaltered
// playback (no Bluetooth re-routing, no external mixers), and PTT
// is a foolproof way to confirm the loop works regardless of audio
// path. Pass --continuous for the Tesla-style always-on flow.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/marcelocantos/jevons/internal/voicelab"
)

func main() {
	systemPrompt := flag.String("system", "You are jevons, a voice-first assistant. Keep replies brief and conversational.", "system prompt sent to Grok")
	voice := flag.String("voice", "Eve", "Grok TTS voice")
	verbose := flag.Bool("v", false, "verbose protocol logging")
	continuous := flag.Bool("continuous", false, "always-on server-VAD mode (uses OS VoiceProcessingIO AEC on macOS)")
	flag.Parse()

	logLevel := slog.LevelWarn
	if *verbose {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	apiKey, err := loadKeychainKey("xai-api-key")
	if err != nil {
		fatal("xai-api-key not found in keychain (expected `security add-generic-password -a jevons -s xai-api-key -w <key>`): %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	dev, err := newAudioDevice()
	if err != nil {
		fatal("audio device: %v", err)
	}
	defer dev.Close()

	if *continuous {
		runContinuous(ctx, apiKey, *voice, *systemPrompt, dev)
	} else {
		runPTT(ctx, apiKey, *voice, *systemPrompt, dev)
	}
}

// audioDevice is the minimal interface main needs from a device.
// Both VoiceProcDevice (macOS) and MalgoDevice satisfy it.
type audioDevice interface {
	Source() voicelab.AudioSource
	Sink() voicelab.AudioSink
	Close() error
}

// runContinuous: always-on, server VAD detects utterance boundaries.
// Beautiful when it works; loops on itself when speakers reach the mic.
func runContinuous(ctx context.Context, apiKey, voice, systemPrompt string, dev audioDevice) {
	loop := &voicelab.Loop{
		APIKey:       apiKey,
		Voice:        voice,
		SystemPrompt: systemPrompt,
		Source:       dev.Source(),
		Sink:         dev.Sink(),
		OnUserTranscript: func(text string) {
			fmt.Printf("\n> %s\n", strings.TrimSpace(text))
		},
		OnTranscript: func(text string) {
			fmt.Print(text)
		},
		OnTranscriptDone: func() {
			fmt.Println()
		},
		OnError: func(err error) {
			slog.Error("voicelab", "err", err)
		},
		OnSessionReady: func() {
			fmt.Fprintln(os.Stderr, "voicelab (continuous): session ready — start talking. Ctrl-C to quit.")
			fmt.Fprintln(os.Stderr, "  AEC + NS + AGC via the OS VoiceProcessingIO audio unit (same as FaceTime).")
		},
	}

	if err := loop.Run(ctx); err != nil && ctx.Err() == nil {
		fatal("loop: %v", err)
	}
	fmt.Fprintln(os.Stderr, "\nvoicelab: shutting down")
}

// runPTT: push-to-talk. Mic is muted until user presses Enter.
// Pressing Enter again disables mic, commits, and waits for response.
// Barge-in is implicit: the first Enter clears any audio Grok is
// still playing, so a half-finished sentence gets cut off and the
// user takes the floor.
func runPTT(ctx context.Context, apiKey, voice, systemPrompt string, dev audioDevice) {
	gated := newGatedSource(dev.Source())
	commit := make(chan struct{}, 1)
	respDone := make(chan struct{}, 1)
	ready := make(chan struct{}, 1)
	sink := dev.Sink()

	loop := &voicelab.Loop{
		APIKey:       apiKey,
		Voice:        voice,
		SystemPrompt: systemPrompt,
		Source:       gated,
		Sink:         sink,
		ManualCommit: true,
		CommitSignal: commit,
		OnUserTranscript: func(text string) {
			fmt.Printf("\n> %s\n", strings.TrimSpace(text))
		},
		OnTranscript: func(text string) {
			fmt.Print(text)
		},
		OnTranscriptDone: func() {
			fmt.Println()
		},
		OnResponseDone: func() {
			select {
			case respDone <- struct{}{}:
			default:
			}
		},
		OnError: func(err error) {
			slog.Error("voicelab", "err", err)
		},
		OnSessionReady: func() {
			select {
			case ready <- struct{}{}:
			default:
			}
		},
	}

	loopErrCh := make(chan error, 1)
	go func() { loopErrCh <- loop.Run(ctx) }()

	select {
	case <-ready:
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "\nvoicelab: shutting down")
		return
	case err := <-loopErrCh:
		fatal("loop failed before ready: %v", err)
	}

	fmt.Fprintln(os.Stderr, "voicelab (push-to-talk): session ready.")
	fmt.Fprintln(os.Stderr, "  press Enter to talk, Enter again to send. Ctrl-C to quit.")
	fmt.Fprintln(os.Stderr, "  (--continuous for always-on, but only with headphones)")

	go ptHandler(ctx, gated, sink, commit, respDone)

	if err := <-loopErrCh; err != nil && ctx.Err() == nil {
		fatal("loop: %v", err)
	}
	fmt.Fprintln(os.Stderr, "\nvoicelab: shutting down")
}

func ptHandler(ctx context.Context, gated *gatedSource, sink voicelab.AudioSink, commit chan<- struct{}, respDone <-chan struct{}) {
	reader := bufio.NewReader(os.Stdin)
	readLine := func() bool {
		_, err := reader.ReadString('\n')
		return err == nil
	}

	for {
		fmt.Fprint(os.Stderr, "\n[press Enter to talk] ")
		if !readLine() {
			return
		}
		// Barge in: drop whatever Grok was still playing.
		sink.Clear()
		gated.Enable()
		fmt.Fprint(os.Stderr, "🎤 listening — press Enter to send ")
		if !readLine() {
			return
		}
		gated.Disable()
		gated.Drain(ctx)
		select {
		case commit <- struct{}{}:
		case <-ctx.Done():
			return
		}
		fmt.Fprintln(os.Stderr, "💭 thinking…")
		select {
		case <-respDone:
		case <-ctx.Done():
			return
		}
	}
}

// gatedSource wraps an inner AudioSource and only forwards frames
// when Enable() has been called. Disable() drops in-flight frames at
// the source side; pre-existing items in the forwarding channel are
// flushed by Drain() before the host signals commit.
type gatedSource struct {
	inner   voicelab.AudioSource
	out     chan []byte
	enabled atomic.Bool
	once    sync.Once
}

func newGatedSource(inner voicelab.AudioSource) *gatedSource {
	return &gatedSource{
		inner: inner,
		out:   make(chan []byte, 16),
	}
}

func (g *gatedSource) Frames() <-chan []byte {
	g.once.Do(func() {
		go func() {
			defer close(g.out)
			for buf := range g.inner.Frames() {
				if !g.enabled.Load() {
					continue
				}
				select {
				case g.out <- buf:
				default:
					// Output buffer full — drop. The Loop consumer
					// runs at the same 20 ms cadence as inputs, so
					// this only fires when the WS write blocks, which
					// is far beyond what the user would tolerate
					// anyway. Surfaces as a brief mic gap.
				}
			}
		}()
	})
	return g.out
}

func (g *gatedSource) Close() error  { return nil }
func (g *gatedSource) Enable()       { g.enabled.Store(true) }
func (g *gatedSource) Disable()      { g.enabled.Store(false) }

// Drain blocks until the forwarding buffer is empty or ctx is
// cancelled. Called between Disable and the commit signal so all
// in-flight frames reach Grok before the commit message — otherwise
// the trailing audio of an utterance can race past the commit and
// confuse the response window.
func (g *gatedSource) Drain(ctx context.Context) {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if len(g.out) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func loadKeychainKey(service string) (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", "jevons", "-s", service, "-w").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "voicelab: "+format+"\n", args...)
	os.Exit(1)
}
