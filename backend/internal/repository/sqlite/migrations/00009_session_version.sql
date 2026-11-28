-- +goose Up
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN session_version INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
