package sqlite

import (
	"context"
	"database/sql"
	"time"

	csb "github.com/Lambels/CSB-Open-API"
	"github.com/Lambels/CSB-Open-API/engage"
)

var _ csb.MarkService = (*MarkService)(nil)

// MarkService wraps aroun an engage client and period service.
type MarkService struct {
	// db for persistance.
	db *DB
	// client for updates.
	c *engage.Client
	// periodService is used to handle periods.
	periodService csb.PeriodService
	// fallback indicates wether failed searches should fallback on the engage client.
	fallback bool
}

// NewMarkService creates a new a new mark service with the provided database, engage client and period service.
func NewMarkService(db *DB, fallback bool, client *engage.Client, periodService csb.PeriodService) *MarkService {
	return &MarkService{
		db:            db,
		c:             client,
		periodService: periodService,
		fallback:      fallback,
	}
}

// FindMarkByID returns a marked based on the passed id.
//
// returns ENOTFOUND if the mark isnt found.
func (s *MarkService) FindMarkByID(ctx context.Context, id int) (*csb.Mark, error) {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	mark, err := findMarkByID(ctx, tx, id)
	if err != nil {
		return nil, err
	} else if err := attachMarkAssociations(ctx, tx, mark); err != nil {
		return nil, err
	}

	return mark, nil
}

// FindMarksByPID returns a range of marks based on the pupil id.
//
// find marks only fetches local marks. To get all marks, use refresh handler.
func (s *MarkService) FindMarksByPID(ctx context.Context, pid int) ([]*csb.Mark, error) {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// find marks by PID only works for local data since we are potentially dealing with allot
	// of marks and we dont want to spam engage. For loading more data use refresh marks.
	marks, err := findMarksByPID(ctx, tx, pid)
	if err != nil {
		return nil, err
	} else if err := attachMarksSubjectsWithStudent(ctx, tx, pid, marks); err != nil {
		return nil, err
	}

	return marks, nil
}

// FindMarksByPeriod returns a range of marks for the specified period.
//
// If the period is full, the request will use the fallback parameter and fallback on the engage
// client optinally.
//
// If the period isnt full, the request will simply provide the local data.
func (s *MarkService) FindMarksByPeriod(ctx context.Context, pid int, period csb.Period) (marks []*csb.Mark, err error) {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	full, err := period.Full()
	if err != nil {
		return nil, err
	}

	if full {
		marks, err = s.findMarksByFullPeriodFallback(ctx, tx, pid, period)
	} else { // if the period isnt full use local data since we are potentially dealing with allot of data.
		periods, err := s.periodService.BuildPeriods(ctx, pid, period.AcademicYear, *period.Term)
		if err != nil {
			return nil, err
		}

		marks, err = findMarks(ctx, tx, csb.MarksFilter{PID: &pid, Periods: periods})
	}

	if err != nil {
		return nil, err
	} else if err := attachMarksSubjectsWithStudent(ctx, tx, pid, marks); err != nil {
		return nil, err
	}

	return marks, tx.Commit()
}

// FindMarksByPeriodRange returns a range of marks in the specified period range.
//
// All the data will be fetched from local storage.
func (s *MarkService) FindMarksByPeriodRange(ctx context.Context, from, to csb.Period, filter csb.MarksFilter) (_ []*csb.Mark, err error) {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if filter.PID == nil {
		return nil, csb.Errorf(csb.EINVALID, "cannot generate marks over period range without a student id")
	}
	// if valid id provided save requests to engage for building period range.
	if filter.ID != nil {
		mark, err := findMarkByID(ctx, tx, *filter.ID)
		if err != nil {
			return nil, err
		} else if err := attachMarkAssociations(ctx, tx, mark); err != nil {
			return nil, err
		}

		return []*csb.Mark{mark}, nil
	}

	filter.Periods, err = s.periodService.PeriodRange(ctx, *filter.PID, from, to)
	if err != nil {
		return nil, err
	}

	marks, err := findMarks(ctx, tx, filter)
	if err != nil {
		return nil, err
	} else if err := attachMarksSubjectsWithStudent(ctx, tx, *filter.PID, marks); err != nil {
		return nil, err
	}
	return marks, nil
}

// FindMarks returns a range of marks based on filter.
func (s *MarkService) FindMarks(ctx context.Context, filter csb.MarksFilter) ([]*csb.Mark, error) {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	marks, err := findMarks(ctx, tx, filter)
	if err != nil {
		return nil, err
	}

	for _, mark := range marks {
		if err := attachMarkAssociations(ctx, tx, mark); err != nil {
			return nil, err
		}
	}

	return marks, nil
}

// DeleteMark permanently deletes a mark with the specified id.
//
// returns ENOTFOUND if the mark isnt found.
func (s *MarkService) DeleteMark(ctx context.Context, id int) error {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := deleteMark(ctx, tx, id); err != nil {
		return err
	}

	return tx.Commit()
}

// RefreshMarks refreshes marks for the student with pid = pid from the period range.
//
// It only checks for new marks since it is very uncommon that a mark gets updated or
// deleted.
func (s *MarkService) RefreshMarks(ctx context.Context, pid int, from, to csb.Period) error {
	// asign student manualy since we are again potentially dealing with allot of marks
	// and dont want to spam engage or the local database.
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := findStudentByPID(ctx, tx, pid); err != nil {
		return err
	}

	periods, err := s.periodService.PeriodRange(ctx, pid, from, to)
	if err != nil {
		return err
	}

	for len(periods) > 0 {
		period := periods[0]

		marksLocal, err := findMarksByFullPeriod(ctx, tx, pid, period)
		if err != nil {
			return err
		} else if err := attachMarksSubjectsWithStudent(ctx, tx, pid, marksLocal); err != nil {
			return err
		}

		marksEngage, err := s.findMarksByFullPeriodEngage(ctx, tx, pid, period)
		if err != nil {
			return err
		} else if err := attachMarksSubjectsWithStudent(ctx, tx, pid, marksEngage); err != nil {
			return err
		}

		if err := createDiff(ctx, tx, marksLocal, marksEngage); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(engage.RequestTimeout):
		}
		periods = periods[1:]
	}

	return nil
}

func findMarkByID(ctx context.Context, tx *sql.Tx, id int) (*csb.Mark, error) {
	m, err := findMarks(ctx, tx, csb.MarksFilter{ID: &id})
	if err != nil {
		return nil, err
	} else if len(m) == 0 {
		return nil, csb.Errorf(csb.ENOTFOUND, "mark not found")
	}

	return m[0], nil
}

func findMarksByPID(ctx context.Context, tx *sql.Tx, pid int) ([]*csb.Mark, error) {
	m, err := findMarks(ctx, tx, csb.MarksFilter{PID: &pid})
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (s *MarkService) findMarksByFullPeriodEngage(ctx context.Context, tx *sql.Tx, pid int, period csb.Period) ([]*csb.Mark, error) {
	engageTerm, err := s.periodService.PeriodToEngageTerm(ctx, pid, period)
	if err != nil {
		return nil, err
	}

	bufResp, err := s.c.GetMarksheetRender(
		ctx,
		pid,
		[]int{period.AcademicYear},
		[]string{engageTerm},
		[]string{*period.Importance},
		nil,
	)

	marks, _, err := engage.GetMarksFromRender(bufResp)
	return marks, nil
}

func findMarksByFullPeriod(ctx context.Context, tx *sql.Tx, pid int, period csb.Period) ([]*csb.Mark, error) {
	m, err := findMarks(ctx, tx, csb.MarksFilter{PID: &pid, Periods: []csb.Period{period}})
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (s *MarkService) findMarksByFullPeriodFallback(ctx context.Context, tx *sql.Tx, pid int, period csb.Period) (marks []*csb.Mark, err error) {
	marks, err = findMarksByFullPeriod(ctx, tx, pid, period)
	if err != nil {
		return nil, err
	}

	if s.fallback {
		engageMarks, err := s.findMarksByFullPeriodEngage(ctx, tx, pid, period)
		if err != nil {
			return nil, err
		}

		if err := createDiff(ctx, tx, marks, engageMarks); err != nil {
			return nil, err
		}
		marks = engageMarks
	}

	return marks, nil
}

func findMarks(ctx context.Context, tx *sql.Tx, filter csb.MarksFilter) ([]*csb.Mark, error) {

}

func deleteMark(ctx context.Context, tx *sql.Tx, id int) error {
	if _, err := findMarkByID(ctx, tx, id); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `DELETE FROM marks WHERE id = ?`, id)
	return err
}

func createMark(ctx context.Context, tx *sql.Tx, mark *csb.Mark) error {
	if err := mark.Validate(); err != nil {
		return err
	}

	mark.CreatedAt = time.Now()

	_, err := tx.ExecContext(ctx, `
		INSERT INTO marks (
			student_id,
			subject_id,
			teacher,
			percentage,
			academic_year,
			term,
			importance,
			created_at,
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		mark.StudentID,
		mark.SubjectID,
		mark.Teacher,
		mark.Percentage,
		mark.Period.AcademicYear,
		mark.Period.Term,
		mark.Period.Importance,
		mark.CreatedAt,
	)
	return err
}

func createDiff(ctx context.Context, tx *sql.Tx, local, engage []*csb.Mark) error {
	// marks in the same period can only have different subjects, do a shallow difference
	// check on only the subjects to save cpu usage.
	//
	// also just add new marks, it isnt often that marks get deleted or updated.
	diff := make(map[string]struct{}, len(local))
	for _, markLocal := range local {
		diff[markLocal.Subject.Name] = struct{}{}
	}

	for _, markEngage := range engage {
		if _, ok := diff[markEngage.Subject.Name]; !ok {
			if err := createMark(ctx, tx, markEngage); err != nil {
				return err
			}
		}
	}

	return nil
}

func attachMarkAssociations(ctx context.Context, tx *sql.Tx, mark *csb.Mark) (err error) {
	mark.Student, err = findStudentByPID(ctx, tx, mark.StudentID)
	if err != nil {
		return err
	}

	return attachMarkSubject(ctx, tx, mark)
}

func attachMarksSubjectsWithStudent(ctx context.Context, tx *sql.Tx, pid int, marks []*csb.Mark) (err error) {
	student, err := findStudentByPID(ctx, tx, pid)
	if err != nil {
		return err
	}

	for _, m := range marks {
		m.Student = student
		if err := attachMarkSubject(ctx, tx, m); err != nil {
			return err
		}
	}

	return nil
}

func attachMarkSubject(ctx context.Context, tx *sql.Tx, mark *csb.Mark) (err error) {
	switch {
	// have name, want engage code + id.
	case mark.Subject.Name != "":
		mark.Subject, err = findSubjectByName(ctx, tx, mark.Subject.Name)

	// have id, want engage code + name.
	case mark.Subject.ID != 0:
		mark.Subject, err = findSubjectByID(ctx, tx, mark.Subject.ID)
	}

	return err
}
