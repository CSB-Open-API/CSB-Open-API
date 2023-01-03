CREATE TABLE IF NOT EXISTS students (
    pid INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    current_year INTEGER,
    attends_school BOOLEAN NOT NULL CHECK (attends_school IN (0, 1)),
    created_at DATE NOT NULL,
    updated_at DATE NOT NULL
) WITHOUT ROWID;