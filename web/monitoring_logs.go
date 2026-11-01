package web

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"bridge-gateway/domain"
)

const (
	logStatusOK    = "ok"
	logStatusError = "error"
	// logDayLayout est la valeur d'un champ date : un jour entier, borne
	// incluse des deux cotes.
	logDayLayout = "2006-01-02"
)

// logTimeLayouts sont les formes acceptees pour une borne. Les trois premieres
// sont locales : comparer un jour saisi en UTC a des horodatages locaux
// decalerait la journee de plusieurs heures.
var logTimeLayouts = []string{logDayLayout, "2006-01-02T15:04", "2006-01-02T15:04:05"}

// logBound est une borne de date deja convertie, avec son niveau de precision.
type logBound struct {
	at       time.Time
	wholeDay bool
}

// logFilter est l'etat des filtres de la modale. Une valeur vide signifie
// "tous", donc la modale et les exports partagent exactement le meme etat.
type logFilter struct {
	Provider string
	Model    string
	Status   string
	// From et To gardent la saisie brute pour la reafficher dans les champs ;
	// from et to sont les bornes converties.
	From        string
	To          string
	from        time.Time
	to          time.Time
	toExclusive bool
}

// isZero indique qu'aucun filtre n'est actif : la liste peut alors etre rendue
// telle quelle, sans copie.
func (f logFilter) isZero() bool {
	return f.Provider == "" && f.Model == "" && f.Status == "" && f.From == "" && f.To == ""
}

// Active expose l'etat des filtres aux templates (bouton de reinitialisation).
func (f logFilter) Active() bool { return !f.isZero() }

// match applique les criteres. Provider et modele sont compares sans tenir
// compte de la casse, pour qu'une saisie manuelle ou une URL bricolee retrouve
// les entrees enregistrees.
func (f logFilter) match(entry domain.CallLog) bool {
	if f.Provider != "" && !strings.EqualFold(entry.Provider, f.Provider) {
		return false
	}
	if f.Model != "" && !strings.EqualFold(entry.Model, f.Model) {
		return false
	}
	switch f.Status {
	case logStatusOK:
		return entry.Error == ""
	case logStatusError:
		return entry.Error != ""
	}
	if !f.withinDates(entry.Time) {
		return false
	}
	return true
}

// withinDates compare l'entree aux bornes. Une borne haute sur un jour entier
// est exclusive : elle pointe le lendemain a minuit, ce qui inclut toute la
// journee demandee. Une borne haute horodatee reste inclusive.
func (f logFilter) withinDates(moment time.Time) bool {
	if !f.from.IsZero() && moment.Before(f.from) {
		return false
	}
	if f.to.IsZero() {
		return true
	}
	if f.toExclusive {
		return moment.Before(f.to)
	}
	return !moment.After(f.to)
}

// query rend la chaine a reporter sur les liens d'export, pour qu'un
// telechargement contienne les memes entrees que la modale.
func (f logFilter) query() string {
	values := url.Values{}
	if f.Provider != "" {
		values.Set("provider", f.Provider)
	}
	if f.Model != "" {
		values.Set("model", f.Model)
	}
	if f.Status != "" {
		values.Set("status", f.Status)
	}
	if f.From != "" {
		values.Set("from", f.From)
	}
	if f.To != "" {
		values.Set("to", f.To)
	}
	encoded := values.Encode()
	if encoded == "" {
		return ""
	}
	return "?" + encoded
}

// monitoringLogsView est le modele de la modale de logs : les appels retenus
// par les filtres, les valeurs proposees dans les listes deroulantes et la
// requete a reporter sur les exports.
type monitoringLogsView struct {
	Title       string
	Active      string
	Total       int
	Filter      logFilter
	Providers   []string
	Models      []string
	ExportQuery string
	Logs        []domain.CallLog
}

// logsModal rend le contenu de la modale (chargé par HTMX dans #logs-modal).
func (s *monitoringController) logsModal(w http.ResponseWriter, r *http.Request) {
	filter := parseLogFilter(r)
	calls := s.calls()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "monitoring_logs", monitoringLogsView{
		Title:       "Logs",
		Active:      "monitoring",
		Total:       len(calls),
		Filter:      filter,
		Providers:   providersOf(calls),
		Models:      modelsOf(calls),
		ExportQuery: filter.query(),
		Logs:        filterCalls(calls, filter),
	})
}

// logsJSON exporte le journal filtre : les memes champs que dans la modale,
// destination d'un telechargement ou d'un script.
func (s *monitoringController) logsJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="bridge-gateway-logs.json"`)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(filterCalls(s.calls(), parseLogFilter(r)))
}

// logsCSV exporte le journal filtre a plat, une ligne par appel : le format
// attendu pour un tableur.
func (s *monitoringController) logsCSV(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="bridge-gateway-logs.csv"`)
	_ = writeLogsCSV(w, filterCalls(s.calls(), parseLogFilter(r)))
}

// calls rend le journal, vide quand le collecteur n'est pas branche (tests de
// l'UI ou demarrage partiel).
func (s *monitoringController) calls() []domain.CallLog {
	if s.metrics == nil {
		return nil
	}
	return s.metrics.Logs()
}

func parseLogFilter(r *http.Request) logFilter {
	query := r.URL.Query()
	status := strings.TrimSpace(query.Get("status"))
	if status != logStatusOK && status != logStatusError {
		status = ""
	}
	filter := logFilter{
		Provider: strings.TrimSpace(query.Get("provider")),
		Model:    strings.TrimSpace(query.Get("model")),
		Status:   status,
	}
	filter.applyBounds(strings.TrimSpace(query.Get("from")), strings.TrimSpace(query.Get("to")))
	return filter
}

// applyBounds convertit les bornes saisies. Une valeur illisible est ignoree
// plutot que rejetee : la modale s'ouvre toujours, simplement sans la borne.
func (f *logFilter) applyBounds(rawFrom, rawTo string) {
	if bound, ok := parseLogBound(rawFrom); ok {
		f.From = rawFrom
		f.from = bound.at
	}
	bound, ok := parseLogBound(rawTo)
	if !ok {
		return
	}
	f.To = rawTo
	f.to = bound.at
	if bound.wholeDay {
		f.to = bound.at.Add(24 * time.Hour)
		f.toExclusive = true
	}
}

func parseLogBound(value string) (logBound, bool) {
	if value == "" {
		return logBound{}, false
	}
	for _, layout := range logTimeLayouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return logBound{at: parsed, wholeDay: layout == logDayLayout}, true
		}
	}
	return logBound{}, false
}

func filterCalls(calls []domain.CallLog, filter logFilter) []domain.CallLog {
	if filter.isZero() {
		return calls
	}
	out := make([]domain.CallLog, 0, len(calls))
	for _, entry := range calls {
		if filter.match(entry) {
			out = append(out, entry)
		}
	}
	return out
}

func providersOf(calls []domain.CallLog) []string {
	return callValues(calls, func(entry domain.CallLog) string { return entry.Provider })
}

func modelsOf(calls []domain.CallLog) []string {
	return callValues(calls, func(entry domain.CallLog) string { return entry.Model })
}

// callValues rend les valeurs distinctes d'un champ, triees, sans les vides :
// les listes deroulantes proposent toujours l'integralite du journal, meme
// quand un filtre est actif, pour pouvoir en changer.
func callValues(calls []domain.CallLog, field func(domain.CallLog) string) []string {
	seen := make(map[string]bool, len(calls))
	out := make([]string, 0, len(calls))
	for _, entry := range calls {
		value := strings.TrimSpace(field(entry))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// writeLogsCSV serialise le journal. L'en-tete est ecrit meme sans entree :
// un fichier exporte doit rester lisible dans un tableur vide.
func writeLogsCSV(dest io.Writer, logs []domain.CallLog) error {
	writer := csv.NewWriter(dest)
	header := []string{"time", "provider", "model", "stream", "duration_ms", "input", "output", "error"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, entry := range logs {
		row := []string{
			entry.Time.UTC().Format(time.RFC3339Nano),
			entry.Provider,
			entry.Model,
			strconv.FormatBool(entry.Stream),
			fmt.Sprintf("%.2f", float64(entry.Duration)/float64(time.Millisecond)),
			entry.Input,
			entry.Output,
			entry.Error,
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
