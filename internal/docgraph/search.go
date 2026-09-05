package docgraph

import (
	"sort"
	"strings"
)

// Match is one row of a docs find answer: the document that satisfied the
// query, its compact discovery guidance, and, when the raw hit was a
// superseded document, the subject it was resolved forward from.
type Match struct {
	Path         string
	Subject      string
	Description  string
	ResolvedFrom string
	ReadWhen     string `json:"read_when"`
	SkipWhen     string `json:"skip_when"`
}

// FindResult is the whole answer to a docs find query. Matches is empty only
// when nothing matched, in which case Suggestions holds the closest subjects
// so the caller never presents a silent empty result.
type FindResult struct {
	Query        string
	Matches      []Match
	Suggestions  []string
	TotalMatches int  `json:"total_matches"`
	Truncated    bool `json:"truncated"`
	Limit        int  `json:"limit"`
}

const (
	scoreNone           = 0
	scoreHeading        = 1
	scoreSubjectContain = 2
	scoreKeywordContain = 3
	scoreKeywordExact   = 4
	scoreSubjectExact   = 5
	scoreReadWhen       = 2
)

// Find runs a deterministic, bounded keyword search over document subjects,
// keywords, positive read_when guidance, and Markdown headings. Multi-word
// queries require every term to match; when that yields nothing it falls back
// to the best single term. Results rank exact subject over keyword alias over
// read_when guidance over heading substring, breaking ties by canonical
// document, then spec, then decision log, then path. skip_when is surfaced as
// guidance only and never treated as a positive match.
func Find(corpus Corpus, query string) FindResult {
	return FindWithLimit(corpus, query, DefaultFindLimit)
}

// FindWithLimit is the explicit bounded form used by callers that need a
// smaller or larger shortlist. A non-positive limit uses DefaultFindLimit so
// no caller accidentally turns a worker-facing query into a full tree dump.
func FindWithLimit(corpus Corpus, query string, limit int) FindResult {
	if limit <= 0 {
		limit = DefaultFindLimit
	}
	result := FindResult{Query: query}
	result.Limit = limit
	terms := splitTerms(query)
	if len(terms) == 0 {
		return result
	}

	scored := scoreDocuments(corpus, terms, true)
	if len(scored) == 0 && len(terms) > 1 {
		scored = scoreDocuments(corpus, terms, false)
	}
	if len(scored) == 0 {
		result.Suggestions = suggestSubjects(corpus, terms)
		return result
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if ki, kj := kindRank(scored[i].doc.Kind), kindRank(scored[j].doc.Kind); ki != kj {
			return ki < kj
		}
		return scored[i].doc.Path < scored[j].doc.Path
	})

	resolver := NewResolver(corpus)
	seen := map[string]bool{}
	for _, hit := range scored {
		doc := hit.doc
		resolvedFrom := ""
		if current, ok := resolver.ResolveCurrent(doc.Subject); ok {
			resolvedFrom = current.ResolvedFrom
			doc = current.Document
		}
		key := strings.ToLower(strings.TrimSpace(doc.Subject))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(doc.Path))
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result.TotalMatches++
		if len(result.Matches) >= limit {
			continue
		}
		result.Matches = append(result.Matches, Match{
			Path:         doc.Path,
			Subject:      doc.Subject,
			Description:  describe(doc),
			ResolvedFrom: resolvedFrom,
			ReadWhen:     compactDiscoveryText(doc, "read_when"),
			SkipWhen:     compactDiscoveryText(doc, "skip_when"),
		})
	}
	result.Truncated = result.TotalMatches > len(result.Matches)
	return result
}

type scoredDoc struct {
	doc   Document
	score int
}

func scoreDocuments(corpus Corpus, terms []string, requireAll bool) []scoredDoc {
	var scored []scoredDoc
	for _, doc := range corpus.Documents {
		best := scoreNone
		matchedTerms := 0
		for _, term := range terms {
			termScore := scoreTerm(doc, term)
			if termScore > scoreNone {
				matchedTerms++
			}
			if termScore > best {
				best = termScore
			}
		}
		if best == scoreNone {
			continue
		}
		if requireAll && matchedTerms < len(terms) {
			continue
		}
		scored = append(scored, scoredDoc{doc: doc, score: best})
	}
	return scored
}

func scoreTerm(doc Document, term string) int {
	subject := strings.ToLower(strings.TrimSpace(doc.Subject))
	switch {
	case subject == term:
		return scoreSubjectExact
	case subject != "" && strings.Contains(subject, term):
		return scoreSubjectContain
	}
	best := scoreNone
	for _, keyword := range doc.Keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword == "" {
			continue
		}
		if keyword == term && scoreKeywordExact > best {
			best = scoreKeywordExact
		} else if strings.Contains(keyword, term) && scoreKeywordContain > best {
			best = scoreKeywordContain
		}
	}
	if best > scoreNone {
		return best
	}
	if readWhen := discoveryText(doc, "read_when"); readWhen != "" && strings.Contains(strings.ToLower(readWhen), term) {
		return scoreReadWhen
	}
	for _, heading := range headings(doc.Body) {
		if strings.Contains(strings.ToLower(heading), term) {
			return scoreHeading
		}
	}
	return scoreNone
}

func describe(doc Document) string {
	if title := scalar(doc.Raw["title"]); title != "" {
		return title
	}
	if heading := firstHeading(doc.Body); heading != "" {
		return heading
	}
	if readWhen := discoveryText(doc, "read_when"); readWhen != "" {
		return readWhen
	}
	return doc.Subject
}

func discoveryText(doc Document, field string) string {
	return strings.Join(strings.Fields(scalar(doc.Raw[field])), " ")
}

func compactDiscoveryText(doc Document, field string) string {
	text := discoveryText(doc, field)
	if text == "" {
		return ""
	}
	words := strings.Fields(text)
	const maxWords = 24
	if len(words) > maxWords {
		return strings.Join(words[:maxWords], " ") + "…"
	}
	const maxCharacters = 220
	if runes := []rune(text); len(runes) > maxCharacters {
		return string(runes[:maxCharacters]) + "…"
	}
	return text
}

func suggestSubjects(corpus Corpus, terms []string) []string {
	type candidate struct {
		subject string
		shared  int
		near    int
	}
	var candidates []candidate
	for _, doc := range corpus.Documents {
		subject := strings.TrimSpace(doc.Subject)
		if subject == "" {
			continue
		}
		shared := sharedTermCount(doc, terms)
		near := bestEditDistance(subject, terms)
		candidates = append(candidates, candidate{subject: subject, shared: shared, near: near})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].shared != candidates[j].shared {
			return candidates[i].shared > candidates[j].shared
		}
		if candidates[i].near != candidates[j].near {
			return candidates[i].near < candidates[j].near
		}
		return candidates[i].subject < candidates[j].subject
	})
	var suggestions []string
	for _, item := range candidates {
		suggestions = append(suggestions, item.subject)
		if len(suggestions) == 3 {
			break
		}
	}
	return suggestions
}

func sharedTermCount(doc Document, terms []string) int {
	fields := append([]string{doc.Subject}, doc.Keywords...)
	fields = append(fields, discoveryText(doc, "read_when"))
	haystack := strings.ToLower(strings.Join(fields, " "))
	count := 0
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			count++
		}
	}
	return count
}

func bestEditDistance(subject string, terms []string) int {
	subject = strings.ToLower(subject)
	best := editDistance(subject, strings.Join(terms, " "))
	for _, term := range terms {
		if distance := editDistance(subject, term); distance < best {
			best = distance
		}
	}
	return best
}

func splitTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	var terms []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			terms = append(terms, field)
		}
	}
	return terms
}

func headings(body string) []string {
	var result []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			result = append(result, strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
		}
	}
	return result
}

func firstHeading(body string) string {
	for _, heading := range headings(body) {
		if heading != "" {
			return heading
		}
	}
	return ""
}

func kindRank(kind Kind) int {
	switch kind {
	case KindCanonical:
		return 0
	case KindSpec:
		return 1
	case KindDecision:
		return 2
	default:
		return 3
	}
}

func editDistance(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	previous := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		current := make([]int, len(br)+1)
		current[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			current[j] = minInt(minInt(current[j-1]+1, previous[j]+1), previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(br)]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
