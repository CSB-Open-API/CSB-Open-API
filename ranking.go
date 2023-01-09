package csb

import (
	"context"
	"time"
)

// RankingFilter represents a filter used to create ranking reports.
type RankingFilter struct {
	// Filter the students. (roughly the same as csb.StudentsFilter)
	Year *int `json:"year"`

	// Filter the marks. (roughly the same as csb.MarksFilter)
	Subjects []Subject `json:"subjects"`
	Periods  Period    `json:"periods"`
}

// Rank represents a ranking record in the CSB Open API. It is similar to a normal engage
// end of year report but can be generated at any time and used for comparisons with other ranks.
// Moreover the rankings can be created on very specific terms such as specific subjects and time
// periods.
type Rank struct {
	// ID of the rank.
	ID int `json:"id"`

	// Score represents the averge of the report calculated from the marks gained on the
	// provided subjects on the period. Note that the period doesent have to be full.
	//
	// to find out more on how the reports are generated / calculated go to: https://github.com/CSB-Open-API/.github/blob/main/generating_reports.md
	Score int `json:"score"`
	// Postion is an optional field which is populated if the rank was created in a "competitive"
	// enviroment. This means that the rank was populated by the GenerateRankingsReport method.
	Postion int `json:"position,omitempty"`

	// StudentID links to the student which own the rank.
	StudentID int      `json:"pid"`
	Student   *Student `json:"student"`
	// Subjects refers to the actual subjects the rank was created on.
	Subjects []Subject `json:"subjects"`
	// Period refers to the time period on which the rank was generated on.
	Period Period `json:"period"`

	// GeneratedAt refers to the time at which the rank was generated.
	// This field is used to move back or forth in time through records.
	GeneratedAt time.Time `json:"generated_at"`
}

// RankingService represents a ranking service.
//
// It opperates usually on top of the database layer and student, mark and period APIs.
type RankingService interface {
	// GenerateRankingsReport creates a hord comparison based on the ranking filter.
	// It populates the (csb.Rank).Position filter to position each student in the ranking
	// system.
	GenerateRankingsReport(ctx context.Context, rankingFilter RankingFilter) ([]Rank, error)

	// ViewEvolution returns time-series data based on ones past rankings. It fetches ranking
	// with the same period and subjects and steps back offset times in the data to truncate
	// the result set.
	ViewEvolution(ctx context.Context, pid, offset int, period Period, subject []Subject) ([]Rank, error)

	// CreateBackupRank creates a "snapshot in time" of your current academic progress.
	// This will further be able to be used for: hord comparisons, self comparisons, 1-1 comparisons and
	// generally looking back at ones marks.
	//
	// The period doesnt need to be full and the subjects can be nil / empty (all subject will be taken).
	CreateBackupRank(ctx context.Context, pid int, period Period, subjects []Subject) (Rank, error)

	// DeleteRank deletes a rank with the provided id.
	DeleteRank(ctx context.Context, id int) error
}
