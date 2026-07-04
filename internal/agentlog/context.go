package agentlog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// contextReadWindow bounds how many trailing bytes of a session's newest
// JSONL file CurrentContextTokens reads to find the most recent usage
// snapshot — one seek + one bounded read per agent per heartbeat tick, not a
// full-file scan. Var (not const) so tests can shrink it to exercise the
// mid-file seek path without constructing multi-hundred-KB fixtures.
var contextReadWindow int64 = 256 * 1024

// contextEntry is the subset of a Claude Code JSONL line needed to find the
// newest main-chain assistant usage snapshot. Distinct from ccEntry in
// claudecode.go: this needs IsSidechain, which ccEntry does not carry
// (parseClaudeCodeLine has no need to distinguish sidechain turns).
type contextEntry struct {
	Type        string     `json:"type"`
	IsSidechain bool       `json:"isSidechain"`
	Message     *ccMessage `json:"message,omitempty"`
}

// ProjectDirFor returns the Claude Code project directory for workDir under
// the given Claude config directory: <configDir>/projects/<hash>, where hash
// is the absolute workDir with '/' replaced by '-' (matching Claude Code's
// own scheme; see claudeProjectDirFor for the Windows drive-letter handling
// this shares).
//
// configDir defaults to "$HOME/.claude" when empty — Claude Code's own
// default CLAUDE_CONFIG_DIR. Every Gas Town role runs Claude Code under its
// own config dir (deacon -> ~/.claude-deacon, witness -> ~/.claude-witness,
// ...), so callers monitoring a specific role's session must pass that
// role's resolved CLAUDE_CONFIG_DIR rather than relying on the default.
func ProjectDirFor(configDir, workDir string) (string, error) {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home dir: %w", err)
		}
		configDir = filepath.Join(home, ".claude")
	}

	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}
	// Normalize to forward slashes and strip a Windows drive letter, matching
	// claudeProjectDirFor's cross-platform handling.
	normalized := filepath.ToSlash(abs)
	if len(normalized) >= 2 && normalized[1] == ':' {
		normalized = normalized[2:]
	}
	hash := strings.ReplaceAll(normalized, "/", "-")
	return filepath.Join(configDir, "projects", hash), nil
}

// CurrentContextTokens returns a SNAPSHOT of the current context size for
// the Claude Code session in projectDir: the token accounting
// (input + output + cache_read + cache_creation) of the newest main-chain
// (non-sidechain) assistant turn's usage block.
//
// This is deliberately a snapshot, not a running sum. cache_read_input_tokens
// grows monotonically because every turn re-reads the entire conversation
// cache — the newest usage event already reflects the full current context,
// so summing across events wildly overcounts. Two corrections on top of the
// raw JSONL:
//  1. Claude Code writes one JSONL line per content block; a turn with
//     multiple content blocks duplicates the same usage object across those
//     lines. Harmless here since we take the newest matching line, not a
//     sum — but fatal to a naive per-line sum.
//  2. Task-tool subagents append "isSidechain":true entries to the same
//     file. These must be skipped, or a subagent turn's (usually much
//     smaller) usage masks the parent session's real context size.
//
// since filters to the newest *.jsonl file modified at or after since — pass
// the owning tmux session's creation time so a stale pre-respawn file is
// never read; after a respawn, the newest file self-corrects to the fresh
// session's small context.
//
// Returns an error if no qualifying file exists or no matching usage event
// is found. Callers should skip the tick on error, not treat it as zero —
// zero would misreport an unreadable session as safely under threshold.
func CurrentContextTokens(projectDir string, since time.Time) (int, error) {
	path, ok := newestJSONLIn(projectDir, since)
	if !ok {
		return 0, fmt.Errorf("no JSONL file in %s modified since %s", projectDir, since)
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}

	var offset int64
	if info.Size() > contextReadWindow {
		offset = info.Size() - contextReadWindow
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seeking %s: %w", path, err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	if offset > 0 && len(lines) > 0 {
		// The read window started mid-file, so the first line is a
		// fragment (we seeked into the middle of it) — discard it.
		lines = lines[1:]
	}

	// Scan backward: the newest matching entry IS the current context — see
	// the snapshot-not-sum rationale above. A parse failure (e.g. the final
	// line was mid-write when we read it) or a non-matching entry (wrong
	// type, sidechain, no usage) just falls through to the next older line.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var entry contextEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" || entry.IsSidechain || entry.Message == nil || entry.Message.Usage == nil {
			continue
		}
		u := entry.Message.Usage
		total := u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
		if total == 0 {
			// A trailing usage event with all-zero token fields is a synthetic
			// turn — Claude Code emits one for server_tool_use (web search/fetch)
			// and interrupted/empty turns. It does NOT reflect the session's real
			// context, so skip it and fall through to the most recent event that
			// carries a real snapshot. Returning 0 here would make the watchdog
			// read a loaded agent as empty and never fire — the exact idle-wedge
			// this feature exists to prevent, defeated by a tool-use turn landing
			// last. Verified against a live witness JSONL (gfork-47p.2 E2E): the
			// newest event was a zero-token web-search turn masking a 45k context.
			continue
		}
		return total, nil
	}

	return 0, fmt.Errorf("no main-chain assistant usage event with non-zero tokens found in %s", path)
}
