package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"findus/backend/internal/domain"
	"findus/backend/internal/transport/http/middleware"
)

const mcpTokenPrefix = "findus_mcp_"

func hashMCPToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// mcpScopeAllowed reports whether the user in ctx may use scope at all. A toggle never exceeds the user's own rights.
func mcpScopeAllowed(ctx context.Context, scope string) bool {
	if !slices.Contains(domain.MCPScopes, scope) {
		return false
	}
	resource, action, _ := strings.Cut(scope, ".")
	if action == "read" {
		return true
	}
	switch resource {
	case "items", "attachments":
		return middleware.Can(ctx, domain.PermItemsWrite)
	case "locations":
		return middleware.Can(ctx, domain.PermLocationsWrite)
	case "labels":
		return middleware.Can(ctx, domain.PermLabelsWrite)
	default: // templates are admin-only, as in the web app
		u, ok := middleware.User(ctx)
		return ok && u.Role.IsAdmin()
	}
}

func (s *Server) writeMCPSettings(w http.ResponseWriter, r *http.Request, token string) {
	ctx := r.Context()
	u, _ := middleware.User(ctx)
	a, err := s.MCP.GetByUserID(ctx, u.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		s.writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	available := []string{}
	for _, sc := range domain.MCPScopes {
		if mcpScopeAllowed(ctx, sc) {
			available = append(available, sc)
		}
	}
	out := map[string]any{
		"configured": a != nil, "scopes": []string{}, "available": available,
		"all_scopes": domain.MCPScopes, "url": strings.TrimRight(s.Config.BaseURL, "/") + "/mcp",
	}
	if a != nil {
		out["scopes"] = a.Scopes
		out["token_created_at"] = a.CreatedAt
	}
	if token != "" {
		out["token"] = token // shown exactly once; only the hash is stored
		w.Header().Set("Cache-Control", "no-store")
	}
	s.writeJSON(w, http.StatusOK, out)
}

func (s *Server) APIMCPGet(w http.ResponseWriter, r *http.Request) {
	s.writeMCPSettings(w, r, "")
}

// APIMCPTokenCreate issues a new token and invalidates the previous one. A first token starts read-only.
func (s *Server) APIMCPTokenCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, _ := middleware.User(ctx)
	a, err := s.MCP.GetByUserID(ctx, u.ID)
	if errors.Is(err, domain.ErrNotFound) {
		a = &domain.MCPAccess{UserID: u.ID, Scopes: []string{}}
		for _, sc := range domain.MCPScopes {
			if strings.HasSuffix(sc, ".read") {
				a.Scopes = append(a.Scopes, sc)
			}
		}
	} else if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	token := mcpTokenPrefix + hex.EncodeToString(b)
	a.TokenHash = hashMCPToken(token)
	a.CreatedAt = time.Now().UTC()
	if err := s.MCP.Save(ctx, a); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	s.writeMCPSettings(w, r, token)
}

func (s *Server) APIMCPScopesSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, _ := middleware.User(ctx)
	var req struct {
		Scopes []string `json:"scopes"`
	}
	if err := readJSON(r, &req); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	a, err := s.MCP.GetByUserID(ctx, u.ID)
	if err != nil {
		s.writeJSONError(w, http.StatusNotFound, "no MCP token yet")
		return
	}
	scopes := []string{}
	for _, sc := range req.Scopes {
		// Keep already stored scopes the user temporarily lost rights for; they stay inert until rights return.
		if !mcpScopeAllowed(ctx, sc) && !slices.Contains(a.Scopes, sc) {
			s.writeJSONError(w, http.StatusBadRequest, "scope not allowed: "+sc)
			return
		}
		if !slices.Contains(scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	a.Scopes = scopes
	if err := s.MCP.Save(ctx, a); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	s.writeMCPSettings(w, r, "")
}

func (s *Server) APIMCPRevoke(w http.ResponseWriter, r *http.Request) {
	u, _ := middleware.User(r.Context())
	if err := s.MCP.Delete(r.Context(), u.ID); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	s.writeMCPSettings(w, r, "")
}
