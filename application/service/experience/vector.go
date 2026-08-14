package experience

import (
	"hash/fnv"
	"math"
	"regexp"
	"strings"
)

const vectorDimensions = 96

var tokenPattern = regexp.MustCompile(`[\p{L}\p{N}]+`)

func vectorize(text string) []float64 {
	vector := make([]float64, vectorDimensions)
	for _, token := range tokenize(text) {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(token))
		value := hasher.Sum64()
		index := int(value % vectorDimensions)
		sign := 1.0
		if value&(1<<63) != 0 {
			sign = -1
		}
		vector[index] += sign
	}
	norm := math.Sqrt(dot(vector, vector))
	if norm > 0 {
		for index := range vector {
			vector[index] /= norm
		}
	}
	return vector
}

func cosine(left, right []float64) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	value := dot(left, right)
	if value < 0 {
		return 0
	}
	return value
}

func keywordScore(query, candidate string) float64 {
	queryTokens := tokenSet(query)
	if len(queryTokens) == 0 {
		return 0
	}
	candidateTokens := tokenSet(candidate)
	matches := 0
	for token := range queryTokens {
		if _, ok := candidateTokens[token]; ok {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTokens))
}

func tokenize(text string) []string {
	matches := tokenPattern.FindAllString(strings.ToLower(text), -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if len([]rune(match)) <= 32 {
			result = append(result, match)
		}
	}
	return result
}

func tokenSet(text string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, token := range tokenize(text) {
		result[token] = struct{}{}
	}
	return result
}

func dot(left, right []float64) float64 {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	var result float64
	for index := 0; index < limit; index++ {
		result += left[index] * right[index]
	}
	return result
}
