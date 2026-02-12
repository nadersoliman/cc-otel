package main

import "time"

// HookInput represents the JSON received on stdin from Claude Code hooks.
type HookInput struct {
	SessionID      string                 `json:"session_id"`
	TranscriptPath string                 `json:"transcript_path"`
	CWD            string                 `json:"cwd"`
	HookEventName  string                 `json:"hook_event_name"`

	// PostToolUse fields
	ToolName     string                 `json:"tool_name,omitempty"`
	ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
	ToolResponse map[string]interface{} `json:"tool_response,omitempty"`
	ToolUseID    string                 `json:"tool_use_id,omitempty"`
}

// ToolSpanData is recorded by PostToolUse for later use by Stop.
type ToolSpanData struct {
	ToolName     string                 `json:"tool_name"`
	ToolUseID    string                 `json:"tool_use_id"`
	ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
	ToolResponse map[string]interface{} `json:"tool_response,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
}

// SessionState persists between hook invocations for one session.
type SessionState struct {
	SessionSpanID  string         `json:"session_span_id"`
	SessionStart   time.Time      `json:"session_start"`
	TranscriptPath string         `json:"transcript_path"`
	CWD            string         `json:"cwd"`
	LastLine       int            `json:"last_line"`
	TurnCount      int            `json:"turn_count"`
	ToolSpans      []ToolSpanData `json:"tool_spans"`
	Updated        time.Time      `json:"updated"`
}

// StateFile is the top-level structure persisted to disk.
type StateFile struct {
	Sessions map[string]*SessionState `json:"sessions"`
}

// Turn represents a parsed conversation turn from the transcript.
type Turn struct {
	Number              int
	UserText            string
	UserTimestamp        time.Time
	AssistantMessages   []map[string]interface{}
	ToolCalls           []ToolCall
	Model               string
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	StartTime           time.Time
	EndTime             time.Time
}

// ToolCall represents a tool_use block matched with its result.
type ToolCall struct {
	Name      string
	ID        string
	Input     interface{}
	Output    interface{}
	StartTime time.Time
	EndTime   time.Time
	Success   bool
}
