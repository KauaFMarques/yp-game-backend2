package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
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

// ─── Cenários ────────────────────────────────────────────────────────────────

// Estruturas para criação de cenário com choices embutidos
type CreateChoiceInput struct {
	Text     string `json:"text"`
	IsBest   bool   `json:"is_best"`
	Feedback string `json:"feedback"`
}

type CreateScenarioInput struct {
	Room        string              `json:"room"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	OrderIndex  int                 `json:"order_index"`
	Choices     []CreateChoiceInput `json:"choices"`
}

// POST /scenario  (body JSON)
// Cria um cenário com suas opções de escolha.
// Body: [{ room, title, description, order_index, choices: [{text, is_best, feedback}] }]
// Também aceita objeto único para compatibilidade.
func (e *Env) CreateScenario(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"erro ao ler body"}`, http.StatusBadRequest)
		return
	}

	var inputs []CreateScenarioInput
	decList := json.NewDecoder(bytes.NewReader(body))
	decList.DisallowUnknownFields()
	if err := decList.Decode(&inputs); err != nil {
		// Fallback para aceitar objeto único
		var single CreateScenarioInput
		decSingle := json.NewDecoder(bytes.NewReader(body))
		decSingle.DisallowUnknownFields()
		if decodeErr := decSingle.Decode(&single); decodeErr == nil {
			inputs = []CreateScenarioInput{single}
		} else {
			http.Error(w, `{"error":"body JSON inválido"}`, http.StatusBadRequest)
			return
		}
	}

	if len(inputs) == 0 {
		http.Error(w, `{"error":"envie ao menos um cenário"}`, http.StatusBadRequest)
		return
	}

	// Inicia transação para garantir consistência entre scenario e choices
	tx, err := e.db.Begin()
	if err != nil {
		WriteError("CreateScenario:Begin", err, w)
		return
	}
	defer tx.Rollback()

	type createdScenario struct {
		ID         int    `json:"id"`
		Title      string `json:"title"`
		OrderIndex int    `json:"order_index"`
	}
	created := make([]createdScenario, 0, len(inputs))
	checkedRooms := map[string]bool{}

	for _, input := range inputs {
		if input.Room == "" || input.Title == "" || input.Description == "" || len(input.Choices) == 0 {
			http.Error(w, `{"error":"campos obrigatórios: room, title, description, choices"}`, http.StatusBadRequest)
			return
		}

		if !checkedRooms[input.Room] {
			var exists bool
			err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM room WHERE id = $1)", input.Room).Scan(&exists)
			if err != nil {
				WriteError("CreateScenario:RoomCheck", err, w)
				return
			}
			if !exists {
				http.Error(w, `{"error":"room não encontrada; crie a sala antes de cadastrar cenários"}`, http.StatusBadRequest)
				return
			}
			checkedRooms[input.Room] = true
		}

		var scenarioID int
		err = tx.QueryRow(
			`INSERT INTO scenario (room, title, description, order_index)
			 VALUES ($1, $2, $3, $4) RETURNING id`,
			input.Room, input.Title, input.Description, input.OrderIndex,
		).Scan(&scenarioID)
		if err != nil {
			if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23503" && pgErr.Constraint == "scenario_room_fkey" {
				http.Error(w, `{"error":"room não encontrada; crie a sala antes de cadastrar cenários"}`, http.StatusBadRequest)
				return
			}
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

		created = append(created, createdScenario{
			ID:         scenarioID,
			Title:      input.Title,
			OrderIndex: input.OrderIndex,
		})
	}

	if err = tx.Commit(); err != nil {
		WriteError("CreateScenario:Commit", err, w)
		return
	}

	WriteJSON(w, map[string]any{"created": created})
}

// ─── Jogo ────────────────────────────────────────────────────────────────────

// Estruturas de resposta para o cliente (aluno)
type ChoiceView struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

type ScenarioView struct {
	ID          int          `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	OrderIndex  int          `json:"order_index"`
	Choices     []ChoiceView `json:"choices"`
}

// GET /scenarios?room=X
// Retorna os cenários da sala.
// NÃO revela is_best nem feedback — esses só chegam após responder.
func (e *Env) GetScenarios(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	room := q.Get("room")

	if room == "" {
		http.Error(w, `{"error":"parâmetro 'room' é obrigatório"}`, http.StatusBadRequest)
		return
	}

	rows, err := e.db.Query(
		`SELECT id, title, description, order_index
		 FROM scenario WHERE room = $1 ORDER BY order_index`,
		room,
	)
	if err != nil {
		WriteError("GetScenarios", err, w)
		return
	}
	defer rows.Close()

	var scenarios []ScenarioView
	for rows.Next() {
		var s ScenarioView
		rows.Scan(&s.ID, &s.Title, &s.Description, &s.OrderIndex)

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

// GET /score?room=X
// Retorna o placar completo da sala.
func (e *Env) Scoreboard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	room := q.Get("room")

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

		detailRows, err := e.db.Query(`
			SELECT r.scenario_id, sc.title,
			       r.choice_id, ch.text,
			       r.is_best, r.response_time_ms, r.responded_at
			FROM response r
			JOIN scenario sc ON sc.id = r.scenario_id
			JOIN choice   ch ON ch.id = r.choice_id
			WHERE r.team_id = $1
			ORDER BY r.responded_at ASC`,
			ts.TeamID,
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
