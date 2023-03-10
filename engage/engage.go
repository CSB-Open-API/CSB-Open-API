package engage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	csb "github.com/Lambels/CSB-Open-API"
)

const RequestTimeout time.Duration = 2 * time.Second

var (
	yearRegexp       = regexp.MustCompile(`Year\s\d+`)
	nameRegexp       = regexp.MustCompile(`<a>(.*?)<\/a>`)
	percentageRegexp = regexp.MustCompile(`\\t\d+`)
)

// engageContext holds all relevant information when making a engage request.
// It is used by the default post method.
type engageContext struct {
	Text             string `json:"Text,omitempty"`
	NumberOfItems    int    `json:"NumberOfItems,omitempty"`
	AcademicYears    string `json:"academicYears,omitempty"`
	ReportingPeriods string `json:"reportingPeriods,omitempty"`
	YearGroupList    string `json:"yearGroupList,omitempty"`
	SubjectList      string `json:"subjectList,omitempty"`
	DivisionList     string `json:"divisionList,omitempty"`
	BatchList        string `json:"batchList,omitempty"`
	PupilIDs         string `json:"pupilIDs"`
}

// engageResponse encapsulates most engage responses.
// It is used by the default post method.
type engageResponse struct {
	D []engageData `json:"d"`
}

type engageData struct {
	Type       string `json:"__type"`
	Text       string `json:"Text"`
	Value      string `json:"Value"`
	Enabled    bool   `json:"Enabled"`
	Attributes struct {
		Checked     bool   `json:"Checked"`
		IsReporting bool   `json:"IsReporting"`
		ColumnType  string `json:"ColumnType"`
	} `json:"Attributes"`
}

// renderMarksheetRequest represents a request to the render marksheet endpoint.
type renderMarksheetRequest struct {
	AcademicYear                  string `json:"academicYear"`
	ReportingPeriodList           string `json:"reportingPeriodList"`
	YearGroupList                 string `json:"yearGroupList"`
	SubjectList                   string `json:"subjectList"`
	DivisionList                  string `json:"divisionList"`
	BatchList                     string `json:"batchList"`
	ColumnList                    string `json:"columnList"`
	PupilIDs                      string `json:"pupilIDs"`
	UniqueID                      string `json:"uniqueID"`
	SetAsPreference               bool   `json:"setAsPreference"`
	DefaultReportingPeriod        string `json:"defaultReportingPeriod"`
	PageIndex                     string `json:"pageIndex"`
	SortField                     string `json:"sortField"`
	SortDirection                 string `json:"sortDirection"`
	Sortable                      bool   `json:"sortable"`
	ShowPupilName                 bool   `json:"showPupilName"`
	AllowCollapseMarksheetColumns string `json:"allowCollapseMarksheetColumns"`
	EnableFrozenHeadings          bool   `json:"enableFrozenHeadings"`
	FilterSearch                  bool   `json:"filterSearch"`
	Page                          int    `json:"page"`
	PageSize                      int    `json:"pageSize"`
}

// error represents an error message from engage, this message will be parsed to a csb error.
type engageError struct {
	Message       string `json:"Message"`
	StackTrace    string `json:"StackTrace"`
	ExceptionType string `json:"ExceptionType"`
}

// decodeError tries to decode the body of a non OK status request in an error
// understandable by the rest of the api.
func decodeError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(body) == 0 {
		return csb.Errorf(csb.FromStatusToErrorCode(resp.StatusCode), "engage: error body empty")
	}

	var engErr engageError
	if err := json.Unmarshal(body, &engErr); err != nil {
		return csb.Errorf(csb.FromStatusToErrorCode(resp.StatusCode), "engage: couldnt decode error body")
	}

	return csb.Errorf(csb.FromStatusToErrorCode(resp.StatusCode), "engage: %v , stack trace: %v , exception type: %v", engErr.Message, engErr.StackTrace, engErr.ExceptionType)
}

// NameFromRender returns the pupils name from the render and the last index.
//
// returns ENOTFOUND if no result matches.
func NameFromRender(renderBuf []byte) (string, int, error) {
	res := nameRegexp.FindIndex(renderBuf)
	if res == nil {
		return "", -1, csb.Errorf(csb.ENOTFOUND, "couldnt match any name from current render")
	}

	out := string(renderBuf[res[0]:res[1]])
	out = strings.TrimLeft(out, "<a>")
	out = strings.TrimRight(out, "</a>")

	return out, res[1], nil
}

// CurrentYearFromRender returns the pupils current year from the render and the last index.
//
// returns ENOTFOUND if no result matches.
func CurrentYearFromRender(renderBuf []byte) (int, int, error) {
	res := yearRegexp.FindAllIndex(renderBuf, -1)
	if res == nil {
		return -1, -1, csb.Errorf(csb.ENOTFOUND, "couldnt match any year from current render")
	}

	lastX := len(res) - 1
	out := string(renderBuf[res[lastX][0]:res[lastX][1]])
	out = strings.TrimLeft(out, "Year ")

	year, err := strconv.Atoi(out)
	if err != nil {
		return -1, -1, err
	}

	return year, res[lastX][1], nil
}

// GetMarkFromRender returns the first mark populated with the subject name, teacher name and
// percentage.
func GetMarkFromRender(renderBuf []byte) (_ *csb.Mark, n int, _ error) {
	mark := new(csb.Mark)

	// get the percentage first.
	res := percentageRegexp.FindIndex(renderBuf)
	if res == nil {
		return nil, -1, csb.Errorf(csb.ENOTFOUND, "couldnt match any percentage from current render")
	}

	raw := string(renderBuf[res[0]:res[1]])
	percentage, err := strconv.Atoi(strings.TrimPrefix(raw, "\t"))
	if err != nil {
		return nil, -1, err
	}
	mark.Percentage = percentage
	n += res[1]

	// get subject name.
	subject, remX := getSubjectName(renderBuf[n:])
	if remX == -1 {
		return nil, -1, errors.New("unexpected buffer index")
	}
	mark.Subject = csb.Subject{
		Name: subject,
	}
	n += remX

	// get teacher name.
	teacher, remX := getTeacherName(renderBuf[n:])
	if remX == 1 {
		return nil, -1, errors.New("unexpected buffer index")
	}
	mark.Teacher = teacher
	n += remX

	return mark, n, nil
}

func getSubjectName(buf []byte) (string, int) {
	x := bytes.IndexByte(buf, ',')
	if x == -1 {
		return "", -1
	}

	y := bytes.IndexByte(buf[x+2:], ',')
	if y == -1 {
		return "", -1
	}

	return string(buf[x+2 : y]), y
}

func getTeacherName(buf []byte) (string, int) {
	x := bytes.IndexByte(buf, '<')
	if x == -1 {
		return "", -1
	}

	y := bytes.LastIndexByte(buf[:x+1], ',')
	if y == -1 {
		return "", -1
	}

	return string(buf[y+2 : y]), y
}
