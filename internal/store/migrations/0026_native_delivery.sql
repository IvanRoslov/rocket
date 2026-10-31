ALTER TABLE sessions ADD COLUMN delivery_transport TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN delivery_instance TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN delivery_endpoint TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN delivery_transport TEXT NOT NULL DEFAULT '';
