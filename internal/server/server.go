package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/csrf"

	"github.com/hitushen/portnotepro/internal/auth"
	"github.com/hitushen/portnotepro/internal/config"
	"github.com/hitushen/portnotepro/internal/models"
	"github.com/hitushen/portnotepro/internal/realtime"
	"github.com/hitushen/portnotepro/internal/scanner"
	"github.com/hitushen/portnotepro/internal/services/fingerprint"
	"github.com/hitushen/portnotepro/internal/store"
	"github.com/hitushen/portnotepro/internal/targets"
)

// Server 负责协调 HTTP 路由、模板渲染与业务逻辑。
type Server struct {
	cfg       *config.Config
	store     *store.Store
	auth      *auth.Manager
	scanner   *scanner.Manager
	broker    *realtime.Broker
	templates *template.Template
}

const maxPort = 65535

func init() {
	rand.Seed(time.Now().UnixNano())
}

// New 创建并初始化带路由的 Server。
func New(cfg *config.Config, st *store.Store) (*Server, error) {
	broker := realtime.NewBroker()
	scanManager := scanner.NewManager(st, cfg.ScanTimeout, cfg.ScanConcurrency, broker)

	tmpl, err := template.ParseGlob(filepath.Join("web", "templates", "*.tmpl"))
	if err != nil {
		return nil, err
	}

	srv := &Server{
		cfg:       cfg,
		store:     st,
		auth:      auth.NewManager(st, cfg.SessionKey),
		scanner:   scanManager,
		broker:    broker,
		templates: tmpl,
	}
	return srv, nil
}

// Close 关闭后台组件。
func (s *Server) Close() {
	s.scanner.Close()
}

// Handler 返回根 HTTP 处理器。
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Heartbeat("/healthz"))

	csrfMiddleware := csrf.Protect(
		s.cfg.CSRFKey,
		csrf.Secure(false),
		csrf.Path("/"),
		csrf.FieldName("csrf_token"),
	)

	r.Group(func(pub chi.Router) {
		pub.Get("/login", s.showLogin)
		pub.Post("/login", s.handleLogin)
	})

	fileServer := http.FileServer(http.Dir(filepath.Join("web", "static")))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	authRoutes := r.With(s.auth.Middleware)
	authRoutes.Get("/", s.dashboard)
	authRoutes.Post("/logout", s.handleLogout)

	authRoutes.Route("/api", func(api chi.Router) {
		api.Get("/events", s.streamEvents)

		api.Get("/hosts", s.apiListHosts)
		api.Post("/hosts", s.apiCreateHost)
		api.Put("/hosts/{hostID}", s.apiUpdateHost)
		api.Delete("/hosts/{hostID}", s.apiDeleteHost)
		api.Post("/hosts/{hostID}/scan", s.apiScanHost)

		api.Get("/hosts/{hostID}/ports", s.apiListPorts)
		api.Post("/hosts/{hostID}/ports", s.apiCreatePort)
		api.Post("/hosts/{hostID}/ports/bulk_hide", s.apiBulkHidePorts)
		api.Post("/hosts/{hostID}/ports/bulk_delete", s.apiBulkDeletePorts)
		api.Get("/hosts/{hostID}/unused_port", s.apiSuggestPort)
		api.Post("/hosts/{hostID}/ports/bulk_delete", s.apiBulkDeletePorts)

		api.Put("/ports/{portID}", s.apiUpdatePort)
		api.Post("/ports/{portID}/hide", s.apiHidePort)
		api.Post("/ports/{portID}/unhide", s.apiUnhidePort)
		api.Delete("/ports/{portID}", s.apiDeletePort)
	})

	return csrfMiddleware(r)
}

func (s *Server) showLogin(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"CSRFField": template.HTML(csrf.TemplateField(r)),
		"CSRFToken": csrf.Token(r),
		"Error":     r.URL.Query().Get("error"),
	}
	if err := s.templates.ExecuteTemplate(w, "login", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if err := s.auth.Authenticate(w, r, username, password); err != nil {
		http.Redirect(w, r, "/login?error=1", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = s.auth.Logout(w, r)
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hosts, err := s.store.ListHosts(ctx)
	if err != nil {
		http.Error(w, "failed to load hosts", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Hosts":     hosts,
		"Username":  s.auth.Username(r),
		"CSRFField": template.HTML(csrf.TemplateField(r)),
		"CSRFToken": csrf.Token(r),
	}
	if err := s.templates.ExecuteTemplate(w, "dashboard", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) apiListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.store.ListHosts(r.Context())
	if err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	if hosts == nil {
		hosts = []models.Host{}
	}
	writeJSON(w, hosts)
}

func (s *Server) apiCreateHost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Address  string `json:"address"`
		AutoScan bool   `json:"autoScan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Address = strings.TrimSpace(body.Address)
	if body.Name == "" || body.Address == "" {
		writeMessage(w, "name and address required", http.StatusBadRequest)
		return
	}
	address := targets.Normalize(body.Address)
	if address == "" {
		writeMessage(w, "invalid host address", http.StatusBadRequest)
		return
	}
	body.Address = address
	hostID, err := s.store.CreateHost(r.Context(), body.Name, body.Address, body.AutoScan)
	if err != nil {
		if isUniqueHostNameError(err) {
			writeMessage(w, "主机名称已存在，请更换名称", http.StatusConflict)
			return
		}
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	host, _ := s.store.GetHost(r.Context(), hostID)
	writeJSON(w, host)

	s.broker.Publish(realtime.Event{
		Type:   "host_created",
		HostID: hostID,
		Payload: map[string]interface{}{
			"name":    body.Name,
			"address": body.Address,
		},
	})

	s.scanner.ScheduleFullRange(hostID)
}

func (s *Server) apiUpdateHost(w http.ResponseWriter, r *http.Request) {
	hostID, err := parseIDParam(chi.URLParam(r, "hostID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	var body struct {
		Name     string `json:"name"`
		Address  string `json:"address"`
		AutoScan bool   `json:"autoScan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Address = strings.TrimSpace(body.Address)
	if body.Name == "" || body.Address == "" {
		writeMessage(w, "name and address required", http.StatusBadRequest)
		return
	}
	address := targets.Normalize(body.Address)
	if address == "" {
		writeMessage(w, "invalid host address", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateHost(r.Context(), hostID, body.Name, address, body.AutoScan); err != nil {
		if isUniqueHostNameError(err) {
			writeMessage(w, "主机名称已存在，请更换名称", http.StatusConflict)
			return
		}
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	body.Address = address
	host, _ := s.store.GetHost(r.Context(), hostID)
	writeJSON(w, host)
	s.broker.Publish(realtime.Event{
		Type:   "host_updated",
		HostID: hostID,
		Payload: map[string]interface{}{
			"name":     host.Name,
			"address":  host.Address,
			"autoScan": host.AutoScan,
		},
	})
}

func (s *Server) apiDeleteHost(w http.ResponseWriter, r *http.Request) {
	hostID, err := parseIDParam(chi.URLParam(r, "hostID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteHost(r.Context(), hostID); err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
	s.broker.Publish(realtime.Event{
		Type:   "host_deleted",
		HostID: hostID,
	})
}

func (s *Server) apiScanHost(w http.ResponseWriter, r *http.Request) {
	hostID, err := parseIDParam(chi.URLParam(r, "hostID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if ok := s.scanner.ScheduleHost(context.Background(), hostID, true); !ok {
		writeJSON(w, map[string]string{"status": "scanning"})
		return
	}
	writeJSON(w, map[string]string{"status": "scheduled"})
}

func (s *Server) apiListPorts(w http.ResponseWriter, r *http.Request) {
	hostID, err := parseIDParam(chi.URLParam(r, "hostID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	queryParams := r.URL.Query()
	includeHidden := queryParams.Get("hidden") == "1"
	page := intParam(queryParams.Get("page"), 1)
	pageSize := intParam(queryParams.Get("pageSize"), 24)
	if pageSize > 200 {
		pageSize = 200
	}
	if pageSize < 1 {
		pageSize = 24
	}
	sortBy := queryParams.Get("sort")
	order := strings.ToLower(queryParams.Get("order"))
	search := queryParams.Get("q")
	status := queryParams.Get("status")
	if strings.TrimSpace(status) == "" {
		status = models.PortStatusOpen
	}

	ports, total, err := s.store.ListPortsWithQuery(r.Context(), hostID, includeHidden, &store.PortQuery{
		Search:   search,
		Status:   status,
		SortBy:   sortBy,
		SortDesc: order == "desc",
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	if ports == nil {
		ports = []models.Port{}
	}
	visibleCount := 0
	hiddenCount := 0
	for _, port := range ports {
		if port.Hidden {
			hiddenCount++
		} else {
			visibleCount++
		}
	}
	response := map[string]interface{}{
		"ports": ports,
		"visibleCount": visibleCount,
		"hiddenCount": hiddenCount,
		"pagination": map[string]interface{}{
			"page":     page,
			"pageSize": pageSize,
			"total":    total,
		},
	}
	writeJSON(w, response)
}

func (s *Server) apiCreatePort(w http.ResponseWriter, r *http.Request) {
	hostID, err := parseIDParam(chi.URLParam(r, "hostID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	var body struct {
		Number      int    `json:"number"`
		Note        string `json:"note"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if body.Number <= 0 || body.Number > 65535 {
		writeMessage(w, "invalid port number", http.StatusBadRequest)
		return
	}
	serviceName := fingerprint.NameForPort(body.Number)
	if strings.TrimSpace(body.Note) == "" {
		body.Note = serviceName
	}
	if strings.TrimSpace(body.Fingerprint) == "" {
		body.Fingerprint = serviceName
	}
	portID, err := s.store.CreatePort(r.Context(), hostID, body.Number, body.Note, body.Fingerprint)
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	port, err := s.store.FindPortByNumber(r.Context(), hostID, body.Number)
	if err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, port)
	s.broker.Publish(realtime.Event{
		Type:   "port_created",
		HostID: hostID,
		PortID: portID,
		Payload: map[string]interface{}{
			"number":      body.Number,
			"note":        body.Note,
			"fingerprint": body.Fingerprint,
		},
	})
	go s.scanner.SchedulePorts(hostID, []models.Port{*port})
}

func (s *Server) apiUpdatePort(w http.ResponseWriter, r *http.Request) {
	portID, err := parseIDParam(chi.URLParam(r, "portID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	var body struct {
		Note        string `json:"note"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Note) == "" {
		writeMessage(w, "note required", http.StatusBadRequest)
		return
	}
	existing, err := s.store.GetPort(r.Context(), portID)
	if err != nil {
		writeErr(w, err, http.StatusNotFound)
		return
	}
	if strings.TrimSpace(body.Fingerprint) == "" {
		body.Fingerprint = fingerprint.NameForPort(existing.Number)
	}
	if err := s.store.UpdatePortNote(r.Context(), portID, body.Note, body.Fingerprint); err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "updated"})
	s.broker.Publish(realtime.Event{
		Type:   "port_updated",
		PortID: portID,
		Payload: map[string]interface{}{
			"note":        body.Note,
			"fingerprint": body.Fingerprint,
		},
	})
}

func (s *Server) apiUnhidePort(w http.ResponseWriter, r *http.Request) {
	portID, err := parseIDParam(chi.URLParam(r, "portID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.store.SetPortHidden(r.Context(), portID, false); err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "visible"})
	s.broker.Publish(realtime.Event{
		Type:   "port_visible",
		PortID: portID,
	})
}

func (s *Server) apiBulkHidePorts(w http.ResponseWriter, r *http.Request) {
	hostID, err := parseIDParam(chi.URLParam(r, "hostID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	var body struct {
		Start int  `json:"start"`
		End   int  `json:"end"`
		Hide  bool `json:"hide"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if body.Start <= 0 || body.End <= 0 || body.End < body.Start {
		writeMessage(w, "invalid range", http.StatusBadRequest)
		return
	}
	affected, err := s.store.BulkSetHidden(r.Context(), hostID, body.Start, body.End, body.Hide)
	if err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"status":   "ok",
		"affected": affected,
	})
	eventType := "ports_unhidden"
	if body.Hide {
		eventType = "ports_hidden"
	}
	s.broker.Publish(realtime.Event{
		Type:   eventType,
		HostID: hostID,
		Payload: map[string]interface{}{
			"start":    body.Start,
			"end":      body.End,
			"affected": affected,
		},
	})
}

func (s *Server) apiBulkDeletePorts(w http.ResponseWriter, r *http.Request) {
	hostID, err := parseIDParam(chi.URLParam(r, "hostID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	var body struct {
		Start int `json:"start"`
		End   int `json:"end"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if body.Start <= 0 || body.End <= 0 || body.End < body.Start {
		writeMessage(w, "invalid range", http.StatusBadRequest)
		return
	}
	affected, err := s.store.BulkDeletePorts(r.Context(), hostID, body.Start, body.End)
	if err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"status":   "ok",
		"affected": affected,
	})
	s.broker.Publish(realtime.Event{
		Type:   "ports_deleted",
		HostID: hostID,
		Payload: map[string]interface{}{
			"start":    body.Start,
			"end":      body.End,
			"affected": affected,
		},
	})
}

func (s *Server) apiSuggestPort(w http.ResponseWriter, r *http.Request) {
	hostID, err := parseIDParam(chi.URLParam(r, "hostID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	host, err := s.store.GetHost(r.Context(), hostID)
	if err != nil {
		writeErr(w, err, http.StatusNotFound)
		return
	}

	startPort := intParam(r.URL.Query().Get("start"), 1024)
	if startPort < 1 {
		startPort = 1
	}

	port, err := s.findUnusedPort(r.Context(), host, startPort)
	if err != nil {
		writeErr(w, err, http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"port": port})
}

func (s *Server) apiDeletePort(w http.ResponseWriter, r *http.Request) {
	portID, err := parseIDParam(chi.URLParam(r, "portID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.store.DeletePort(r.Context(), portID); err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
	s.broker.Publish(realtime.Event{
		Type:   "port_deleted",
		PortID: portID,
	})
}

func (s *Server) apiHidePort(w http.ResponseWriter, r *http.Request) {
	portID, err := parseIDParam(chi.URLParam(r, "portID"))
	if err != nil {
		writeErr(w, err, http.StatusBadRequest)
		return
	}
	if err := s.store.SetPortHidden(r.Context(), portID, true); err != nil {
		writeErr(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "hidden"})
	s.broker.Publish(realtime.Event{
		Type:   "port_hidden",
		PortID: portID,
	})
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cleanup := s.broker.Subscribe()
	defer cleanup()

	notify := r.Context().Done()
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case msg := <-ch:
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(msg)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-notify:
			return
		}
	}
}

func defaultNote(port int) string {
	return fingerprint.NameForPort(port)
}

func isUniqueHostNameError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed: hosts.name")
}

func parseIDParam(raw string) (int64, error) {
	return strconv.ParseInt(raw, 10, 64)
}

func intParam(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	return fallback
}

func (s *Server) findUnusedPort(ctx context.Context, host *models.Host, startPort int) (int, error) {
	if startPort < 1 {
		startPort = 1
	}
	existing, err := s.store.ListPorts(ctx, host.ID, true)
	if err != nil {
		return 0, fmt.Errorf("list ports: %w", err)
	}
	used := make(map[int]struct{}, len(existing))
	for _, port := range existing {
		used[port.Number] = struct{}{}
	}

	if startPort > maxPort {
		startPort = 1
	}

	maxRange := maxPort - startPort + 1
	if maxRange <= 0 {
		maxRange = maxPort
	}

	maxAttempts := maxRange
	if maxAttempts > 5000 {
		maxAttempts = 5000
	}

	for i := 0; i < maxAttempts; i++ {
		candidate := startPort + rand.Intn(maxRange)
		if _, usedAlready := used[candidate]; usedAlready {
			continue
		}
		return candidate, nil
	}
	return 0, fmt.Errorf("no available port found")
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, err error, status int) {
	writeMessage(w, err.Error(), status)
}

func writeMessage(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
