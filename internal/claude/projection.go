package claude

import (
	"errors"
	"slices"

	"github.com/psw7205/herdr-remote/internal/transcript"
)

type Projection struct {
	nodes map[string]Record
	leaf  string
}

func NewProjection() *Projection { return &Projection{nodes: make(map[string]Record)} }
func (p *Projection) Add(r Record) error {
	if r.ID == "" {
		return nil
	}
	if prior, ok := p.nodes[r.ID]; ok {
		if !prior.Equal(r) {
			return errors.New("conflicting transcript UUID requires resync")
		}
		return nil
	}
	seen := map[string]bool{r.ID: true}
	for id := r.ParentID; id != ""; id = p.nodes[id].ParentID {
		if seen[id] {
			return errors.New("transcript parent cycle")
		}
		seen[id] = true
	}
	p.nodes[r.ID] = r
	p.leaf = r.ID
	return nil
}

// Messages returns the current branch: the parent chain of the newest record.
// Text records become messages and every tool call a Role "tool" item at its
// place, paired with a result on the same chain. Claude Code starts the next
// API message, or reads the next prompt, only after writing every result, so a
// call still without one when the chain has moved on will never get one.
func (p *Projection) Messages() []Record {
	var chain []Record
	for id := p.leaf; id != ""; {
		r, ok := p.nodes[id]
		if !ok {
			break
		}
		chain = append(chain, r)
		id = r.ParentID
	}
	slices.Reverse(chain)
	results := make(map[string]transcript.ToolResult)
	lastPrompt, lastAssistant := -1, -1
	for i, r := range chain {
		for _, result := range r.Results {
			if _, ok := results[result.ToolUseID]; !ok {
				results[result.ToolUseID] = result
			}
		}
		switch {
		case r.Role == "user" && r.Text != "" && len(r.Results) == 0:
			lastPrompt = i
		case r.Role == "assistant":
			lastAssistant = i
		}
	}
	out := []Record{}
	for i, r := range chain {
		if r.Text != "" {
			out = append(out, r)
		}
		movedOn := lastPrompt > i || lastAssistant > i && (r.Turn == "" || chain[lastAssistant].Turn != r.Turn)
		for _, call := range r.Calls {
			tool := transcript.Tool{ID: call.ID, Name: call.Name, Summary: call.Summary, State: transcript.ToolRunning, Input: call.Input, InputTruncated: call.InputTruncated}
			if result, ok := results[call.ID]; ok {
				tool.State, tool.Result, tool.ResultTruncated = transcript.ToolCompleted, result.Text, result.Truncated
				if result.Error {
					tool.State = transcript.ToolError
				}
			} else if movedOn {
				tool.State = transcript.ToolUnknown
			}
			out = append(out, Record{ID: r.ID + "/" + call.ID, ParentID: r.ID, Role: "tool", Timestamp: r.Timestamp, Turn: r.Turn, Tool: tool})
		}
	}
	return out
}
