package bot

import (
	"errors"
	"testing"
)

// TestPyBool locks the Python str(bool) casing the /admin stats reply mirrors.
func TestPyBool(t *testing.T) {
	if got := pyBool(true); got != "True" {
		t.Errorf("pyBool(true) = %q; want True", got)
	}
	if got := pyBool(false); got != "False" {
		t.Errorf("pyBool(false) = %q; want False", got)
	}
}

// TestAt locks the positional-arg accessor that maps every missing /admin arg to
// errWrongSyntax (Python IndexError -> "wrong syntax").
func TestAt(t *testing.T) {
	args := []string{"a", "b"}
	if v, err := at(args, 0); err != nil || v != "a" {
		t.Errorf("at(args,0) = (%q,%v); want (a,nil)", v, err)
	}
	if v, err := at(args, 1); err != nil || v != "b" {
		t.Errorf("at(args,1) = (%q,%v); want (b,nil)", v, err)
	}
	for _, i := range []int{2, -1, 99} {
		if _, err := at(args, i); !errors.Is(err, errWrongSyntax) {
			t.Errorf("at(args,%d) err = %v; want errWrongSyntax", i, err)
		}
	}
	if _, err := at(nil, 0); !errors.Is(err, errWrongSyntax) {
		t.Errorf("at(nil,0) err = %v; want errWrongSyntax", err)
	}
}
