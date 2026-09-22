// Package web provides the HTTP handlers that render the dashboard.
package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"log"
	"math"
	"net/http"
	"time"

	"hshow/internal/dashboard"
	"hshow/internal/hledger"
)

const (
	expensesPrefix = "expenses"
	cycleDay       = 25
	expenseTopN    = 7
	sessionCookie  = "hshow_session"
)

var trackingStart = time.Date(2022, time.December, 5, 0, 0, 0, 0, time.UTC)

// Server renders the dashboard by querying hledger on each request.
type Server struct {
	tmpl        *template.Template
	staticFS    fs.FS
	runner      hledger.Runner
	now         func() time.Time
	topN        int
	cookieToken string // empty means no auth required
	hasAuth     bool
}

// NewServer parses templates from assetsFS and returns a Server.
// password may be empty to disable authentication.
func NewServer(assetsFS fs.FS, runner hledger.Runner, password string) (*Server, error) {
	tmpl, err := template.ParseFS(assetsFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}

	staticFS, err := fs.Sub(assetsFS, "static")
	if err != nil {
		return nil, err
	}

	s := &Server{
		tmpl:     tmpl,
		staticFS: staticFS,
		runner:   runner,
		now:      time.Now,
		topN:     5,
	}

	if password != "" {
		s.cookieToken = deriveToken(password)
		s.hasAuth = true
	}

	return s, nil
}

// deriveToken produces a stable session token from a password using HMAC-SHA256.
// Changing the password automatically invalidates all existing sessions.
func deriveToken(password string) string {
	mac := hmac.New(sha256.New, []byte(password))
	mac.Write([]byte("session"))
	return hex.EncodeToString(mac.Sum(nil))
}

// isAuthenticated reports whether the request carries a valid session cookie.
func (s *Server) isAuthenticated(r *http.Request) bool {
	if !s.hasAuth {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(s.cookieToken))
}

// requireAuth is middleware that redirects unauthenticated requests to /login.
// For htmx requests it sets HX-Redirect instead of a 302.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.isAuthenticated(r) {
			next(w, r)
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/login")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func (s *Server) setSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.cookieToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   365 * 24 * 60 * 60, // 1 year
	})
}

// expenseChartData is marshaled to JSON and consumed by Chart.js.
type expenseChartData struct {
	Labels   []string  `json:"labels"`
	Current  []float64 `json:"current"`
	Previous []float64 `json:"previous"`
}

// viewData is the data passed to all dashboard templates.
type viewData struct {
	GeneratedAt        time.Time
	CycleStart         time.Time
	Changes            []dashboard.Change
	ExpenseTop7        []dashboard.Change
	TotalCycleExpenses float64
	TotalPrevExpenses  float64
	TotalDiff          float64
	AvgDailyExpenses   float64
	TrackingDays       int
	ChartJSON          template.JS
	HasAuth            bool
	Error              string
}

func (s *Server) loadView() viewData {
	now := s.now()
	curStart, curEnd, prevStart, prevEnd := dashboard.PeriodBounds(now, cycleDay)

	current, err := s.runner.Balances(curStart, curEnd)
	if err != nil {
		return viewData{GeneratedAt: now, HasAuth: s.hasAuth, Error: err.Error()}
	}
	previous, err := s.runner.Balances(prevStart, prevEnd)
	if err != nil {
		return viewData{GeneratedAt: now, HasAuth: s.hasAuth, Error: err.Error()}
	}

	currentExp := dashboard.FilterByPrefix(current, expensesPrefix)
	previousExp := dashboard.FilterByPrefix(previous, expensesPrefix)
	expenseTop7 := dashboard.TopChanges(currentExp, previousExp, expenseTopN)
	totalCycle := sumBalances(currentExp)
	totalPrev := sumBalances(previousExp)

	allTime, err := s.runner.Balances(trackingStart, curEnd, "^"+expensesPrefix)
	if err != nil {
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
		HasAuth:            s.hasAuth,
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

// Routes registers all HTTP handlers on the given mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", s.handleLoginGet)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.HandleFunc("GET /logout", s.handleLogout)
	mux.HandleFunc("GET /manifest.json", s.handleManifest)
	mux.HandleFunc("GET /icon-192.png", func(w http.ResponseWriter, r *http.Request) { s.handleIcon(w, r, 192) })
	mux.HandleFunc("GET /icon-512.png", func(w http.ResponseWriter, r *http.Request) { s.handleIcon(w, r, 512) })
	mux.HandleFunc("GET /sw.js", s.handleSW)
	mux.HandleFunc("GET /{$}", s.requireAuth(s.handleIndex))
	mux.HandleFunc("GET /dashboard", s.requireAuth(s.handleDashboardPartial))
}

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if !s.hasAuth || s.isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.renderLogin(w, "")
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, "Invalid request.")
		return
	}
	password := r.FormValue("password")
	if !s.hasAuth || deriveToken(password) == s.cookieToken {
		s.setSessionCookie(w)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.renderLogin(w, "Incorrect password.")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookie,
		Value:   "",
		Path:    "/",
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	manifest := map[string]any{
		"name":             "hshow",
		"short_name":       "hshow",
		"description":      "Account activity dashboard",
		"start_url":        "/",
		"display":          "standalone",
		"background_color": "#f9fafb",
		"theme_color":      "#2563eb",
		"icons": []map[string]string{
			{"src": "/icon-192.png", "sizes": "192x192", "type": "image/png"},
			{"src": "/icon-512.png", "sizes": "512x512", "type": "image/png"},
		},
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	json.NewEncoder(w).Encode(manifest)
}

// handleIcon generates a solid-color square PNG icon on the fly.
func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request, size int) {
	iconColor := color.RGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0xff} // #2563eb
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.Set(x, y, iconColor)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		http.Error(w, "icon error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(buf.Bytes())
}

// handleSW serves the service worker from the embedded static directory.
func (s *Server) handleSW(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Service-Worker-Allowed", "/")
	data, err := fs.ReadFile(s.staticFS, "sw.js")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Write(data)
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

func (s *Server) renderLogin(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "login", map[string]string{"Error": errMsg}); err != nil {
		log.Printf("render login: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
