// Provides variable expansion with security limits.

package internal

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Mode controls how variables are expanded.
type Mode int

const (
	// ModeEnv expands $VAR and ${VAR} syntax, including the brace operators
	// (${VAR:-default}, ${VAR:=default}, ${VAR:?err}) — the same treatment a
	// POSIX shell gives an expansion. Brace operators are NOT disabled in
	// this mode.
	// This is the zero value, making it the default when Mode is not explicitly set.
	ModeEnv Mode = iota
	// ModeNone disables variable expansion entirely.
	ModeNone
	// ModeAll expands $VAR, ${VAR}, and the brace operators. Behaviorally
	// identical to ModeEnv — expansion is all-or-nothing, gated only on
	// ModeNone; it exists for explicitness at call sites.
	ModeAll
)

// braceOperators is a lookup table for valid brace expansion operators.
// Used for O(1) operator validation in parseBraceOperator.
var braceOperators = [256]bool{
	'-': true, // ${VAR:-default}
	'=': true, // ${VAR:=default}
	'?': true, // ${VAR:?error}
}

// findClosingBrace returns the index of the '}' that matches the '{' at s[1],
// accounting for nested braces, or -1 when the braces are unbalanced. Shared
// by the single-variable fast path and the general expansion path so the
// matching rules cannot drift apart.
// Precondition: len(s) >= 2, s[0] == '$', s[1] == '{'.
func findClosingBrace(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseBraceOperator splits ${...} content at the first valid :-, := or :?
// operator (validated via braceOperators). It returns the variable name, the
// default/error text after the operator, the operator byte, and whether an
// operator was present at all. Single source of truth for both expansion
// paths: they previously re-implemented this scan independently, and the
// divergence produced the :? regression fixed in expandBracedVariable.
func parseBraceOperator(content string) (key, def string, op byte, hasDefault bool) {
	for i := 0; i < len(content)-1; i++ {
		if content[i] == ':' && braceOperators[content[i+1]] {
			return content[:i], content[i+2:], content[i+1], true
		}
	}
	return content, "", 0, false
}

// scanVarName extracts the $VAR name at the start of s ('$' at s[0]) and
// returns it together with the index just past the last variable character.
// Returns end == 1 when no valid variable character follows the '$'.
// Precondition: len(s) >= 2, s[0] == '$'.
func scanVarName(s string) (key string, end int) {
	end = 1
	for end < len(s) && isVarChar(s[end]) {
		end++
	}
	return s[1:end], end
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
	// MaxValueLength bounds the OUTPUT of expanding a single value. Raw
	// values are length-checked at parse time, but a multi-reference chain
	// (V1=${V2}x${V2}) doubles per level without ever cycling, so the
	// expanded result could otherwise grow as 2^depth × raw size.
	// <=0 means DefaultMaxValueLength; values above HardMaxValueLength are
	// clamped to it.
	MaxValueLength int
}

// Expander handles variable expansion with security limits.
type Expander struct {
	maxDepth        int
	lookup          func(string) (string, bool)
	mode            Mode
	keyPattern      *regexp.Regexp
	useDefaultCheck bool // If true, use isValidDefaultKey instead of regex
	maxValueLen     int  // Bound on one expanded value; always >0 via NewExpander
	// literal names keys whose values came from single-quoted .env source
	// and must be inserted verbatim wherever they are referenced (bash '...'
	// semantics). Nil on standalone expanders — no keys are literal.
	literal map[string]struct{}
	// assignments collects ':=' write-backs. It is non-nil only on the
	// per-batch expander built by ExpandAllInMap; a standalone Expander has
	// nowhere to store an assignment, so ':=' degrades to ':-' there.
	assignments map[string]string
}

// NewExpander creates a new Expander with the specified configuration.
func NewExpander(cfg ExpanderConfig) *Expander {
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxExpansionDepth
	}
	if maxDepth > HardMaxExpansionDepth {
		maxDepth = HardMaxExpansionDepth
	}

	maxValueLen := cfg.MaxValueLength
	if maxValueLen <= 0 {
		maxValueLen = DefaultMaxValueLength
	}
	if maxValueLen > HardMaxValueLength {
		maxValueLen = HardMaxValueLength
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
		maxValueLen:     maxValueLen,
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
// boundValue enforces the MaxValueLength output bound on single-variable
// fast paths, which return looked-up values directly (external lookups are
// os.LookupEnv results that never passed parse-time validation) instead of
// going through the builder the general path checks after every write
// (GEN-001). Mirrors the general path's error exactly.
func (e *Expander) boundValue(value string) (string, error) {
	if len(value) > e.maxValueLen {
		return "", &ValidationError{
			Field:   "value",
			Rule:    "max_length",
			Message: "expanded value exceeds maximum length",
		}
	}
	return value, nil
}

func (e *Expander) expandSingleVar(s string) (string, error) {
	// Note: Caller (Expand) guarantees len(s) >= 2, so we skip the check here

	if s[1] == '{' {
		return e.expandSingleBracedVar(s)
	}
	return e.expandSingleSimpleVar(s)
}

// expandFull runs the general expansion path with a fresh visited map.
// The single-variable fast paths call it whenever the variable reference does
// not consume the entire string, so that any literal suffix is preserved
// (the general path tracks consumed counts and writes the remaining segment).
func (e *Expander) expandFull(s string) (string, error) {
	visited := getVisitedMap()
	defer putVisitedMap(visited)
	return e.expandWithDepth(s, 0, visited)
}

// expandSingleSimpleVar handles $VAR syntax with leaf-value fast path.
// Skips the visited map allocation when the resolved value has no further $ references.
func (e *Expander) expandSingleSimpleVar(s string) (string, error) {
	key, end := scanVarName(s)
	if end == 1 {
		// No valid variable name after $ — fall back to full expansion
		// which handles the remaining characters correctly
		visited := getVisitedMap()
		defer putVisitedMap(visited)
		return e.expandWithDepth(s, 0, visited)
	}

	// Look up value
	value, ok := e.lookup(key)
	if !ok {
		if end == len(s) {
			return "", nil
		}
		// Unset variable with a literal suffix — the general path keeps the
		// suffix ("$FOO bar" -> " bar" when FOO is unset).
		return e.expandFull(s)
	}

	// Literal (single-quoted) source: insert verbatim. The whole-string case
	// returns directly; with a suffix the general path resolves the reference
	// through expandSimpleVariable's literal check and keeps expanding only
	// the suffix.
	if _, literal := e.literal[key]; literal {
		if end == len(s) {
			return value, nil
		}
		return e.expandFull(s)
	}

	// Fast path: if the value is a leaf (no $), no recursion needed.
	// This avoids the visited map pool Get/Put + map set/delete overhead.
	if strings.IndexByte(value, '$') == -1 {
		// If the entire string was consumed, return directly (bounded —
		// see boundValue)
		if end == len(s) {
			return e.boundValue(value)
		}
		// Otherwise fall back to full expansion
		visited := getVisitedMap()
		defer putVisitedMap(visited)
		return e.expandWithDepth(s, 0, visited)
	}

	// Value has $ references — need full expansion with cycle detection.
	// visited is freshly acquired (and therefore empty), so no pre-check is
	// needed: the cycle test runs inside expandWithDepth once the reference
	// is marked below.
	visited := getVisitedMap()
	defer putVisitedMap(visited)

	visited[key] = true
	expanded, err := e.expandWithDepth(value, 1, visited)
	delete(visited, key)
	if err != nil {
		return "", err
	}

	// If the entire string was consumed, return the expansion
	if end == len(s) {
		return expanded, nil
	}
	// Otherwise fall back to full expansion with a cleared visited map
	clear(visited)
	return e.expandWithDepth(s, 0, visited)
}

// expandSingleBracedVar handles ${VAR} syntax with leaf-value fast path.
// Skips the visited map allocation when the resolved value has no further $ references.
func (e *Expander) expandSingleBracedVar(s string) (string, error) {
	// Fast path: try to resolve without visited map first
	end := findClosingBrace(s)
	if end == -1 {
		return s, nil
	}

	content := s[2:end]
	if content == "" {
		if end+1 == len(s) {
			return "{}", nil
		}
		// Literal suffix follows the empty braces — keep it ("${} tail" -> "{} tail").
		return e.expandFull(s)
	}

	// Parse operator (:-,:=,:?) via the shared parser
	key, defaultValue, opType, hasDefault := parseBraceOperator(content)

	if hasDefault && opType == '?' {
		value, ok := e.lookup(key)
		if !ok || value == "" {
			// SECURITY (SEC-05): do not echo the file-supplied :? message
			// into the error struct (see expandBracedVariable).
			return "", &ExpansionError{
				Key:   key,
				Kind:  ExpansionRequiredKind,
				Chain: "required variable not set",
			}
		}
		// Value is set — if entire string consumed and no $, return directly
		// (bounded — see boundValue)
		if end+1 == len(s) && strings.IndexByte(value, '$') == -1 {
			return e.boundValue(value)
		}
		// Fall through to full expansion
	}

	// Validate key
	var valid bool
	if e.useDefaultCheck {
		valid = isValidDefaultKey(key)
	} else {
		valid = e.keyPattern.MatchString(key)
	}
	if !valid {
		if end+1 == len(s) {
			return s[:end+1], nil
		}
		// Invalid key with a literal suffix — the general path returns the
		// reference verbatim and preserves the suffix ("${1BAD} tail").
		return e.expandFull(s)
	}

	// Look up the value
	value, ok := e.lookup(key)
	// ':=' on an unset variable assigns the expanded default; on this leaf
	// fast path the raw default has no '$', so it is already expanded.
	assign := !ok && hasDefault && opType == '='
	if !ok {
		if hasDefault {
			value = defaultValue
		} else if end+1 == len(s) {
			return "", nil
		} else {
			// Unset variable with a literal suffix — keep the suffix.
			return e.expandFull(s)
		}
	}

	// Literal (single-quoted) source: verbatim when the reference spans the
	// whole string; otherwise the general path inserts it verbatim via
	// expandBracedVariable's literal check.
	if ok {
		if _, literal := e.literal[key]; literal {
			if end+1 == len(s) {
				return value, nil
			}
			return e.expandFull(s)
		}
	}

	// Fast path: leaf value (no $) and entire string consumed (bounded —
	// see boundValue)
	if end+1 == len(s) && strings.IndexByte(value, '$') == -1 {
		if assign {
			e.recordAssign(key, value)
		}
		return e.boundValue(value)
	}

	// Need full expansion with cycle detection
	visited := getVisitedMap()
	defer putVisitedMap(visited)

	expanded, consumed, err := e.expandBracedVariable(s, 0, visited)
	if err != nil {
		clear(visited)
		return "", err
	}
	if consumed == len(s) {
		return expanded, nil
	}
	clear(visited)
	return e.expandWithDepth(s, 0, visited)
}

// expandWithDepth performs expansion with depth tracking and cycle detection.
// SECURITY: recursion is bounded by maxDepth, which NewExpander clamps to
// HardMaxExpansionDepth — no separate stack-depth guard is needed.
func (e *Expander) expandWithDepth(s string, depth int, visited map[string]bool) (string, error) {
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
			// SECURITY: bound the builder as it grows. Multi-reference DAG
			// amplification (V1=${V2}x${V2}) doubles per level without ever
			// cycling, so depth and cycle checks cannot see it; checking
			// here aborts the moment any intermediate expansion crosses
			// maxValueLen, keeping peak memory O(depth × maxValueLen)
			// instead of 2^depth × raw size.
			if result.Len() > e.maxValueLen {
				return "", &ValidationError{
					Field:   "value",
					Rule:    "max_length",
					Message: "expanded value exceeds maximum length",
				}
			}
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
	// SECURITY: covers the tail segment written above (and doubles as the
	// final output bound for this level of the recursion).
	if result.Len() > e.maxValueLen {
		return "", &ValidationError{
			Field:   "value",
			Rule:    "max_length",
			Message: "expanded value exceeds maximum length",
		}
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
	end := findClosingBrace(s)
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

	// Check for default value syntax (:-, :=, :?) via the shared parser
	key, defaultValue, opType, hasDefault := parseBraceOperator(content)

	// Handle :? operator specially
	if hasDefault && opType == '?' {
		value, ok := e.lookup(key)
		if !ok || value == "" {
			// SECURITY (SEC-05): the :? message is file content — echoing
			// it into the error leaks attacker-controlled (or
			// secret-bearing) text; the masked Key already identifies the
			// variable.
			return "", end + 1, &ExpansionError{
				Key:   key,
				Kind:  ExpansionRequiredKind,
				Chain: "required variable not set",
			}
		}
		// The set-value must be expanded like the plain ${VAR} path below:
		// it may itself contain $VAR references (HOST=$BASE with
		// ${HOST:?required} must yield the expansion of $BASE, not the raw
		// "$BASE" — regression: this branch previously returned the value
		// unexpanded). Cycle detection mirrors the main path so a required
		// variable that participates in a cycle errors instead of recursing.
		// Literal (single-quoted) source values are the exception: insert
		// verbatim, no recursion.
		if _, literal := e.literal[key]; literal {
			return value, end + 1, nil
		}
		if visited[key] {
			return "", end + 1, &ExpansionError{
				Key:   key,
				Depth: depth,
				Chain: e.buildChain(visited),
			}
		}
		visited[key] = true
		expanded, err := e.expandWithDepth(value, depth+1, visited)
		delete(visited, key)
		if err != nil {
			return "", end + 1, err
		}
		return expanded, end + 1, nil
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
	// ':=' on an unset variable assigns the expanded default (POSIX ':=');
	// the write-back is recorded after the value is fully expanded below.
	assign := !ok && hasDefault && opType == '='
	if !ok {
		// Variable not set - use default if available
		if hasDefault {
			value = defaultValue
		} else {
			return "", end + 1, nil // Return empty for unset variables
		}
	}
	// Note: An empty string is a valid explicit value and should NOT trigger the default

	// Literal (single-quoted) source values insert verbatim — no recursion
	// and no cycle participation (a cycle through a literal key is
	// unreachable, matching DetectCycle's treatment of them).
	if ok {
		if _, literal := e.literal[key]; literal {
			return value, end + 1, nil
		}
	}

	// Mark as visited for cycle detection
	visited[key] = true

	// Recursively expand the value
	expanded, err := e.expandWithDepth(value, depth+1, visited)
	// Clean up visited marker (manual cleanup is faster than defer)
	delete(visited, key)
	if err != nil {
		return "", end + 1, err
	}

	if assign {
		e.recordAssign(key, expanded)
	}

	return expanded, end + 1, nil
}

// expandSimpleVariable expands $VAR syntax.
func (e *Expander) expandSimpleVariable(s string, depth int, visited map[string]bool) (string, int, error) {
	key, end := scanVarName(s)
	if end == 1 {
		// No valid variable name after $
		return "$", 1, nil
	}

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

	// Literal (single-quoted) source values insert verbatim — no recursion
	// and no cycle participation.
	if _, literal := e.literal[key]; literal {
		return value, end, nil
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
	// Use the masked length for sensitive keys (MaskKey returns the fixed
	// "[MASKED]" token, SEC-005), otherwise the full key length.
	totalLen := 0
	for _, k := range keys {
		if IsKey(k) {
			totalLen += len(MaskKey(k))
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
// The noExpand set names keys whose values are literal (single-quoted) and
// contribute no dependency edges; nil considers every key.
// Returns the key that causes the cycle and true if a cycle is detected.
func DetectCycle(vars map[string]string, noExpand map[string]struct{}) (string, bool) {
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
		// Literal (single-quoted) values contribute no dependency edges —
		// not just as scan roots (the outer loop skips those) but also when
		// reached through another value's reference: their '$' runs are
		// literal text the expander never follows. Without this guard,
		// A='$B' (literal) with B=$A still closed a false cycle through
		// dfs(B) scanning A's literal text.
		if _, literal := noExpand[key]; literal {
			visited[key] = true
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
					// ${VAR} syntax — reuse the shared brace matcher and
					// operator parser (findClosingBrace / parseBraceOperator)
					// so the edge set here cannot drift from what expansion
					// actually follows. DetectCycle previously split at ANY
					// colon, so a single-colon ${A:x} — which the expander
					// treats as an invalid key and emits verbatim — created a
					// phantom edge to A and could close a false cycle,
					// rejecting the whole file.
					if end := findClosingBrace(value[i:]); end != -1 {
						refKey, _, _, _ = parseBraceOperator(value[i+2 : i+end])
						// Skip past '}': references inside operator default
						// text (${B:-$C}) are conditional, not unconditional
						// edges. Continuing the scan at i+1 made the $C in
						// the default look like a hard dependency, so
						// B=x / A=${B:-$C} / C=$A was rejected as a cycle
						// even though A resolves without ever consulting the
						// default. Loops reachable only through an unused
						// default are still caught at expansion time by the
						// visited-map checks.
						i += end
					}
				} else if nextChar == '$' {
					// Escaped dollar sign, skip
					i++
					continue
				} else if isVarChar(nextChar) {
					// $VAR syntax - extract variable name
					refKey, _ = scanVarName(value[i:])
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
		if _, literal := noExpand[key]; literal {
			continue // literal values contribute no edges
		}
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
// The noExpand set names keys whose values are literal (single-quoted in the
// source .env) and must be copied through unchanged; nil expands every value.
// Exempt values still answer lookups from other values' references, and the
// text they contribute is never rescanned for further references.
//
// Brace-operator semantics: ${VAR:=default} assigns the expanded default to
// VAR when VAR is unset everywhere (map and external lookup). The assigned
// value becomes visible to references in keys sorted later in the pass and
// is materialized in the returned map. Keys are processed in sorted order so
// the outcome never depends on Go's random map iteration order. A standalone
// Expander (used via Expand, without a map context) has nowhere to store an
// assignment, so ':=' degrades to ':-' there.
//
// The method:
// 1. Checks if any values need expansion (fast path for no variables)
// 2. Detects cycles to prevent infinite loops
// 3. Expands all values recursively using a cached lookup for performance
//
// Returns the original map if no expansion is needed, avoiding allocations.
func (e *Expander) ExpandAllInMap(vars map[string]string, noExpand map[string]struct{}) (map[string]string, error) {
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
	if cycle, found := DetectCycle(vars, noExpand); found {
		return nil, &ExpansionError{
			Key:   cycle,
			Chain: "cycle detected",
		}
	}

	// Build a lookup cache for EXTERNAL lookups only. References resolved
	// from the parsed map itself (the most common case) are answered by a
	// direct map probe — caching them would add a second probe plus a cache
	// write per hit for no benefit. Only e.lookup results (os.LookupEnv
	// syscalls, expensive and frequently repeated for missing keys) are
	// cached. Resolution order and results are identical to the previous
	// cache-everything variant. ':=' assignments are probed after vars: an
	// assignment only exists for a key that was absent from vars AND the
	// external lookup, so it can never shadow a genuinely set variable.
	assignments := make(map[string]string)
	lookupCache := getLookupCache()
	defer putLookupCache(lookupCache)
	cachedLookup := func(key string) (string, bool) {
		// Check vars first (most common source for $VAR refs)
		if v, ok := vars[key]; ok {
			return v, true
		}
		if v, ok := assignments[key]; ok {
			return v, true
		}
		if r, ok := lookupCache[key]; ok {
			return r.value, r.ok
		}
		// Fall back to external lookup (os.LookupEnv) and cache the result
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
		maxValueLen:     e.maxValueLen,
		literal:         noExpand,
		assignments:     assignments,
	}

	// Expand in sorted key order. ':=' write-backs become visible to keys
	// sorted later in the pass, so the outcome must not depend on Go's
	// random map iteration order. (A POSIX shell applies the same rule in
	// document order; alphabetical order is this library's deterministic
	// equivalent.)
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Pre-allocate the result map. ':=' write-backs happen inside the loop
	// below, so assignments is still empty here — len(vars) is the honest
	// floor, and the map grows naturally when write-backs add keys (bounded
	// afterwards by the HardMaxVariables check).
	result := make(map[string]string, len(vars))

	// Expand all values using the cached expander
	for _, key := range keys {
		value := vars[key]
		// Literal (single-quoted) values pass through untouched but remain
		// visible as lookup sources through the vars map above.
		if _, literal := noExpand[key]; literal {
			result[key] = value
			continue
		}
		expanded, err := cached.Expand(value)
		if err != nil {
			return nil, err
		}
		result[key] = expanded
	}

	// ':=' assignments materialize as variables in the returned map, so
	// later pipeline stages (validation, Apply) treat them like any other
	// parsed variable. Bound them by the hard variable ceiling: the parse
	// path enforces MaxVariables on the PARSED map, and a crafted document
	// could otherwise bypass it through many small assignment references
	// (raw values are length-bounded, but a 1 KB value still holds ~100 of
	// them).
	if len(assignments) > HardMaxVariables-len(vars) {
		return nil, &ExpansionError{
			Chain: fmt.Sprintf("':=' assignments exceed the hard variable limit (%d)", HardMaxVariables),
		}
	}
	for key, value := range assignments {
		result[key] = value
	}

	return result, nil
}

// lookupResult caches a single lookup result to avoid repeated map lookups.
type lookupResult struct {
	value string
	ok    bool
}

// recordAssign stores a ':=' write-back when this expander is bound to a map
// context (ExpandAllInMap). Standalone expanders have a nil map and ':='
// degrades to ':-' — there is nowhere to store the assignment.
func (e *Expander) recordAssign(key, value string) {
	if e.assignments != nil {
		e.assignments[key] = value
	}
}

// lookupCachePool pools the per-expansion lookup cache. The cache is only
// needed for the duration of one ExpandAllInMap call; pooling it removes a
// size-proportional map allocation from every batch expansion.
var lookupCachePool = sync.Pool{
	New: func() any {
		return make(map[string]lookupResult, 16)
	},
}

// getLookupCache retrieves a lookup cache from the pool.
func getLookupCache() map[string]lookupResult {
	m, ok := lookupCachePool.Get().(map[string]lookupResult)
	if !ok {
		return make(map[string]lookupResult, 16)
	}
	return m
}

// putLookupCache returns a lookup cache to the pool after clearing it.
// SECURITY: clear() (Go 1.21+) guarantees no stale cached values — including
// resolved secrets — survive into the next use.
func putLookupCache(m map[string]lookupResult) {
	if m == nil {
		return
	}
	// Check size before clearing — after clear() the length is 0.
	if len(m) > MaxPooledMapSize {
		return // too large, let GC reclaim it
	}
	clear(m)
	lookupCachePool.Put(m)
}
