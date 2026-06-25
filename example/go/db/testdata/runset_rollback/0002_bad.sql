-- First statement succeeds (creates a table), second statement fails. Because
-- the whole migration runs in one transaction, the table creation must be
-- rolled back — runset_rb_partial must not exist after this migration fails.
CREATE TABLE runset_rb_partial (id INT);
INSERT INTO definitely_missing_table VALUES (1);
