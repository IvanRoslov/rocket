-- Реестр профилей запуска (задача #5026, choose-model): имя, агент, модель,
-- усилие и описание «для чего подходит». '' в model/effort — значение агента
-- по умолчанию, флаг не передаётся. enabled — глобальное разрешение.
CREATE TABLE model_profiles (
  name        TEXT PRIMARY KEY,
  agent       TEXT NOT NULL,
  model       TEXT NOT NULL DEFAULT '',
  effort      TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  enabled     INTEGER NOT NULL DEFAULT 1,
  position    INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

-- allowed_profiles: JSON-массив имён профилей для воркеров фичи; '' — все включённые.
-- orchestrator_profile: профиль, выбранный оркестратору при старте задачи.
ALTER TABLE tasks ADD COLUMN allowed_profiles TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN orchestrator_profile TEXT NOT NULL DEFAULT '';

-- Снимок профиля на момент запуска: restore поднимает сессию из него, а не из реестра.
ALTER TABLE sessions ADD COLUMN profile TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN model   TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN effort  TEXT NOT NULL DEFAULT '';
