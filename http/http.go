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

	status := FromErrorCodeToStatus(code)
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

var codes = map[string]int{
	csb.ECONFLICT:       http.StatusConflict,
	csb.EINVALID:        http.StatusBadRequest,
	csb.ENOTFOUND:       http.StatusNotFound,
	csb.ENOTIMPLEMENTED: http.StatusNotImplemented,
	csb.EUNAUTHORIZED:   http.StatusUnauthorized,
	csb.EINTERNAL:       http.StatusInternalServerError,
}

// FromErrorCodeToStatus maps a csb error code to a http status code, if no mapping is possible
// status code 500 is returned.
func FromErrorCodeToStatus(code string) int {
	if v, ok := codes[code]; ok {
		return v
	}
	return http.StatusInternalServerError
}

// FromStatusToErrorCode maps a http status code to a csb error code, if no mapping is possible
// csb.EINTERNAL is returned.
func FromStatusToErrorCode(code int) string {
	for k, v := range codes {
		if v == code {
			return k
		}
	}
	return csb.EINTERNAL
}
