package vector

import (
	"math"
)

// CosineSimilarity calculates the cosine similarity between two vectors
// Returns a value between -1 and 1, where 1 means identical direction
func CosineSimilarity(vec1, vec2 []float32) float32 {
	if len(vec1) != len(vec2) {
		return 0
	}

	var dotProduct, norm1, norm2 float64
	for i := 0; i < len(vec1); i++ {
		dotProduct += float64(vec1[i]) * float64(vec2[i])
		norm1 += float64(vec1[i]) * float64(vec1[i])
		norm2 += float64(vec2[i]) * float64(vec2[i])
	}

	if norm1 == 0 || norm2 == 0 {
		return 0
	}

	return float32(dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2)))
}
