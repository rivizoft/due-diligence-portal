-- Keep version numbers monotonic even after deleting the newest version.
CREATE TABLE IF NOT EXISTS document_version_counters (
    document_id TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
    last_version INTEGER NOT NULL
);
INSERT INTO document_version_counters (document_id, last_version)
SELECT document_id, MAX(version_number) FROM document_versions GROUP BY document_id
ON CONFLICT(document_id) DO UPDATE SET last_version = MAX(last_version, excluded.last_version);
CREATE TRIGGER IF NOT EXISTS document_version_counter_insert
AFTER INSERT ON document_versions BEGIN
    INSERT INTO document_version_counters (document_id, last_version)
    VALUES (NEW.document_id, NEW.version_number)
    ON CONFLICT(document_id) DO UPDATE SET last_version = MAX(last_version, excluded.last_version);
END;

-- Preserve archived document references without exposing deleted categories.
CREATE TABLE IF NOT EXISTS deleted_categories (
    category_id TEXT PRIMARY KEY REFERENCES categories(id) ON DELETE CASCADE
);

CREATE TRIGGER IF NOT EXISTS document_category_active_insert
BEFORE INSERT ON documents WHEN EXISTS (SELECT 1 FROM deleted_categories WHERE category_id = NEW.category_id)
BEGIN SELECT RAISE(ABORT, 'category deleted'); END;
CREATE TRIGGER IF NOT EXISTS document_category_active_update
BEFORE UPDATE OF category_id ON documents WHEN EXISTS (SELECT 1 FROM deleted_categories WHERE category_id = NEW.category_id)
BEGIN SELECT RAISE(ABORT, 'category deleted'); END;
CREATE TRIGGER IF NOT EXISTS category_parent_active_insert
BEFORE INSERT ON categories WHEN EXISTS (SELECT 1 FROM deleted_categories WHERE category_id = NEW.parent_id)
BEGIN SELECT RAISE(ABORT, 'parent category deleted'); END;
CREATE TRIGGER IF NOT EXISTS category_parent_active_update
BEFORE UPDATE OF parent_id ON categories WHEN EXISTS (SELECT 1 FROM deleted_categories WHERE category_id = NEW.parent_id)
BEGIN SELECT RAISE(ABORT, 'parent category deleted'); END;
