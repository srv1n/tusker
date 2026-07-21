package docgraph

import (
	"sort"
	"strings"
)

// Match is one row of a docs find answer: the document that satisfied the
// query, a one-line description, and, when the raw hit was a superseded
// document, the subject it was resolved forward from.
type Match struct {
	Path         string
	Subject      string
	Description  string
	ResolvedFrom string
}

// FindResult is the whole answer to a docs find query. Matches is empty only
// when nothing matched, in which case Suggestions holds the closest subjects
// so the caller never presents a silent empty result.
type FindResult struct {
	Query       string
	Matches     []Match
	Suggestions []string
}

const (
	scoreNone           = 0
	scoreHeading        = 1
	scoreSubjectContain = 2
	scoreKeywordContain = 3
	scoreKeywordExact   = 4
	scoreSubjectExact   = 5
)

// Find runs a deterministic keyword search over document subjects, keywords,
// and Markdown headings. Multi-word queries require every term to match; when
// that yields nothing it falls back to the best single term. Results rank
// exact subject over keyword alias over heading substring, breaking ties by
// canonical document, then spec, then decision log, then path.
func Find(corpus Corpus, query string) FindResult {
	result := FindResult{Query: query}
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

	subjectIndex := indexBySubject(corpus)
	for _, hit := range scored {
		doc := hit.doc
		resolvedFrom := ""
		if successor, ok := resolveSuccessor(doc, subjectIndex); ok {
			resolvedFrom = doc.Subject
			doc = successor
		}
		result.Matches = append(result.Matches, Match{
			Path:         doc.Path,
			Subject:      doc.Subject,
			Description:  describe(doc),
			ResolvedFrom: resolvedFrom,
		})
	}
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
	for _, heading := range headings(doc.Body) {
		if strings.Contains(strings.ToLower(heading), term) {
			return scoreHeading
		}
	}
	return scoreNone
}

func resolveSuccessor(doc Document, subjectIndex map[string]Document) (Document, bool) {
	if !strings.EqualFold(strings.TrimSpace(doc.Status), "superseded") {
		return Document{}, false
	}
	successor := strings.TrimSpace(doc.SupersededBy)
	if successor == "" {
		return Document{}, false
	}
	next, ok := subjectIndex[strings.ToLower(successor)]
	if !ok {
		return Document{}, false
	}
	return next, true
}

func describe(doc Document) string {
	if title := scalar(doc.Raw["title"]); title != "" {
		return title
	}
	if heading := firstHeading(doc.Body); heading != "" {
		return heading
	}
	if readWhen := scalar(doc.Raw["read_when"]); readWhen != "" {
		return readWhen
	}
	return doc.Subject
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
	haystack := strings.ToLower(strings.Join(append([]string{doc.Subject}, doc.Keywords...), " "))
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

func indexBySubject(corpus Corpus) map[string]Document {
	index := make(map[string]Document, len(corpus.Documents))
	for _, doc := range corpus.Documents {
		subject := strings.ToLower(strings.TrimSpace(doc.Subject))
		if subject == "" {
			continue
		}
		if _, exists := index[subject]; !exists {
			index[subject] = doc
		}
	}
	return index
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
