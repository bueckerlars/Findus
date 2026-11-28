package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"findus/backend/internal/domain"
	"findus/backend/internal/service"
)

// mcpTool is one MCP tool. Its scope is the toggle that must be enabled to see and call it.
type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`

	scope string
	run   func(ctx context.Context, can func(string) bool, a mcpArgs) (any, error)
}

// mcpArgs is the union of all tool arguments. Pointers distinguish "keep as is" from "set to empty" on updates.
type mcpArgs struct {
	ID            string          `json:"id"`
	ItemID        string          `json:"item_id"`
	Query         string          `json:"query"`
	Limit         int             `json:"limit"`
	Name          *string         `json:"name"`
	Description   *string         `json:"description"`
	LocationID    *string         `json:"location_id"`
	ParentID      *string         `json:"parent_id"`
	TemplateType  string          `json:"template_type"`
	Fields        map[string]any  `json:"fields"`
	LabelIDs      *[]string       `json:"label_ids"`
	Color         *string         `json:"color"`
	DefaultTpl    *string         `json:"default_template_type"`
	DisplayName   *string         `json:"display_name"`
	FieldSchema   json.RawMessage `json:"field_schema"`
	SortOrder     *int            `json:"sort_order"`
	Title         string          `json:"title"`
	Filename      string          `json:"filename"`
	ContentBase64 string          `json:"content_base64"`
}

// mcpContent is a tool result that is already MCP content (images, files) instead of JSON data.
type mcpContent map[string]any

func (s *Server) runMCPTool(ctx context.Context, t mcpTool, scopes []string, raw json.RawMessage) map[string]any {
	fail := func(msg string) map[string]any {
		return map[string]any{"isError": true, "content": []any{mcpContent{"type": "text", "text": msg}}}
	}
	var args mcpArgs
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&args); err != nil {
			return fail("invalid arguments: " + err.Error())
		}
	}
	required, _ := t.InputSchema["required"].([]string)
	var given map[string]any
	_ = json.Unmarshal(raw, &given)
	for _, k := range required {
		if v, ok := given[k]; !ok || v == nil {
			return fail("missing argument: " + k)
		}
	}
	out, err := t.run(ctx, func(sc string) bool { return slices.Contains(scopes, sc) }, args)
	if err != nil {
		return fail(s.clientMessage(err))
	}
	if c, ok := out.(mcpContent); ok {
		return map[string]any{"content": []any{c}}
	}
	b, err := json.Marshal(out)
	if err != nil {
		s.Log.Error("mcp tool encode", "tool", t.Name, "err", err)
		return fail("server error")
	}
	return map[string]any{"content": []any{mcpContent{"type": "text", "text": string(b)}}}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func intg(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

func obj(required []string, props map[string]any) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func deref(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return strings.TrimSpace(*p)
}

func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	return min(n, max)
}

var okResult = map[string]bool{"ok": true}

// mcpItem is the item shape exposed to agents. Data behind a disabled read toggle is left out.
func (s *Server) mcpItem(ctx context.Context, can func(string) bool, it *domain.Item, locNames map[string]string) (map[string]any, error) {
	fields, err := service.MergeTemplateIntoAdditional(it.AdditionalData, it.TemplateData)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"id": it.ID, "name": it.Name, "description": it.Description, "location_id": it.LocationID,
		"template_type": it.TemplateType, "fields": fields, "has_photo": it.PhotoPath != nil && *it.PhotoPath != "",
		"created_at": it.CreatedAt.UTC().Format(time.RFC3339), "updated_at": it.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if can("locations.read") {
		out["location_path"] = locNames[it.LocationID]
	}
	if can("labels.read") {
		labels, err := s.Items.ListLabelsForItem(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		rows := []map[string]string{}
		for _, l := range labels {
			rows = append(rows, map[string]string{"id": l.ID, "name": l.Name})
		}
		out["labels"] = rows
	}
	return out, nil
}

func (s *Server) mcpItems(ctx context.Context, can func(string) bool, items []domain.Item) (any, error) {
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].LocationID
	}
	names := s.locationDisplayNamesForIDs(ctx, ids)
	out := []map[string]any{}
	for i := range items {
		row, err := s.mcpItem(ctx, can, &items[i], names)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return map[string]any{"items": out}, nil
}

func (s *Server) mcpOneItem(ctx context.Context, can func(string) bool, it *domain.Item) (map[string]any, error) {
	return s.mcpItem(ctx, can, it, s.locationDisplayNamesForIDs(ctx, []string{it.LocationID}))
}

// mcpWritten answers a create/update: without the read toggle only the id is echoed, never stored data.
func (s *Server) mcpWritten(ctx context.Context, can func(string) bool, it *domain.Item) (any, error) {
	if !can("items.read") {
		return map[string]string{"id": it.ID}, nil
	}
	return s.mcpOneItem(ctx, can, it)
}

func mcpLocation(l domain.Location) map[string]any {
	return map[string]any{"id": l.ID, "name": l.Name, "description": l.Description, "parent_id": l.ParentID}
}

func mcpAttachment(a domain.ItemAttachment) map[string]any {
	return map[string]any{
		"id": a.ID, "item_id": a.ItemID, "title": a.Title, "filename": a.OriginalFilename,
		"mime_type": a.MimeType, "size_bytes": a.SizeBytes,
	}
}

func mcpTemplate(t domain.ItemTemplate) map[string]any {
	return map[string]any{"id": t.ID, "display_name": t.DisplayName, "sort_order": t.SortOrder, "field_schema": t.Fields}
}

func decodeBase64(v string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("%w: content_base64 is not valid base64", domain.ErrValidation)
	}
	return b, nil
}

// fieldStrings converts agent-supplied field values to the string form Findus stores; null becomes "".
func fieldStrings(in map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for k, v := range in {
		switch x := v.(type) {
		case nil:
			out[k] = ""
		case string:
			out[k] = x
		case bool, float64:
			b, _ := json.Marshal(x)
			out[k] = string(b)
		default:
			return nil, fmt.Errorf("%w: field %q must be a string, number or boolean", domain.ErrValidation, k)
		}
	}
	return out, nil
}

func (s *Server) mcpCreateItem(ctx context.Context, can func(string) bool, a mcpArgs) (any, error) {
	tplID := a.TemplateType
	if tplID == "" {
		list, err := s.Inventory.ListItemTemplates(ctx)
		if err != nil {
			return nil, err
		}
		if len(list) > 0 {
			tplID = list[0].ID
		}
	}
	tpl, err := s.Inventory.GetItemTemplate(ctx, tplID)
	if err != nil {
		return nil, fmt.Errorf("template_type %q: %w", tplID, err)
	}
	fields, err := fieldStrings(a.Fields)
	if err != nil {
		return nil, err
	}
	td, err := domain.NormalizeTemplateDataFromFields(tpl.Fields, fields)
	if err != nil {
		return nil, err
	}
	for k, v := range fields {
		if v == "" {
			delete(fields, k)
		}
	}
	add, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	merged, err := service.MergeTemplateIntoAdditional(add, td)
	if err != nil {
		return nil, err
	}
	var labelIDs []string
	if a.LabelIDs != nil {
		labelIDs = *a.LabelIDs
	}
	it, err := s.Inventory.CreateItem(ctx, deref(a.Name, ""), deref(a.Description, ""), deref(a.LocationID, ""),
		domain.TemplateType(tpl.ID), json.RawMessage(`{}`), merged, labelIDs)
	if err != nil {
		return nil, err
	}
	return s.mcpWritten(ctx, can, it)
}

// mcpUpdateItem is a partial update: everything the agent leaves out is kept.
func (s *Server) mcpUpdateItem(ctx context.Context, can func(string) bool, a mcpArgs) (any, error) {
	it, err := s.Items.GetByID(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	current, err := service.MergeTemplateIntoAdditional(it.AdditionalData, it.TemplateData)
	if err != nil {
		return nil, err
	}
	var values map[string]any
	if err := json.Unmarshal(current, &values); err != nil {
		return nil, err
	}
	changes, err := fieldStrings(a.Fields)
	if err != nil {
		return nil, err
	}
	for k, v := range changes {
		if v == "" {
			delete(values, k)
		} else {
			values[k] = v
		}
	}
	add, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var labelIDs []string
	if a.LabelIDs != nil {
		labelIDs = *a.LabelIDs
	} else {
		labels, err := s.Items.ListLabelsForItem(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		for _, l := range labels {
			labelIDs = append(labelIDs, l.ID)
		}
	}
	updated, err := s.Inventory.UpdateItem(ctx, it.ID, deref(a.Name, it.Name), deref(a.Description, it.Description),
		deref(a.LocationID, it.LocationID), it.TemplateType, json.RawMessage(`{}`), add, labelIDs)
	if err != nil {
		return nil, err
	}
	return s.mcpWritten(ctx, can, updated)
}

func (s *Server) mcpTools() []mcpTool {
	id := str("ID of the record")
	itemID := str("ID of the item")
	limit := intg("Maximum number of results")
	labelIDs := map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Label IDs; replaces all labels of the item"}
	itemProps := func(extra map[string]any) map[string]any {
		m := map[string]any{
			"name": str("Item name"), "description": str("Free text"), "location_id": str("ID of the location holding the item"),
			"fields":    map[string]any{"type": "object", "description": "Custom key/value fields, including template fields. On update only the given keys change; null or empty removes a key."},
			"label_ids": labelIDs,
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	locationProps := map[string]any{"name": str("Location name"), "description": str("Free text"), "parent_id": str("Parent location ID; empty string means top level")}
	labelProps := map[string]any{"name": str("Label name"), "color": str("CSS color, e.g. #64748b"), "default_template_type": str("Template ID suggested for this label; empty string for none")}
	templateProps := map[string]any{
		"display_name": str("Human readable name"), "sort_order": intg("Position in lists"),
		"field_schema": map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": `Field definitions: {"key","label","widget":"text"|"select","required",options:[{"value","label"}]}`},
	}
	with := func(base map[string]any, k string, v any) map[string]any {
		m := map[string]any{k: v}
		for bk, bv := range base {
			m[bk] = bv
		}
		return m
	}

	return []mcpTool{
		// Items
		{Name: "search_items", scope: "items.read", Description: "Full-text search over items (name, description, fields, labels, location).",
			InputSchema: obj([]string{"query"}, map[string]any{"query": str("Search text"), "limit": limit}),
			run: func(ctx context.Context, can func(string) bool, a mcpArgs) (any, error) {
				res, err := s.Items.Search(ctx, strings.TrimSpace(a.Query), clampLimit(a.Limit, 25, 100))
				if err != nil {
					return nil, err
				}
				return s.mcpItems(ctx, can, res)
			}},
		{Name: "list_items", scope: "items.read", Description: "List items, optionally only those directly in one location.",
			InputSchema: obj(nil, map[string]any{"location_id": str("Only items in this location"), "limit": limit}),
			run: func(ctx context.Context, can func(string) bool, a mcpArgs) (any, error) {
				n := clampLimit(a.Limit, 100, 500)
				var res []domain.Item
				var err error
				if loc := deref(a.LocationID, ""); loc != "" {
					res, err = s.Items.ListByLocation(ctx, loc)
					res = res[:min(n, len(res))]
				} else {
					res, err = s.Items.ListAll(ctx, n)
				}
				if err != nil {
					return nil, err
				}
				return s.mcpItems(ctx, can, res)
			}},
		{Name: "get_item", scope: "items.read", Description: "Get one item with all its fields.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": id}),
			run: func(ctx context.Context, can func(string) bool, a mcpArgs) (any, error) {
				it, err := s.Items.GetByID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				return s.mcpOneItem(ctx, can, it)
			}},
		{Name: "get_item_photo", scope: "items.read", Description: "Get the photo of an item as an image.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": id}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				it, err := s.Items.GetByID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				if it.PhotoPath == nil || *it.PhotoPath == "" {
					return nil, fmt.Errorf("photo: %w", domain.ErrNotFound)
				}
				p, err := service.DataFile(s.Config.DataDir, "images", *it.PhotoPath)
				if err != nil {
					return nil, err
				}
				b, err := os.ReadFile(p)
				if err != nil {
					return nil, err
				}
				return mcpContent{"type": "image", "mimeType": "image/webp", "data": base64.StdEncoding.EncodeToString(b)}, nil
			}},
		{Name: "create_item", scope: "items.create", Description: "Create an item. Without template_type the first template is used.",
			InputSchema: obj([]string{"name", "location_id"}, itemProps(map[string]any{"template_type": str("Template ID")})),
			run:         s.mcpCreateItem},
		{Name: "update_item", scope: "items.update", Description: "Change an item. Omitted arguments stay unchanged.",
			InputSchema: obj([]string{"id"}, itemProps(map[string]any{"id": id})),
			run:         s.mcpUpdateItem},
		{Name: "set_item_photo", scope: "items.update", Description: "Set or replace the photo of an item (JPEG or PNG).",
			InputSchema: obj([]string{"id", "content_base64"}, map[string]any{"id": id, "content_base64": str("Base64 encoded image")}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				it, err := s.Items.GetByID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				b, err := decodeBase64(a.ContentBase64)
				if err != nil {
					return nil, err
				}
				// it.ID comes from the database, so it is safe as a file name.
				if err := s.saveItemPhoto(ctx, it.ID, bytes.NewReader(b)); err != nil {
					return nil, fmt.Errorf("%w: photo: %v", domain.ErrValidation, err)
				}
				rel := "images/" + it.ID + ".webp"
				it.PhotoPath = &rel
				it.UpdatedAt = time.Now().UTC()
				return okResult, s.Items.Update(ctx, it)
			}},
		{Name: "delete_item", scope: "items.delete", Description: "Permanently delete an item with its photo and attachments.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": id}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				return okResult, s.deleteItem(ctx, a.ID)
			}},

		// Locations
		{Name: "list_locations", scope: "locations.read", Description: "List all locations. parent_id describes the hierarchy.",
			InputSchema: obj(nil, nil),
			run: func(ctx context.Context, _ func(string) bool, _ mcpArgs) (any, error) {
				all, err := s.Locs.ListAllExport(ctx)
				if err != nil {
					return nil, err
				}
				out := []map[string]any{}
				for _, l := range all {
					out = append(out, mcpLocation(l))
				}
				return map[string]any{"locations": out}, nil
			}},
		{Name: "get_location", scope: "locations.read", Description: "Get a location with its path, child locations and (if items may be read) its items.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": id}),
			run: func(ctx context.Context, can func(string) bool, a mcpArgs) (any, error) {
				l, err := s.Locs.GetByID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				out := mcpLocation(*l)
				out["path"] = s.locationDisplayNamesForIDs(ctx, []string{l.ID})[l.ID]
				children, err := s.Locs.ListChildren(ctx, &l.ID)
				if err != nil {
					return nil, err
				}
				rows := []map[string]any{}
				for _, c := range children {
					rows = append(rows, mcpLocation(c))
				}
				out["children"] = rows
				if can("items.read") {
					items, err := s.Items.ListByLocation(ctx, l.ID)
					if err != nil {
						return nil, err
					}
					res, err := s.mcpItems(ctx, can, items)
					if err != nil {
						return nil, err
					}
					out["items"] = res.(map[string]any)["items"]
				}
				return out, nil
			}},
		{Name: "create_location", scope: "locations.create", Description: "Create a location.",
			InputSchema: obj([]string{"name"}, locationProps),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				l, err := s.Inventory.CreateLocation(ctx, deref(a.Name, ""), deref(a.Description, ""), optional(deref(a.ParentID, "")))
				if err != nil {
					return nil, err
				}
				return map[string]string{"id": l.ID}, nil
			}},
		{Name: "update_location", scope: "locations.update", Description: "Rename, describe or move a location. Omitted arguments stay unchanged.",
			InputSchema: obj([]string{"id"}, with(locationProps, "id", id)),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				l, err := s.Locs.GetByID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				parent := ""
				if l.ParentID != nil {
					parent = *l.ParentID
				}
				_, err = s.Inventory.UpdateLocation(ctx, l.ID, deref(a.Name, l.Name), deref(a.Description, l.Description), optional(deref(a.ParentID, parent)))
				return okResult, err
			}},
		{Name: "delete_location", scope: "locations.delete", Description: "Delete an empty location (no child locations, no items).",
			InputSchema: obj([]string{"id"}, map[string]any{"id": id}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				return okResult, s.Inventory.DeleteLocation(ctx, a.ID)
			}},

		// Labels
		{Name: "list_labels", scope: "labels.read", Description: "List all labels.",
			InputSchema: obj(nil, nil),
			run: func(ctx context.Context, _ func(string) bool, _ mcpArgs) (any, error) {
				list, err := s.Inventory.ListLabels(ctx)
				if err != nil {
					return nil, err
				}
				out := []map[string]any{}
				for _, l := range list {
					out = append(out, map[string]any{"id": l.ID, "name": l.Name, "color": l.Color, "default_template_type": l.DefaultTemplateType})
				}
				return map[string]any{"labels": out}, nil
			}},
		{Name: "create_label", scope: "labels.create", Description: "Create a label.",
			InputSchema: obj([]string{"name"}, labelProps),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				l, err := s.Inventory.CreateLabel(ctx, deref(a.Name, ""), deref(a.Color, ""), a.DefaultTpl)
				if err != nil {
					return nil, err
				}
				return map[string]string{"id": l.ID}, nil
			}},
		{Name: "update_label", scope: "labels.update", Description: "Change a label. Omitted arguments stay unchanged.",
			InputSchema: obj([]string{"id"}, with(labelProps, "id", id)),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				l, err := s.Labels.GetByID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				tpl := a.DefaultTpl
				if tpl == nil {
					tpl = l.DefaultTemplateType
				}
				_, err = s.Inventory.UpdateLabel(ctx, l.ID, deref(a.Name, l.Name), deref(a.Color, l.Color), tpl)
				return okResult, err
			}},
		{Name: "delete_label", scope: "labels.delete", Description: "Delete a label; it is removed from all items.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": id}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				return okResult, s.Inventory.DeleteLabel(ctx, a.ID)
			}},

		// Templates
		{Name: "list_templates", scope: "templates.read", Description: "List item templates with their field definitions.",
			InputSchema: obj(nil, nil),
			run: func(ctx context.Context, _ func(string) bool, _ mcpArgs) (any, error) {
				list, err := s.Inventory.ListItemTemplates(ctx)
				if err != nil {
					return nil, err
				}
				out := []map[string]any{}
				for _, t := range list {
					out = append(out, mcpTemplate(t))
				}
				return map[string]any{"templates": out}, nil
			}},
		{Name: "create_template", scope: "templates.create", Description: "Create an item template.",
			InputSchema: obj([]string{"id", "display_name"}, with(templateProps, "id", str("New slug: lowercase letters, digits, underscore"))),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				order := 0
				if a.SortOrder != nil {
					order = *a.SortOrder
				}
				return okResult, s.Inventory.CreateItemTemplate(ctx, a.ID, deref(a.DisplayName, ""), a.FieldSchema, order)
			}},
		{Name: "update_template", scope: "templates.update", Description: "Change an item template. Omitted arguments stay unchanged; field_schema replaces all fields.",
			InputSchema: obj([]string{"id"}, with(templateProps, "id", id)),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				t, err := s.Inventory.GetItemTemplate(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				schema := a.FieldSchema
				if len(schema) == 0 || string(schema) == "null" {
					schema = t.FieldsJSON
				}
				order := t.SortOrder
				if a.SortOrder != nil {
					order = *a.SortOrder
				}
				return okResult, s.Inventory.UpdateItemTemplate(ctx, t.ID, deref(a.DisplayName, t.DisplayName), schema, order)
			}},
		{Name: "delete_template", scope: "templates.delete", Description: "Delete a template; items using it are moved to another template.",
			InputSchema: obj([]string{"id"}, map[string]any{"id": id}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				return okResult, s.Inventory.DeleteItemTemplate(ctx, a.ID)
			}},

		// Attachments
		{Name: "list_attachments", scope: "attachments.read", Description: "List the files attached to an item.",
			InputSchema: obj([]string{"item_id"}, map[string]any{"item_id": itemID}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				list, err := s.Inventory.ListItemAttachments(ctx, a.ItemID)
				if err != nil {
					return nil, err
				}
				out := []map[string]any{}
				for _, att := range list {
					out = append(out, mcpAttachment(att))
				}
				return map[string]any{"attachments": out}, nil
			}},
		{Name: "get_attachment", scope: "attachments.read", Description: "Download the content of one attachment.",
			InputSchema: obj([]string{"item_id", "id"}, map[string]any{"item_id": itemID, "id": str("Attachment ID")}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				att, err := s.Inventory.ItemAttachments.GetByID(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				if att.ItemID != a.ItemID {
					return nil, domain.ErrNotFound
				}
				p, err := service.DataFile(s.Config.DataDir, "item-attachments", att.StoragePath)
				if err != nil {
					return nil, err
				}
				f, err := os.Open(p)
				if err != nil {
					return nil, err
				}
				defer f.Close()
				b, err := io.ReadAll(f)
				if err != nil {
					return nil, err
				}
				data := base64.StdEncoding.EncodeToString(b)
				if strings.HasPrefix(att.MimeType, "image/") {
					return mcpContent{"type": "image", "mimeType": att.MimeType, "data": data}, nil
				}
				return mcpContent{"type": "resource", "resource": map[string]any{
					"uri": "findus://items/" + att.ItemID + "/attachments/" + att.ID, "mimeType": att.MimeType, "blob": data,
				}}, nil
			}},
		{Name: "add_attachment", scope: "attachments.create", Description: "Attach a file (PDF, JPEG, PNG, WebP, GIF) to an item.",
			InputSchema: obj([]string{"item_id", "filename", "content_base64"}, map[string]any{
				"item_id": itemID, "filename": str("Original file name"), "title": str("Display title"), "content_base64": str("Base64 encoded file")}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				b, err := decodeBase64(a.ContentBase64)
				if err != nil {
					return nil, err
				}
				att, err := s.Inventory.AddItemAttachment(ctx, s.Config.DataDir, a.ItemID, a.Title, a.Filename, bytes.NewReader(b))
				if err != nil {
					return nil, err
				}
				return map[string]string{"id": att.ID}, nil
			}},
		{Name: "update_attachment", scope: "attachments.update", Description: "Change the title of an attachment.",
			InputSchema: obj([]string{"item_id", "id", "title"}, map[string]any{"item_id": itemID, "id": str("Attachment ID"), "title": str("New title")}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				return okResult, s.Inventory.UpdateItemAttachmentTitle(ctx, a.ItemID, a.ID, a.Title)
			}},
		{Name: "delete_attachment", scope: "attachments.delete", Description: "Permanently delete an attachment.",
			InputSchema: obj([]string{"item_id", "id"}, map[string]any{"item_id": itemID, "id": str("Attachment ID")}),
			run: func(ctx context.Context, _ func(string) bool, a mcpArgs) (any, error) {
				return okResult, s.Inventory.DeleteItemAttachment(ctx, s.Config.DataDir, a.ItemID, a.ID)
			}},
	}
}
