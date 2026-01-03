CREATE TABLE IF NOT EXISTS scores
(
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id    TEXT      NOT NULL,
    user_id    TEXT      NOT NULL,
    user_name  TEXT      NOT NULL,
    score      INTEGER   NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_scores__chat_id__user_id ON scores (chat_id, user_id);


CREATE TABLE IF NOT EXISTS messages
(
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id       TEXT      NOT NULL,
    chat_id          TEXT      NOT NULL,
    sender_user_id   TEXT      NOT NULL,
    sender_user_name TEXT      NOT NULL,
    text             TEXT      NOT NULL,
    created_at       TIMESTAMP NOT NULL,
    action           TEXT      NULL,
    action_note      TEXT      NULL,
    error            TEXT      NULL,
    media_type       TEXT      NULL,
    media_size       INTEGER   NULL,
    media_file_id    TEXT      NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages (created_at);

CREATE TABLE IF NOT EXISTS chats
(
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id    TEXT      NOT NULL,
    title      TEXT      NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_chats__chat_id ON chats (chat_id);

