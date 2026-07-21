-- Uploaded dashboard attachments (pasted screenshots). The bytes live on
-- disk under cfg.AttachmentsDir as <id>.<ext>; this table holds identity and
-- metadata so /v1/attachments/{id} can serve with the right Content-Type.
CREATE TABLE attachments (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  mime       TEXT NOT NULL,                  -- image/png|image/jpeg|image/webp
  size       INTEGER NOT NULL,               -- bytes
  created_at INTEGER NOT NULL
);
