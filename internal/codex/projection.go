package codex

import "errors"

// Projection keeps a rollout's messages in file order. A rollout is linear;
// Codex does not branch it the way Claude Code branches by parentUuid.
type Projection struct {
	order []Record
	seen  map[string]Record
}

func NewProjection() *Projection { return &Projection{seen: make(map[string]Record)} }

func (p *Projection) Add(r Record) error {
	if r.ID == "" {
		return nil
	}
	if prior, ok := p.seen[r.ID]; ok {
		if !prior.Equal(r) {
			return errors.New("conflicting rollout ordinal requires resync")
		}
		return nil
	}
	p.seen[r.ID] = r
	p.order = append(p.order, r)
	return nil
}

func (p *Projection) Messages() []Record {
	out := []Record{}
	for _, r := range p.order {
		if r.Text != "" {
			out = append(out, r)
		}
	}
	return out
}
