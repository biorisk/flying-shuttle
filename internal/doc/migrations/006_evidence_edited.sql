-- edited marks an evidence row whose excerpt text was changed by the author
-- (trim / splice / free-form rewrite) so it no longer matches
-- chunk[char_start:char_end]. `shuttle doctor` uses it to tell a deliberate
-- customization from a chunk that changed underneath the citation.
ALTER TABLE evidence ADD COLUMN edited INTEGER NOT NULL DEFAULT 0;
