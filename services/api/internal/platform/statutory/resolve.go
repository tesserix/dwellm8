package statutory

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Scope is where the rule that answered came from.
//
// It is returned rather than inferred because the two are genuinely different
// answers to the same question: a state row means that state legislated, a
// national row means it did not and the central rule stands. An invoice that
// records which one it used can be explained years later.
type Scope string

const (
	ScopeState    Scope = "state"
	ScopeNational Scope = "national"
)

// Resolution is one answer: the rule, where it came from, and the date it was
// asked about.
//
// Callers stamp Rule.ID on whatever they compute. That is what makes a
// recomputation reproducible in the strong sense — not "the same query returns
// the same row today", but "this invoice says which row it used".
type Resolution struct {
	Rule  Rule
	Scope Scope
	Asked Jurisdiction
	On    effective.Date
}

// Gap is no rule in force. It names all four coordinates, because the useful
// question after this failure is which of them is wrong.
type Gap struct {
	Type         Type
	Jurisdiction Jurisdiction
	Key          string
	On           effective.Date
	// Fallback records that the national rule was tried too, so "Karnataka has no
	// row" and "nobody has a row" are distinguishable from the message alone.
	Fallback bool
}

// ErrNoRule is what a caller checks for with errors.Is.
var ErrNoRule = errors.New("statutory: no rule in force")

func (g *Gap) Error() string {
	where := string(g.Jurisdiction)
	if g.Fallback {
		where += " (and no national rule)"
	}
	return fmt.Sprintf("%s: %s/%s in %s on %s", ErrNoRule, g.Type, g.Key, where, g.On)
}

func (g *Gap) Unwrap() error { return ErrNoRule }

// Table is the registry loaded and indexed. Immutable once built: a rule change
// is a reload, so a request that has begun resolving cannot see the registry
// change under it half way through an invoice.
type Table struct {
	timelines map[coord]effective.Timeline[Rule]
	rules     []Rule
}

type coord struct {
	Type         Type
	Jurisdiction Jurisdiction
	Key          string
}

// NewTable validates every rule and indexes them by coordinate.
//
// It refuses a set with two live rules true on the same day, which the schema's
// exclusion constraint also refuses — deliberately both, because a table built
// from a fixture, a migration or an admin form never went through the constraint,
// and an ambiguous registry resolves to whichever row sorted first.
func NewTable(rules []Rule) (*Table, error) {
	t := &Table{timelines: map[coord]effective.Timeline[Rule]{}}
	for _, r := range rules {
		if r.Retired {
			continue
		}
		if err := r.Validate(); err != nil {
			return nil, err
		}
		c := coord{Type: r.Type, Jurisdiction: r.Jurisdiction, Key: r.Key}
		tl := t.timelines[c]
		kind := effective.KindChange
		if r.Corrects != "" {
			kind = effective.KindCorrection
		}
		tl.Records = append(tl.Records, effective.Record[Rule]{
			ID: r.ID, Range: r.Validity, Value: r, Kind: kind, Corrects: r.Corrects,
		})
		t.timelines[c] = tl
		t.rules = append(t.rules, r)
	}
	for c, tl := range t.timelines {
		if err := tl.Validate(); err != nil {
			return nil, fmt.Errorf("statutory: %s/%s/%s: %w", c.Type, c.Jurisdiction, c.Key, err)
		}
	}
	return t, nil
}

// Resolve answers what rule was in force for this type and key, in this
// jurisdiction, on this date.
//
// A state row wins where one exists; otherwise the national row stands and the
// resolution says so. There is no third step: no default, no nearest date, no
// most-recent-anywhere. A gap is an error naming the gap, because a calculation
// that proceeds with an unauthorised number is worse than one that stops.
func (t *Table) Resolve(typ Type, j Jurisdiction, key string, on effective.Date) (Resolution, error) {
	switch {
	case !typ.Valid():
		return Resolution{}, fmt.Errorf("statutory: %q is not a rule type", typ)
	case !j.Valid():
		return Resolution{}, fmt.Errorf("statutory: %q is not a jurisdiction", j)
	case on.Zero():
		return Resolution{}, errors.New("statutory: a resolution must say which date it is asking about")
	}

	if j != National {
		if r, ok := t.at(coord{typ, j, key}, on); ok {
			return Resolution{Rule: r, Scope: ScopeState, Asked: j, On: on}, nil
		}
	}
	if r, ok := t.at(coord{typ, National, key}, on); ok {
		return Resolution{Rule: r, Scope: ScopeNational, Asked: j, On: on}, nil
	}
	return Resolution{}, &Gap{Type: typ, Jurisdiction: j, Key: key, On: on, Fallback: j != National}
}

func (t *Table) at(c coord, on effective.Date) (Rule, bool) {
	tl, ok := t.timelines[c]
	if !ok {
		return Rule{}, false
	}
	rec, found := tl.AsOf(on)
	if !found {
		return Rule{}, false
	}
	return rec.Value, true
}

// Rules is every live rule, for an admin listing and for the contract test.
func (t *Table) Rules() []Rule { return append([]Rule(nil), t.rules...) }

// DueForReview is the rules whose review date falls on or before today plus
// within days, soonest first — the ones a Budget or a Council meeting may already
// have overtaken.
//
// Superseded rules are excluded: a row that stopped applying before today is
// history, and reviewing it changes nothing. Overdue is not an error here for the
// reason it is not an exception in the schema either — an overdue rule still
// resolves, and the operational answer is an alert with an owner on it, not a
// service that stops computing rent.
func (t *Table) DueForReview(today effective.Date, within int) []Rule {
	horizon := today.AddDays(within)
	var due []Rule
	for _, r := range t.rules {
		if !r.Validity.Open() && !today.Before(r.Validity.To()) {
			continue
		}
		if r.ReviewDue.After(horizon) {
			continue
		}
		due = append(due, r)
	}
	sort.SliceStable(due, func(i, j int) bool {
		if due[i].ReviewDue.Equal(due[j].ReviewDue) {
			return due[i].Key < due[j].Key
		}
		return due[i].ReviewDue.Before(due[j].ReviewDue)
	})
	return due
}
