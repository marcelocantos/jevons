// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfigDefaultsWhenAbsent(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Enabled || cfg.FeedEnabled {
		t.Fatalf("shipped posture is context-on, feeds-off: %+v", cfg)
	}
	if len(cfg.Feeds) != 0 || len(cfg.AllowedHosts) != 0 {
		t.Fatalf("no feed subscriptions by default: %+v", cfg)
	}
	if cfg.IntervalDuration() != DefaultInterval || cfg.Lookback() != DefaultLookback {
		t.Fatalf("default cadence wrong: %v %v", cfg.IntervalDuration(), cfg.Lookback())
	}
}

func TestLoadConfigRejectsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, DefaultConfig()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := os.WriteFile(ConfigPath(dir), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("malformed config must be a hard error, never a silent reset")
	}
}

func TestPatchConfigSubscribeAndUnsubscribe(t *testing.T) {
	dir := t.TempDir()
	cfg, err := PatchConfig(dir, ConfigPatch{
		AddFeed:   &Feed{Name: "model-news", URL: "https://news.example.com/rss", Enabled: true},
		UpdatedBy: "jevons",
		Now:       at(0),
	})
	if err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	if len(cfg.Feeds) != 1 || !cfg.HostAllowed("https://news.example.com/rss") {
		t.Fatalf("subscribing should register the feed and its host: %+v", cfg)
	}
	if cfg.UpdatedBy != "jevons" || cfg.UpdatedAt != stamp(at(0)) {
		t.Fatalf("retune provenance missing: %+v", cfg)
	}

	// Re-adding the same name updates in place rather than duplicating.
	cfg, err = PatchConfig(dir, ConfigPatch{
		AddFeed: &Feed{Name: "model-news", URL: "https://news.example.com/atom", Enabled: false},
		Now:     at(1),
	})
	if err != nil {
		t.Fatalf("PatchConfig update: %v", err)
	}
	if len(cfg.Feeds) != 1 || cfg.Feeds[0].URL != "https://news.example.com/atom" {
		t.Fatalf("feed should be replaced in place: %+v", cfg.Feeds)
	}
	if len(cfg.EnabledFeeds()) != 0 {
		t.Fatalf("disabled feed must not be polled: %+v", cfg.EnabledFeeds())
	}

	cfg, err = PatchConfig(dir, ConfigPatch{RemoveFeed: "MODEL-NEWS", Now: at(2)})
	if err != nil {
		t.Fatalf("PatchConfig remove: %v", err)
	}
	if len(cfg.Feeds) != 0 {
		t.Fatalf("unsubscribe should drop the feed: %+v", cfg.Feeds)
	}
}

func TestPatchConfigRejectsBadFeedURLs(t *testing.T) {
	dir := t.TempDir()
	for _, feed := range []Feed{
		{Name: "", URL: "https://news.example.com/rss"},
		{Name: "x", URL: ""},
		{Name: "x", URL: "file:///etc/passwd"},
		{Name: "x", URL: "https://"},
	} {
		if _, err := PatchConfig(dir, ConfigPatch{AddFeed: &feed, Now: at(0)}); err == nil {
			t.Fatalf("feed %+v should be rejected", feed)
		}
	}
}

func TestHostAllowedIsOptInAndSuffixMatched(t *testing.T) {
	cfg := Config{AllowedHosts: []string{"example.com"}}
	if !cfg.HostAllowed("https://news.example.com/rss") {
		t.Fatal("subdomain of an allowed host should pass")
	}
	if cfg.HostAllowed("https://notexample.com/rss") {
		t.Fatal("suffix match must respect the dot boundary")
	}
	if cfg.HostAllowed("not a url") {
		t.Fatal("unparseable url must not pass")
	}
	if (Config{}).HostAllowed("https://example.com") {
		t.Fatal("an empty allowlist allows nothing")
	}
}

func TestEffectiveBoundsFallBackToDefaults(t *testing.T) {
	var cfg Config
	if cfg.EffectiveMaxCommits() != DefaultMaxCommits ||
		cfg.EffectiveMaxRepos() != DefaultMaxRelatedRepos ||
		cfg.EffectiveMaxBriefsPerHour() != DefaultMaxBriefsPerHour ||
		cfg.EffectiveMaxFeedItems() != DefaultMaxFeedItems ||
		cfg.EffectiveMaxFeedCyclesPerHour() != DefaultMaxFeedCyclesPerHour ||
		cfg.EffectiveOverseer() != "jevons" {
		t.Fatalf("zero config must fall back to shipped bounds: %+v", cfg)
	}
	negative := Config{IntervalSec: -1, FeedIntervalSec: -1}
	if negative.IntervalDuration() != -time.Second || negative.FeedIntervalDuration() != -time.Second {
		t.Fatal("a negative interval must stay negative so the ticker can be disabled")
	}
}
