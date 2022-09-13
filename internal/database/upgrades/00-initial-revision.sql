-- v1: Initial revision
CREATE TABLE "user" (
    mxid TEXT PRIMARY KEY,
    uin TEXT UNIQUE,
    management_room TEXT,
    space_room TEXT
);

CREATE TABLE puppet (
    uin TEXT PRIMARY KEY,
    displayname TEXT,
    name_quality SMALLINT,
    avatar TEXT,
    avatar_url TEXT,
    name_set BOOLEAN NOT NULL DEFAULT false,
    avatar_set BOOLEAN NOT NULL DEFAULT false,
    last_sync BIGINT NOT NULL DEFAULT 0,
    custom_mxid TEXT,
    access_token TEXT,
    next_batch TEXT,
    enable_presence BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE portal (
    uid TEXT,
    receiver TEXT,
    mxid TEXT UNIQUE,
    name TEXT NOT NULL,
    name_set BOOLEAN NOT NULL DEFAULT false,
    topic TEXT NOT NULL,
    topic_set BOOLEAN NOT NULL DEFAULT false,
    avatar TEXT NOT NULL,
    avatar_url TEXT,
    avatar_set BOOLEAN NOT NULL DEFAULT false,
    encrypted BOOLEAN NOT NULL DEFAULT false,
    last_sync BIGINT NOT NULL DEFAULT 0,
    first_event_id TEXT,
    next_batch_id TEXT,
    PRIMARY KEY (uid, receiver)
);

CREATE TABLE user_portal (
    user_mxid TEXT,
    portal_uid TEXT,
    portal_receiver TEXT,
    last_read_ts BIGINT NOT NULL DEFAULT 0,
    in_space BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (user_mxid, portal_uid, portal_receiver),
    FOREIGN KEY (user_mxid) REFERENCES "user"(mxid) ON UPDATE CASCADE ON DELETE CASCADE,
    FOREIGN KEY (portal_uid, portal_receiver) REFERENCES portal(uid, receiver) ON UPDATE CASCADE ON DELETE CASCADE
);

-- only: postgres
CREATE TYPE error_type AS ENUM ('', 'decryption_failed', 'media_not_found');

CREATE TABLE message (
    chat_uid TEXT,
    chat_receiver TEXT,
    msg_id TEXT,
    mxid TEXT UNIQUE,
    sender TEXT,
    timestamp BIGINT,
    sent BOOLEAN,
    error error_type,
    type TEXT,
    PRIMARY KEY (chat_uid, chat_receiver, msg_id),
    FOREIGN KEY (chat_uid, chat_receiver) REFERENCES portal(uid, receiver) ON DELETE CASCADE
);