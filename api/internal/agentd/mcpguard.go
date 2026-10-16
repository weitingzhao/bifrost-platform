package agentd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MCP isolation, failing closed (review 163 A8). A task's agent must not get
// the host user's MCP servers (the platform's tools among them). Neither
// codex nor cursor has a switch that clears them: codex's -c mcp_servers={} does
// not (measured 10-14: it still lists the user's servers), and cursor loads
// ~/.cursor/mcp.json whatever its flags. So agentd offers these two executors
// only on a host whose user has no MCP server that executor would load, and
// never offers one it could not check. Claude clears MCP configuration itself.

// MCPCheck counts the MCP servers an executor would load for this user.
type MCPCheck func() (int, error)

// CodexMCP counts the enabled servers codex lists for this user.
func CodexMCP() (int, error) {
	// The probe gets no more of agentd's environment than a task does (review
	// 196 A1): a secret of the host must not reach codex this way either.
	out, err := helper("codex", "mcp", "list", "--json").Output()
	if err != nil {
		return 0, fmt.Errorf("codex mcp list: %w", err)
	}
	var list []struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return 0, fmt.Errorf("codex mcp list: %w", err)
	}
	n := 0
	for _, s := range list {
		if s.Enabled {
			n++
		}
	}
	return n, nil
}

// CursorMCP counts the servers in the user's ~/.cursor/mcp.json, which
// cursor loads without asking (no file: none).
func CursorMCP() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var cfg struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return 0, fmt.Errorf("~/.cursor/mcp.json: %w", err)
	}
	return len(cfg.Servers), nil
}

// MCPChecks are the checks of the executors that need one.
// Claude needs no check: --strict-mcp-config with an empty --mcp-config
// excludes the host user's servers on every run.
var MCPChecks = map[string]MCPCheck{"exec:codex": CodexMCP, "exec:cursor": CursorMCP}

// Isolated keeps the executors whose MCP check finds nothing, and says why
// each other one is not offered.
func Isolated(executors map[string]Executor, checks map[string]MCPCheck) (map[string]Executor, []string) {
	kept := map[string]Executor{}
	var why []string
	for name, e := range executors {
		check, needs := checks[name]
		if !needs {
			kept[name] = e
			continue
		}
		n, err := check()
		switch {
		case err != nil:
			why = append(why, fmt.Sprintf("%s not offered: its MCP servers cannot be checked: %v", name, err))
		case n > 0:
			why = append(why, fmt.Sprintf("%s not offered: this user has %d MCP server(s) it would load", name, n))
		default:
			kept[name] = e
		}
	}
	return kept, why
}
