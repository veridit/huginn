package task

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
)

func (d *Document) parseFrontmatter() {
	var root yaml.Node
	source := strings.Join(d.lines[1:d.frontEnd], "")
	if e := yaml.Unmarshal([]byte(source), &root); e != nil {
		d.add(2, "malformed frontmatter: "+e.Error())
		return
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		d.add(2, "frontmatter must be a mapping")
		return
	}
	mapping := root.Content[0]
	d.frontmatterValid = true
	for n := 0; n+1 < len(mapping.Content); n += 2 {
		key, value := mapping.Content[n], mapping.Content[n+1]
		k := key.Value
		if key.Kind != yaml.ScalarNode {
			d.add(key.Line+1, "frontmatter keys must be strings")
			continue
		}
		if _, ok := d.fields[k]; ok {
			d.add(key.Line+1, "duplicate field "+k)
		}
		d.fields[k] = key.Line
		end := d.frontEnd
		if n+2 < len(mapping.Content) {
			end = mapping.Content[n+2].Line
		}
		for end > key.Line+1 {
			line := strings.TrimSpace(d.lines[end-1])
			if line == "" || strings.HasPrefix(line, "#") {
				end--
			} else {
				break
			}
		}
		d.fieldEnds[k] = end
		var target any
		switch k {
		case "id":
			target = &d.Task.ID
		case "title":
			target = &d.Task.Title
		case "status":
			target = &d.Task.Status
		case "type":
			target = &d.Task.Type
		case "priority":
			target = &d.Task.Priority
		case "assignee":
			target = &d.Task.Assignee
		case "labels":
			target = &d.Task.Labels
		case "aliases":
			target = &d.Task.Aliases
		case "created":
			target = &d.Task.Created
		case "updated":
			target = &d.Task.Updated
		default:
			continue
		}
		if e := value.Decode(target); e != nil {
			d.add(key.Line+1, fmt.Sprintf("invalid %s: %v", k, e))
		}
	}
}
