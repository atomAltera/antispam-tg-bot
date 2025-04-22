CREATE TABLE IF NOT EXISTS scores
(
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source     TEXT      NOT NULL,
    chat_id    TEXT      NOT NULL,
    user_id    TEXT      NOT NULL,
    score      INTEGER   NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_scores__source__chat_id__user_id ON scores (source, chat_id, user_id);


CREATE TABLE IF NOT EXISTS messages
(
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    source         TEXT      NOT NULL,
    message_id     TEXT      NOT NULL,
    chat_id        TEXT      NOT NULL,
    sender_user_id TEXT      NOT NULL,
    text           TEXT      NOT NULL,
    created_at     TIMESTAMP NOT NULL,
    action         TEXT      NULL,
    action_note    TEXT      NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages (created_at);

