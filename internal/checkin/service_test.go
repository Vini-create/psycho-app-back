package checkin

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// plainCipher deixa o texto passar. Os testes aqui verificam regra de
// domínio, não criptografia — essa tem os testes dela em internal/auth.
type plainCipher struct{}

func (plainCipher) Encrypt(value []byte) ([]byte, error) { return value, nil }
func (plainCipher) Decrypt(value []byte) ([]byte, error) { return value, nil }

func newTestService(t *testing.T, now time.Time) *Service {
	t.Helper()
	service, err := NewService(&Repository{}, plainCipher{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	return service
}

func validTemplateInput() TemplateInput {
	return TemplateInput{
		Title:  "Check-in diário",
		Legend: "Responda pensando no dia de hoje.",
		Questions: []QuestionInput{{
			Prompt: "Como estava seu humor hoje?",
			Legend: "A primeira alternativa é o pior dia possível; a última, o melhor.",
			Options: []OptionInput{
				{Label: "Muito ruim"},
				{Label: "Ruim"},
				{Label: "Nem bom nem ruim"},
				{Label: "Bom"},
				{Label: "Muito bom"},
			},
		}},
	}
}

func TestTemplateWriteRejectsInvalidInput(t *testing.T) {
	service := newTestService(t, time.Now().UTC())

	tests := []struct {
		name   string
		mutate func(*TemplateInput)
	}{
		{"título vazio", func(input *TemplateInput) { input.Title = "   " }},
		{"título longo", func(input *TemplateInput) { input.Title = strings.Repeat("a", maxTitleRunes+1) }},
		{"sem perguntas", func(input *TemplateInput) { input.Questions = nil }},
		{"perguntas demais", func(input *TemplateInput) {
			input.Questions = make([]QuestionInput, MaxQuestionsPerTemplate+1)
			for index := range input.Questions {
				input.Questions[index] = validTemplateInput().Questions[0]
			}
		}},
		{"menos de cinco alternativas", func(input *TemplateInput) {
			input.Questions[0].Options = input.Questions[0].Options[:4]
		}},
		{"mais de cinco alternativas", func(input *TemplateInput) {
			input.Questions[0].Options = append(
				input.Questions[0].Options, OptionInput{Label: "Excelente"},
			)
		}},
		{"rótulo vazio", func(input *TemplateInput) {
			input.Questions[0].Options[0].Label = "  "
		}},
		{"enunciado vazio", func(input *TemplateInput) {
			input.Questions[0].Prompt = ""
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validTemplateInput()
			test.mutate(&input)
			if _, err := service.templateWriteFromInput(input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("templateWriteFromInput() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestTemplateWriteAcceptsValidInput(t *testing.T) {
	service := newTestService(t, time.Now().UTC())
	write, err := service.templateWriteFromInput(validTemplateInput())
	if err != nil {
		t.Fatalf("templateWriteFromInput() error = %v", err)
	}
	if string(write.TitleCiphertext) != "Check-in diário" {
		t.Fatalf("title = %q, want trimmed original", write.TitleCiphertext)
	}
	if len(write.Questions) != 1 || len(write.Questions[0].Options) != OptionsPerQuestion {
		t.Fatalf("questions = %+v, want 1 pergunta com 5 alternativas", write.Questions)
	}
	// A nota é a posição: primeiro rótulo é 1, último é 5.
	for index, option := range write.Questions[0].Options {
		if option.Score != index+MinOptionScore {
			t.Fatalf("nota da alternativa %d = %d, want %d", index, option.Score, index+1)
		}
	}
}

// O dia do registro é do paciente, mas não é livre: aceitamos apenas a janela
// que qualquer fuso do mundo justifica.
func TestResolveLocalDateWindow(t *testing.T) {
	now := time.Date(2026, 8, 26, 15, 0, 0, 0, time.UTC)
	service := newTestService(t, now)

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "vazio cai em hoje", value: "", want: "2026-08-26"},
		{name: "hoje", value: "2026-08-26", want: "2026-08-26"},
		{name: "ontem", value: "2026-08-25", want: "2026-08-25"},
		{name: "amanhã", value: "2026-08-27", want: "2026-08-27"},
		{name: "anteontem é recusado", value: "2026-08-24", wantErr: true},
		{name: "depois de amanhã é recusado", value: "2026-08-28", wantErr: true},
		{name: "formato inválido", value: "26/08/2026", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := service.resolveLocalDate(test.value)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("resolveLocalDate(%q) error = %v, want ErrInvalidInput", test.value, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveLocalDate(%q) error = %v", test.value, err)
			}
			if formatDate(got) != test.want {
				t.Fatalf("resolveLocalDate(%q) = %q, want %q", test.value, formatDate(got), test.want)
			}
		})
	}
}

func TestBoundsNormalizeFixedScale(t *testing.T) {
	scale := boundsOf(Question{
		Options: []Option{{Score: 1}, {Score: 2}, {Score: 3}, {Score: 4}, {Score: 5}},
	})

	if scale.Min != 1 || scale.Max != 5 {
		t.Fatalf("bounds = %+v, want 1–5", scale)
	}
	if got := scale.normalize(5); got != 1 {
		t.Fatalf("normalize(5) = %v, want 1", got)
	}
	if got := scale.normalize(1); got != 0 {
		t.Fatalf("normalize(1) = %v, want 0", got)
	}
	if got := scale.normalize(3); got != 0.5 {
		t.Fatalf("normalize(3) = %v, want 0.5", got)
	}
	// Um template legado com escala degenerada não pode virar divisão por zero.
	flat := boundsOf(Question{Options: []Option{{Score: 3}, {Score: 3}}})
	if got := flat.normalize(3); got != 0 {
		t.Fatalf("normalize on flat scale = %v, want 0", got)
	}
}

func TestAggregateCheckinComputesAveragesAndExtremes(t *testing.T) {
	scale := []Option{{Score: 1}, {Score: 2}, {Score: 3}, {Score: 4}, {Score: 5}}
	template := Template{
		Title: "Humor e sono",
		Questions: []Question{
			{ID: "question-mood", Position: 1, Prompt: "Humor", Options: scale},
			{ID: "question-sleep", Position: 2, Prompt: "Sono", Options: scale},
		},
	}
	day := func(offset int) time.Time {
		return time.Date(2026, 8, 20+offset, 0, 0, 0, 0, time.UTC)
	}
	entries := []storedEntry{
		{ID: "entry-1", EntryDate: day(0)},
		{ID: "entry-2", EntryDate: day(1)},
		{ID: "entry-3", EntryDate: day(2)},
	}
	answers := map[string][]storedAnswer{
		"entry-1": {
			{EntryID: "entry-1", QuestionID: "question-mood", Score: 5},
			{EntryID: "entry-1", QuestionID: "question-sleep", Score: 5},
		},
		"entry-2": {
			{EntryID: "entry-2", QuestionID: "question-mood", Score: 1},
			{EntryID: "entry-2", QuestionID: "question-sleep", Score: 1},
		},
		"entry-3": {
			{EntryID: "entry-3", QuestionID: "question-mood", Score: 3},
			{EntryID: "entry-3", QuestionID: "question-sleep", Score: 3},
		},
	}

	result := aggregateCheckin(template, entries, answers, 7)

	if result.AnsweredDayCount != 3 || result.PeriodDayCount != 7 {
		t.Fatalf("adesão = %d/%d, want 3/7", result.AnsweredDayCount, result.PeriodDayCount)
	}
	if len(result.Questions) != 2 {
		t.Fatalf("questions = %d, want 2", len(result.Questions))
	}
	mood := result.Questions[0]
	if mood.Average != 3 || mood.Normalized != 0.5 || mood.AnswerCount != 3 {
		t.Fatalf("humor = %+v, want média 3, normalizada 0.5, 3 respostas", mood)
	}
	if mood.ScoreMin != 1 || mood.ScoreMax != 5 {
		t.Fatalf("escala = %d–%d, want 1–5", mood.ScoreMin, mood.ScoreMax)
	}
	if result.BestDay == nil || result.BestDay.Date != "2026-08-20" {
		t.Fatalf("melhor dia = %+v, want 2026-08-20", result.BestDay)
	}
	if result.WorstDay == nil || result.WorstDay.Date != "2026-08-21" {
		t.Fatalf("pior dia = %+v, want 2026-08-21", result.WorstDay)
	}
	// O melhor dia é o de nota máxima nas duas perguntas: normalizada 1.
	if result.BestDay.Normalized != 1 || result.WorstDay.Normalized != 0 {
		t.Fatalf("extremos normalizados = %v e %v, want 1 e 0",
			result.BestDay.Normalized, result.WorstDay.Normalized)
	}
}

// Sem dia respondido não há retrato: o profissional não pode receber um
// gráfico de zeros que pareça um relato de piora.
func TestAggregateCheckinWithoutEntries(t *testing.T) {
	template := Template{
		Title: "Humor",
		Questions: []Question{{
			ID: "question-mood", Position: 1, Prompt: "Humor",
			Options: []Option{{Score: 1}, {Score: 2}, {Score: 3}, {Score: 4}, {Score: 5}},
		}},
	}
	result := aggregateCheckin(template, nil, map[string][]storedAnswer{}, 14)
	if result.AnsweredDayCount != 0 || result.BestDay != nil || result.WorstDay != nil {
		t.Fatalf("resultado vazio = %+v, want zero dias e extremos nulos", result)
	}
	if result.Questions[0].AnswerCount != 0 || result.Questions[0].Average != 0 {
		t.Fatalf("pergunta sem resposta = %+v, want contagem e média zeradas", result.Questions[0])
	}
}

// Com um dia só, melhor e pior apontam para o mesmo dia. A UI precisa dizer
// isso; fingir variação seria inventar um dado clínico.
func TestExtremesWithSingleDay(t *testing.T) {
	only := DayScore{Date: "2026-08-20", Normalized: 0.4}
	best, worst := extremes([]DayScore{only})
	if best == nil || worst == nil || best.Date != only.Date || worst.Date != only.Date {
		t.Fatalf("extremes() = %+v e %+v, want o mesmo dia nos dois", best, worst)
	}
}
