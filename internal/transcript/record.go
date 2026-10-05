package transcript

import "slices"

// Record is one native transcript record decoded by an agent adapter. Records
// without Text or tool blocks stay in a projection (Claude keeps them as graph
// nodes) but never reach Chat. ParentID is empty for linear transcripts.
//
// A projection also returns Role "tool" items: one per tool call, carrying
// Tool. Calls and Results are the decoded tool blocks of a native record and
// never reach Chat themselves.
type Record struct {
	ID        string
	ParentID  string
	Role      string
	Text      string
	Timestamp string
	// Turn names the agent API message the record belongs to (Claude
	// message.id); records of one turn are written together.
	Turn    string
	Calls   []ToolCall
	Results []ToolResult
	Tool    Tool
}

// Equal reports whether two decodings of a record are the same.
func (r Record) Equal(o Record) bool {
	return r.ID == o.ID && r.ParentID == o.ParentID && r.Role == o.Role && r.Text == o.Text &&
		r.Timestamp == o.Timestamp && r.Turn == o.Turn && r.Tool == o.Tool &&
		slices.Equal(r.Calls, o.Calls) && slices.Equal(r.Results, o.Results)
}

// ToolCall is a tool_use block. Input is display text, already capped.
type ToolCall struct {
	ID             string
	Name           string
	Summary        string
	Input          string
	InputTruncated bool
}

// ToolResult is a tool_result block. Text is display text, already capped.
type ToolResult struct {
	ToolUseID string
	Text      string
	Truncated bool
	Error     bool
}

// Tool states. A call is running until its result arrives, or unknown once the
// transcript moved on without one (an interrupted or abandoned call).
const (
	ToolRunning   = "running"
	ToolCompleted = "completed"
	ToolError     = "error"
	ToolUnknown   = "unknown"
)

// Tool is a call paired with its result as one Chat item.
type Tool struct {
	ID              string
	Name            string
	Summary         string
	State           string
	Input           string
	InputTruncated  bool
	Result          string
	ResultTruncated bool
}
