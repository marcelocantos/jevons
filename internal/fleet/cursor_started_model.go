// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/marcelocantos/claudia"
)

// The model an unpinned Cursor process started on. cli-config.json is read
// once, immediately before launch, and kept here with that process's pid.
// The badge reads it back. Nothing writes AgentDef.Model and nothing adds
// --model: the process still takes the CLI default itself.

const cursorStartedModelFile = "started-model.json"

type cursorStartedModelJSON struct {
	Model string `json:"model"`
	PID   int    `json:"pid"`
}

type cursorCLIConfigJSON struct {
	Model struct {
		ModelID string `json:"modelId"`
	} `json:"model"`
	SelectedModel struct {
		ModelID string `json:"modelId"`
	} `json:"selectedModel"`
}

var cursorStartedModelMu sync.Mutex

func cursorStartedModelPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".jevons", cursorStartedModelFile)
}

// cursorCLIDefaultModel is the model id in ~/.cursor/cli-config.json.
// model.modelId is the default a process with no --model starts on.
// selectedModel is used only when that field is absent. Empty if the
// file cannot be read.
func cursorCLIDefaultModel() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".cursor", "cli-config.json"))
	if err != nil {
		return ""
	}
	var cfg cursorCLIConfigJSON
	if err := json.Unmarshal(b, &cfg); err != nil {
		return ""
	}
	if id := strings.TrimSpace(cfg.Model.ModelID); id != "" {
		return id
	}
	return strings.TrimSpace(cfg.SelectedModel.ModelID)
}

func loadCursorStartedModels() map[string]cursorStartedModelJSON {
	out := map[string]cursorStartedModelJSON{}
	path := cursorStartedModelPath()
	if path == "" {
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func saveCursorStartedModels(all map[string]cursorStartedModelJSON) error {
	path := cursorStartedModelPath()
	if path == "" {
		return errors.New("cursor started model: no home")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveCursorStartedModel records that sessionID's process pid started on
// model. It does not touch the registry.
func SaveCursorStartedModel(sessionID string, pid int, model string) error {
	sessionID = strings.TrimSpace(sessionID)
	model = strings.TrimSpace(model)
	if sessionID == "" || pid <= 0 || model == "" {
		return errors.New("cursor started model: session, pid, and model are required")
	}
	cursorStartedModelMu.Lock()
	defer cursorStartedModelMu.Unlock()
	all := loadCursorStartedModels()
	all[sessionID] = cursorStartedModelJSON{Model: model, PID: pid}
	return saveCursorStartedModels(all)
}

// CursorStartedModel is the model captured for sessionID and the pid of
// the process that captured it. ok is false when this session has no record.
func CursorStartedModel(sessionID string) (model string, pid int, ok bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", 0, false
	}
	cursorStartedModelMu.Lock()
	defer cursorStartedModelMu.Unlock()
	rec, found := loadCursorStartedModels()[sessionID]
	if !found {
		return "", 0, false
	}
	rec.Model = strings.TrimSpace(rec.Model)
	if rec.Model == "" || rec.PID <= 0 {
		return "", 0, false
	}
	return rec.Model, rec.PID, true
}

// CursorStartLook is the CLI default read before an unpinned Cursor launch.
// applicable is false when this start is not that case. Record after the
// launch writes the look only if a new process came up.
type CursorStartLook struct {
	applicable bool
	wasAlive   bool
	beforePID  int
	model      string
}

// LookCursorStart reads the CLI default when name is a Cursor seat with an
// empty pin that is not already running. The read happens before launch.
func LookCursorStart(reg *claudia.Registry, name string) CursorStartLook {
	if reg == nil {
		return CursorStartLook{}
	}
	def := reg.Def(name)
	if def == nil || def.Provider != claudia.ProviderCursor || strings.TrimSpace(def.Model) != "" {
		return CursorStartLook{}
	}
	look := CursorStartLook{applicable: true}
	if proc := reg.Get(name); proc != nil && proc.Alive() {
		look.wasAlive = true
		look.beforePID = proc.PID()
		if look.beforePID <= 0 {
			look.beforePID = def.ConnectPID
		}
		return look
	}
	look.model = cursorCLIDefaultModel()
	return look
}

// Record writes started-model.json for the process Launch just started:
// the model id read before Launch, and that process's ConnectPID. An
// adopt of the same process does not write. A pin on the def does not
// write. AgentDef.Model is not set.
func (look CursorStartLook) Record(reg *claudia.Registry, name string) {
	look.RecordAgent(reg, name, 0)
}

// RecordAgent is Record, using pid when the registry has not stored a
// ConnectPID yet. Boot's AdoptOrLaunch returns the process before the
// def's connect pid is published.
func (look CursorStartLook) RecordAgent(reg *claudia.Registry, name string, pid int) {
	if reg == nil || !look.applicable || look.model == "" {
		return
	}
	def := reg.Def(name)
	if def == nil || strings.TrimSpace(def.Model) != "" || def.SessionID == "" {
		return
	}
	if def.ConnectPID > 0 {
		pid = def.ConnectPID
	}
	if look.wasAlive && pid == look.beforePID {
		return
	}
	if pid <= 0 {
		return
	}
	if err := SaveCursorStartedModel(def.SessionID, pid, look.model); err != nil {
		slog.Warn("cursor started model not recorded", "name", name, "session", def.SessionID, "err", err)
	}
}

// LaunchRecording is registry.Launch, and when the seat is an unpinned
// Cursor seat it remembers the CLI default from just before the launch.
func LaunchRecording(reg *claudia.Registry, name string) (*claudia.Agent, error) {
	if reg == nil {
		return nil, errors.New("launch: no agent registry")
	}
	look := LookCursorStart(reg, name)
	agent, err := reg.Launch(name)
	if err != nil {
		return nil, err
	}
	look.Record(reg, name)
	return agent, nil
}
