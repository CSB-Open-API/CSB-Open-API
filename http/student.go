package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	csb "github.com/Lambels/CSB-Open-API"
	"github.com/go-chi/chi/v5"
)

// registerStudentRoutes registers all the routes of the student service.
func (s *Server) registerStudentRoutes(r chi.Router) {
	// CRUD methods.
	r.Post("/", s.handleGetStudents)
	r.Get("/{pid}", s.handleGetStudent)
	r.Delete("/{pid}", s.handleDeleteStudent)

	// refresh pub/sub endpoints.
	r.Post("/refresh", s.handleRefreshStudent)
	r.Delete("/refresh/{id}", s.handleCancelTransaction)
	r.Get("/refresh/{id}", s.handleRefreshUpdates)
}

// POST "/students"
//
// handleGetStudents parses a student filter from the request body and finds all students
// with the provided filter.
func (s *Server) handleGetStudents(w http.ResponseWriter, r *http.Request) {
	var filter csb.StudentFilter
	if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
		SendErr(w, r, csb.Errorf(csb.EINVALID, "decode: invalid request body"))
		return
	}

	students, err := s.StudentService.FindStudents(r.Context(), filter)
	if err != nil {
		SendErr(w, r, err)
		return
	}

	if err := WriteJSON(w, students); err != nil {
		LogError(r, err)
	}
}

// GET "students/{pid}"
//
// handleGetStudent gets the student with the provided pupil ID. returns 404 if the student
// isnt found.
func (s *Server) handleGetStudent(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(chi.URLParam(r, "pid"))
	if err != nil {
		SendErr(w, r, csb.Errorf(csb.EINVALID, "invalid pupil id format"))
		return
	}

	student, err := s.StudentService.FindStudentByPID(r.Context(), pid)
	if err != nil {
		SendErr(w, r, err)
		return
	}

	if err := WriteJSON(w, student); err != nil {
		LogError(r, err)
	}
}

// DELETE "students/{pid}"
//
// handleDeleteStudent permanently deletes the student with the provided pupil ID. return 404
// if the student isnt found and 204 if the delete is sucessful.
func (s *Server) handleDeleteStudent(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(chi.URLParam(r, "pid"))
	if err != nil {
		SendErr(w, r, csb.Errorf(csb.EINVALID, "invalid pupil id format"))
		return
	}

	if err := s.StudentService.DeleteStudent(r.Context(), pid); err != nil {
		SendErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST "students/refresh"
//
// handleRefreshStudent parses a refresh students filter from the request body and
// queues a transaction on the work queue with the specified request body data.
//
// It returns the scheduled transaction along side the transaction id.
func (s *Server) handleRefreshStudent(w http.ResponseWriter, r *http.Request) {
	var refresh csb.RefreshStudents
	if err := json.NewDecoder(r.Body).Decode(&refresh); err != nil {
		SendErr(w, r, csb.Errorf(csb.EINVALID, "decode: invalid request body"))
		return
	}

	transaction, err := s.pushTransaction(r.Context(), refresh)
	if err != nil {
		SendErr(w, r, err)
		return
	}

	if err := WriteJSON(w, transaction); err != nil {
		LogError(r, err)
	}
}
