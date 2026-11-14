package protocol

import "encoding/json"

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
)
