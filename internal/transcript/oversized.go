// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// 🎯T661 — a session line over the broker's 1 MiB wire limit must not make
// the seat unreadable.
//
// On 2026-09-15 jv-t657-steer-ui's session carried three tool_result records
// of ~1.2 MB each (a screenshot's base64 PNG stored twice per line). This
// reader capped bufio.Scanner at 1 MiB, so the FIRST such line aborted the
// whole read with "bufio.Scanner: token too long" and jevons_transcript_read
// reported nothing at all for a 5.9 MB conversation — at the same moment
// claudia's broker refused sends to the seat for the same line. The PO was
// left with no working tool on a seat it needed to steer.
//
// The cap never bounded memory: every caller slurps the whole file into a
// []string anyway. It only decided WHICH file was unreadable. So lines are
// now read at any length up to hardLineCap (a defence against a file that is
// not a transcript at all), and the census below names the lines that exceed
// BrokerLineLimit so the send path and agent_list can say why the seat is
// hard to reach and how to recover it.

// BrokerLineLimit is claudia's broker wire line limit
// (internal/broker/transport.go maxLineLen). A session record over this size
// is the 🎯T661 signature: still readable here, but the seat's sends can fail
// when the daemon relays it over the broker connection.
const BrokerLineLimit = 1 << 20

// hardLineCap is where this reader stops believing the file is a transcript.
// A single JSON record this large is not something any provider writes; the
// error names the line so the operator can look, rather than the reader
// growing without bound on a stray binary.
const hardLineCap = 64 << 20

// OversizedLine names one session record over BrokerLineLimit. Line is the
// 1-based file line number (blank lines counted), so `sed -n <Line>p` lands on
// the record.
type OversizedLine struct {
	Line  int
	Bytes int
}

func (o OversizedLine) String() string {
	return fmt.Sprintf("line %d (%d bytes)", o.Line, o.Bytes)
}

// numberedLine pairs a non-blank raw line with its file line number.
type numberedLine struct {
	n   int
	raw string
}

// readNumberedLines reads every non-blank line of path at any length up to
// hardLineCap, keeping the file line number of each.
func readNumberedLines(path string) ([]numberedLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	return readNumbered(f, path)
}

func readNumbered(r io.Reader, path string) ([]numberedLine, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var out []numberedLine
	for n := 1; ; n++ {
		line, err := br.ReadString('\n')
		if len(line) > hardLineCap {
			return nil, fmt.Errorf("transcript %s: line %d is %d bytes, over the %d-byte cap — not a session record",
				path, n, len(line), hardLineCap)
		}
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read transcript: %w", err)
		}
		if trimmed := strings.TrimRight(line, "\r\n"); strings.TrimSpace(trimmed) != "" {
			out = append(out, numberedLine{n: n, raw: trimmed})
		}
		if err == io.EOF {
			return out, nil
		}
	}
}

// Oversized is the census of lines over BrokerLineLimit, in file order.
func Oversized(lines []numberedLine) []OversizedLine {
	var out []OversizedLine
	for _, l := range lines {
		if len(l.raw) > BrokerLineLimit {
			out = append(out, OversizedLine{Line: l.n, Bytes: len(l.raw)})
		}
	}
	return out
}

// ScanOversized streams path and reports the lines over BrokerLineLimit
// without retaining the file — the cheap probe for agent_list and the send
// path, which only need to know whether the seat carries the 🎯T661 signature.
func ScanOversized(path string) ([]OversizedLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64<<10)
	var out []OversizedLine
	size := 0
	for n := 1; ; {
		chunk, err := br.ReadSlice('\n')
		size += len(chunk)
		switch {
		case err == bufio.ErrBufferFull:
			continue
		case err == nil, err == io.EOF:
			if size > BrokerLineLimit {
				out = append(out, OversizedLine{Line: n, Bytes: size - strings.Count(string(chunk), "\n")})
			}
			if err == io.EOF {
				return out, nil
			}
			size = 0
			n++
		default:
			return nil, fmt.Errorf("read transcript: %w", err)
		}
	}
}

// OversizedMarker is the transcript-read note for a session with oversized
// lines: what was found, what it means for the seat, and the recovery.
func OversizedMarker(lines []OversizedLine) string {
	if len(lines) == 0 {
		return ""
	}
	names := make([]string, len(lines))
	for i, l := range lines {
		names[i] = l.String()
	}
	return fmt.Sprintf("oversized session: %d record(s) over the %d-byte broker line limit — %s. "+
		"The turns below still render; sends to this seat can fail with \"message exceeds the %d-byte line limit\" "+
		"until it is re-minted (jevons_agent_kill then jevons_agent_start under the same name) — 🎯T661.",
		len(lines), BrokerLineLimit, strings.Join(names, ", "), BrokerLineLimit)
}

// MarkerRole is the role of the note Read prepends for an oversized session.
// It is not a turn: it carries no turn_number and the transcript tool prints
// it as a note line rather than "Turn N".
const MarkerRole = "marker"
