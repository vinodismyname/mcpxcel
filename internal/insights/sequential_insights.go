package insights

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodismyname/mcpxcel/internal/runtime"
)

// Input schema for the generalized sequential_insights tool (reference-inspired).
// It tracks the LLM's thinking steps without domain heuristics.
type SequentialInsightsInput struct {
    Thought           string `json:"thought" validate:"required" jsonschema_description:"Your current thinking step"`
    NextThoughtNeeded bool   `json:"next_thought_needed" jsonschema_description:"Whether another thought step is needed"`
    ThoughtNumber     int    `json:"thought_number" validate:"min=1" jsonschema_description:"Current thought number (>=1)"`
    TotalThoughts     int    `json:"total_thoughts" validate:"min=1" jsonschema_description:"Estimated total thoughts needed (>=1)"`

	IsRevision        bool   `json:"is_revision,omitempty"`
	RevisesThought    int    `json:"revises_thought,omitempty"`
	BranchFromThought int    `json:"branch_from_thought,omitempty"`
	BranchID          string `json:"branch_id,omitempty"`
	NeedsMoreThoughts bool   `json:"needs_more_thoughts,omitempty"`

	// Sessions & flags
    SessionID          string `json:"session_id,omitempty" jsonschema_description:"Optional session identifier to resume in-memory planning state"`
	ResetSession       bool   `json:"reset_session,omitempty" jsonschema_description:"When true, reset the session referenced by session_id"`
	ShowAvailableTools bool   `json:"show_available_tools,omitempty" jsonschema_description:"When true, include the available tool catalog in text output"`
}

// InsightCard is a compact, optional planning card.
type InsightCard struct {
	Title       string   `json:"title"`
	Finding     string   `json:"finding"`
	Evidence    []string `json:"evidence,omitempty"`
	Assumptions []string `json:"assumptions,omitempty"`
	NextAction  string   `json:"next_action,omitempty"`
}

// PlannerMeta returns effective limits and flags indicating whether compute is enabled.
type PlannerMeta struct {
	Limits       runtime.Limits `json:"limits"`
	PlanningOnly bool           `json:"planning_only"`
	Truncated    bool           `json:"truncated"`
}

// Output schema for the generalized sequential_insights tool.
type SequentialInsightsOutput struct {
	// Echo/loop fields
	ThoughtNumber     int    `json:"thought_number"`
	TotalThoughts     int    `json:"total_thoughts"`
	NextThoughtNeeded bool   `json:"next_thought_needed"`
	SessionID         string `json:"session_id"`

	// Minimal state summary
	Branches             []string      `json:"branches,omitempty"`
	ThoughtHistoryLength int           `json:"thought_history_length"`
	InsightCards         []InsightCard `json:"insight_cards,omitempty"`
	Meta                 PlannerMeta   `json:"meta"`
}

// Planner encapsulates runtime limits and the in-memory session store.
type Planner struct {
	Limits   runtime.Limits
	Sessions *SessionStore
}

// Plan records the thought into a session and returns updated loop state.
func (p *Planner) Plan(ctx context.Context, in SequentialInsightsInput) (SequentialInsightsOutput, error) {
	var out SequentialInsightsOutput
	// Always planning-only and always emit a minimal planning card.
	out.Meta = PlannerMeta{Limits: p.Limits, PlanningOnly: true, Truncated: false}

	// Basic validation (mirrors reference expectations)
	if strings.TrimSpace(in.Thought) == "" || in.ThoughtNumber <= 0 || in.TotalThoughts <= 0 {
		return out, fmt.Errorf("VALIDATION: thought, thought_number>=1, total_thoughts>=1 are required")
	}

	// Resolve session (create or resume)
	if p.Sessions == nil {
		p.Sessions = NewSessionStore(20)
	}
	var sess *Session
	var ok bool
	if strings.TrimSpace(in.SessionID) != "" {
		if in.ResetSession {
			sess = p.Sessions.Reset(in.SessionID)
		} else if sess, ok = p.Sessions.Get(in.SessionID); !ok {
			// Resume failed; create a fresh session with the requested ID
			sess = p.Sessions.Reset(in.SessionID)
		}
	} else {
		sess = p.Sessions.NewSession()
	}

	// Reference behavior: if thought_number > total_thoughts, total := thought_number
	total := in.TotalThoughts
	if in.ThoughtNumber > total {
		total = in.ThoughtNumber
	}

	// Append current thought
	p.Sessions.AppendThought(sess, Thought{
		Thought:           in.Thought,
		ThoughtNumber:     in.ThoughtNumber,
		TotalThoughts:     total,
		NextThoughtNeeded: in.NextThoughtNeeded,
		IsRevision:        in.IsRevision,
		RevisesThought:    in.RevisesThought,
		BranchFromThought: in.BranchFromThought,
		BranchID:          in.BranchID,
		NeedsMoreThoughts: in.NeedsMoreThoughts,
	})

	// Build branches list
	var branches []string
	for k := range sess.Branches {
		branches = append(branches, k)
	}

	// Always include a tiny planning card with stronger interleaving cues.
	out.InsightCards = append(out.InsightCards, InsightCard{
		Title:      "Planning Context",
		Finding:    fmt.Sprintf("Thought %d/%d accepted", in.ThoughtNumber, total),
		NextAction: "After each domain tool call, summarize here, then call your next tool; set next_thought_needed accordingly.",
	})

	out.ThoughtNumber = in.ThoughtNumber
	out.TotalThoughts = total
	out.NextThoughtNeeded = in.NextThoughtNeeded
	out.SessionID = sess.ID
	out.ThoughtHistoryLength = len(sess.Thoughts)
	out.Branches = branches
	return out, nil
}
