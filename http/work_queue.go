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

func (s *Server) registerTransactionRoutes(r chi.Router) {
	r.Get("/{id}", s.handleTransactionUpdates)
	r.Delete("/{id}", s.handleCancelTransaction)
}

// GET "transactions/{id}"
//
// This is a websocket endpoint, the connection is upgraded to a websocket connection
// and updates are fed to the client. After the final status message "Done" or "Cancelled" the
// connection is closed.
func (s *Server) handleTransactionUpdates(w http.ResponseWriter, r *http.Request) {
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

	timer := time.NewTicker(websocketPingConnections)
	defer timer.Stop()
	defer conn.Close()
	defer sub.Close()
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

// DELETE "transactions/{id}"
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

// pushTransaction is a helper method to conviniently push transactions to the work queue.
//
// calling this method doesent require the caller to hold a mutex since the publish call
// to the work queue should be inheritedly race safe and should produce unique ids.
//
// A transaction with a populated id field or an non nil error are returned.
func (s *Server) pushTransaction(ctx context.Context, data any) (*csb.Transaction, error) {
	ctx, cancel := context.WithCancel(ctx)
	transaction := &csb.Transaction{
		Data: data,
		Ctx:  ctx,
	}

	if err := s.WorkQueue.Publish(transaction); err != nil {
		return nil, err
	}
	s.cancelTransactions[transaction.Id] = cancel

	return transaction, nil
}
