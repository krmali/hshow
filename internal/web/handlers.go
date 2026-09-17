// Package web provides the HTTP handlers that render the dashboard.
package web

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"time"

	"hshow/internal/dashboard"
	"hshow/internal/hledger"
)

// Server renders the dashboard by querying hledger on each request.
type Server struct {
	tmpl   *template.Template
	runner hledger.Runner
	now    func() time.Time
	topN   int
}

// NewServer parses templates from templatesFS and returns a Server that
// queries the given hledger Runner on each request.
func NewServer(templatesFS fs.FS, runner hledger.Runner) (*Server, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{
		tmpl:   tmpl,
		runner: runner,
		now:    time.Now,
		topN:   5,
	}, nil
}

// expensesPrefix is the hledger account (sub)tree treated as "expenses" for
// the charts below.
const expensesPrefix = "expenses"

// expenseChartData is marshaled to JSON and consumed by Chart.js in the
// dashboard partial to render the expense pie and bar charts.
type expenseChartData struct {
	Labels   []string  `json:"labels"`
	Current  []float64 `json:"current"`
	Previous []float64 `json:"previous"`
}

// viewData is the data passed to the dashboard templates.
type viewData struct {
	GeneratedAt time.Time
	Changes     []dashboard.Change
	Expenses    []dashboard.Change
	ChartJSON   template.JS
	Error       string
}

func (s *Server) loadView() viewData {
	now := s.now()
	curStart, curEnd, prevStart, prevEnd := dashboard.PeriodBounds(now)

	current, err := s.runner.Balances(curStart, curEnd)
	if err != nil {
		return viewData{GeneratedAt: now, Error: err.Error()}
	}
	previous, err := s.runner.Balances(prevStart, prevEnd)
	if err != nil {
		return viewData{GeneratedAt: now, Error: err.Error()}
	}

	expenses := dashboard.TopChanges(
		dashboard.FilterByPrefix(current, expensesPrefix),
		dashboard.FilterByPrefix(previous, expensesPrefix),
		s.topN,
	)

	return viewData{
		GeneratedAt: now,
		Changes:     dashboard.TopChanges(current, previous, s.topN),
		Expenses:    expenses,
		ChartJSON:   expenseChartJSON(expenses),
	}
}

func expenseChartJSON(expenses []dashboard.Change) template.JS {
	data := expenseChartData{
		Labels:   make([]string, len(expenses)),
		Current:  make([]float64, len(expenses)),
		Previous: make([]float64, len(expenses)),
	}
	for i, e := range expenses {
		data.Labels[i] = e.Account
		data.Current[i] = math.Abs(e.Current)
		data.Previous[i] = math.Abs(e.Previous)
	}
	b, err := json.Marshal(data)
	if err != nil {
		// data is a fixed, JSON-safe struct; Marshal cannot fail here.
		return "{}"
	}
	return template.JS(b)
}

// Routes registers the server's handlers on the given mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /dashboard", s.handleDashboardPartial)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, "layout", s.loadView())
}

func (s *Server) handleDashboardPartial(w http.ResponseWriter, r *http.Request) {
	s.render(w, "dashboard-partial", s.loadView())
}

func (s *Server) render(w http.ResponseWriter, name string, data viewData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
