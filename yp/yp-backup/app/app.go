package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/rs/cors"
)

type Env struct {
	db *sql.DB
}

func InitServer(db *sql.DB) (*http.Server, error) {
	env := Env{db}
	mux := http.NewServeMux()

	// Sala e times
	mux.HandleFunc("POST /room", env.CreateRoom)
	mux.HandleFunc("POST /join", env.CreateTeam)

	// Etapas
	mux.HandleFunc("POST /stage", env.CreateStage)
	mux.HandleFunc("PATCH /stage", env.SetStageActive)

	// Cenários (professor cadastra)
	mux.HandleFunc("POST /scenario", env.CreateScenario)

	// Jogo (alunos)
	mux.HandleFunc("GET /scenarios", env.GetScenarios)
	mux.HandleFunc("POST /respond", env.Respond)

	// Placar
	mux.HandleFunc("GET /score", env.Scoreboard)

	s := &http.Server{
		Addr:    ":8080",
		Handler: cors.Default().Handler(mux),
	}
	return s, nil
}

// ─── Utilitários ─────────────────────────────────────────────────────────────

func GenerateRandomString() string {
	const a = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	rs := make([]byte, 0, 7)
	for range 7 {
		rs = append(rs, byte(a[rand.Intn(len(a))]))
	}
	return string(rs)
}

func WriteError(funcName string, err error, w http.ResponseWriter) {
	w.WriteHeader(http.StatusInternalServerError)
	slog.Error(fmt.Sprintf("at %s: %v", funcName, err))
}

func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ─── Sala ────────────────────────────────────────────────────────────────────

// POST /room?prof=X&room=Y
// Cria uma sala e retorna o ID gerado.
func (e *Env) CreateRoom(w http.ResponseWriter, r *http.Request) {
	prof := r.URL.Query().Get("prof")
	room := r.URL.Query().Get("room")

	if prof == "" || room == "" {
		http.Error(w, `{"error":"parâmetros 'prof' e 'room' são obrigatórios"}`, http.StatusBadRequest)
		return
	}

	id := GenerateRandomString()
	_, err := e.db.Exec("INSERT INTO room (id, name, professor) VALUES ($1, $2, $3)", id, room, prof)
	if err != nil {
		WriteError("CreateRoom", err, w)
		return
	}

	WriteJSON(w, map[string]string{"id": id})
}

// POST /join?team=X&room=Y
// Cria um time numa sala e retorna o ID do time.
func (e *Env) CreateTeam(w http.ResponseWriter, r *http.Request) {
	team := r.URL.Query().Get("team")
	room := r.URL.Query().Get("room")

	if team == "" || room == "" {
		http.Error(w, `{"error":"parâmetros 'team' e 'room' são obrigatórios"}`, http.StatusBadRequest)
		return
	}

	var id int
	err := e.db.QueryRow("INSERT INTO team (name, room) VALUES ($1, $2) RETURNING id", team, room).Scan(&id)
	if err != nil {
		WriteError("CreateTeam", err, w)
		return
	}

	WriteJSON(w, map[string]int{"id": id})
}

// ─── Etapas ──────────────────────────────────────────────────────────────────

// POST /stage?room=X&number=N&label=Y
// Cria uma etapa para a sala. number = 1, 2, 3…
func (e *Env) CreateStage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	room := q.Get("room")
	label := q.Get("label")
	number, err := strconv.Atoi(q.Get("number"))
	if err != nil || room == "" {
		http.Error(w, `{"error":"parâmetros 'room' e 'number' são obrigatórios"}`, http.StatusBadRequest)
		return
	}

	var id int
	err = e.db.QueryRow(
		"INSERT INTO stage (room, number, label) VALUES ($1, $2, $3) RETURNING id",
		room, number, label,
	).Scan(&id)
	if err != nil {
		WriteError("CreateStage", err, w)
		return
	}

	WriteJSON(w, map[string]int{"id": id})
}

// PATCH /stage?id=N&active=true|false
// Ativa ou desativa uma etapa (o professor controla qual etapa está aberta).
func (e *Env) SetStageActive(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, err := strconv.Atoi(q.Get("id"))
	active, err2 := strconv.ParseBool(q.Get("active"))
	if err != nil || err2 != nil {
		http.Error(w, `{"error":"parâmetros 'id' e 'active' são obrigatórios"}`, http.StatusBadRequest)
		return
	}

	_, err = e.db.Exec("UPDATE stage SET active = $1 WHERE id = $2", active, id)
	if err != nil {
		WriteError("SetStageActive", err, w)
		return
	}

	WriteJSON(w, map[string]string{"status": "ok"})
}

// ─── Cenários ────────────────────────────────────────────────────────────────

// Estruturas para criação de cenário com choices embutidos
type CreateChoiceInput struct {
	Text     string `json:"text"`
	IsBest   bool   `json:"is_best"`
	Feedback string `json:"feedback"`
}

type CreateScenarioInput struct {
	Room        string              `json:"room"`
	Stage       int                 `json:"stage"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	OrderIndex  int                 `json:"order_index"`
	Choices     []CreateChoiceInput `json:"choices"`
}

// POST /scenario  (body JSON)
// Cria um cenário com suas opções de escolha.
// Body: { room, stage, title, description, order_index, choices: [{text, is_best, feedback}] }
func (e *Env) CreateScenario(w http.ResponseWriter, r *http.Request) {
	var input CreateScenarioInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"body JSON inválido"}`, http.StatusBadRequest)
		return
	}

	if input.Room == "" || input.Title == "" || input.Description == "" || len(input.Choices) == 0 {
		http.Error(w, `{"error":"campos obrigatórios: room, title, description, choices"}`, http.StatusBadRequest)
		return
	}

	// Inicia transação para garantir consistência entre scenario e choices
	tx, err := e.db.Begin()
	if err != nil {
		WriteError("CreateScenario:Begin", err, w)
		return
	}
	defer tx.Rollback()

	var scenarioID int
	err = tx.QueryRow(
		`INSERT INTO scenario (room, stage, title, description, order_index)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		input.Room, input.Stage, input.Title, input.Description, input.OrderIndex,
	).Scan(&scenarioID)
	if err != nil {
		WriteError("CreateScenario:Insert", err, w)
		return
	}

	for _, c := range input.Choices {
		_, err = tx.Exec(
			`INSERT INTO choice (scenario_id, text, is_best, feedback) VALUES ($1, $2, $3, $4)`,
			scenarioID, c.Text, c.IsBest, c.Feedback,
		)
		if err != nil {
			WriteError("CreateScenario:Choice", err, w)
			return
		}
	}

	if err = tx.Commit(); err != nil {
		WriteError("CreateScenario:Commit", err, w)
		return
	}

	WriteJSON(w, map[string]int{"id": scenarioID})
}

// ─── Jogo ────────────────────────────────────────────────────────────────────

// Estruturas de resposta para o cliente (aluno)
type ChoiceView struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

type ScenarioView struct {
	ID          int          `json:"id"`
	Stage       int          `json:"stage"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	OrderIndex  int          `json:"order_index"`
	Choices     []ChoiceView `json:"choices"`
}

// GET /scenarios?room=X&stage=N
// Retorna os cenários ativos da sala para uma etapa.
// NÃO revela is_best nem feedback — esses só chegam após responder.
func (e *Env) GetScenarios(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	room := q.Get("room")
	stageNum := q.Get("stage")

	if room == "" || stageNum == "" {
		http.Error(w, `{"error":"parâmetros 'room' e 'stage' são obrigatórios"}`, http.StatusBadRequest)
		return
	}

	// Verifica se a etapa está ativa
	var active bool
	err := e.db.QueryRow(
		"SELECT active FROM stage WHERE room = $1 AND number = $2",
		room, stageNum,
	).Scan(&active)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"etapa não encontrada"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		WriteError("GetScenarios:stage check", err, w)
		return
	}
	if !active {
		http.Error(w, `{"error":"esta etapa não está ativa"}`, http.StatusForbidden)
		return
	}

	rows, err := e.db.Query(
		`SELECT id, stage, title, description, order_index
		 FROM scenario WHERE room = $1 AND stage = $2 ORDER BY order_index`,
		room, stageNum,
	)
	if err != nil {
		WriteError("GetScenarios", err, w)
		return
	}
	defer rows.Close()

	var scenarios []ScenarioView
	for rows.Next() {
		var s ScenarioView
		rows.Scan(&s.ID, &s.Stage, &s.Title, &s.Description, &s.OrderIndex)

		choiceRows, err := e.db.Query(
			"SELECT id, text FROM choice WHERE scenario_id = $1",
			s.ID,
		)
		if err != nil {
			WriteError("GetScenarios:choices", err, w)
			return
		}
		for choiceRows.Next() {
			var c ChoiceView
			choiceRows.Scan(&c.ID, &c.Text)
			s.Choices = append(s.Choices, c)
		}
		choiceRows.Close()

		scenarios = append(scenarios, s)
	}

	WriteJSON(w, scenarios)
}

// RespondInput é o body esperado em POST /respond
type RespondInput struct {
	TeamID         int `json:"team_id"`
	ScenarioID     int `json:"scenario_id"`
	ChoiceID       int `json:"choice_id"`
	ResponseTimeMs int `json:"response_time_ms"` // medido pelo front-end
}

// RespondOutput é o que retornamos ao aluno após a resposta
type RespondOutput struct {
	IsBest   bool   `json:"is_best"`
	Feedback string `json:"feedback"`
	Score    int    `json:"score"` // total de melhores decisões do time até agora
}

// POST /respond  (body JSON)
// Registra a resposta do time e retorna feedback imediato.
// Body: { team_id, scenario_id, choice_id, response_time_ms }
func (e *Env) Respond(w http.ResponseWriter, r *http.Request) {
	var input RespondInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"body JSON inválido"}`, http.StatusBadRequest)
		return
	}

	// Busca is_best e feedback da choice escolhida
	var isBest bool
	var feedback string
	err := e.db.QueryRow(
		"SELECT is_best, feedback FROM choice WHERE id = $1 AND scenario_id = $2",
		input.ChoiceID, input.ScenarioID,
	).Scan(&isBest, &feedback)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"choice não encontrada para este cenário"}`, http.StatusBadRequest)
		return
	}
	if err != nil {
		WriteError("Respond:choice lookup", err, w)
		return
	}

	// Insere a resposta (ignora duplicata — time já respondeu este cenário)
	_, err = e.db.Exec(
		`INSERT INTO response (team_id, scenario_id, choice_id, is_best, response_time_ms)
		 VALUES ($1, $2, $3, $4, $5)`,
		input.TeamID, input.ScenarioID, input.ChoiceID, isBest, input.ResponseTimeMs,
	)
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code.Name() == "unique_violation" {
			http.Error(w, `{"error":"este cenário já foi respondido por este time"}`, http.StatusConflict)
			return
		}
		WriteError("Respond:insert", err, w)
		return
	}

	// Calcula score atual do time
	var score int
	e.db.QueryRow(
		"SELECT COUNT(*) FROM response WHERE team_id = $1 AND is_best = true",
		input.TeamID,
	).Scan(&score)

	WriteJSON(w, RespondOutput{
		IsBest:   isBest,
		Feedback: feedback,
		Score:    score,
	})
}

// ─── Placar ──────────────────────────────────────────────────────────────────

type ResponseDetail struct {
	ScenarioID     int    `json:"scenario_id"`
	ScenarioTitle  string `json:"scenario_title"`
	ChoiceID       int    `json:"choice_id"`
	ChoiceText     string `json:"choice_text"`
	IsBest         bool   `json:"is_best"`
	ResponseTimeMs int    `json:"response_time_ms"`
	RespondedAt    string `json:"responded_at"`
}

type TeamScore struct {
	TeamID    int              `json:"team_id"`
	TeamName  string           `json:"team_name"`
	Score     int              `json:"score"` // número de melhores decisões
	Total     int              `json:"total"` // total de cenários respondidos
	AvgTimeMs float64          `json:"avg_time_ms"`
	Details   []ResponseDetail `json:"details"`
}

// GET /score?room=X&stage=N
// Retorna o placar completo da sala para uma etapa.
// stage é opcional — sem ele retorna todas as etapas.
func (e *Env) Scoreboard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	room := q.Get("room")
	stageNum := q.Get("stage")

	if room == "" {
		http.Error(w, `{"error":"parâmetro 'room' é obrigatório"}`, http.StatusBadRequest)
		return
	}

	rows, err := e.db.Query("SELECT id, name FROM team WHERE room = $1 ORDER BY id", room)
	if err != nil {
		WriteError("Scoreboard:teams", err, w)
		return
	}
	defer rows.Close()

	var scoreboard []TeamScore

	for rows.Next() {
		var ts TeamScore
		rows.Scan(&ts.TeamID, &ts.TeamName)

		// Monta filtro de etapa se fornecida
		stageFilter := ""
		args := []any{ts.TeamID}
		if stageNum != "" {
			stageFilter = " AND sc.stage = $2"
			args = append(args, stageNum)
		}

		detailRows, err := e.db.Query(fmt.Sprintf(`
			SELECT r.scenario_id, sc.title,
			       r.choice_id, ch.text,
			       r.is_best, r.response_time_ms, r.responded_at
			FROM response r
			JOIN scenario sc ON sc.id = r.scenario_id
			JOIN choice   ch ON ch.id = r.choice_id
			WHERE r.team_id = $1%s
			ORDER BY r.responded_at ASC`, stageFilter),
			args...,
		)
		if err != nil {
			WriteError("Scoreboard:details", err, w)
			return
		}

		var totalTime int
		for detailRows.Next() {
			var d ResponseDetail
			var respondedAt time.Time
			detailRows.Scan(
				&d.ScenarioID, &d.ScenarioTitle,
				&d.ChoiceID, &d.ChoiceText,
				&d.IsBest, &d.ResponseTimeMs, &respondedAt,
			)
			d.RespondedAt = respondedAt.Format(time.RFC3339)
			ts.Details = append(ts.Details, d)
			ts.Total++
			totalTime += d.ResponseTimeMs
			if d.IsBest {
				ts.Score++
			}
		}
		detailRows.Close()

		if ts.Total > 0 {
			ts.AvgTimeMs = float64(totalTime) / float64(ts.Total)
		}

		scoreboard = append(scoreboard, ts)
	}

	WriteJSON(w, scoreboard)
}
