package llm

// Usage reports token consumption and monetary cost for one LLM call.
type Usage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cacheRead"`
	CacheWrite  int64 `json:"cacheWrite"`
	Reasoning   int64 `json:"reasoning,omitempty"`
	TotalTokens int64 `json:"totalTokens"`
	Cost        Cost  `json:"cost"`
}

// Cost is the monetary breakdown of one LLM call.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}
