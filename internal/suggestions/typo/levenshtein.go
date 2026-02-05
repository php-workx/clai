package typo

// LevenshteinDistance computes the Levenshtein edit distance between two strings.
// This is the minimum number of single-character edits (insertions, deletions,
// or substitutions) required to change one string into the other.
func LevenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Convert to runes for proper Unicode handling
	runesA := []rune(a)
	runesB := []rune(b)

	// Use two-row optimization to reduce memory
	prev := make([]int, len(runesB)+1)
	curr := make([]int, len(runesB)+1)

	// Initialize first row
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(runesA); i++ {
		curr[0] = i

		for j := 1; j <= len(runesB); j++ {
			cost := 0
			if runesA[i-1] != runesB[j-1] {
				cost = 1
			}

			curr[j] = min(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}

		// Swap rows
		prev, curr = curr, prev
	}

	return prev[len(runesB)]
}

// DamerauLevenshteinDistance computes the Damerau-Levenshtein distance.
// This is like Levenshtein but also allows transpositions of two adjacent
// characters as a single edit operation.
func DamerauLevenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	runesA := []rune(a)
	runesB := []rune(b)

	lenA := len(runesA)
	lenB := len(runesB)

	// Create distance matrix
	d := make([][]int, lenA+1)
	for i := range d {
		d[i] = make([]int, lenB+1)
	}

	// Initialize first row and column
	for i := 0; i <= lenA; i++ {
		d[i][0] = i
	}
	for j := 0; j <= lenB; j++ {
		d[0][j] = j
	}

	for i := 1; i <= lenA; i++ {
		for j := 1; j <= lenB; j++ {
			cost := 0
			if runesA[i-1] != runesB[j-1] {
				cost = 1
			}

			d[i][j] = min(
				d[i-1][j]+1,      // deletion
				d[i][j-1]+1,      // insertion
				d[i-1][j-1]+cost, // substitution
			)

			// Transposition
			if i > 1 && j > 1 && runesA[i-1] == runesB[j-2] && runesA[i-2] == runesB[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+cost)
			}
		}
	}

	return d[lenA][lenB]
}

// Similarity computes a similarity score between 0 and 1 based on
// Damerau-Levenshtein distance. 1.0 means identical, 0.0 means completely different.
func Similarity(a, b string) float64 {
	if a == b {
		return 1.0
	}

	maxLen := max(len([]rune(a)), len([]rune(b)))
	if maxLen == 0 {
		return 1.0
	}

	distance := DamerauLevenshteinDistance(a, b)
	return 1.0 - float64(distance)/float64(maxLen)
}

// Note: Using Go 1.21+ builtin min/max functions
