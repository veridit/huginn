package task

// TypeInfo is the short, agent-readable description used by `huginn types`.
type TypeInfo struct {
	Value       string `json:"value"`
	Description string `json:"description"`
}

var TypeDescriptions = []TypeInfo{
	{"feature", "Add a new user-visible capability."},
	{"bug", "Correct behavior that does not match expectations."},
	{"chore", "Maintain tooling, dependencies, or project infrastructure."},
	{"spike", "Investigate a question and record evidence."},
	{"docs", "Write or improve documentation."},
}

// NextHint returns the concrete follow-up command for this task's state.
func NextHint(t Task) string { return hint(t) }

// Get resolves human input once and reads the authoritative document.
func (s *Store) Get(ref string) (*Document, error) { d, _, e := s.load(ref); return d, e }

// Next syncs through Refresh before applying the rank-ordered filters.
func (s *Store) Next(f Filter) (Result, error) {
	ts, e := s.Tasks()
	if e != nil {
		return Result{}, e
	}
	return Next(ts, f)
}
