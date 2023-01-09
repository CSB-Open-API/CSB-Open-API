package sqlite

import (
	"context"
	"database/sql"

	csb "github.com/Lambels/CSB-Open-API"
)

func findSubjectByName(ctx context.Context, tx *sql.Tx, name string) (csb.Subject, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			id,
			engage_code,
			name,
		FROM subjects
		WHERE name = ?
	`,
		name,
	)

	var subject csb.Subject
	err := row.Scan(
		&subject.ID,
		&subject.EngageCode,
		&subject.Name,
	)
	if err == sql.ErrNoRows {
		err = csb.Errorf(csb.ENOTFOUND, "no subject found")
	}

	return subject, err
}

func findSubjectByID(ctx context.Context, tx *sql.Tx, id int) (csb.Subject, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			id,
			engage_code,
			name,
		FROM subjects
		WHERE id = ?
	`,
		id,
	)

	var subject csb.Subject
	err := row.Scan(
		&subject.ID,
		&subject.EngageCode,
		&subject.Name,
	)
	if err == sql.ErrNoRows {
		err = csb.Errorf(csb.ENOTFOUND, "no subject found")
	}

	return subject, err
}

func findSubjectByEngageCode(ctx context.Context, tx *sql.Tx, code string) (csb.Subject, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			id,
			engage_code,
			name,
		FROM subjects
		WHERE engage_code = ?
	`,
		code,
	)

	var subject csb.Subject
	err := row.Scan(
		&subject.ID,
		&subject.EngageCode,
		&subject.Name,
	)
	if err == sql.ErrNoRows {
		err = csb.Errorf(csb.ENOTFOUND, "no subject found")
	}

	return subject, err
}

func attachSubjectCodeToStudent(ctx context.Context, tx *sql.Tx, pid int, code string) error {
	subject, err := findSubjectByEngageCode(ctx, tx, code)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO student_takes (
			student_id,
			subject_id,
		) VALUES (?, ?)
	`,
		pid,
		subject.ID,
	)

	return err
}
