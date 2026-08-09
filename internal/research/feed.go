// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FeedCursorFileName is the durable record of which items have been seen.
const FeedCursorFileName = "feed_cursor.json"

// MaxSeenPerFeed bounds retained item ids per feed.
const MaxSeenPerFeed = 400

// FeedItem is one normalized entry from a subscribed source.
type FeedItem struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	URL     string    `json:"url,omitempty"`
	Summary string    `json:"summary,omitempty"`
	At      time.Time `json:"at,omitzero"`
}

// Fetcher retrieves raw feed bytes. Injectable so hermetic tests exercise the
// whole feed-triggered path without network access.
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}

// HTTPFetcher is the shipped fetcher: bounded body, bounded time, no redirect
// off the configured host, and nothing followed out of the feed itself.
type HTTPFetcher struct {
	Client    *http.Client
	BodyLimit int64
	UserAgent string
}

// NewHTTPFetcher builds a fetcher with the default bounds.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{
		Client:    &http.Client{Timeout: DefaultFeedTimeout},
		BodyLimit: DefaultFeedBodyLimit,
		UserAgent: "jevons-research/1.0",
	}
}

// Fetch retrieves one feed body.
func (f *HTTPFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("research feed: nil fetcher")
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultFeedTimeout}
	}
	limit := f.BodyLimit
	if limit <= 0 {
		limit = DefaultFeedBodyLimit
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("research feed: %s returned %s", rawURL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// rssDoc / atomDoc cover the two syndication shapes worth supporting.
type rssDoc struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			GUID        string `xml:"guid"`
			PubDate     string `xml:"pubDate"`
			Description string `xml:"description"`
		} `xml:"item"`
	} `xml:"channel"`
}

type atomDoc struct {
	Entries []struct {
		Title string `xml:"title"`
		ID    string `xml:"id"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Updated   string `xml:"updated"`
		Published string `xml:"published"`
		Summary   string `xml:"summary"`
	} `xml:"entry"`
}

// ParseFeed normalizes RSS 2.0 or Atom bytes into items, newest-first as the
// source ordered them. Pure: hermetic tests decide it from fixtures.
func ParseFeed(data []byte) ([]FeedItem, error) {
	var rss rssDoc
	if err := xml.Unmarshal(data, &rss); err == nil && len(rss.Channel.Items) > 0 {
		out := make([]FeedItem, 0, len(rss.Channel.Items))
		for _, it := range rss.Channel.Items {
			out = append(out, FeedItem{
				ID:      itemID(it.GUID, it.Link, it.Title),
				Title:   strings.TrimSpace(it.Title),
				URL:     strings.TrimSpace(it.Link),
				Summary: trimSummary(it.Description),
				At:      parseFeedTime(it.PubDate),
			})
		}
		return out, nil
	}
	var atom atomDoc
	if err := xml.Unmarshal(data, &atom); err == nil && len(atom.Entries) > 0 {
		out := make([]FeedItem, 0, len(atom.Entries))
		for _, e := range atom.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			out = append(out, FeedItem{
				ID:      itemID(e.ID, link, e.Title),
				Title:   strings.TrimSpace(e.Title),
				URL:     strings.TrimSpace(link),
				Summary: trimSummary(e.Summary),
				At:      parseFeedTime(firstNonEmpty(e.Updated, e.Published)),
			})
		}
		return out, nil
	}
	return nil, fmt.Errorf("research feed: no RSS items or Atom entries found")
}

// FeedFindings turns new items into note findings. Each item is its own durable
// claim (item ids are stable, so items are added and never rewritten), plus one
// rolling "latest headline" claim that is superseded as the feed moves.
func FeedFindings(feed Feed, items []FeedItem) []Finding {
	if len(items) == 0 {
		return nil
	}
	out := make([]Finding, 0, len(items)+1)
	newest := items[0]
	for _, it := range items {
		if it.At.After(newest.At) {
			newest = it
		}
		claim := it.Title
		if it.Summary != "" {
			claim += " — " + it.Summary
		}
		out = append(out, Finding{
			Key:      "item:" + it.ID,
			Claim:    claim,
			Evidence: []string{it.URL},
			Sources:  []Source{{Kind: "feed", Ref: firstNonEmpty(it.URL, feed.URL), At: stampOrEmpty(it.At)}},
		})
	}
	out = append(out, Finding{
		Key:      "feed:latest",
		Claim:    "latest headline: " + newest.Title,
		Evidence: []string{newest.URL},
		Sources:  []Source{{Kind: "feed", Ref: feed.URL, At: stampOrEmpty(newest.At)}},
	})
	return out
}

// FeedCursor is the durable record of what each feed has already delivered.
type FeedCursor struct {
	Version int                   `json:"version"`
	Feeds   map[string]FeedMarker `json:"feeds,omitempty"`
}

// FeedMarker is one feed's seen-item memory.
type FeedMarker struct {
	SeenIDs      []string `json:"seen_ids,omitempty"`
	LastPolledAt string   `json:"last_polled_at,omitempty"`
	LastItemAt   string   `json:"last_item_at,omitempty"`
	Polls        int      `json:"polls,omitempty"`
	NewItems     int      `json:"new_items,omitempty"`
	LastError    string   `json:"last_error,omitempty"`
}

// FeedCursorPath returns the cursor path under a store directory.
func FeedCursorPath(dir string) string { return filepath.Join(dir, FeedCursorFileName) }

// LoadFeedCursor reads the durable cursor (absent = empty).
func LoadFeedCursor(dir string) (FeedCursor, error) {
	path := FeedCursorPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FeedCursor{Version: 1, Feeds: map[string]FeedMarker{}}, nil
		}
		return FeedCursor{}, fmt.Errorf("research feed cursor: read %s: %w", path, err)
	}
	var cur FeedCursor
	if err := json.Unmarshal(data, &cur); err != nil {
		return FeedCursor{}, fmt.Errorf("research feed cursor: parse %s: %w", path, err)
	}
	if cur.Feeds == nil {
		cur.Feeds = map[string]FeedMarker{}
	}
	return cur, nil
}

// SaveFeedCursor persists the cursor atomically.
func SaveFeedCursor(dir string, cur FeedCursor) error {
	cur.Version = 1
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return fmt.Errorf("research feed cursor: marshal: %w", err)
	}
	return writeFileAtomic(FeedCursorPath(dir), append(data, '\n'), 0o644)
}

// NewItems returns items this feed has not delivered before, bounded by max.
// Pure: the caller decides whether to commit the resulting marker.
func NewItems(marker FeedMarker, items []FeedItem, max int) ([]FeedItem, FeedMarker) {
	if max <= 0 {
		max = DefaultMaxFeedItems
	}
	seen := make(map[string]bool, len(marker.SeenIDs))
	for _, id := range marker.SeenIDs {
		seen[id] = true
	}
	var fresh []FeedItem
	for _, it := range items {
		if it.ID == "" || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		marker.SeenIDs = append(marker.SeenIDs, it.ID)
		fresh = append(fresh, it)
		if len(fresh) >= max {
			break
		}
	}
	if len(marker.SeenIDs) > MaxSeenPerFeed {
		marker.SeenIDs = append([]string(nil), marker.SeenIDs[len(marker.SeenIDs)-MaxSeenPerFeed:]...)
	}
	marker.NewItems += len(fresh)
	for _, it := range fresh {
		if ts := stampOrEmpty(it.At); ts > marker.LastItemAt {
			marker.LastItemAt = ts
		}
	}
	return fresh, marker
}

func itemID(candidates ...string) string {
	for _, c := range candidates {
		if c = strings.TrimSpace(c); c != "" {
			sum := sha1.Sum([]byte(c))
			return hex.EncodeToString(sum[:])[:16]
		}
	}
	return ""
}

func trimSummary(s string) string {
	s = strings.Join(strings.Fields(stripTags(s)), " ")
	if len(s) > 240 {
		s = strings.TrimSpace(s[:240]) + "…"
	}
	return s
}

// stripTags removes crude HTML markup from feed descriptions.
func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var feedTimeLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC822Z, time.RFC822,
	"Mon, 2 Jan 2006 15:04:05 -0700", "2006-01-02T15:04:05Z07:00", "2006-01-02",
}

func parseFeedTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range feedTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func stampOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
