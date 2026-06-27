package monitor

import (
	"math/rand"
	"strings"
	"unicode"
)

const maxAnswerTokens = 6

type challenge struct {
	Prompt         string
	ExpectedAnswer string
	Difficulty     int
}

type validationResult struct {
	Valid      bool
	Normalized string
}

var newChallenge = generateChallenge

var categoryBank = map[string][]string{
	"animal":     {"cat", "dog", "tiger", "horse", "rabbit", "eagle", "dolphin", "wolf"},
	"fruit":      {"apple", "banana", "grape", "mango", "peach", "lemon", "cherry", "pear"},
	"color":      {"red", "blue", "green", "yellow", "purple", "pink", "black", "white"},
	"country":    {"japan", "france", "brazil", "canada", "egypt", "india", "norway", "kenya"},
	"metal":      {"iron", "gold", "copper", "silver", "zinc", "nickel", "lead", "tin"},
	"vehicle":    {"car", "truck", "train", "bicycle", "airplane", "boat", "scooter", "tram"},
	"instrument": {"piano", "guitar", "violin", "drum", "flute", "trumpet", "harp", "cello"},
	"drink":      {"coffee", "tea", "juice", "milk", "soda", "water", "cocoa", "lemonade"},
}

var compColors = []string{"brown", "gray", "golden", "spotted", "striped", "pale", "dark", "bright"}
var compAnimals = []string{"fox", "owl", "bear", "deer", "frog", "crow", "otter", "lynx"}
var compActions = []string{"slept", "jumped", "rested", "waited", "played", "hid", "stared", "wandered"}
var compPlaces = []string{"river", "mountain", "garden", "market", "forest", "lake", "bridge", "castle"}

func generateChallenge() challenge {
	if rand.Intn(2) == 0 {
		return generateCategorySelect()
	}
	return generateReadingComprehension()
}

func generateCategorySelect() challenge {
	categories := make([]string, 0, len(categoryBank))
	for category := range categoryBank {
		categories = append(categories, category)
	}
	target := pick(categories)
	correct := pick(categoryBank[target])
	var others []string
	for _, category := range categories {
		if category != target {
			others = append(others, categoryBank[category]...)
		}
	}
	options := append([]string{correct}, sample(others, 5)...)
	shuffle(options)
	return challenge{
		Prompt: `Pick the word that belongs to the given category. Reply with ONLY that one word.

Category: fruit
Options: car, banana, iron, blue, dog
A: banana

Category: ` + target + `
Options: ` + strings.Join(options, ", ") + `
A:`,
		ExpectedAnswer: correct,
		Difficulty:     1,
	}
}

func generateReadingComprehension() challenge {
	count := 6 + rand.Intn(2)
	animals := sample(compAnimals, count)
	type fact struct{ animal, color, action, place string }
	facts := make([]fact, 0, len(animals))
	lines := make([]string, 0, len(animals))
	for _, animal := range animals {
		f := fact{animal: animal, color: pick(compColors), action: pick(compActions), place: pick(compPlaces)}
		facts = append(facts, f)
		lines = append(lines, "The "+f.color+" "+f.animal+" "+f.action+" near the "+f.place+".")
	}
	target := pick(facts)
	question := "What color was the " + target.animal + "?"
	answer := target.color
	if rand.Intn(2) == 0 {
		question = "Where was the " + target.animal + "?"
		answer = target.place
	}
	return challenge{
		Prompt: `Read the passage and answer the question with ONLY one word.

Passage: The small dog rested near the garden. The happy cat slept near the lake.
Question: Where was the cat?
A: lake

Passage: ` + strings.Join(lines, " ") + `
Question: ` + question + `
A:`,
		ExpectedAnswer: answer,
		Difficulty:     2,
	}
}

func validateResponse(response, expectedAnswer string) validationResult {
	if response == "" || expectedAnswer == "" {
		return validationResult{}
	}
	normalized := normalizeWords(response)
	if normalized == "" {
		return validationResult{}
	}
	expected := normalizeWords(expectedAnswer)
	words := strings.Fields(normalized)
	valid := len(words) <= maxAnswerTokens
	if valid {
		valid = false
		for _, word := range words {
			if word == expected {
				valid = true
				break
			}
		}
	}
	if len(normalized) > 80 {
		normalized = normalized[:80] + "..."
	}
	return validationResult{Valid: valid, Normalized: normalized}
}

func normalizeWords(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			space = false
			continue
		}
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if b.Len() > 0 && !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func pick[T any](items []T) T {
	return items[rand.Intn(len(items))]
}

func sample[T any](items []T, count int) []T {
	pool := append([]T(nil), items...)
	out := make([]T, 0, count)
	for len(out) < count && len(pool) > 0 {
		i := rand.Intn(len(pool))
		out = append(out, pool[i])
		pool = append(pool[:i], pool[i+1:]...)
	}
	return out
}

func shuffle[T any](items []T) {
	for i := len(items) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		items[i], items[j] = items[j], items[i]
	}
}
