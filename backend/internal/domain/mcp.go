package domain

import "time"

// MCPScopes lists every permission toggle for MCP access as "<resource>.<action>".
var MCPScopes = []string{
	"items.read", "items.create", "items.update", "items.delete",
	"locations.read", "locations.create", "locations.update", "locations.delete",
	"labels.read", "labels.create", "labels.update", "labels.delete",
	"templates.read", "templates.create", "templates.update", "templates.delete",
	"attachments.read", "attachments.create", "attachments.update", "attachments.delete",
}

// MCPAccess is a user's MCP connection: one access token plus the enabled toggles.
type MCPAccess struct {
	UserID    string
	TokenHash string
	Scopes    []string
	CreatedAt time.Time // when the current token was issued
}
