package insights

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vinodismyname/mcpxcel/internal/runtime"
)

func TestPlanner_NewSessionThought(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	p := &Planner{Limits: limits, Sessions: NewSessionStore(10)}

	in := SequentialInsightsInput{
		Thought:           "First step",
		ThoughtNumber:     1,
		TotalThoughts:     3,
		NextThoughtNeeded: true,
	}
	out, err := p.Plan(context.Background(), in)
	require.NoError(t, err)
	require.NotEmpty(t, out.SessionID)
	require.Equal(t, 1, out.ThoughtNumber)
	require.Equal(t, 3, out.TotalThoughts)
	require.True(t, out.NextThoughtNeeded)
	require.Equal(t, 1, out.ThoughtHistoryLength)
}

func TestPlanner_TotalAutoAdjust(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	p := &Planner{Limits: limits, Sessions: NewSessionStore(10)}

	out, err := p.Plan(context.Background(), SequentialInsightsInput{
		Thought:           "Late step",
		ThoughtNumber:     5,
		TotalThoughts:     3,
		NextThoughtNeeded: true,
	})
	require.NoError(t, err)
	require.Equal(t, 5, out.TotalThoughts)
}

func TestPlanner_BranchRecording(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	p := &Planner{Limits: limits, Sessions: NewSessionStore(10)}

	// First call creates a session
	first, err := p.Plan(context.Background(), SequentialInsightsInput{
		Thought:           "Base",
		ThoughtNumber:     1,
		TotalThoughts:     3,
		NextThoughtNeeded: true,
	})
	require.NoError(t, err)
	// Second call with a branch
	second, err := p.Plan(context.Background(), SequentialInsightsInput{
		SessionID:         first.SessionID,
		Thought:           "Branch step",
		ThoughtNumber:     2,
		TotalThoughts:     3,
		NextThoughtNeeded: true,
		BranchFromThought: 1,
		BranchID:          "A",
	})
	require.NoError(t, err)
	require.Equal(t, 2, second.ThoughtHistoryLength)
	found := false
	for _, b := range second.Branches {
		if b == "A" {
			found = true
			break
		}
	}
	require.True(t, found, "expected branch id A recorded")
}
