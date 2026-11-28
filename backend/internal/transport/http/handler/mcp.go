package handler

// Minimal MCP server over Streamable HTTP: stateless, tools only, plain JSON responses.
// shortcut: no SSE, sessions, resources or prompts; switch to the official Go SDK if any of those are needed.

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"findus/backend/internal/transport/http/middleware"
)

const maxMCPRequestBytes = 48 << 20 // a 32 MiB attachment is about 43 MiB as base64

var mcpProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// mcpAuth resolves the token (X-API-Key or Authorization: Bearer, clients differ in what they can send) to an active user and returns a ctx carrying their rights plus the enabled scopes.
func (s *Server) mcpAuth(r *http.Request) (context.Context, []string, bool) {
	token := r.Header.Get("X-API-Key")
	if token == "" {
		if scheme, t, _ := strings.Cut(r.Header.Get("Authorization"), " "); strings.EqualFold(scheme, "Bearer") {
			token = t
		}
	}
	if !strings.HasPrefix(token, mcpTokenPrefix) {
		return nil, nil, false
	}
	a, err := s.MCP.GetByTokenHash(r.Context(), hashMCPToken(token))
	if err != nil {
		return nil, nil, false
	}
	u, err := s.Users.GetByID(r.Context(), a.UserID)
	if err != nil || !u.IsActive {
		return nil, nil, false
	}
	ctx := middleware.WithUser(r.Context(), u)
	perms, err := s.Groups.EffectivePermissions(ctx, u.ID)
	if err != nil {
		perms = nil
	}
	ctx = middleware.WithPermissions(ctx, perms)
	var scopes []string
	for _, sc := range a.Scopes {
		if mcpScopeAllowed(ctx, sc) {
			scopes = append(scopes, sc)
		}
	}
	return ctx, scopes, true
}

// MCPHandle serves POST /mcp.
func (s *Server) MCPHandle(w http.ResponseWriter, r *http.Request) {
	ctx, scopes, ok := s.mcpAuth(r)
	if !ok {
		s.writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req mcpRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMCPRequestBytes)).Decode(&req); err != nil {
		s.writeMCPError(w, nil, -32700, "parse error")
		return
	}
	// Notifications and client responses expect no body.
	if len(req.ID) == 0 || req.Method == "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := mcpProtocolVersions[0]
		if slices.Contains(mcpProtocolVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "findus", "version": "1.0.0"},
			"instructions":    "Findus is a home inventory: items live in nested locations and can carry labels. Only the tools the user enabled are available.",
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		tools := []mcpTool{}
		for _, t := range s.mcpTools() {
			if slices.Contains(scopes, t.scope) {
				tools = append(tools, t)
			}
		}
		result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.writeMCPError(w, req.ID, -32602, "invalid params")
			return
		}
		tools := s.mcpTools()
		i := slices.IndexFunc(tools, func(t mcpTool) bool { return t.Name == p.Name && slices.Contains(scopes, t.scope) })
		if i < 0 {
			s.writeMCPError(w, req.ID, -32602, "unknown or disabled tool: "+p.Name)
			return
		}
		result = s.runMCPTool(ctx, tools[i], scopes, p.Arguments)
	default:
		s.writeMCPError(w, req.ID, -32601, "method not found")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func (s *Server) writeMCPError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg},
	})
}
