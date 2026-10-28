-- inject_at: unix milliseconds taken right before the text was pasted into a
-- recipient whose agent confirms delivery from its own journal (the Codex
-- path, status "accepted"); 0 for every other message. It is the lower bound
-- for the journal search and the reference point for the "turn ended without
-- confirmation" rule.
ALTER TABLE messages ADD COLUMN inject_at INTEGER NOT NULL DEFAULT 0;
