-- Attention set (задача #1023, спека v1 §«Attention set»). До этой миграции
-- очередь ответа («чей ход») ВЫЧИСЛЯЛАСЬ из последней записи треда: адресаты
-- `--to` последней записи, иначе все участники, кроме её автора. Отсюда главная
-- боль: `--to` был свойством одной записи и затирался следующей — любая реплика
-- без `--to` возвращала ход всем сразу, включая тех, кого уже вывели.
--
-- Теперь «чей ход» — ХРАНИМОЕ множество question_attention с автоправилами
-- Gerrit-типа (см. internal/store/attention.go): автор записи выходит,
-- названные в `--to` входят, опустевшее множество передаёт ход остальным.
CREATE TABLE question_attention (
  question_id    INTEGER NOT NULL REFERENCES questions(id),
  participant_id TEXT NOT NULL,            -- 'human' | id агента | session id
  added_at       INTEGER NOT NULL,
  UNIQUE (question_id, participant_id)
);

CREATE INDEX idx_question_attention_participant
  ON question_attention(participant_id, question_id);

-- Тип треда: decision ждёт решения и считается в бейджах; fyi — статусное
-- сообщение в историю, создаётся сразу resolved и никого не ждёт.
ALTER TABLE questions ADD COLUMN type TEXT NOT NULL DEFAULT 'decision';

-- Варианты ответа: JSON-массив строк; '' — вариантов нет.
ALTER TABLE questions ADD COLUMN options TEXT NOT NULL DEFAULT '';

-- Бэкфилл: для каждого ОТКРЫТОГО треда attention = ровно то, что до этой
-- миграции вычислял waitingOn() в internal/api, чтобы миграция не двигала
-- очередь ни в одном живом треде. Секция ограничена маркерами: тест
-- TestBackfillAttentionMatchesLegacyWaitingOn выполняет именно её.
-- BACKFILL-BEGIN
-- Ветка 1: у последней записи нет адресатов → ход у всех участников, кроме её
-- автора. Последняя запись — последнее сообщение, а если сообщений нет, сам
-- вопрос (у него asked_by может хранить человека как '' — нормализуем).
WITH last_entry AS (
  SELECT q.id AS qid,
         q.asked_at AS at,
         COALESCE(lm.author, CASE WHEN q.asked_by = '' THEN 'human' ELSE q.asked_by END) AS author,
         COALESCE(lm.addressed_to, q.addressed_to) AS addressed
  FROM questions q
  LEFT JOIN question_messages lm
    ON lm.id = (SELECT MAX(m.id) FROM question_messages m WHERE m.question_id = q.id)
  WHERE q.status = 'open'
)
INSERT OR IGNORE INTO question_attention (question_id, participant_id, added_at)
SELECT p.question_id, p.participant_id, le.at
FROM question_participants p
JOIN last_entry le ON le.qid = p.question_id
WHERE le.addressed = '' AND p.participant_id <> le.author;

-- Ветка 2: адресаты заданы → ход ровно у них. addressed_to хранится как CSV,
-- поэтому разбираем его рекурсивным CTE.
WITH RECURSIVE last_entry AS (
  SELECT q.id AS qid,
         q.asked_at AS at,
         COALESCE(lm.addressed_to, q.addressed_to) AS addressed
  FROM questions q
  LEFT JOIN question_messages lm
    ON lm.id = (SELECT MAX(m.id) FROM question_messages m WHERE m.question_id = q.id)
  WHERE q.status = 'open'
),
split(qid, item, rest, at) AS (
  SELECT qid, '', addressed || ',', at FROM last_entry WHERE addressed <> ''
  UNION ALL
  SELECT qid,
         substr(rest, 1, instr(rest, ',') - 1),
         substr(rest, instr(rest, ',') + 1),
         at
  FROM split WHERE rest <> ''
)
INSERT OR IGNORE INTO question_attention (question_id, participant_id, added_at)
SELECT qid, item, at FROM split WHERE item <> '';
-- BACKFILL-END
