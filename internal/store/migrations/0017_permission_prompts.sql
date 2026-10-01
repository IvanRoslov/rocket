-- Журнал диалогов разрешений Claude Code, которые монитор нашёл на панели
-- сессии (задача #4881). Транскрипт агента про них молчит, поэтому закрытые
-- записи вмешиваются в ленту чата как role:"permission". Открытая запись
-- (resolved_at IS NULL) — диалог, висящий сейчас; answered_via — chat, если
-- ответ ушёл через API (answer_label задан), иначе terminal.

CREATE TABLE permission_prompts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT NOT NULL,
    title        TEXT NOT NULL,
    context      TEXT NOT NULL DEFAULT '',
    options_json TEXT NOT NULL DEFAULT '[]',
    asked_at     INTEGER NOT NULL,
    resolved_at  INTEGER,
    answer_label TEXT NOT NULL DEFAULT '',
    answered_via TEXT NOT NULL DEFAULT ''
);

CREATE INDEX permission_prompts_session ON permission_prompts(session_id, id);
