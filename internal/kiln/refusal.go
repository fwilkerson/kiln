package kiln

import (
	"errors"
	"strings"
)

// refusal is one reason kiln refuses a project's files: the problem, and the fix that ends it. A command prints it
// as the problem, a colon, then the fix.
type refusal struct {
	problem, fix string
}

func (r refusal) String() string { return r.problem + ": " + r.fix }

// refusals are each reason kiln refuses a project's files. A command that refuses prints each on a line of its own.
type refusals []refusal

func (r refusals) Error() string {
	lines := make([]string, len(r))
	for i, why := range r {
		lines[i] = why.String()
	}

	return strings.Join(lines, "\n")
}

// err is r as an error, or nil when r holds none.
func (r refusals) err() error {
	if len(r) == 0 {
		return nil
	}

	return r
}

// about puts subject before each problem of r, as the start of its sentence.
func (r refusals) about(subject string) refusals {
	about := make(refusals, len(r))
	for i, why := range r {
		about[i] = refusal{subject + " " + why.problem, why.fix}
	}

	return about
}

// add appends to r each refusal err holds. It answers err when err is no refusal, and nil otherwise.
func (r *refusals) add(err error) error {
	if refused, ok := errors.AsType[refusals](err); ok {
		*r = append(*r, refused...)
		return nil
	}

	return err
}

// rule is one rule of a project's files: whether the files break it, and the refusal that says so.
type rule struct {
	broken bool
	refusal
}

// broken is the refusal of each of rules that the files break.
func broken(rules ...rule) refusals {
	var refused refusals
	for _, r := range rules {
		if r.broken {
			refused = append(refused, r.refusal)
		}
	}

	return refused
}

// unreadable refuses a file kiln cannot read as its kind, such as kiln.json that is no JSON object.
// Nothing more in it can be checked.
type unreadable struct {
	refusals
}
