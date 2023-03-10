CREATE TABLE IF NOT EXISTS marks(
    id INTEGER PRIMARY KEY,
    student_id INTEGER NOT NULL,
    subject_id INTEGER NOT NULL,
    teacher TEXT NOT NULL,
    percentage INTEGER NOT NULL,
    academic_year INTEGER NOT NULL,
    term INTEGER NOT NULL,
    importance TEXT NOT NULL,
    created_at DATE NOT NULL,

    FOREIGN KEY (student_id)
        REFERENCES students (pid)
            ON DELETE CASCADE
            ON UPDATE NO ACTION
    FOREIGN KEY (subject_id)
        REFERENCES subjects (id)
            ON DELETE CASCADE
            ON UPDATE NO ACTION

    CHECK (percentage >= 0 AND percentage <= 100)
);