ALTER TABLE creative_runs ADD COLUMN closure_state TEXT NOT NULL DEFAULT 'open' CHECK(closure_state IN ('open','closing','accepted'));
ALTER TABLE creative_runs ADD COLUMN acceptance_requested_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE creative_runs ADD COLUMN accepted_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE creative_runs ADD COLUMN closure_error TEXT NOT NULL DEFAULT '';

-- Generic messaging/question paths share these tables with Development. Check
-- ownership in SQL so a stale request cannot race the durable closure freeze.
CREATE TRIGGER creative_closed_message_insert BEFORE INSERT ON messages
WHEN EXISTS (SELECT 1 FROM creative_session_contexts c JOIN creative_runs r ON r.id=c.run_id WHERE r.closure_state<>'open' AND (c.session_id=NEW.to_session OR c.session_id=NEW.from_session))
BEGIN SELECT RAISE(ABORT,'creative work is read-only'); END;
CREATE TRIGGER creative_closed_question_message BEFORE INSERT ON question_messages
WHEN EXISTS (SELECT 1 FROM creative_discussion_contexts c JOIN creative_runs r ON r.id=c.run_id WHERE c.question_id=NEW.question_id AND r.closure_state<>'open')
BEGIN SELECT RAISE(ABORT,'creative work is read-only'); END;
CREATE TRIGGER creative_closed_question_update BEFORE UPDATE ON questions
WHEN EXISTS (SELECT 1 FROM creative_discussion_contexts c JOIN creative_runs r ON r.id=c.run_id WHERE c.question_id=OLD.id AND r.closure_state<>'open')
BEGIN SELECT RAISE(ABORT,'creative work is read-only'); END;
CREATE TRIGGER creative_closed_session_resume BEFORE UPDATE OF state ON sessions
WHEN NEW.state IN ('running','spawning') AND EXISTS (SELECT 1 FROM creative_session_contexts c JOIN creative_runs r ON r.id=c.run_id WHERE c.session_id=OLD.id AND r.closure_state<>'open')
BEGIN SELECT RAISE(ABORT,'creative work is read-only'); END;
