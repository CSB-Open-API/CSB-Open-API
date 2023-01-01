CREATE TABLE IF NOT EXISTS subjects(
    id INTEGER PRIMARY KEY,
    engage_code TEXT NOT NULL,
    name TEXT NOT NULL,

    UNQIUE(engage_code)
    UNQIUE(name)
)

-- many 2 many relationships:
CREATE TABLE IF NOT EXISTS student_takes(
    student_id INTEGER NOT NULL,
    subject_id INTEGER NOT NULL,

    FOREIGN KEY (student_id)
        REFERENCES students (pid)
            ON DELETE CASCADE
            ON UPDATE NO ACTION;
    FOREIGN KEY (subject_id)
        REFERENCES subjects (id)
            ON DELETE CASCADE
            ON UPDATE NO ACTION;
    UNQIUE(student_id, subject_id) -- one student takes a subject only once.
)