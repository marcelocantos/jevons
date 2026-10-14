// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ownerquestions provides a durable, per-question notification outbox.
// A blurter success is a spool acknowledgement, not owner delivery.
package ownerquestions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Question has a stable identity independent of mutable wording. Key should
// include repo and target (or report id); Text is the actual owner decision.
type Question struct {
	Key  string `json:"key"`
	Text string `json:"text"`
	Link string `json:"link,omitempty"`
}

// Entry is queryable durable state. SpoolPath is an acknowledgement from
// blurter, never a delivery receipt. LastError remains visible until retry.
type Entry struct {
	Question    Question  `json:"question"`
	Digest      string    `json:"digest"`
	Status      string    `json:"status"` // pending | spooled | failed
	SpoolPath   string    `json:"spool_path,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	LastAttempt time.Time `json:"last_attempt,omitempty"`
	LastSpooled time.Time `json:"last_spooled,omitempty"`
	Delivered   bool      `json:"delivered"` // always false: blurter exposes no delivery receipt
}

// Sender returns the spool acknowledgement, or an error. It MUST NOT claim
// delivery. Its idempotency key is advisory only: blurter dedups per app.
type Sender func(subject, body, key, link string) (string, error)

// BlurterSend reuses the T775 blurter CLI transport, with a distinct key.
func BlurterSend(subject, body, key, link string) (string, error) {
	args := []string{"send", "--app", "jevons", "--severity", "info", "--subject", subject, "--body", body, "--key", key}
	if link != "" {
		args = append(args, "--link", link)
	}
	out, err := exec.Command("blurter", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("blurter send: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Store serializes concurrent writers, including across daemon processes.
// One store file lives under the configured daemon state directory.
type Store struct {
	Path          string
	Send          Sender
	ReminderAfter time.Duration
}

func New(stateDir string) *Store {
	return &Store{Path: filepath.Join(stateDir, "owner-questions-notifications.json"), Send: BlurterSend, ReminderAfter: 24 * time.Hour}
}

type disk struct {
	Entries     map[string]Entry `json:"entries"`
	LastDigest  time.Time        `json:"last_digest,omitempty"`
	DigestError string           `json:"digest_error,omitempty"`
}

func (s *Store) locked(fn func(*disk) error) error {
	if s == nil || s.Path == "" {
		return fmt.Errorf("notification store path required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	d := disk{Entries: map[string]Entry{}}
	b, err := os.ReadFile(s.Path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &d); err != nil {
			return fmt.Errorf("notification state corrupt: %w", err)
		}
	}
	if d.Entries == nil {
		d.Entries = map[string]Entry{}
	}
	if err := fn(&d); err != nil {
		return err
	}
	b, err = json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".owner-questions-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// subject exposes the repo and target in desktop banners and Slack, not only an opaque hash.
func subject(key string) string {
	parts := strings.Split(key, "#")
	if len(parts) >= 3 {
		return "jevons: owner action " + filepath.Base(parts[0]) + "/" + parts[1] + " [" + hash(key)[:8] + "]"
	}
	return "jevons: owner decision needed [" + hash(key)[:8] + "]"
}
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// Observe records a new or changed question then attempts a prompt spool.
// Duplicate observations do not send again. Failure remains retryable.
func (s *Store) Observe(q Question, now time.Time) (Entry, error) {
	if strings.TrimSpace(q.Key) == "" || strings.TrimSpace(q.Text) == "" {
		return Entry{}, fmt.Errorf("question key and text required")
	}
	if now.IsZero() {
		now = time.Now()
	}
	var result Entry
	var sendErr error
	err := s.locked(func(d *disk) error {
		e, ok := d.Entries[q.Key]
		digest := hash(q.Text)
		if !ok || e.Digest != digest {
			e = Entry{Question: q, Digest: digest, Status: "pending"}
		}
		if e.Status != "spooled" {
			e.LastAttempt = now
			send := s.Send
			if send == nil {
				send = BlurterSend
			}
			// Key cannot substitute for local per-question dedup: blurter's LastDigest is per APP.
			path, err := send(subject(q.Key), q.Text, "owner-question-"+hash(q.Key)[:16], q.Link)
			if err != nil {
				e.Status = "failed"
				e.LastError = err.Error()
				sendErr = err
			} else {
				e.Status = "spooled"
				e.LastError = ""
				e.SpoolPath = path
				e.LastSpooled = now
			}
		}
		d.Entries[q.Key] = e
		result = e
		return nil
	})
	if err != nil {
		return Entry{}, err
	}
	return result, sendErr
}

// Resolve removes an answered or moot question from the active outbox.
func (s *Store) Resolve(key string) error {
	return s.locked(func(d *disk) error { delete(d.Entries, key); return nil })
}

// Snapshot returns open questions and the most recent digest failure.
func (s *Store) Snapshot() ([]Entry, string, error) {
	var entries []Entry
	var failure string
	err := s.locked(func(d *disk) error {
		for _, e := range d.Entries {
			entries = append(entries, e)
		}
		failure = d.DigestError
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Question.Key < entries[j].Question.Key })
	return entries, failure, err
}

// Remind sends at most one digest per interval for still-open spooled
// questions. Failed digests are retryable; reminder cadence is a provisional
// daily default pending owner approval (T1028.3).
func (s *Store) Remind(now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	after := s.ReminderAfter
	if after <= 0 {
		after = 24 * time.Hour
	}
	var sendErr error
	err := s.locked(func(d *disk) error {
		// Retry failed prompt sends after a daemon restart or transport outage.
		// Throttle retries to one per minute; no report needs to be repeated.
		send := s.Send
		if send == nil {
			send = BlurterSend
		}
		for key, e := range d.Entries {
			if e.Status != "failed" && e.Status != "pending" {
				continue
			}
			if !e.LastAttempt.IsZero() && now.Sub(e.LastAttempt) < time.Minute {
				continue
			}
			e.LastAttempt = now
			path, err := send(subject(key), e.Question.Text, "owner-question-"+hash(key)[:16], e.Question.Link)
			if err != nil {
				e.Status = "failed"
				e.LastError = err.Error()
				sendErr = err
			} else {
				e.Status = "spooled"
				e.LastError = ""
				e.SpoolPath = path
				e.LastSpooled = now
			}
			d.Entries[key] = e
		}
		keys := make([]string, 0, len(d.Entries))
		for k, e := range d.Entries {
			if e.Status == "spooled" && now.Sub(e.LastSpooled) >= after {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 || (!d.LastDigest.IsZero() && now.Sub(d.LastDigest) < after) {
			return nil
		}
		sort.Strings(keys)
		lines := make([]string, 0, len(keys))
		for _, k := range keys {
			lines = append(lines, "• "+d.Entries[k].Question.Text)
		}
		_, sendErr = send("jevons: open owner questions", strings.Join(lines, "\n"), "owner-questions-digest", "")
		if sendErr != nil {
			d.DigestError = sendErr.Error()
		} else {
			d.DigestError = ""
			d.LastDigest = now
		}
		return nil
	})
	if err != nil {
		return err
	}
	return sendErr
}
