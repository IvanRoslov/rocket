-- Вопрос шторма (тип треда brainstorm, задача #4901): номер рекомендованного
-- агентом варианта, выбранный человеком вариант, его комментарий, откуда пришёл
-- ответ (ui | terminal) и вычисленный исход (accepted | corrected |
-- wrong_turn), который человек может переопределить. Номера вариантов 1-based;
-- NULL — варианта нет. У старых тредов всё пусто.

ALTER TABLE questions ADD COLUMN recommended_option INTEGER;
ALTER TABLE questions ADD COLUMN chosen_option INTEGER;
ALTER TABLE questions ADD COLUMN answer_comment TEXT NOT NULL DEFAULT '';
ALTER TABLE questions ADD COLUMN answer_source TEXT NOT NULL DEFAULT '';
ALTER TABLE questions ADD COLUMN outcome TEXT NOT NULL DEFAULT '';
ALTER TABLE questions ADD COLUMN outcome_overridden INTEGER NOT NULL DEFAULT 0;
