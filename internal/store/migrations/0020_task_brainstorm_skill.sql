-- Какой скилл шторма назвал промпт оркестратора задачи (задача #4901):
-- orchestrator-brainstorming или superpowers:brainstorming. Пишется при
-- старте задачи из настройки orchestrator_brainstorm_custom и дальше не
-- меняется — restore и метрика берут значение отсюда. '' — задачи,
-- стартовавшие до этой миграции.

ALTER TABLE tasks ADD COLUMN brainstorm_skill TEXT NOT NULL DEFAULT '';
