// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package uidaemon

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// LooksLikeReactDocument reports whether body is the product React
// cockpit rather than the frozen vanilla reference.
func LooksLikeReactDocument(body string) bool {
	if strings.Contains(body, "DEPRECATED REFERENCE") {
		return false
	}
	return strings.Contains(body, `id="root"`)
}

// ProbeResult is the React-surface check the LaunchAgent runs.
type ProbeResult struct {
	OK     bool
	Status int
	Reason string
}

// ProbeReact fetches url and checks the document is React.
func ProbeReact(client *http.Client, url string) ProbeResult {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Get(url)
	if err != nil {
		return ProbeResult{Reason: "unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ProbeResult{Status: resp.StatusCode, Reason: "read: " + err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return ProbeResult{Status: resp.StatusCode, Reason: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	body := string(raw)
	if !LooksLikeReactDocument(body) {
		return ProbeResult{Status: resp.StatusCode, Reason: "GET / is not the React cockpit"}
	}
	return ProbeResult{OK: true, Status: resp.StatusCode}
}

// DailyReactURL is the probe target for the owner's daily surface.
func DailyReactURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", config.DailyPort)
}
