-- Гейт выхода из шторма (задача #4901, спека v1 §2.3). Запрос гейта фиксирует
-- последние версии spec и plan задачи; человек решает go | changes. Новая
-- версия спеки переводит ожидающий гейт в superseded — нужен новый запрос.
-- plan_version NULL — плана на момент запроса не было; decided_at NULL — гейт
-- ещё не решён. Времена — unix-секунды, как во всей схеме.
--
-- IF NOT EXISTS: версия миграции — её позиция в списке файлов, поэтому при
-- неудачном порядке мержа файл может прогнаться повторно; это не должно падать.
CREATE TABLE IF NOT EXISTS task_gates (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      INTEGER NOT NULL REFERENCES tasks(id),
    spec_version INTEGER NOT NULL,
    plan_version INTEGER,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'go', 'changes', 'superseded')),
    comment      TEXT NOT NULL DEFAULT '',
    requested_by TEXT NOT NULL DEFAULT '',
    decided_by   TEXT NOT NULL DEFAULT '',
    requested_at INTEGER NOT NULL,
    decided_at   INTEGER
);

CREATE INDEX IF NOT EXISTS idx_task_gates_task ON task_gates(task_id, id);

-- Не больше одного ожидающего гейта на задачу: запрос нового снимает старый в
-- той же транзакции, а индекс страхует от гонки двух запросов.
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_gates_one_pending ON task_gates(task_id) WHERE status = 'pending';
