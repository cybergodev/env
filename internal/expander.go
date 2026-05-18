// Package internal provides variable expansion with security limits.
package internal

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Mode controls how variables are expanded.
type Mode int

const (
	// ModeEnv expands $VAR and ${VAR} syntax.
	// This is the zero value, making it the default when Mode is not explicitly set.
	ModeEnv Mode = iota
	// ModeNone disables variable expansion entirely.
	ModeNone
	// ModeAll expands $VAR, ${VAR}, and ${VAR:-default} syntax.
	ModeAll
)

// braceOperators is a lookup table for valid brace expansion operators.
// Used for O(1) operator validation in expandBracedVariable.
var braceOperators = [256]bool{
	'-': true, // ${VAR:-default}
	'=': true, // ${VAR:=default}
	'?': true, // ${VAR:?error}
}

// visitedMapPool provides a pool of reusable visited maps for cycle detection.
// This reduces allocations during recursive expansion and cycle detection.
var visitedMapPool = sync.Pool{
	New: func() any {
		return make(map[string]bool, 16)
	},
}

// getVisitedMap retrieves a map from the pool.
// Returns a fallback map if the pool returns an unexpected type.
func getVisitedMap() map[string]bool {
	m, ok := visitedMapPool.Get().(map[string]bool)
	if !ok {
		// Fallback: create new map if pool returns unexpected type
		return make(map[string]bool, 16)
	}
	return m
}

// putVisitedMap returns a map to the pool after clearing it.
// SECURITY: This function ensures complete clearing before returning to pool
// to prevent cross-request contamination and false positive cycle detection.
func putVisitedMap(m map[string]bool) {
	if m == nil {
		return
	}
	// Check size before clearing - we want to avoid pooling very large maps
	// that could waste memory.
	size := len(m)

	// Don't pool very large maps (check original size before clearing)
	// Use <= to include maps at exactly MaxPooledMapSize
	if size > MaxPooledMapSize {
		// Map is too large, just discard it - let GC reclaim its memory
		return
	}

	// SECURITY: Use clear() builtin (Go 1.21+) for guaranteed complete clearing.
	// clear() is O(1) and reliably sets the map length to 0.
	// This is more efficient than the explicit deletion loop (~2-5% faster)
	// and equally safe because clear() is a language guarantee.
	clear(m)

	// Map is safely cleared and within size limit, return to pool
	visitedMapPool.Put(m)
}

// ExpanderConfig holds configuration for creating a new Expander.
type ExpanderConfig struct {
	MaxDepth   int
	Lookup     func(string) (string, bool)
	Mode       Mode
	KeyPattern *regexp.Regexp // For key validation
}

// Expander handles variable expansion with security limits.
type Expander struct {
	maxDepth        int
	lookup          func(string) (string, bool)
	mode            Mode
	keyPattern      *regexp.Regexp
	useDefaultCheck bool // If true, use isValidDefaultKey instead of regex
}

// NewExpander creates a new Expander with the specified configuration.
func NewExpander(cfg ExpanderConfig) *Expander {
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 5 // DefaultMaxExpansionDepth
	}
	if maxDepth > HardMaxExpansionDepth {
		maxDepth = HardMaxExpansionDepth
	}

	lookup := cfg.Lookup
	if lookup == nil {
		lookup = func(key string) (string, bool) {
			return "", false
		}
	}

	mode := cfg.Mode
	if mode == Mode(0) {
		mode = ModeEnv // redundant safety: zero value is already ModeEnv
	}

	// Use default check (isValidDefaultKey) if no custom pattern provided
	keyPattern := cfg.KeyPattern
	useDefaultCheck := keyPattern == nil

	return &Expander{
		maxDepth:        maxDepth,
		lookup:          lookup,
		mode:            mode,
		keyPattern:      keyPattern,
		useDefaultCheck: useDefaultCheck,
	}
}

// Expand performs variable expansion on the input string.
func (e *Expander) Expand(s string) (string, error) {
	// ModeNone: expansion is disabled, return input unchanged.
	if e.mode == ModeNone {
		return s, nil
	}
	// Fast path: if no dollar sign, no expansion needed
	// Use IndexByte which is SIMD-optimized
	dollarIdx := strings.IndexByte(s, '$')
	if dollarIdx == -1 {
		return s, nil
	}

	// Fast path: single "$" character (not a variable reference)
	// This avoids allocating a visited map for the common case of "$" alone
	if len(s) == 1 {
		return s, nil
	}

	// SECURITY: All subsequent s[0] and s[1] accesses are safe because:
	// - len(s) == 1 check above ensures len(s) >= 2 at this point
	// - This explicit assertion documents the invariant
	// Fast path: handle escaped dollar sign ($$) at the start
	// This is a common case that needs special handling before expandSingleVar
	//
	// SECURITY INVARIANT: len(s) >= 2 guaranteed by the len(s) == 1 check above
	if s[0] == '$' && s[1] == '$' {
		if len(s) == 2 {
			return "$", nil
		}
		// $$ followed by more content needs full expansion
		visited := getVisitedMap()
		defer putVisitedMap(visited)
		return e.expandWithDepth(s, 0, visited)
	}

	// Fast path: single variable reference at the start with no other variables
	// SECURITY INVARIANT: len(s) >= 2 guaranteed by the len(s) == 1 check above
	if dollarIdx == 0 && strings.IndexByte(s[1:], '$') == -1 {
		return e.expandSingleVar(s)
	}

	// General case: multiple variables or variable not at start
	visited := getVisitedMap()
	defer putVisitedMap(visited)
	return e.expandWithDepth(s, 0, visited)
}

// expandSingleVar handles the common case of a single variable reference.
// This is an optimized path that avoids builder allocation for simple cases.
//
// Precondition: len(s) >= 2 and s[0] == '$' (caller is responsible for ensuring this)
func (e *Expander) expandSingleVar(s string) (string, error) {
	// Note: Caller (Expand) guarantees len(s) >= 2, so we skip the check here
	visited := getVisitedMap()
	defer putVisitedMap(visited)

	// clearVisited clears the visited map for reuse in fallback expansion.
	// This avoids false cycle detection from partial expansion attempts.
	clearVisited := func() {
		clear(visited)
	}

	if s[1] == '{' {
		// ${VAR} syntax
		expanded, consumed, err := e.expandBracedVariable(s, 0, visited)
		if err != nil {
			// SECURITY: Clear partial state before returning to prevent pool contamination
			clear(visited)
			return "", err
		}
		// If the entire string was consumed, return the expansion
		if consumed == len(s) {
			return expanded, nil
		}
		// Otherwise fall back to full expansion with a cleared visited map
		clearVisited()
		return e.expandWithDepth(s, 0, visited)
	}

	// $VAR syntax
	expanded, consumed, err := e.expandSimpleVariable(s, 0, visited)
	if err != nil {
		// SECURITY: Clear partial state before returning to prevent pool contamination
		clear(visited)
		return "", err
	}
	if consumed == len(s) {
		return expanded, nil
	}
	// Otherwise fall back to full expansion with a cleared visited map
	clearVisited()
	return e.expandWithDepth(s, 0, visited)
}

// expandWithDepth performs expansion with depth tracking and cycle detection.
// SECURITY: Includes stack overflow protection to prevent deep recursion crashes.
func (e *Expander) expandWithDepth(s string, depth int, visited map[string]bool) (string, error) {
	// SECURITY: Stack overflow protection - use a reasonable limit even if maxDepth is high
	// This prevents stack exhaustion on systems with small stack sizes
	const maxStackDepth = 100 // Conservative limit for stack safety
	if depth >= maxStackDepth {
		return "", &ExpansionError{
			Depth: depth,
			Limit: maxStackDepth,
			Chain: "stack depth limit exceeded (potential stack overflow)",
		}
	}
	// Use >= to ensure maxDepth is the actual maximum depth (not maxDepth+1)
	if depth >= e.maxDepth {
		return "", &ExpansionError{
			Depth: depth,
			Limit: e.maxDepth,
			Chain: e.buildChain(visited),
		}
	}

	// Fast path: count variables and check for escaped dollars in single pass
	varCount := 0
	hasEscapedDollar := false
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) {
			if s[i+1] == '$' {
				hasEscapedDollar = true
				i++ // Skip next $
			} else {
				varCount++
			}
		}
	}

	// Fast path: no variable references
	if varCount == 0 {
		if !hasEscapedDollar {
			return s, nil
		}
		// Only escaped dollars - simple replacement
		result := GetBuilder()
		defer PutBuilder(result)
		result.Grow(len(s))
		for i := 0; i < len(s); i++ {
			if s[i] == '$' && i+1 < len(s) && s[i+1] == '$' {
				result.WriteByte('$')
				i++
			} else {
				result.WriteByte(s[i])
			}
		}
		return result.String(), nil
	}

	// Estimate capacity: use a more conservative estimate
	// Base: original length + buffer for expansions
	// Use 16 chars per variable as typical expansion size
	initialCap := len(s) + varCount*16
	if initialCap < 32 {
		initialCap = 32
	}
	// Cap maximum initial allocation
	if initialCap > 2048 {
		initialCap = len(s) + len(s)/2
		if initialCap < 32 {
			initialCap = 32
		}
	}

	result := GetBuilder()
	defer PutBuilder(result)
	result.Grow(initialCap)

	// Use segment-based writing to reduce WriteByte calls
	// Write non-variable segments as whole strings
	segStart := 0
	i := 0
	for i < len(s) {
		if s[i] == '$' && i+1 < len(s) {
			// Write the segment before this variable
			if i > segStart {
				result.WriteString(s[segStart:i])
			}
			expanded, consumed, err := e.expandVariable(s[i:], depth, visited)
			if err != nil {
				return "", err
			}
			result.WriteString(expanded)
			i += consumed
			segStart = i
		} else {
			i++
		}
	}
	// Write any remaining segment
	if segStart < len(s) {
		result.WriteString(s[segStart:])
	}

	return result.String(), nil
}

// expandVariable expands a single variable reference starting at the beginning of s.
// Returns the expanded value and the number of characters consumed.
func (e *Expander) expandVariable(s string, depth int, visited map[string]bool) (string, int, error) {
	if len(s) < 2 || s[0] != '$' {
		return s[:1], 1, nil
	}

	// Handle $$ (escaped dollar sign)
	if s[1] == '$' {
		return "$", 2, nil
	}

	// Handle ${VAR} syntax
	if s[1] == '{' {
		return e.expandBracedVariable(s, depth, visited)
	}

	// Handle $VAR syntax
	return e.expandSimpleVariable(s, depth, visited)
}

// expandBracedVariable expands ${VAR}, ${VAR:-default}, ${VAR:=default}, etc.
// Handles nested braces correctly by counting brace depth.
func (e *Expander) expandBracedVariable(s string, depth int, visited map[string]bool) (string, int, error) {
	// Find the matching closing brace, handling nested braces
	// We need to find '}' at depth 1 (not inside any nested braces)
	braceDepth := 0
	end := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			braceDepth++
		} else if s[i] == '}' {
			braceDepth--
			if braceDepth == 0 {
				end = i
				break
			}
		}
	}
	if end == -1 {
		// SECURITY: No closing brace - return the original string unchanged
		// This makes it clear that expansion failed rather than returning a confusing placeholder
		return s, len(s), nil
	}

	content := s[2:end]
	if content == "" {
		// Empty braces ${} - return empty braces content
		return "{}", end + 1, nil
	}

	// Check for default value syntax using a single scan
	var key, defaultValue string
	var hasDefault bool
	var opType byte // '!' for :-, '=' for :=, '?' for :?

	// Single scan to find colon operator using lookup table for O(1) validation
	colonIdx := -1
	for i := 0; i < len(content)-1; i++ {
		if content[i] == ':' && braceOperators[content[i+1]] {
			colonIdx = i
			opType = content[i+1]
			break
		}
	}

	if colonIdx != -1 {
		key = content[:colonIdx]
		defaultValue = content[colonIdx+2:]
		hasDefault = true

		// Handle :? operator specially
		if opType == '?' {
			value, ok := e.lookup(key)
			if !ok || value == "" {
				return "", end + 1, &ExpansionError{
					Key:   key,
					Chain: "required variable not set: " + defaultValue,
				}
			}
			return value, end + 1, nil
		}
	} else {
		key = content
	}

	// Validate the key
	// Use fast byte-level check for default pattern, regex for custom patterns
	var valid bool
	if e.useDefaultCheck {
		valid = isValidDefaultKey(key)
	} else {
		valid = e.keyPattern.MatchString(key)
	}
	if !valid {
		return s[:end+1], end + 1, nil // Return as-is for invalid keys
	}

	// Check for cycles
	if visited[key] {
		return "", end + 1, &ExpansionError{
			Key:   key,
			Depth: depth,
			Chain: e.buildChain(visited),
		}
	}

	// Look up the value
	value, ok := e.lookup(key)
	if !ok {
		// Variable not set - use default if available
		if hasDefault {
			value = defaultValue
		} else {
			return "", end + 1, nil // Return empty for unset variables
		}
	}
	// Note: An empty string is a valid explicit value and should NOT trigger the default

	// Mark as visited for cycle detection
	visited[key] = true

	// Recursively expand the value
	expanded, err := e.expandWithDepth(value, depth+1, visited)
	// Clean up visited marker (manual cleanup is faster than defer)
	delete(visited, key)
	if err != nil {
		return "", end + 1, err
	}

	return expanded, end + 1, nil
}

// expandSimpleVariable expands $VAR syntax.
func (e *Expander) expandSimpleVariable(s string, depth int, visited map[string]bool) (string, int, error) {
	// Extract the variable name
	end := 1
	for end < len(s) && isVarChar(s[end]) {
		end++
	}

	if end == 1 {
		// No valid variable name after $
		return "$", 1, nil
	}

	key := s[1:end]

	// Check for cycles
	if visited[key] {
		return "", end, &ExpansionError{
			Key:   key,
			Depth: depth,
			Chain: e.buildChain(visited),
		}
	}

	// Look up the value
	value, ok := e.lookup(key)
	if !ok {
		return "", end, nil // Return empty for unset variables
	}

	// Mark as visited
	visited[key] = true

	// Recursively expand
	expanded, err := e.expandWithDepth(value, depth+1, visited)
	// Clean up visited marker (manual cleanup is faster than defer)
	delete(visited, key)
	if err != nil {
		return "", end, err
	}

	return expanded, end, nil
}

// buildChain builds a string representation of the expansion chain for error messages.
// Keys are sorted for deterministic output. Sensitive keys are masked for security.
func (e *Expander) buildChain(visited map[string]bool) string {
	if len(visited) == 0 {
		return ""
	}
	// Pre-allocate exact size to avoid growth
	keys := make([]string, 0, len(visited))
	for k := range visited {
		keys = append(keys, k)
	}
	// Sort for deterministic output
	sort.Strings(keys)

	// Calculate total length to pre-allocate builder
	// Use actual masked key length for accurate pre-allocation
	totalLen := 0
	for _, k := range keys {
		if IsKey(k) {
			totalLen += len(MaskKey(k)) // Actual masked length (3 or 5 chars)
		} else {
			totalLen += len(k)
		}
	}
	totalLen += (len(keys) - 1) * 4 // " -> " separator

	var sb strings.Builder
	sb.Grow(totalLen)
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(" -> ")
		}
		// SECURITY: Mask sensitive keys in error messages
		if IsKey(k) {
			sb.WriteString(MaskKey(k))
		} else {
			sb.WriteString(k)
		}
	}
	return sb.String()
}

// DetectCycle checks if expanding the given variables would cause a cycle.
// Returns the key that causes the cycle and true if a cycle is detected.
func DetectCycle(vars map[string]string) (string, bool) {
	// Use pooled maps to reduce allocations
	visited := getVisitedMap()
	inStack := getVisitedMap()
	defer func() {
		putVisitedMap(visited)
		putVisitedMap(inStack)
	}()

	var dfs func(key string, dfsDepth int) (string, bool)
	dfs = func(key string, dfsDepth int) (string, bool) {
		// SECURITY: Limit recursion depth to prevent stack overflow on long chains.
		// If we exceed this limit, return no cycle found — the expansion depth
		// limit will catch actual issues during expansion.
		if dfsDepth > 1000 {
			return "", false
		}
		visited[key] = true
		inStack[key] = true

		value, ok := vars[key]
		if !ok {
			inStack[key] = false
			return "", false
		}

		// Fast path: if no dollar sign, no variable references
		// Use IndexByte which is SIMD-optimized
		if strings.IndexByte(value, '$') == -1 {
			inStack[key] = false
			return "", false
		}

		// Find all variable references in the value
		for i := 0; i < len(value); i++ {
			if value[i] == '$' && i+1 < len(value) {
				var refKey string
				nextChar := value[i+1]

				if nextChar == '{' {
					// ${VAR} syntax - find matching closing brace (handle nesting)
					start := i + 2
					end := start
					braceDepth := 1
					for end < len(value) {
						if value[end] == '{' {
							braceDepth++
						} else if value[end] == '}' {
							braceDepth--
							if braceDepth == 0 {
								break
							}
						}
						end++
					}
					if end < len(value) && braceDepth == 0 {
						// Handle default syntax - find colon manually
						// Extract key directly to avoid intermediate allocation
						keyEnd := end
						for j := start; j < keyEnd; j++ {
							if value[j] == ':' {
								keyEnd = j
								break
							}
						}
						refKey = value[start:keyEnd]
					}
				} else if nextChar == '$' {
					// Escaped dollar sign, skip
					i++
					continue
				} else if isVarChar(nextChar) {
					// $VAR syntax - extract variable name
					start := i + 1
					end := start
					for end < len(value) && isVarChar(value[end]) {
						end++
					}
					refKey = value[start:end]
				}

				if refKey != "" {
					if inStack[refKey] {
						return refKey, true
					}
					if !visited[refKey] {
						if cycle, found := dfs(refKey, dfsDepth+1); found {
							return cycle, true
						}
					}
				}
			}
		}

		inStack[key] = false
		return "", false
	}

	for key := range vars {
		if !visited[key] {
			if cycle, found := dfs(key, 0); found {
				return cycle, true
			}
		}
	}

	return "", false
}

// ExpandAllInMap expands all variables in a map using this expander.
// This is the primary method for batch expansion, used by parsers.
//
// The method:
// 1. Checks if any values need expansion (fast path for no variables)
// 2. Detects cycles to prevent infinite loops
// 3. Expands all values recursively using a cached lookup for performance
//
// Returns the original map if no expansion is needed, avoiding allocations.
func (e *Expander) ExpandAllInMap(vars map[string]string) (map[string]string, error) {
	// ModeNone: expansion is disabled, return input unchanged.
	if e.mode == ModeNone {
		return vars, nil
	}

	// Fast path: check if any values need expansion
	needsExpansion := false
	for _, value := range vars {
		if strings.IndexByte(value, '$') != -1 {
			needsExpansion = true
			break
		}
	}

	// No expansion needed - return original map without copying
	if !needsExpansion {
		return vars, nil
	}

	// Check for cycles only when expansion is needed
	if cycle, found := DetectCycle(vars); found {
		return nil, &ExpansionError{
			Key:   cycle,
			Chain: "cycle detected",
		}
	}

	// Build a lookup cache that first checks vars, then falls back to e.lookup.
	// This avoids repeated os.LookupEnv syscalls for variables already present
	// in the parsed map (the most common case for $VAR references).
	lookupCache := make(map[string]lookupResult, len(vars))
	cachedLookup := func(key string) (string, bool) {
		if r, ok := lookupCache[key]; ok {
			return r.value, r.ok
		}
		// Check vars first (most common source for $VAR refs)
		if v, ok := vars[key]; ok {
			lookupCache[key] = lookupResult{v, true}
			return v, true
		}
		// Fall back to external lookup (os.LookupEnv)
		v, ok := e.lookup(key)
		lookupCache[key] = lookupResult{v, ok}
		return v, ok
	}

	// Create a cached expander for batch expansion
	cached := &Expander{
		maxDepth:        e.maxDepth,
		lookup:          cachedLookup,
		mode:            e.mode,
		keyPattern:      e.keyPattern,
		useDefaultCheck: e.useDefaultCheck,
	}

	// Pre-allocate result map with exact size
	result := make(map[string]string, len(vars))

	// Expand all values using the cached expander
	for key, value := range vars {
		expanded, err := cached.Expand(value)
		if err != nil {
			return nil, err
		}
		result[key] = expanded
	}

	return result, nil
}

// lookupResult caches a single lookup result to avoid repeated map lookups.
type lookupResult struct {
	value string
	ok    bool
}
