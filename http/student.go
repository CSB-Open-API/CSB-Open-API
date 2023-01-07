package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	csb "github.com/Lambels/CSB-Open-API"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
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

	ctx, cancel := context.WithCancel(r.Context())
	transaction := &csb.Transaction{
		Data: refresh,
		Ctx:  ctx,
	}
	if err := s.WorkQueue.Publish(transaction); err != nil {
		SendErr(w, r, err)
		return
	}

	s.cancelTransactions[transaction.Id] = cancel
	if err := WriteJSON(w, transaction); err != nil {
		LogError(r, err)
	}
}

// DELETE "students/refresh/{id}"
//
// handleCancelTransaction cancels the transaction with the provided id. returns 404
// if the transaction isnt found and 204 if the transaction was cancelled.
func (s *Server) handleCancelTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt((chi.URLParam(r, "id")), 10, 64)
	if err != nil {
		SendErr(w, r, csb.Errorf(csb.EINVALID, "invalid id format"))
		return
	}

	s.transactionMu.Lock()
	defer s.transactionMu.Unlock()
	cancel, ok := s.cancelTransactions[id]
	if !ok {
		SendErr(w, r, csb.Errorf(csb.ENOTFOUND, "transaction not found"))
		return
	}
	cancel()
	delete(s.cancelTransactions, id)
	w.WriteHeader(http.StatusNoContent)
}

// GET "students/refresh/{id}"
//
// This is a websocket endpoint, the connection is upgraded to a websocket connection
// and updates are fed to the client. After the final status message "Done" or "Cancelled" the
// connection is closed.
func (s *Server) handleRefreshUpdates(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt((chi.URLParam(r, "id")), 10, 64)
	if err != nil {
		SendErr(w, r, csb.Errorf(csb.EINVALID, "invalid id format"))
		return
	}

	sub, err := s.WorkQueue.Subscribe(r.Context(), id)
	if err != nil {
		SendErr(w, r, err)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		LogError(r, err)
		return
	}
	// close subscription when peer closing.
	conn.SetCloseHandler(func(code int, text string) error {
		sub.Close()

		closeMsg := websocket.FormatCloseMessage(code, "")
		conn.WriteControl(code, closeMsg, time.Now().Add(1*time.Second))
		return nil
	})

	timer := time.NewTicker(websocketPingConnections)
	defer timer.Stop()
	defer conn.Close()
	for {
		select {
		case status, ok := <-sub.C():
			// subscription closed, notify peer that the connection is closing.
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			sendBuf, err := json.Marshal(status)
			if err != nil {
				LogError(r, err)
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, sendBuf); err != nil {
				LogError(r, err)
				return
			}

		case <-timer.C:
			conn.SetWriteDeadline(time.Now().Add(websocketWriteTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				LogError(r, err)
				return
			}
		}
	}
}
