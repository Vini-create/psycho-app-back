package checkin

import (
	"errors"
	"time"
)

var (
	ErrNotFound             = errors.New("resource not found")
	ErrInvalidInput         = errors.New("invalid input")
	ErrConflict             = errors.New("resource conflict")
	ErrForbidden            = errors.New("operation is forbidden")
	ErrRequestResolved      = errors.New("request is already resolved")
	ErrSubscriptionRequired = errors.New("active subscription is required")
	ErrProfileIncomplete    = errors.New("professional profile is incomplete")
	ErrTemplatePublished    = errors.New("published template cannot be edited")
	ErrTooManyAssignments   = errors.New("connection has too many active check-ins")
	ErrNoEntries            = errors.New("period has no answered days")
)

// Limites do domínio. Vivem aqui, e não em constantes espalhadas pelos
// handlers, porque a mesma regra vale para criação, edição e resposta.
const (
	MaxQuestionsPerTemplate = 12
	MinQuestionsPerTemplate = 1
	// A escala é fixa: cinco alternativas, notas de 1 a 5, sempre. Escala
	// uniforme é o que permite perguntas diferentes dividirem os mesmos eixos
	// de um radar sem que a forma minta sobre a proporção — e tira do
	// profissional uma decisão que ele não tem por que tomar.
	OptionsPerQuestion   = 5
	MinOptionScore       = 1
	MaxOptionScore       = 5
	MaxActiveAssignments = 5
	MaxCollectionDays    = 92
	MaxSharedCheckins    = 10
)

// DateLayout é o formato de dia trocado com o frontend: o dia local do
// paciente, sem hora e sem fuso. Um timestamp aqui abriria a porta para o
// fuso de quem lê deslocar o dia de quem respondeu.
const DateLayout = "2006-01-02"

type Option struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	Label    string `json:"label"`
	Score    int    `json:"score"`
}

type Question struct {
	ID       string   `json:"id"`
	Position int      `json:"position"`
	Prompt   string   `json:"prompt"`
	Legend   string   `json:"legend,omitempty"`
	Options  []Option `json:"options"`
}

type Template struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Legend      string     `json:"legend,omitempty"`
	Status      string     `json:"status"`
	Questions   []Question `json:"questions"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type TemplateInput struct {
	Title     string
	Legend    string
	Questions []QuestionInput
}

type QuestionInput struct {
	Prompt  string
	Legend  string
	Options []OptionInput
}

// OptionInput não carrega nota: ela é a posição. Deixar o cliente escolher a
// nota abriria a porta para uma escala invertida, que quebraria a leitura de
// "mais alto é melhor" sem que nenhuma validação pudesse perceber.
type OptionInput struct {
	Label string
}

// Assignment é o check-in na vida do paciente: um template entregue a um
// vínculo, que só passa a existir para ele depois do aceite.
type Assignment struct {
	ID                      string     `json:"id"`
	ConnectionID            string     `json:"connection_id"`
	Status                  string     `json:"status"`
	ProfessionalDisplayName string     `json:"professional_display_name,omitempty"`
	PatientDisplayName      string     `json:"patient_display_name,omitempty"`
	Template                Template   `json:"template"`
	RequestedAt             time.Time  `json:"requested_at"`
	RespondedAt             *time.Time `json:"responded_at,omitempty"`
	EndedAt                 *time.Time `json:"ended_at,omitempty"`
	// Estado do dia corrente, calculado no servidor a partir do dia local
	// informado pelo cliente. A tela inicial do paciente só desenha.
	AnsweredToday bool   `json:"answered_today"`
	TodayEntry    *Entry `json:"today_entry,omitempty"`
	LastEntryDate string `json:"last_entry_date,omitempty"`
	AnsweredDays  int    `json:"answered_days"`
}

type Answer struct {
	QuestionID string `json:"question_id"`
	OptionID   string `json:"option_id"`
	Score      int    `json:"score"`
}

type Entry struct {
	ID           string    `json:"id"`
	AssignmentID string    `json:"assignment_id"`
	EntryDate    string    `json:"entry_date"`
	Answers      []Answer  `json:"answers"`
	SubmittedAt  time.Time `json:"submitted_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AnswerInput struct {
	QuestionID string
	OptionID   string
}

type CollectionRequest struct {
	ID                      string     `json:"id"`
	ConnectionID            string     `json:"connection_id"`
	ProfessionalDisplayName string     `json:"professional_display_name,omitempty"`
	PatientDisplayName      string     `json:"patient_display_name,omitempty"`
	PeriodStart             string     `json:"period_start"`
	PeriodEnd               string     `json:"period_end"`
	Status                  string     `json:"status"`
	RequestedAt             time.Time  `json:"requested_at"`
	RespondedAt             *time.Time `json:"responded_at,omitempty"`
}

// QuestionAggregate é uma ponta do radar. `Normalized` acompanha a média
// bruta porque escalas diferentes (0–3, 0–5) não podem dividir um mesmo eixo
// sem serem trazidas à mesma régua — e essa conta é do servidor.
type QuestionAggregate struct {
	QuestionID  string  `json:"question_id"`
	Prompt      string  `json:"prompt"`
	Position    int     `json:"position"`
	Average     float64 `json:"average"`
	Normalized  float64 `json:"normalized"`
	ScoreMin    int     `json:"score_min"`
	ScoreMax    int     `json:"score_max"`
	AnswerCount int     `json:"answer_count"`
}

type DayScore struct {
	Date        string  `json:"date"`
	Average     float64 `json:"average"`
	Normalized  float64 `json:"normalized"`
	AnswerCount int     `json:"answer_count"`
}

type CollectionCheckin struct {
	AssignmentID string `json:"assignment_id"`
	Title        string `json:"title"`
	Legend       string `json:"legend,omitempty"`
	// O profissional que lê não recebe o nome de quem autorou um check-in de
	// outro vínculo: a existência de outro acompanhamento é informação do
	// paciente, e ele não foi perguntado sobre revelá-la.
	AuthoredByYou    bool                `json:"authored_by_you"`
	PeriodDayCount   int                 `json:"period_day_count"`
	AnsweredDayCount int                 `json:"answered_day_count"`
	Average          float64             `json:"average"`
	Normalized       float64             `json:"normalized"`
	Questions        []QuestionAggregate `json:"questions"`
	Days             []DayScore          `json:"days"`
	BestDay          *DayScore           `json:"best_day,omitempty"`
	WorstDay         *DayScore           `json:"worst_day,omitempty"`
}

type Collection struct {
	ID           string              `json:"id"`
	ConnectionID string              `json:"connection_id"`
	RequestID    string              `json:"request_id"`
	PeriodStart  string              `json:"period_start"`
	PeriodEnd    string              `json:"period_end"`
	SharedAt     time.Time           `json:"shared_at"`
	Checkins     []CollectionCheckin `json:"checkins"`
}

type SendCollectionResult struct {
	RequestID    string `json:"request_id"`
	Status       string `json:"status"`
	CheckinCount int    `json:"checkin_count"`
}

// collectionPayload é o retrato congelado, como ele vive cifrado no banco.
// Guarda a filiação de quem autorou cada check-in para que a leitura possa
// dizer "foi você quem mandou este" sem nunca nomear outro profissional.
type collectionPayload struct {
	Checkins []payloadCheckin `json:"checkins"`
}

type payloadCheckin struct {
	CollectionCheckin
	AuthoredByMembershipID string `json:"authored_by_membership_id"`
}
