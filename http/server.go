package http

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	csb "github.com/Lambels/CSB-Open-API"
	"github.com/Lambels/CSB-Open-API/engage"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gorilla/websocket"
)

// Server represents an http server which exposes the injected services over http.
//
// It is used to provide an abstraction from the net/http package when running the http server.
type Server struct {
	server   *http.Server
	router   *chi.Mux
	upgrader *websocket.Upgrader

	// The URL address of the server.
	Addr string
	// The URL address of the frontend server.
	FrontendURL string
	// The engage Token. This field is validated on each request and on startup.
	Token string

	// Services exposed via http.
	WorkQueue      csb.WorkQueue
	MarkService    csb.MarkService
	StudentService csb.StudentService
	PeriodService  csb.PeriodService
	EngageClient   *engage.Client

	// keep track of transaction contexts.
	transactionMu      sync.Mutex
	cancelTransactions map[int64]context.CancelFunc

	closed atomic.Bool
}

// NewServer creates a new server instance.
func NewServer() *Server {
	s := &Server{
		server: &http.Server{},
		router: chi.NewRouter(),
		upgrader: &websocket.Upgrader{
			HandshakeTimeout: 3 * time.Second,
			CheckOrigin:      func(r *http.Request) bool { return true },
		},
	}

	// common middleware.
	s.router.Use(chimw.Logger)
	s.router.Use(s.validateTokenMiddleware)
	s.router.Use(chimw.SetHeader("Content-Type", "application/json"))
	s.router.Use(cors.Handler(
		cors.Options{
			AllowedOrigins:   []string{s.FrontendURL},
			AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions},
			AllowCredentials: true,
		},
	))

	// routes for creating and reading transactions.
	s.router.Route("/transactions", func(r chi.Router) {
		s.registerTransactionRoutes(r)
	})
	// routes for refreshing, getting and deleting students.
	s.router.Route("/students", func(r chi.Router) {
		s.registerStudentRoutes(r)
	})
	// routes for refreshing, getting and deleting marks.
	s.router.Route("/marks", func(r chi.Router) {
		s.registerMarkRoutes(r)
	})
	// routes for building, validating and generating period ranges.
	s.router.Route("/periods", func(r chi.Router) {
		s.registerPeriodRoutes(r)
	})

	s.server.Handler = s.router
	return s
}

// Listen validates the token and starts listening on the provided address using the
// (*http.Server).Serve() method.
func (s *Server) Listen() error {
	if err := s.validateToken(); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}

	return s.server.Serve(ln)
}

// Close gracefully closes the http server and closes the work queue.
//
// no-op if already closed.
func (s *Server) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTime)
		defer cancel()
		if err := s.server.Shutdown(ctx); err != nil {
			return err
		}

		// close the work queue since the server is the only writer to the work queue.
		return s.WorkQueue.Close()
	}
	return nil
}

// validateTokenMiddleware validates that before a request is carried out a valid token
// is possesed.
//
// it closes the server if there is an invalid token.
func (s *Server) validateTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.validateToken(); err != nil {
			log.Printf("Invalid token %v, exiting...\n", err)
			s.Close()
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) validateToken() error {
	_, err := s.EngageClient.GetAcademicYears(context.Background(), csb.PatrickArvatuPID)
	return err
}
