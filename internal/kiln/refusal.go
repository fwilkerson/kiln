package kiln

import (
	"errors"
	"strings"
)

// refusals say why kiln refuses a project's files, each a sentence that names the fix. A command that refuses
// prints each on a line of its own.
type refusals []string

func (r refusals) Error() string { return strings.Join(r, "\n") }

// err is r as an error, or nil when r holds none.
func (r refusals) err() error {
	if len(r) == 0 {
		return nil
	}

	return r
}

// about puts subject before each of r, as the start of its sentence.
func (r refusals) about(subject string) refusals {
	about := make(refusals, len(r))
	for i, why := range r {
		about[i] = subject + " " + why
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
	why    string
}

// broken is the refusal of each of rules that the files break.
func broken(rules ...rule) refusals {
	var refused refusals
	for _, r := range rules {
		if r.broken {
			refused = append(refused, r.why)
		}
	}

	return refused
}
