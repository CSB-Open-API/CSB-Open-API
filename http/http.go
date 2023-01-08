package http

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	csb "github.com/Lambels/CSB-Open-API"
)

const (
	// time allowed for connections to resolve before server shuts down.
	serverShutdownTime = 3 * time.Second
	// heartbeat for websocket connections.
	websocketPingConnections = 5 * time.Second
	websocketWriteTimeout    = 5 * time.Second
)

// errResponse represents the strucuture of an error sent over http.
type errResponse struct {
	Status int    `json:"status"`
	Trace  string `json:"trace"`
}

// SendErr sends the err over http and logs internal errors.
func SendErr(w http.ResponseWriter, r *http.Request, err error) {
	code, message := csb.ErrorCode(err), csb.ErrorMessage(err)

	if code == csb.EINTERNAL {
		LogError(r, err)
	}

	status := csb.FromErrorCodeToStatus(code)
	w.WriteHeader(status)
	WriteJSON(w, errResponse{Status: status, Trace: message})
}

func LogError(r *http.Request, err error) {
	log.Printf("[HTTP] error: %s %s: %s\n", r.URL.Path, r.Method, err)
}

func WriteJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	return enc.Encode(data)
}
