//go:build sibling_claudia

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/marcelocantos/claudia"
)

// smokeLaunchModels are bundled pi-catalog ids the sidecar will accept
// on load. One per subscription provider (🎯T865).
var smokeLaunchModels = []struct {
	provider claudia.Provider
	model    string
}{
	{claudia.Provider("anthropic"), "claude-haiku-4-5"},
	{claudia.Provider("openai-codex"), "gpt-5.5"},
	{claudia.ProviderCursor, "composer-1.5"},
	{claudia.Provider("xai-oauth"), "grok-4.6"},
}

func smoke(args []string) error {
	fs := flag.NewFlagSet("smoke", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := claudia.OpenOMPPlans(ctx); err != nil {
		return err
	}
	defer func() {
		if err := claudia.FlushOMPPlans(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "jevons-broker: flush:", err)
		}
	}()
	sawTool := false
	claudia.SetOMPToolExec(func(name, _, _ string) string {
		sawTool = true
		return "t865-smoke " + name
	})
	if _, err := claudia.EnsureOMPSidecar(ctx); err != nil {
		return err
	}
	n := 0
	for _, row := range smokeLaunchModels {
		if len(fs.Args()) > 0 && !containsID(fs.Args(), string(row.provider)) {
			continue
		}
		name := "t865-smoke-" + string(row.provider)
		agent, err := claudia.StartDirectContext(ctx, claudia.Config{
			Provider:    row.provider,
			Name:        name,
			Model:       row.model,
			WorkDir:     os.TempDir(),
			TermLogPath: "-",
		})
		if err != nil {
			fmt.Printf("launch %s failed: %v\n", row.provider, err)
			continue
		}
		if err := agent.Send("Reply with the single word pong. Do not call tools."); err != nil {
			agent.Stop()
			return fmt.Errorf("send %s: %w", row.provider, err)
		}
		if _, err := agent.Steer("keep it to one word"); err != nil {
			agent.Stop()
			return fmt.Errorf("steer %s: %w", row.provider, err)
		}
		if err := agent.Interrupt(); err != nil {
			agent.Stop()
			return fmt.Errorf("abort %s: %w", row.provider, err)
		}
		agent.Stop()
		fmt.Printf("ok %s launch send steer abort\n", row.provider)
		n++
	}
	if n == 0 {
		return fmt.Errorf("no subscription seat launched; Keychain login or refresh is required")
	}
	if sawTool {
		fmt.Println("ok jevons_*")
	} else {
		fmt.Println("ok jevons_* callback armed")
	}
	return nil
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
