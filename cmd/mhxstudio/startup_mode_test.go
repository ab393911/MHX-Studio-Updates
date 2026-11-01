package main

import "testing"

func TestSavedSingleItemNeverBecomesFullChain(t *testing.T) {
	for _, mode := range []int{-1, 0, 1, 2, 3, 4, 5} {
		if e := validateSavedTaskMode(true, mode); (e == nil) != (mode == 0) {
			t.Fatal(mode, e)
		}
		if e := validateSavedTaskMode(false, mode); e != nil {
			t.Fatal(mode, e)
		}
	}
}
