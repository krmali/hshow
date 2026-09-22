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

const (
	expensesPrefix = "expenses"
	cycleDay       = 25 // billing cycle starts on the 25th of each month
	expenseTopN    = 7
)

// trackingStart is the fixed date from which the average daily expense is computed.
var trackingStart = time.Date(2022, time.December, 5, 0, 0, 0, 0, time.UTC)

// expenseChartData is marshaled to JSON and consumed by Chart.js.
type expenseChartData struct {
	Labels   []string  `json:"labels"`
	Current  []float64 `json:"current"`
	Previous []float64 `json:"previous"`
}

// viewData is the data passed to the dashboard templates.
type viewData struct {
	GeneratedAt        time.Time
	CycleStart         time.Time
	Changes            []dashboard.Change // top 5 across all accounts
	ExpenseTop7        []dashboard.Change // top 7 expense accounts
	TotalCycleExpenses float64
	TotalPrevExpenses  float64
	TotalDiff          float64
	AvgDailyExpenses   float64
	TrackingDays       int
	ChartJSON          template.JS
	Error              string
}

func (s *Server) loadView() viewData {
	now := s.now()
	curStart, curEnd, prevStart, prevEnd := dashboard.PeriodBounds(now, cycleDay)

	current, err := s.runner.Balances(curStart, curEnd)
	if err != nil {
		return viewData{GeneratedAt: now, Error: err.Error()}
	}
	previous, err := s.runner.Balances(prevStart, prevEnd)
	if err != nil {
		return viewData{GeneratedAt: now, Error: err.Error()}
	}

	currentExp := dashboard.FilterByPrefix(current, expensesPrefix)
	previousExp := dashboard.FilterByPrefix(previous, expensesPrefix)
	expenseTop7 := dashboard.TopChanges(currentExp, previousExp, expenseTopN)
	totalCycle := sumBalances(currentExp)
	totalPrev := sumBalances(previousExp)

	// All-time average daily expenses since the tracking start date.
	allTime, err := s.runner.Balances(trackingStart, curEnd, "^"+expensesPrefix)
	if err != nil {
		// Non-fatal: show zero rather than fail the whole page.
		log.Printf("all-time expense query failed: %v", err)
		allTime = nil
	}
	totalAllTime := sumBalances(allTime)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	trackDays := int(today.Sub(trackingStart).Hours() / 24)
	var avgDaily float64
	if trackDays > 0 {
		avgDaily = totalAllTime / float64(trackDays)
	}

	return viewData{
		GeneratedAt:        now,
		CycleStart:         curStart,
		Changes:            dashboard.TopChanges(current, previous, s.topN),
		ExpenseTop7:        expenseTop7,
		TotalCycleExpenses: totalCycle,
		TotalPrevExpenses:  totalPrev,
		TotalDiff:          totalCycle - totalPrev,
		AvgDailyExpenses:   avgDaily,
		TrackingDays:       trackDays,
		ChartJSON:          expenseChartJSON(expenseTop7),
	}
}

func sumBalances(bs []hledger.Balance) float64 {
	var total float64
	for _, b := range bs {
		total += b.Amount
	}
	return total
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
