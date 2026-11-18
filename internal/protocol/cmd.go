package protocol

import (
	"encoding/json"
	"time"
)

// Command and stream message types (server <-> agent).
const (
	TypeCmd          = "cmd"
	TypeCmdResult    = "cmd.result"
	TypeStreamOpen   = "stream.open"
	TypeStreamData   = "stream.data"
	TypeStreamInput  = "stream.input"
	TypeStreamResize = "stream.resize"
	TypeStreamClose  = "stream.close"
)

// Cmd asks an agent module to perform one action and answer with CmdResult.
type Cmd struct {
	ID     string          `json:"id"`
	Module string          `json:"module"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
	// Dangerous is set by the server only after the administrator confirmed; agents refuse
	// dangerous actions without it.
	Dangerous bool `json:"dangerous,omitempty"`
}

// CmdResult answers a Cmd.
type CmdResult struct {
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// StreamOpen starts a long-running bidirectional stream (terminal, logs, apt output).
type StreamOpen struct {
	SID    string          `json:"sid"`
	Module string          `json:"module"`
	Params json.RawMessage `json:"params,omitempty"`
}

// StreamData carries output (agent -> server) or input (server -> agent) bytes.
type StreamData struct {
	SID  string `json:"sid"`
	Data []byte `json:"data"`
}

// StreamResize changes the terminal size.
type StreamResize struct {
	SID  string `json:"sid"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// StreamClose ends a stream in either direction.
type StreamClose struct {
	SID    string `json:"sid"`
	Reason string `json:"reason,omitempty"`
}

// Standard error strings in CmdResult.Error and StreamClose.Reason.
const (
	ErrModuleDisabled = "module disabled"
	ErrBusy           = "busy"
	ErrDangerous      = "dangerous action requires confirmation"
	ErrTooLarge       = "file too large"
	ErrBinary         = "binary file"
	ErrConflict       = "file changed"
	ErrExists         = "already exists"
	ErrNotFound       = "not found"
	ErrNoPending      = "no such pending change"
)

// ChangeWindow is how long a network or firewall change waits for confirmation before the agent rolls it back.
const ChangeWindow = 30 * time.Second

// TypeUpdateEvent reports the progress of an agent self-update to the server.
const TypeUpdateEvent = "update.event"

// AgentRollbackDelay is how long after arming the rollback timer the old binary is restored if the new agent
// has not confirmed itself. The agent must reconnect within 60 seconds; the extra time covers the restart.
const AgentRollbackDelay = 70 * time.Second

// AgentUpdateTimeout is how long an agent may take for the whole update command (download of the release, check,
// backup, replace). Other commands have a shorter limit of their own. The server waits a little longer than this.
const AgentUpdateTimeout = 10 * time.Minute

// Update event states. An empty state is an event of an agent older than the stages: "this step is reached".
const (
	UpdateRunning = "running"
	UpdateDone    = "done"
	UpdateFailed  = "failed"
)

// UpdateEvent is the payload of TypeUpdateEvent.
type UpdateEvent struct {
	Version string `json:"version"`
	Step    int    `json:"step"`  // 1..Total
	Total   int    `json:"total"` // 5
	ID      string `json:"id"`    // download | verify | backup | replace | restart | confirm
	Error   string `json:"error,omitempty"`
	// State is running, done or failed; Detail is a short note such as "12.3 / 18.4 MB".
	State  string `json:"state,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Bytes and Size describe the progress of a download (Size 0 or -1: not known).
	Bytes int64 `json:"bytes,omitempty"`
	Size  int64 `json:"size,omitempty"`
}
