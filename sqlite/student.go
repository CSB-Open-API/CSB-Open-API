package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	csb "github.com/Lambels/CSB-Open-API"
	"github.com/Lambels/CSB-Open-API/engage"
)

var _ csb.StudentService = (*StudentService)(nil)

// StudentService wraps around an engage client.
type StudentService struct {
	// db for persistance.
	db *DB
	// client for updates.
	c *engage.Client
	// fallback indicates wether fetch to new students should be saved.
	fallback bool
}

// NewStudentService creates a new student service with the provided database and engage client.
func NewStudentService(db *DB, client *engage.Client, fallback bool) *StudentService {
	return &StudentService{
		db:       db,
		c:        client,
		fallback: fallback,
	}
}

// FindStudentByPID returns a student based on the passed pid.
//
// If the student isnt originally found in the database, the service will try to search
// engage using the engage client, if the user isnt found, ultimately ENOTFOUND is returned.
//
// If the user is found in engage and not in the db and saveNew is true the
// user is saved before returned.
func (s *StudentService) FindStudentByPID(ctx context.Context, pid int) (*csb.Student, error) {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	student, err := findStudentByPID(ctx, tx, pid)
	switch csb.ErrorCode(err) {
	case "":
		if err := attachStudentMarks(ctx, tx, student); err != nil {
			return nil, err
		}

		return student, nil

	case csb.ENOTFOUND:
		student, err := s.findStudentByPIDEngage(ctx, pid)
		if err != nil {
			return nil, err
		}
		if !s.fallback {
			return student, nil
		}

		if err := createStudent(ctx, tx, student); err != nil {
			return student, nil
		}
		return student, tx.Commit()

	default:
		return nil, err
	}
}

// FindStudents returns a range of students based on the filter.
func (s *StudentService) FindStudents(ctx context.Context, filter csb.StudentFilter) ([]*csb.Student, error) {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	students, err := findStudents(ctx, tx, filter)
	if err != nil {
		return nil, err
	}

	for _, student := range students {
		if err := attachStudentMarks(ctx, tx, student); err != nil {
			return nil, err
		}
	}

	return students, nil
}

// DeleteStudent permanently deletes a student specified by pid.
// returns ENOTFOUND if student isnt found.
func (s *StudentService) DeleteStudent(ctx context.Context, pid int) error {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil
	}
	defer tx.Rollback()

	if err := deleteStudent(ctx, tx, pid); err != nil {
		return err
	}

	return tx.Commit()
}

// RefreshStudents refreshes students incrementally starting from refresh.StartPID, refresh.N
// times.
//
// If a student is in engage but not in the local copy of students then the student is added
// to the local copy. If refresh.Purge is set to true and the student from engage is not
// attending school then the copy from engage to local storage wont be made.
//
// If refresh.Purge is set to true and a student in the local database is not attending the school
// any more in engage, then the user is deleted.
//
// If the student is both in engage and local storage, an update will be so that your local
// storage has the newest data.
func (s *StudentService) RefreshStudents(ctx context.Context, refresh csb.RefreshStudents) error {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	PIDCount := refresh.StartPID
	for i := 0; i < refresh.N; i++ {
		// engage copy.
		studentEngage, err := s.findStudentByPIDEngage(ctx, PIDCount)
		if err != nil && csb.ErrorCode(err) != csb.ENOTFOUND {
			return err
		}

		// local copy.
		studentLocal, err := findStudentByPID(ctx, tx, PIDCount)
		if err != nil && csb.ErrorCode(err) != csb.ENOTFOUND {
			return err
		}

		switch {
		case studentEngage == nil && studentLocal == nil:
			// no data from engage or local db.
		case studentEngage != nil && studentLocal == nil:
			// engage ahead of local db.
			// if engage is ahead of local db with students who dont attend the school
			// and this request is actively purgeing, skip the creation.
			if !studentEngage.AttendsSchool && refresh.Purge {
				break
			}

			if err := createStudent(ctx, tx, studentEngage); err != nil {
				return err
			}
		case studentEngage != nil && studentLocal != nil:
			if !studentEngage.AttendsSchool && refresh.Purge {
				if err := deleteStudent(ctx, tx, PIDCount); err != nil {
					return err
				}

				break
			}

			// data from both engage and local db, update local db.
			if err := updateStudent(ctx, tx, studentLocal, studentEngage); err != nil {
				return err
			}
		}

		PIDCount++

		// dont spam engage.
		select {
		case <-ctx.Done():
			return fmt.Errorf("refresh students: %w", ctx.Err())
		case <-time.After(engage.RequestTimeout):
		}
	}

	return nil
}

func findStudentByPID(ctx context.Context, tx *sql.Tx, pid int) (*csb.Student, error) {
	s, err := findStudents(ctx, tx, csb.StudentFilter{PID: &pid})
	if err != nil {
		return nil, err
	} else if len(s) == 0 {
		return nil, csb.Errorf(csb.ENOTFOUND, "student not found")
	}

	return s[0], nil
}

func (s *StudentService) findStudentByPIDEngage(ctx context.Context, pid int) (*csb.Student, error) {
	stud := new(csb.Student)
	stud.PID = pid

	academicYears, err := s.c.GetAcademicYears(ctx, pid)
	if err != nil {
		return nil, err
	}

	for _, academicYear := range academicYears {
		if academicYear == csb.CurrentAcademicYear {
			stud.AttendsSchool = true
			break
		}
	}

	periods, err := s.c.GetReportingPeriods(ctx, pid, academicYears)
	if err != nil {
		return nil, err
	}

	subjects, err := s.c.GetReportingSubjects(ctx, pid, academicYears, periods)
	if err != nil {
		return nil, err
	}
	stud.Subjects = subjects

	columns, err := s.c.GetColumnsForSubjects(ctx, pid, academicYears, periods, subjects)
	if err != nil {
		return nil, err
	}

	renderAll, err := s.c.GetMarksheetRender(ctx, pid, academicYears, columns, periods, subjects)
	if err != nil {
		return nil, err
	}

	stud.Name = engage.NameFromRender(renderAll)
	if stud.AttendsSchool {
		stud.CurrentYear = engage.CurrentYearFromRender(renderAll)
	}

	return stud, nil
}

func findStudents(ctx context.Context, tx *sql.Tx, filter csb.StudentFilter) ([]*csb.Student, error) {
}

func createStudent(ctx context.Context, tx *sql.Tx, student *csb.Student) error {
	if err := student.Validate(); err != nil {
		return err
	}

	student.CreatedAt = time.Now()
	student.UpdatedAt = student.CreatedAt
	currYear := sql.NullInt64{Int64: int64(student.CurrentYear), Valid: student.AttendsSchool}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO students (
			pid,
			name,
			current_year,
			attends_school,
			created_at,
			updated_at,
		) VALUES (?, ?, ?, ?, ?, ?)
	`,
		student.PID,
		student.Name,
		currYear,
		student.AttendsSchool,
		student.CreatedAt,
		student.UpdatedAt,
	)
	if err != nil {
		return err
	}

	for _, subject := range student.Subjects {
		if err := attachSubjectCode(ctx, tx, student.PID, subject.EngageCode); err != nil {
			return err
		}
	}
	return nil
}

func updateStudent(ctx context.Context, tx *sql.Tx, prev, next *csb.Student) error {
	if err := next.Validate(); err != nil {
		return err
	}

	var addSubjects []csb.Subject
	if len(next.Subjects) != len(prev.Subjects) {
		diff := len(next.Subjects) - len(prev.Subjects)
		addSubjects = make([]csb.Subject, diff)

		diffMap := make(map[string]struct{}, len(prev.Subjects))
		for _, v := range prev.Subjects {
			diffMap[v.EngageCode] = struct{}{}
		}

		for _, v := range next.Subjects {
			if _, ok := diffMap[v.EngageCode]; !ok {
				addSubjects = append(addSubjects, v)
			}
		}
	}

	// add new subjects.
	for _, v := range addSubjects {
		if err := attachSubjectCode(ctx, tx, prev.PID, v.EngageCode); err != nil {
			return err
		}
	}
	// change in current year and current school atendance.
	currYear := sql.NullInt64{Int64: int64(next.CurrentYear), Valid: next.AttendsSchool}
	// change updated at.
	next.UpdatedAt = time.Now()

	_, err := tx.ExecContext(ctx, `
		UPDATE students SET
			current_year = ?,
			attends_school = ?,
			updated_at = ?
		WHERE pid = ?
	`,
		currYear,
		next.AttendsSchool,
		next.UpdatedAt,
		prev.PID,
	)
	return err
}

func deleteStudent(ctx context.Context, tx *sql.Tx, pid int) error {
	if _, err := findStudentByPID(ctx, tx, pid); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `DELETE FROM students WHERE pid = ?`, pid)
	return err
}

func attachStudentMarks(ctx context.Context, tx *sql.Tx, student *csb.Student) (err error) {
	if student.Marks, err = findMarksByPID(ctx, tx, student.PID); err != nil {
		fmt.Errorf("attach student marks: %w", err)
	}
	return nil
}
