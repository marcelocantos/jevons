// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Schedule and policy defaults. Every bound here exists so an ambient cycle
// stays a staff cycle (🎯T325 shape) instead of a permanent monologue.
const (
	// DefaultInterval is the context-refresh cadence.
	DefaultInterval = 90 * time.Minute
	// DefaultFeedInterval is the news/feed poll cadence.
	DefaultFeedInterval = 30 * time.Minute
	// DefaultLookback bounds how far back one context pass reaches.
	DefaultLookback = 72 * time.Hour
	// DefaultMaxCommits bounds git mining per repo per pass.
	DefaultMaxCommits = 300
	// DefaultMaxRelatedRepos bounds sibling repos scanned per pass.
	DefaultMaxRelatedRepos = 6
	// DefaultMaxBriefsPerHour bounds delivery to the overseer.
	DefaultMaxBriefsPerHour = 2
	// DefaultMaxFeedItems bounds items consumed from one feed per cycle.
	DefaultMaxFeedItems = 10
	// DefaultMaxFeedCyclesPerHour bounds feed-triggered cycles.
	DefaultMaxFeedCyclesPerHour = 4
	// DefaultFeedBodyLimit caps bytes read from a feed response.
	DefaultFeedBodyLimit = 2 << 20
	// DefaultFeedTimeout bounds one feed fetch.
	DefaultFeedTimeout = 20 * time.Second
	// ConfigFileName is the durable config under state_dir/research.
	ConfigFileName = "config.json"
)

// Feed is one subscribed news source. Feeds are explicit: the agent fetches
// exactly these URLs and never follows links out of them (no web crawl).
type Feed struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
	// Topic overrides the note topic for this feed (default feed/<name>).
	Topic string `json:"topic,omitempty"`
}

// NoteTopic returns the note topic this feed folds into.
func (f Feed) NoteTopic() string {
	if t := strings.TrimSpace(f.Topic); t != "" {
		return t
	}
	return "feed/" + strings.TrimSpace(f.Name)
}

// Config is the durable, overseer-retunable research configuration.
type Config struct {
	Enabled       bool   `json:"enabled"`
	IntervalSec   int    `json:"interval_sec"`
	LookbackHours int    `json:"lookback_hours"`
	MaxCommits    int    `json:"max_commits"`
	MaxRepos      int    `json:"max_repos"`
	Workdir       string `json:"workdir,omitempty"`
	// RelatedRoots are directories whose immediate children are scanned for
	// sibling repositories (e.g. ~/work/github.com/marcelocantos).
	RelatedRoots []string `json:"related_roots,omitempty"`

	Deliver          bool   `json:"deliver"`
	Overseer         string `json:"overseer,omitempty"`
	MaxBriefsPerHour int    `json:"max_briefs_per_hour"`

	FeedEnabled          bool     `json:"feed_enabled"`
	FeedIntervalSec      int      `json:"feed_interval_sec"`
	Feeds                []Feed   `json:"feeds,omitempty"`
	AllowedHosts         []string `json:"allowed_hosts,omitempty"`
	MaxFeedItems         int      `json:"max_feed_items"`
	MaxFeedCyclesPerHour int      `json:"max_feed_cycles_per_hour"`

	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// DefaultConfig is the shipped posture: context refresh on, feeds off until the
// owner subscribes to something (no surprise outbound traffic).
func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		IntervalSec:          int(DefaultInterval / time.Second),
		LookbackHours:        int(DefaultLookback / time.Hour),
		MaxCommits:           DefaultMaxCommits,
		MaxRepos:             DefaultMaxRelatedRepos,
		Deliver:              true,
		Overseer:             "jevons",
		MaxBriefsPerHour:     DefaultMaxBriefsPerHour,
		FeedEnabled:          false,
		FeedIntervalSec:      int(DefaultFeedInterval / time.Second),
		MaxFeedItems:         DefaultMaxFeedItems,
		MaxFeedCyclesPerHour: DefaultMaxFeedCyclesPerHour,
	}
}

// ConfigPath returns the durable config path for a state dir.
func ConfigPath(stateDir string) string {
	return filepath.Join(expandHome(stateDir), DirName, ConfigFileName)
}

// LoadConfig reads durable config, falling back to defaults when absent.
func LoadConfig(stateDir string) (Config, error) {
	path := ConfigPath(stateDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("research config: read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Malformed durable state is a hard error, never a silent reset.
		return Config{}, fmt.Errorf("research config: parse %s: %w", path, err)
	}
	return cfg, nil
}

// SaveConfig persists config atomically.
func SaveConfig(stateDir string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("research config: marshal: %w", err)
	}
	return writeFileAtomic(ConfigPath(stateDir), append(data, '\n'), 0o644)
}

// ConfigPatch is an overseer retune. Nil fields are left unchanged.
type ConfigPatch struct {
	Enabled              *bool
	IntervalSec          *int
	LookbackHours        *int
	MaxCommits           *int
	MaxRepos             *int
	Workdir              *string
	RelatedRoots         *[]string
	Deliver              *bool
	Overseer             *string
	MaxBriefsPerHour     *int
	FeedEnabled          *bool
	FeedIntervalSec      *int
	AllowedHosts         *[]string
	MaxFeedItems         *int
	MaxFeedCyclesPerHour *int
	// AddFeed subscribes (or updates) one feed by name.
	AddFeed *Feed
	// RemoveFeed unsubscribes one feed by name.
	RemoveFeed string
	UpdatedBy  string
	Now        time.Time
}

// PatchConfig applies a retune and persists the result.
func PatchConfig(stateDir string, patch ConfigPatch) (Config, error) {
	cfg, err := LoadConfig(stateDir)
	if err != nil {
		return Config{}, err
	}
	assign := func(dst *int, src *int) {
		if src != nil {
			*dst = *src
		}
	}
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	assign(&cfg.IntervalSec, patch.IntervalSec)
	assign(&cfg.LookbackHours, patch.LookbackHours)
	assign(&cfg.MaxCommits, patch.MaxCommits)
	assign(&cfg.MaxRepos, patch.MaxRepos)
	assign(&cfg.MaxBriefsPerHour, patch.MaxBriefsPerHour)
	assign(&cfg.FeedIntervalSec, patch.FeedIntervalSec)
	assign(&cfg.MaxFeedItems, patch.MaxFeedItems)
	assign(&cfg.MaxFeedCyclesPerHour, patch.MaxFeedCyclesPerHour)
	if patch.Workdir != nil {
		cfg.Workdir = strings.TrimSpace(*patch.Workdir)
	}
	if patch.RelatedRoots != nil {
		cfg.RelatedRoots = dedupeStrings(*patch.RelatedRoots)
	}
	if patch.Deliver != nil {
		cfg.Deliver = *patch.Deliver
	}
	if patch.Overseer != nil {
		cfg.Overseer = strings.TrimSpace(*patch.Overseer)
	}
	if patch.FeedEnabled != nil {
		cfg.FeedEnabled = *patch.FeedEnabled
	}
	if patch.AllowedHosts != nil {
		cfg.AllowedHosts = dedupeStrings(*patch.AllowedHosts)
	}
	if patch.AddFeed != nil {
		feed, err := normalizeFeed(*patch.AddFeed)
		if err != nil {
			return Config{}, err
		}
		replaced := false
		for i, f := range cfg.Feeds {
			if strings.EqualFold(f.Name, feed.Name) {
				cfg.Feeds[i] = feed
				replaced = true
				break
			}
		}
		if !replaced {
			cfg.Feeds = append(cfg.Feeds, feed)
		}
		// Subscribing implies the host is allowed; the allowlist is a policy
		// bound on fetching, not a second place to remember the same URL.
		if u, err := url.Parse(feed.URL); err == nil && u.Host != "" {
			cfg.AllowedHosts = dedupeStrings(append(cfg.AllowedHosts, strings.ToLower(u.Hostname())))
		}
	}
	if name := strings.TrimSpace(patch.RemoveFeed); name != "" {
		kept := cfg.Feeds[:0]
		for _, f := range cfg.Feeds {
			if !strings.EqualFold(f.Name, name) {
				kept = append(kept, f)
			}
		}
		cfg.Feeds = append([]Feed(nil), kept...)
	}
	now := patch.Now
	if now.IsZero() {
		now = time.Now()
	}
	cfg.UpdatedAt = stamp(now)
	if by := strings.TrimSpace(patch.UpdatedBy); by != "" {
		cfg.UpdatedBy = by
	}
	if err := SaveConfig(stateDir, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func normalizeFeed(f Feed) (Feed, error) {
	f.Name = strings.TrimSpace(f.Name)
	f.URL = strings.TrimSpace(f.URL)
	f.Topic = strings.TrimSpace(f.Topic)
	if f.Name == "" || f.URL == "" {
		return Feed{}, fmt.Errorf("research config: feed name and url required")
	}
	u, err := url.Parse(f.URL)
	if err != nil {
		return Feed{}, fmt.Errorf("research config: feed url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Feed{}, fmt.Errorf("research config: feed url must be http(s), got %q", u.Scheme)
	}
	if u.Host == "" {
		return Feed{}, fmt.Errorf("research config: feed url needs a host")
	}
	return f, nil
}

// IntervalDuration is the context-refresh period (negative disables the ticker).
func (c Config) IntervalDuration() time.Duration {
	if c.IntervalSec == 0 {
		return DefaultInterval
	}
	return time.Duration(c.IntervalSec) * time.Second
}

// FeedIntervalDuration is the feed poll period (negative disables the ticker).
func (c Config) FeedIntervalDuration() time.Duration {
	if c.FeedIntervalSec == 0 {
		return DefaultFeedInterval
	}
	return time.Duration(c.FeedIntervalSec) * time.Second
}

// Lookback is how far back one context pass reaches.
func (c Config) Lookback() time.Duration {
	if c.LookbackHours <= 0 {
		return DefaultLookback
	}
	return time.Duration(c.LookbackHours) * time.Hour
}

// EffectiveMaxCommits bounds git mining per repo.
func (c Config) EffectiveMaxCommits() int {
	if c.MaxCommits <= 0 {
		return DefaultMaxCommits
	}
	return c.MaxCommits
}

// EffectiveMaxRepos bounds sibling repositories scanned.
func (c Config) EffectiveMaxRepos() int {
	if c.MaxRepos <= 0 {
		return DefaultMaxRelatedRepos
	}
	return c.MaxRepos
}

// EffectiveMaxBriefsPerHour bounds overseer delivery.
func (c Config) EffectiveMaxBriefsPerHour() int {
	if c.MaxBriefsPerHour <= 0 {
		return DefaultMaxBriefsPerHour
	}
	return c.MaxBriefsPerHour
}

// EffectiveMaxFeedItems bounds items taken from one feed per cycle.
func (c Config) EffectiveMaxFeedItems() int {
	if c.MaxFeedItems <= 0 {
		return DefaultMaxFeedItems
	}
	return c.MaxFeedItems
}

// EffectiveMaxFeedCyclesPerHour bounds feed-triggered cycles.
func (c Config) EffectiveMaxFeedCyclesPerHour() int {
	if c.MaxFeedCyclesPerHour <= 0 {
		return DefaultMaxFeedCyclesPerHour
	}
	return c.MaxFeedCyclesPerHour
}

// EffectiveOverseer is the delivery target agent.
func (c Config) EffectiveOverseer() string {
	if o := strings.TrimSpace(c.Overseer); o != "" {
		return o
	}
	return "jevons"
}

// EnabledFeeds returns subscribed feeds that are switched on.
func (c Config) EnabledFeeds() []Feed {
	var out []Feed
	for _, f := range c.Feeds {
		if f.Enabled && strings.TrimSpace(f.URL) != "" {
			out = append(out, f)
		}
	}
	return out
}

// HostAllowed reports whether a URL's host is inside the fetch policy.
// An empty allowlist allows nothing: policy is opt-in, never implicit.
func (c Config) HostAllowed(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, allowed := range c.AllowedHosts {
		a := strings.ToLower(strings.TrimSpace(allowed))
		if a == "" {
			continue
		}
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}
