-- Initial schema (SPEC §3, §10). There are no ON DELETE CASCADE clauses: a
-- parent is deleted together with its dependants by the repositories, as SPEC
-- §3 and §7.4 require, and a foreign key stops anything that is forgotten.
-- Jobs deliberately have no foreign keys: the REPL job uses project and command
-- -1, and a job keeps the server host it ran on even if the server is deleted.

CREATE TABLE servers (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT    NOT NULL,
    host                TEXT    NOT NULL,
    code_name           TEXT    NOT NULL UNIQUE,
    is_local            INTEGER NOT NULL DEFAULT 0,
    max_concurrent_jobs INTEGER NOT NULL DEFAULT 1 CHECK (max_concurrent_jobs >= 1),
    created_at          TEXT    NOT NULL,
    updated_at          TEXT    NOT NULL
);

CREATE TABLE generators (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    name                   TEXT    NOT NULL,
    code_name              TEXT    NOT NULL UNIQUE,
    workflow               TEXT    NOT NULL,
    positive_prompt_title  TEXT    NOT NULL DEFAULT '',
    negative_prompt_title  TEXT    NOT NULL DEFAULT '',
    output_type            TEXT    NOT NULL,
    output_attributes      TEXT    NOT NULL DEFAULT '{}',
    output_node_class_type TEXT    NOT NULL DEFAULT '',
    output_node_title      TEXT    NOT NULL DEFAULT '',
    has_random_seed        INTEGER NOT NULL DEFAULT 0,
    created_at             TEXT    NOT NULL,
    updated_at             TEXT    NOT NULL
);

CREATE TABLE fixers (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    name                  TEXT NOT NULL,
    code_name             TEXT NOT NULL UNIQUE,
    workflow              TEXT NOT NULL,
    load_image_title      TEXT NOT NULL DEFAULT '',
    save_image_title      TEXT NOT NULL DEFAULT '',
    positive_prompt       TEXT NOT NULL DEFAULT '',
    negative_prompt       TEXT NOT NULL DEFAULT '',
    positive_prompt_title TEXT NOT NULL DEFAULT '',
    negative_prompt_title TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);

CREATE TABLE groups (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    name                   TEXT    NOT NULL,
    description            TEXT    NOT NULL DEFAULT '',
    code_name              TEXT    NOT NULL UNIQUE,
    thumbnail              TEXT    NOT NULL DEFAULT '',
    use_lora               INTEGER NOT NULL DEFAULT 0,
    use_controlnet         INTEGER NOT NULL DEFAULT 0,
    use_ip_adapter         INTEGER NOT NULL DEFAULT 0,
    use_mask_region        INTEGER NOT NULL DEFAULT 0,
    use_coordinates_region INTEGER NOT NULL DEFAULT 0,
    use_type_attributes    INTEGER NOT NULL DEFAULT 0,
    created_at             TEXT    NOT NULL,
    updated_at             TEXT    NOT NULL
);

CREATE TABLE group_tags (
    group_id INTEGER NOT NULL REFERENCES groups (id),
    tag      TEXT    NOT NULL,
    PRIMARY KEY (group_id, tag)
);
CREATE INDEX group_tags_by_tag ON group_tags (tag);

CREATE TABLE items (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id               INTEGER NOT NULL REFERENCES groups (id),
    code_name              TEXT    NOT NULL,
    name                   TEXT    NOT NULL DEFAULT '',
    positive_prompt        TEXT    NOT NULL DEFAULT '',
    negative_prompt        TEXT    NOT NULL DEFAULT '',
    loras                  TEXT    NOT NULL DEFAULT '[]',
    ip_adapter             TEXT,
    controlnets            TEXT    NOT NULL DEFAULT '[]',
    mask_region            TEXT,
    coordinated_regions    TEXT    NOT NULL DEFAULT '[]',
    output_type            TEXT    NOT NULL DEFAULT '',
    output_type_attributes TEXT    NOT NULL DEFAULT '{}',
    thumbnail              TEXT    NOT NULL DEFAULT '',
    created_at             TEXT    NOT NULL,
    updated_at             TEXT    NOT NULL,
    UNIQUE (group_id, code_name)
);

CREATE TABLE item_tags (
    item_id INTEGER NOT NULL REFERENCES items (id),
    tag     TEXT    NOT NULL,
    PRIMARY KEY (item_id, tag)
);
CREATE INDEX item_tags_by_tag ON item_tags (tag);

CREATE TABLE projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- order_no is not UNIQUE: inserting a command shifts its successors, and SQLite
-- checks a unique index row by row. The repository keeps it contiguous.
CREATE TABLE commands (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id    INTEGER NOT NULL REFERENCES projects (id),
    order_no      INTEGER NOT NULL,
    status        TEXT    NOT NULL DEFAULT 'idle',
    name          TEXT    NOT NULL DEFAULT '',
    description   TEXT    NOT NULL DEFAULT '',
    command_code  TEXT    NOT NULL,
    command_json  TEXT    NOT NULL DEFAULT '{}',
    download_file TEXT    NOT NULL DEFAULT '',
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL
);
CREATE INDEX commands_by_project ON commands (project_id, order_no);

CREATE TABLE jobs (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id          INTEGER NOT NULL,
    command_id          INTEGER NOT NULL,
    items               TEXT    NOT NULL DEFAULT '[]',
    command_text        TEXT    NOT NULL DEFAULT '',
    status              TEXT    NOT NULL DEFAULT 'idle',
    queue_seq           INTEGER NOT NULL DEFAULT 0,
    server_id           INTEGER,
    server_host         TEXT    NOT NULL DEFAULT '',
    preferred_server_id INTEGER,
    pinned              INTEGER NOT NULL DEFAULT 0,
    fixers              TEXT    NOT NULL DEFAULT '[]',
    finished_fixers     INTEGER NOT NULL DEFAULT 0,
    repeat_index        INTEGER NOT NULL DEFAULT 0,
    region_index        INTEGER NOT NULL DEFAULT -1,
    seed                TEXT,
    attempts            INTEGER NOT NULL DEFAULT 0,
    generator_code_name TEXT    NOT NULL DEFAULT '',
    output_type         TEXT    NOT NULL DEFAULT '',
    output_attributes   TEXT    NOT NULL DEFAULT '{}',
    prompt_id           TEXT    NOT NULL DEFAULT '',
    positive_prompt     TEXT    NOT NULL DEFAULT '',
    negative_prompt     TEXT    NOT NULL DEFAULT '',
    controlnets         TEXT    NOT NULL DEFAULT '[]',
    region_prompts      TEXT    NOT NULL DEFAULT '[]',
    ip_adapters         TEXT    NOT NULL DEFAULT '[]',
    loras               TEXT    NOT NULL DEFAULT '[]',
    result_path         TEXT    NOT NULL DEFAULT '',
    error               TEXT    NOT NULL DEFAULT '',
    error_trace         TEXT    NOT NULL DEFAULT '',
    warnings            TEXT    NOT NULL DEFAULT '[]',
    is_generated        INTEGER NOT NULL DEFAULT 0,
    is_fixed            INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT    NOT NULL,
    updated_at          TEXT    NOT NULL
);
CREATE INDEX jobs_by_command ON jobs (command_id, id);
CREATE INDEX jobs_by_queue ON jobs (status, queue_seq, id);
