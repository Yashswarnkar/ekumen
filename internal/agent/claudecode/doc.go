// Package claudecode is the Claude Code adapter of C-adapter v1. Managed
// sessions are Claude Code background sessions (D-session-delivery-bg);
// status comes from `claude agents --json`, refined by per-session hook
// records when hooks run (D-claude-status-source). It drives the user's own
// installed `claude` and never touches credentials (D-claude-auth). See
// README.md for the verified versions, capabilities and status mapping.
package claudecode
