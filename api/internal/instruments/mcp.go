package instruments

import (
	"encoding/json"
	"sort"
	"strings"
)

// parseServers reads the names and the transports of .mcp.json. The entries
// hold environment, arguments and possibly addresses with credentials: they
// are decoded only as far as telling the transport, and neither their values
// nor a decoder's message about them go any further.
func parseServers(r *reading, raw []byte) []MCPServer {
	source := sourceName(infraRepo, mcpConfigPath)
	servers := []MCPServer{}
	var doc struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		r.malformed(source, "the file is not valid JSON")
		return servers
	}
	if len(doc.Servers) == 0 {
		r.malformed(source, "the file declares no mcpServers")
	}
	for name, entry := range doc.Servers {
		transport := serverTransport(entry)
		if transport == "" {
			r.malformed(source+"#"+name, "the transport could not be told")
		}
		servers = append(servers, MCPServer{Name: name, Transport: transport})
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return servers
}

func serverTransport(entry json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(entry, &fields) != nil {
		return ""
	}
	if raw, ok := fields["type"]; ok {
		var kind string
		if json.Unmarshal(raw, &kind) != nil {
			return ""
		}
		switch kind {
		case "stdio", "http", "sse", "ws", "streamable-http":
			return kind
		}
		return "" // an unknown value is not repeated: it is not known to be harmless
	}
	if _, ok := fields["command"]; ok {
		return "stdio"
	}
	if _, ok := fields["url"]; ok {
		return "http"
	}
	return ""
}

// readTools reads the tools of the platform server from its TypeScript source.
func readTools(r *reading, platform repoFiles) []MCPTool {
	tools := []MCPTool{}
	if platform.err != nil {
		r.failed(sourceName(platformRepo, mcpSourceDir), platform.err)
		return tools
	}
	sources := map[string]string{}
	for _, p := range childFiles(platform.files, mcpSourceDir) {
		if !strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".test.ts") {
			continue
		}
		if raw, ok := r.file(platform, p); ok {
			sources[p] = string(raw)
		}
	}
	found, problems := scanTools(sources)
	for _, p := range problems {
		r.malformed(sourceName(platformRepo, p.at), p.problem)
	}
	if len(found) == 0 && r.complete {
		r.malformed(sourceName(platformRepo, mcpSourceDir), "no tool registration found")
	}
	return append(tools, found...)
}
