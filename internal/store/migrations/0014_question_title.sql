-- Задача #1264: у вопроса появляется собственный заголовок, а context
-- перестаёт быть отдельной сущностью.
--
-- Колонка context НЕ удаляется: физическое удаление — отдельная задача. Здесь
-- её содержимое дописывается в конец тела через канонический разделитель
-- "\n\n---\n\n", после чего код перестаёт её читать и писать.
--
-- Заголовки существующих строк заполняются не здесь: вывод заголовка из тела
-- живёт в Go (store.DeriveTitle), SQL его не повторяет. Store делает это сразу
-- после применения миграций — см. backfillQuestionTitles.

ALTER TABLE questions ADD COLUMN title TEXT NOT NULL DEFAULT '';

UPDATE questions
SET body = body || char(10) || char(10) || '---' || char(10) || char(10) || context
WHERE context IS NOT NULL AND trim(context) <> '';
