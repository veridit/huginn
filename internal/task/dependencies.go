package task

// DependencyDetail is the id-based display/MCP form of a source dependency.
// Ref in Document.Task is the original source token, which may be an alias.
type DependencyDetail struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Note    string `json:"note,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

func DependencyDetails(t Task, tasks []Task) []DependencyDetail {
	out := make([]DependencyDetail, 0, len(t.Dependencies))
	for _, d := range t.Dependencies {
		target, e := ResolveDependency(tasks, d.Ref)
		detail := DependencyDetail{ID: target.ID, Title: target.Title, Status: target.Status, Path: target.Path, Line: d.Line, Note: d.Note}
		if e != nil {
			detail.ID = d.Ref
			detail.Missing = true
		}
		out = append(out, detail)
	}
	return out
}
