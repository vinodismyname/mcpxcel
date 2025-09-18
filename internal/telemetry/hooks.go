package telemetry

import (
	"time"

	"github.com/rs/zerolog"
)

// Hooks implements mcp-go server lifecycle callbacks for basic telemetry and logging.
// It is intentionally minimal; metrics backends can be added later under this package.
type Hooks struct {
	logger zerolog.Logger
}

// NewHooks constructs a Hooks instance with the provided logger.
func NewHooks(logger zerolog.Logger) *Hooks {
	return &Hooks{logger: logger}
}

// OnServerStart is called when the server begins accepting connections.
func (h *Hooks) OnServerStart() {
	h.logger.Info().Msg("MCP server starting")
}

// OnServerStop is called during server shutdown.
func (h *Hooks) OnServerStop() {
	h.logger.Info().Msg("MCP server stopping")
}

// OnSessionStart records the start of a client session.
func (h *Hooks) OnSessionStart(sessionID string) {
	h.logger.Info().Str("session_id", sessionID).Msg("session started")
}

// OnSessionEnd records the end of a client session.
func (h *Hooks) OnSessionEnd(sessionID string) {
	h.logger.Info().Str("session_id", sessionID).Msg("session ended")
}

// OnToolCall logs tool invocations and their outcomes.
func (h *Hooks) OnToolCall(sessionID, toolName string, duration time.Duration, err error) {
	evt := h.logger.Info().Str("session_id", sessionID).Str("tool", toolName).Dur("duration", duration)
	if err != nil {
		h.logger.Error().Str("session_id", sessionID).Str("tool", toolName).Dur("duration", duration).Err(err).Msg("tool call error")
		return
	}
	evt.Msg("tool call completed")
}

// OnResourceRead logs resource reads and their outcomes.
func (h *Hooks) OnResourceRead(sessionID, uri string, duration time.Duration, err error) {
	evt := h.logger.Info().Str("session_id", sessionID).Str("uri", uri).Dur("duration", duration)
	if err != nil {
		h.logger.Error().Str("session_id", sessionID).Str("uri", uri).Dur("duration", duration).Err(err).Msg("resource read error")
		return
	}
	evt.Msg("resource read completed")
}
