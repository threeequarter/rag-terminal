package vector

import (
	"container/heap"
	"math"
	"math/rand"
	"sync"
)

// HNSWConfig holds configuration parameters for the HNSW index
type HNSWConfig struct {
	M              int     // Maximum number of connections per node
	EfConstruction int     // Size of dynamic candidate list during construction
	EfSearch       int     // Size of dynamic candidate list during search
	Ml             float64 // Normalization factor for level generation
	MaxLevel       int     // Maximum level in the hierarchy
}

// DefaultHNSWConfig returns sensible default configuration
func DefaultHNSWConfig() *HNSWConfig {
	return &HNSWConfig{
		M:              16,   // Standard value
		EfConstruction: 200,  // Higher = better quality, slower build
		EfSearch:       100,  // Higher = better recall, slower search
		Ml:             1.0 / math.Log(2.0),
		MaxLevel:       16,
	}
}

// HNSWNode represents a node in the HNSW graph
type HNSWNode struct {
	ID         string
	Vector     []float32
	Level      int
	Neighbors  [][]string // Neighbors at each level (level -> neighbor IDs)
	IsMessage  bool       // true for Message, false for DocumentChunk
	IsContext  bool       // true if role == "context"
}

// HNSWIndex is an in-memory HNSW index for fast approximate nearest neighbor search
type HNSWIndex struct {
	config     *HNSWConfig
	nodes      map[string]*HNSWNode // ID -> Node
	entryPoint string               // ID of the top-level entry point
	maxLevel   int                  // Current maximum level in the index
	mu         sync.RWMutex
	rng        *rand.Rand
}

// NewHNSWIndex creates a new HNSW index
func NewHNSWIndex(config *HNSWConfig) *HNSWIndex {
	if config == nil {
		config = DefaultHNSWConfig()
	}
	return &HNSWIndex{
		config: config,
		nodes:  make(map[string]*HNSWNode),
		rng:    rand.New(rand.NewSource(42)), // Fixed seed for reproducibility
	}
}

// Add inserts a new vector into the index
func (idx *HNSWIndex) Add(id string, vector []float32, isMessage bool, isContext bool) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Check if already exists
	if _, exists := idx.nodes[id]; exists {
		return
	}

	// Determine level for new node
	level := idx.randomLevel()

	// Create new node
	node := &HNSWNode{
		ID:        id,
		Vector:    vector,
		Level:     level,
		Neighbors: make([][]string, level+1),
		IsMessage: isMessage,
		IsContext: isContext,
	}

	// Initialize neighbor lists for each level
	for i := 0; i <= level; i++ {
		node.Neighbors[i] = make([]string, 0, idx.config.M)
	}

	idx.nodes[id] = node

	// If this is the first node
	if idx.entryPoint == "" {
		idx.entryPoint = id
		idx.maxLevel = level
		return
	}

	// Insert into the graph
	idx.insert(node)

	// Update entry point if necessary
	if level > idx.maxLevel {
		idx.maxLevel = level
		idx.entryPoint = id
	}
}

// Search performs k-nearest neighbor search
func (idx *HNSWIndex) Search(query []float32, k int, filterContext bool) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.entryPoint == "" || len(idx.nodes) == 0 {
		return []string{}
	}

	// Search from entry point
	ep := idx.entryPoint
	currDist := idx.distance(query, idx.nodes[ep].Vector)

	// Navigate through layers from top to bottom
	for level := idx.maxLevel; level > 0; level-- {
		changed := true
		for changed {
			changed = false
			node := idx.nodes[ep]

			// Check neighbors at current level
			if level < len(node.Neighbors) {
				for _, neighborID := range node.Neighbors[level] {
					neighbor := idx.nodes[neighborID]
					d := idx.distance(query, neighbor.Vector)
					if d < currDist {
						currDist = d
						ep = neighborID
						changed = true
					}
				}
			}
		}
	}

	// Search at layer 0 with ef parameter
	candidates := idx.searchLayer(query, ep, idx.config.EfSearch, 0, filterContext)

	// Return top k results
	result := make([]string, 0, k)
	for i := 0; i < k && i < len(candidates); i++ {
		result = append(result, candidates[i].id)
	}

	return result
}

// insert adds a node to the HNSW graph structure
func (idx *HNSWIndex) insert(node *HNSWNode) {
	// Find nearest neighbors at each level
	ep := idx.entryPoint
	currDist := idx.distance(node.Vector, idx.nodes[ep].Vector)

	// Navigate to insertion point from top level
	for level := idx.maxLevel; level > node.Level; level-- {
		changed := true
		for changed {
			changed = false
			epNode := idx.nodes[ep]

			if level < len(epNode.Neighbors) {
				for _, neighborID := range epNode.Neighbors[level] {
					neighbor := idx.nodes[neighborID]
					d := idx.distance(node.Vector, neighbor.Vector)
					if d < currDist {
						currDist = d
						ep = neighborID
						changed = true
					}
				}
			}
		}
	}

	// Insert at levels from node.Level down to 0
	for level := node.Level; level >= 0; level-- {
		candidates := idx.searchLayer(node.Vector, ep, idx.config.EfConstruction, level, false)

		// Select M nearest neighbors
		m := idx.config.M
		if level == 0 {
			m = idx.config.M * 2 // Allow more connections at bottom layer
		}

		neighbors := idx.selectNeighbors(candidates, m)

		// Add bidirectional links
		for _, neighbor := range neighbors {
			// Add neighbor to node
			node.Neighbors[level] = append(node.Neighbors[level], neighbor.id)

			// Add node to neighbor
			neighborNode := idx.nodes[neighbor.id]
			if level < len(neighborNode.Neighbors) {
				neighborNode.Neighbors[level] = append(neighborNode.Neighbors[level], node.ID)

				// Prune neighbor's connections if needed
				if len(neighborNode.Neighbors[level]) > m {
					idx.pruneNeighbors(neighborNode, level, m)
				}
			}
		}

		// Update entry point for next level
		if len(neighbors) > 0 {
			ep = neighbors[0].id
		}
	}
}

// searchLayer performs greedy search at a specific layer
func (idx *HNSWIndex) searchLayer(query []float32, ep string, ef int, level int, filterContext bool) []distanceNode {
	visited := make(map[string]bool)
	candidates := &minHeap{}
	results := &maxHeap{}

	// Initialize with entry point
	dist := idx.distance(query, idx.nodes[ep].Vector)
	heap.Push(candidates, distanceNode{id: ep, distance: dist})
	heap.Push(results, distanceNode{id: ep, distance: dist})
	visited[ep] = true

	// Explore graph
	for candidates.Len() > 0 {
		current := heap.Pop(candidates).(distanceNode)

		if current.distance > results.Top().distance {
			break
		}

		node := idx.nodes[current.id]
		if level >= len(node.Neighbors) {
			continue
		}

		// Check neighbors
		for _, neighborID := range node.Neighbors[level] {
			if visited[neighborID] {
				continue
			}
			visited[neighborID] = true

			neighbor := idx.nodes[neighborID]

			// Apply filter if needed
			if filterContext && (!neighbor.IsMessage || !neighbor.IsContext) {
				continue
			}

			d := idx.distance(query, neighbor.Vector)

			if d < results.Top().distance || results.Len() < ef {
				heap.Push(candidates, distanceNode{id: neighborID, distance: d})
				heap.Push(results, distanceNode{id: neighborID, distance: d})

				if results.Len() > ef {
					heap.Pop(results)
				}
			}
		}
	}

	// Convert max heap to sorted slice (best first)
	resultList := make([]distanceNode, 0, results.Len())
	for results.Len() > 0 {
		resultList = append(resultList, heap.Pop(results).(distanceNode))
	}

	// Reverse to get ascending order
	for i, j := 0, len(resultList)-1; i < j; i, j = i+1, j-1 {
		resultList[i], resultList[j] = resultList[j], resultList[i]
	}

	return resultList
}

// selectNeighbors selects the best neighbors using a simple heuristic
func (idx *HNSWIndex) selectNeighbors(candidates []distanceNode, m int) []distanceNode {
	if len(candidates) <= m {
		return candidates
	}
	return candidates[:m]
}

// pruneNeighbors removes excess neighbors from a node
func (idx *HNSWIndex) pruneNeighbors(node *HNSWNode, level int, m int) {
	if level >= len(node.Neighbors) {
		return
	}

	if len(node.Neighbors[level]) <= m {
		return
	}

	// Calculate distances and sort
	neighbors := make([]distanceNode, 0, len(node.Neighbors[level]))
	for _, nid := range node.Neighbors[level] {
		n := idx.nodes[nid]
		d := idx.distance(node.Vector, n.Vector)
		neighbors = append(neighbors, distanceNode{id: nid, distance: d})
	}

	// Sort by distance
	sortDistanceNodes(neighbors)

	// Keep only top m
	node.Neighbors[level] = make([]string, m)
	for i := 0; i < m; i++ {
		node.Neighbors[level][i] = neighbors[i].id
	}
}

// randomLevel generates a random level for a new node
func (idx *HNSWIndex) randomLevel() int {
	level := 0
	for level < idx.config.MaxLevel && idx.rng.Float64() < 0.5 {
		level++
	}
	return level
}

// distance calculates Euclidean distance (can be swapped for cosine)
func (idx *HNSWIndex) distance(vec1, vec2 []float32) float32 {
	// Use 1 - cosine similarity for distance metric
	return 1.0 - CosineSimilarity(vec1, vec2)
}

// Clear removes all nodes from the index
func (idx *HNSWIndex) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.nodes = make(map[string]*HNSWNode)
	idx.entryPoint = ""
	idx.maxLevel = 0
}

// Size returns the number of nodes in the index
func (idx *HNSWIndex) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.nodes)
}

// distanceNode represents a node with its distance from query
type distanceNode struct {
	id       string
	distance float32
}

// Min heap for candidates (lower distance = higher priority)
type minHeap []distanceNode

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].distance < h[j].distance }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *minHeap) Push(x interface{}) {
	*h = append(*h, x.(distanceNode))
}

func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// Max heap for results (higher distance = higher priority for removal)
type maxHeap []distanceNode

func (h maxHeap) Len() int           { return len(h) }
func (h maxHeap) Less(i, j int) bool { return h[i].distance > h[j].distance }
func (h maxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *maxHeap) Push(x interface{}) {
	*h = append(*h, x.(distanceNode))
}

func (h *maxHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h maxHeap) Top() distanceNode {
	if len(h) == 0 {
		return distanceNode{distance: math.MaxFloat32}
	}
	return h[0]
}

// sortDistanceNodes sorts distance nodes by distance (ascending)
func sortDistanceNodes(nodes []distanceNode) {
	// Simple bubble sort for small arrays
	for i := 0; i < len(nodes)-1; i++ {
		for j := i + 1; j < len(nodes); j++ {
			if nodes[j].distance < nodes[i].distance {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}
}
