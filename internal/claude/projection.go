package claude

import (
	"errors"
	"slices"
)

type Projection struct {
	nodes map[string]Record
	leaf  string
}

func NewProjection() *Projection { return &Projection{nodes: make(map[string]Record)} }
func (p *Projection) Add(r Record) error {
	if r.ID == "" {
		return nil
	}
	if prior, ok := p.nodes[r.ID]; ok {
		if prior != r {
			return errors.New("conflicting transcript UUID requires resync")
		}
		return nil
	}
	seen := map[string]bool{r.ID: true}
	for id := r.ParentID; id != ""; id = p.nodes[id].ParentID {
		if seen[id] {
			return errors.New("transcript parent cycle")
		}
		seen[id] = true
	}
	p.nodes[r.ID] = r
	p.leaf = r.ID
	return nil
}
func (p *Projection) Messages() []Record {
	out := []Record{}
	for id := p.leaf; id != ""; {
		r, ok := p.nodes[id]
		if !ok {
			break
		}
		if r.Text != "" {
			out = append(out, r)
		}
		id = r.ParentID
	}
	slices.Reverse(out)
	return out
}
