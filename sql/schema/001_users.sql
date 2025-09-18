-- +goose Up
CREATE TABLE users(
    id UUID,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    email TEXT NOT NULL
);

-- +goose Down
DROP TABLE users;
