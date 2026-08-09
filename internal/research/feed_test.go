// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"strings"
	"testing"
)

const rssFixture = `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Model news</title>
    <item>
      <title>Frontier model ships tool use</title>
      <link>https://news.example.com/a</link>
      <guid>news-a</guid>
      <pubDate>Sat, 09 Aug 2026 10:00:00 +0000</pubDate>
      <description>&lt;p&gt;A &lt;b&gt;long&lt;/b&gt; description.&lt;/p&gt;</description>
    </item>
    <item>
      <title>Agent harness benchmark</title>
      <link>https://news.example.com/b</link>
      <guid>news-b</guid>
      <pubDate>Fri, 08 Aug 2026 09:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`

const atomFixture = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Lab blog</title>
  <entry>
    <title>Interpretability update</title>
    <id>tag:example.com,2026:1</id>
    <link rel="alternate" href="https://blog.example.com/1"/>
    <updated>2026-08-09T08:00:00Z</updated>
    <summary>Short summary.</summary>
  </entry>
</feed>`

func TestParseFeedHandlesRSS(t *testing.T) {
	items, err := ParseFeed([]byte(rssFixture))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].Title != "Frontier model ships tool use" || items[0].URL != "https://news.example.com/a" {
		t.Fatalf("bad first item: %+v", items[0])
	}
	if items[0].Summary != "A long description." {
		t.Fatalf("markup should be stripped, got %q", items[0].Summary)
	}
	if items[0].At.IsZero() || items[0].At.Format("2006-01-02") != "2026-08-09" {
		t.Fatalf("pubDate not parsed: %v", items[0].At)
	}
	if items[0].ID == "" || items[0].ID == items[1].ID {
		t.Fatalf("items need distinct stable ids: %q %q", items[0].ID, items[1].ID)
	}
}

func TestParseFeedHandlesAtom(t *testing.T) {
	items, err := ParseFeed([]byte(atomFixture))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(items) != 1 || items[0].URL != "https://blog.example.com/1" {
		t.Fatalf("bad atom parse: %+v", items)
	}
}

func TestParseFeedRejectsJunk(t *testing.T) {
	if _, err := ParseFeed([]byte("<html><body>not a feed</body></html>")); err == nil {
		t.Fatal("non-feed body must error")
	}
}

func TestNewItemsIsIdempotentAcrossPolls(t *testing.T) {
	items, err := ParseFeed([]byte(rssFixture))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	fresh, marker := NewItems(FeedMarker{}, items, 10)
	if len(fresh) != 2 || marker.NewItems != 2 {
		t.Fatalf("first poll should yield both items, got %d", len(fresh))
	}
	again, marker := NewItems(marker, items, 10)
	if len(again) != 0 {
		t.Fatalf("re-poll of an unchanged feed must yield nothing, got %d", len(again))
	}
	if marker.LastItemAt == "" {
		t.Fatal("marker should record the newest item time")
	}
	if capped, _ := NewItems(FeedMarker{}, items, 1); len(capped) != 1 {
		t.Fatalf("max_feed_items must bound one cycle, got %d", len(capped))
	}
}

func TestNewItemsBoundsSeenMemory(t *testing.T) {
	marker := FeedMarker{}
	var items []FeedItem
	for i := range MaxSeenPerFeed + 50 {
		items = append(items, FeedItem{ID: itemID(string(rune('a'+i%26)) + strings.Repeat("x", i))})
	}
	_, marker = NewItems(marker, items, len(items))
	if len(marker.SeenIDs) != MaxSeenPerFeed {
		t.Fatalf("seen memory must stay bounded, got %d", len(marker.SeenIDs))
	}
}

func TestFeedFindingsAddItemsAndRollLatest(t *testing.T) {
	items, _ := ParseFeed([]byte(rssFixture))
	feed := Feed{Name: "model-news", URL: "https://news.example.com/rss"}
	findings := FeedFindings(feed, items)
	if len(findings) != 3 {
		t.Fatalf("want one finding per item plus latest, got %d", len(findings))
	}
	latest := findings[len(findings)-1]
	if latest.Key != "feed:latest" || !strings.Contains(latest.Claim, "Frontier model ships tool use") {
		t.Fatalf("latest headline wrong: %+v", latest)
	}
	if FeedFindings(feed, nil) != nil {
		t.Fatal("no items must produce no findings")
	}
}

func TestFeedNoteTopicDefaultsToFeedName(t *testing.T) {
	if got := (Feed{Name: "model-news"}).NoteTopic(); got != "feed/model-news" {
		t.Fatalf("got %q", got)
	}
	if got := (Feed{Name: "model-news", Topic: "context/ai"}).NoteTopic(); got != "context/ai" {
		t.Fatalf("explicit topic must win, got %q", got)
	}
}
