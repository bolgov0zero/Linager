package agent

import (
	"reflect"
	"sort"
	"testing"
)

func TestProcessDescendants(t *testing.T) {
	parent := map[int32]int32{10: 1, 11: 10, 12: 10, 13: 11, 20: 1, 21: 20}
	got := processDescendants(parent, nil, 10)
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if want := []int32{11, 12, 13}; !reflect.DeepEqual(got, want) {
		t.Errorf("descendants of 10: got %v, want %v", got, want)
	}
	// the deepest first: 13 (a child of 11) comes before 11
	order := processDescendants(parent, nil, 10)
	pos := map[int32]int{}
	for i, p := range order {
		pos[p] = i
	}
	if pos[13] > pos[11] {
		t.Errorf("a child must come before its parent: %v", order)
	}
	if got := processDescendants(parent, nil, 99); len(got) != 0 {
		t.Errorf("no such process: got %v", got)
	}
}

func TestProcessDescendantsReusedParentPID(t *testing.T) {
	// 12 says its parent is 10, but 12 started before 10: the real parent has ended and the PID went to another process
	parent := map[int32]int32{11: 10, 12: 10}
	created := map[int32]int64{10: 2000, 11: 3000, 12: 1000}
	got := processDescendants(parent, created, 10)
	if !reflect.DeepEqual(got, []int32{11}) {
		t.Errorf("got %v, want [11]", got)
	}
}

func TestProcessDescendantsLoop(t *testing.T) {
	parent := map[int32]int32{5: 6, 6: 5, 7: 7}
	if got := processDescendants(parent, nil, 5); !reflect.DeepEqual(got, []int32{6}) {
		t.Errorf("got %v, want [6]", got)
	}
}
