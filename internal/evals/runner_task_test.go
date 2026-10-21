package evals

import "testing"

func TestTaskResultCountsRealCalls(t *testing.T) {
	store, err := LoadFixtures(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := NewReplay(store, t.TempDir())
	r.note(ReplayCall{Signature: "shell pwd", Real: true})
	r.note(ReplayCall{Signature: "shell ls", Real: true})
	r.note(ReplayCall{Signature: "shell uname -a", Miss: true, Err: ErrNoFixture})
	opts := withDefaults(RunOptions{})
	opts.scope = &runScope{}
	out := finishTask(Task{ID: "x", Expect: Expect{MaxIterations: 1}}, TaskResult{ID: "x"}, turnResult{}, nil, r, store, opts)
	if out.row.RealCalls != 2 || out.row.FixtureMisses != 1 {
		t.Fatalf("real %d, misses %d", out.row.RealCalls, out.row.FixtureMisses)
	}
}
