package engage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	csb "github.com/Lambels/CSB-Open-API"
)

var (
	baseURL               = "https://cambridgeschoolportal.engagehosted.com/Services/ReportCommentServices.asmx/"
	academicYearsURL      = "GetMarksheetAcademicYears"
	reportingPeriodsURL   = "GetReportingPeriods"
	reportingSubjectsURL  = "GetPupilMarksheetSubjects"
	columnsForSubjectsURL = "GetColumnsForSubjects"
	marksheetRenderURL    = "RenderPupilMarksheet"
)

// Client is a client used to interface with the engage api.
type Client struct {
	cc *http.Client
}

// GetAcademicYears gets all the possible academic years for a PID.
func (c *Client) GetAcademicYears(ctx context.Context, pid int) ([]int, error) {
	resURL := baseURL + academicYearsURL

	res, err := c.post(ctx, resURL, engageContext{PupilIDs: fmt.Sprint(pid)})
	if err != nil {
		return nil, err
	}

	out := make([]int, len(res.D))
	for _, data := range res.D {
		v, err := strconv.Atoi(data.Value)
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, nil
}

// GetReportingPeriods gets the reporting periods for a PID in a specific range of academic years.
func (c *Client) GetReportingPeriods(ctx context.Context, pid int, academicYears []int) ([]string, error) {
	resURL := baseURL + reportingPeriodsURL

	res, err := c.post(ctx, resURL, engageContext{
		PupilIDs:      fmt.Sprint(pid),
		AcademicYears: concatAcademicYears(academicYears),
	})
	if err != nil {
		return nil, err
	}

	out := make([]string, len(res.D))
	for _, data := range res.D {
		out = append(out, data.Value)
	}

	return out, nil
}

// GetReportingSubjects gets the reporting subjects for a PID in a specific range of academic years and reporting periods (terms).
func (c *Client) GetReportingSubjects(ctx context.Context, pid int, academicYears []int, reportingTerms []string) ([]csb.Subject, error) {
	resURL := baseURL + reportingSubjectsURL

	res, err := c.post(ctx, resURL, engageContext{
		PupilIDs:         fmt.Sprint(pid),
		AcademicYears:    concatAcademicYears(academicYears),
		ReportingPeriods: strings.Join(reportingTerms, ","),
	})
	if err != nil {
		return nil, err
	}

	out := make([]csb.Subject, len(res.D))
	for _, data := range res.D {
		out = append(out, csb.Subject{EngageCode: data.Value})
	}

	return out, nil
}

// GetColumnsForSubjects gets the "columns" for a pid in the specified academic years and periods range (terms) for the specified subjects.
// A column refers to the type of exam.
func (c *Client) GetColumnsForSubjects(ctx context.Context, pid int, academicYears []int, reportingTerms []string, subjects []csb.Subject) ([]string, error) {
	resURL := baseURL + columnsForSubjectsURL

	res, err := c.post(ctx, resURL, engageContext{
		PupilIDs:         fmt.Sprint(pid),
		AcademicYears:    concatAcademicYears(academicYears),
		ReportingPeriods: strings.Join(reportingTerms, ","),
		SubjectList:      concatSubjects(subjects),
	})
	if err != nil {
		return nil, err
	}

	out := make([]string, len(res.D))
	for _, data := range res.D {
		out = append(out, data.Value)
	}

	return out, nil
}

func (c *Client) GetMarksheetRender(ctx context.Context, pid int, academicYears []int, reportingTerms, reportingColumns []string, reportingSubjects []csb.Subject) ([]byte, error) {
	resURL := baseURL + marksheetRenderURL

	body, err := json.Marshal(renderMarksheetRequest{
		PupilIDs:                      fmt.Sprint(pid),
		AcademicYear:                  concatAcademicYears(academicYears),
		ReportingPeriodList:           strings.Join(reportingTerms, ","),
		ColumnList:                    strings.Join(reportingColumns, "|||"),
		SubjectList:                   concatSubjects(reportingSubjects),
		UniqueID:                      "Portal_PupilDetails",
		SetAsPreference:               true,
		PageIndex:                     "0",
		SortField:                     "Surname",
		SortDirection:                 "ASC",
		Sortable:                      true,
		ShowPupilName:                 true,
		AllowCollapseMarksheetColumns: "true",
		FilterSearch:                  true,
		Page:                          1,
		PageSize:                      500,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := c.cc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(resp)
	}

	// 9.2KB indicates that the user wasnt found for some reason?
	if resp.ContentLength == 9200 {
		return nil, csb.Errorf(csb.ENOTFOUND, "couldnt find pupil")
	}

	buf := make([]byte, resp.ContentLength)
	_, err = resp.Body.Read(buf)
	return buf, err
}

// post sends a post request to url with the specified engage context. It checks for any errors during
// the exchange process with engage. It returns an engage response which has
// at least one piece of data inside.
func (c *Client) post(ctx context.Context, url string, engCtx engageContext) (res *engageResponse, err error) {
	body, err := json.Marshal(engCtx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := c.cc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	// parse response here for status code not found since engage is very wierd with response
	// codes. (an invalid pid results in StatusCodeOK)
	if len(res.D) == 0 {
		return nil, csb.Errorf(csb.ENOTFOUND, "engage: invalid PID: %v", engCtx.PupilIDs)
	}

	return res, nil
}

// NewClient creates a new engage client with the provided token used for
// authentification.
func NewClient(c *http.Client, token string) *Client {
	c.Transport = &cookieHeaderTransport{
		cookie: token,
		d:      c.Transport,
	}

	return &Client{
		cc: c,
	}
}

type cookieHeaderTransport struct {
	cookie string
	d      http.RoundTripper
}

func (t *cookieHeaderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Add("Cookie", t.cookie)
	return t.d.RoundTrip(r)
}

func concatSubjects(subjects []csb.Subject) string {
	switch len(subjects) {
	case 0:
		return ""
	case 1:
		return subjects[0].EngageCode
	}

	n := len(subjects) - 1
	for _, v := range subjects {
		n += len(v.EngageCode)
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(subjects[0].EngageCode)
	for _, v := range subjects {
		b.WriteByte(',')
		b.WriteString(v.EngageCode)
	}

	return b.String()
}

func concatAcademicYears(years []int) string {
	switch len(years) {
	case 0:
		return ""
	case 1:
		return fmt.Sprint(years[0])
	}

	// assume years will be length of 4, at least for the next 8,000 years.
	n := (len(years) - 1) * 5 // 5 since we concat with "," and the 4 digit long year. (1 + 4)

	var b strings.Builder
	b.Grow(n)
	b.WriteString(fmt.Sprint(years[0]))
	for _, v := range years {
		b.WriteByte(',')
		b.WriteString(fmt.Sprint(v))
	}

	return b.String()
}
