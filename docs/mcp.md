# MCP server

Findus has a built-in [MCP](https://modelcontextprotocol.io) server at `<FINDUS_BASE_URL>/mcp` (Streamable HTTP). No extra process is needed.

## Setup

1. Open **Profile → AI access (MCP)** and create an access token. It is shown once.
2. Enable the permissions the agent should have.
3. Add the server to your client. For Claude Code:

```bash
claude mcp add --transport http findus https://findus.example.com/mcp --header "Authorization: Bearer <token>"
```

Other clients need the same URL and `Authorization` header. Clients that only support OAuth are not supported. Cloud-hosted clients must be able to reach your instance. Use HTTPS for anything beyond localhost, because the token is sent with every request.

## Permissions

Each user has one token with a toggle per data type and action. A new token is read-only. Changes apply immediately.

| Data | Read tools | Write tools (create / update / delete) | Account permission needed to write |
|---|---|---|---|
| Items | `search_items`, `list_items`, `get_item`, `get_item_photo` | `create_item` / `update_item`, `set_item_photo` / `delete_item` | `items.write` |
| Locations | `list_locations`, `get_location` | `create_location` / `update_location` / `delete_location` | `locations.write` |
| Labels | `list_labels` | `create_label` / `update_label` / `delete_label` | `labels.write` |
| Templates | `list_templates` | `create_template` / `update_template` / `delete_template` | administrator |
| Attachments | `list_attachments`, `get_attachment` | `add_attachment` / `update_attachment` / `delete_attachment` | `items.write` |

- Disabled tools are hidden from the agent and rejected if called.
- A toggle never exceeds your account's permissions. Removing a permission or deactivating the account takes effect at once.
- Without label or location read access, item results omit label names and location paths; without item read access, location results omit items. IDs may still appear.
- `search_items` also matches label and location names, so with item read access a search can reveal whether a name matches.
- Delete and update cascade as in the web app: deleting an item removes its files, deleting a label removes it from items, deleting a template moves its items to another template.
- Users, invites, groups, settings, backup, import/export and label PDFs are not exposed.
