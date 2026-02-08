/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"reflect"
	"testing"
)

func TestPickOperationWeighted(t *testing.T) {
	ops := []operation{{name: "A", weight: 1}, {name: "B", weight: 1}}
	counts := map[string]int{"A": 0, "B": 0}
	for i := 0; i < 100; i++ {
		r := deterministicPick(ops, i)
		counts[r]++
	}
	if counts["A"] == 0 || counts["B"] == 0 {
		t.Fatalf("expected both operations to be selected: %v", counts)
	}
}

func TestPercentile(t *testing.T) {
	in := []float64{1, 2, 3, 4, 5}
	if got := percentile(in, 50); got != 3 {
		t.Fatalf("p50 mismatch: %.2f", got)
	}
	if got := percentile(in, 95); got != 4 {
		t.Fatalf("p95 mismatch: %.2f", got)
	}
}

func TestBuildComparisons(t *testing.T) {
	g := gateConfig{MinThroughputRatio: 0.7, MaxP99Ratio: 1.5}
	mvp := []scenarioResult{{Scenario: "ping_only", Throughput: 700, P99Ms: 1.5, Errors: 0}}
	ref := []scenarioResult{{Scenario: "ping_only", Throughput: 1000, P99Ms: 1.0, Errors: 0}}

	out := buildComparisons(g, mvp, ref)
	if len(out) != 1 {
		t.Fatalf("unexpected comparison size: %d", len(out))
	}
	if !out[0].OverallPass {
		t.Fatalf("expected gate pass, got %+v", out[0])
	}

	want := comparison{
		Scenario:            "ping_only",
		ThroughputRatio:     0.7,
		P99Ratio:            1.5,
		ThroughputPass:      true,
		P99Pass:             true,
		OverallPass:         true,
		MVPThroughputRPS:    700,
		RefThroughputRPS:    1000,
		MVPP99Ms:            1.5,
		ReferenceP99Ms:      1.0,
		MVPErrorCount:       0,
		ReferenceErrorCount: 0,
	}
	if !reflect.DeepEqual(out[0], want) {
		t.Fatalf("comparison mismatch: got=%+v want=%+v", out[0], want)
	}
}

func deterministicPick(ops []operation, seed int) string {
	// deterministic proxy without depending on random internals.
	total := 0
	for _, op := range ops {
		total += op.weight
	}
	pick := seed % total
	acc := 0
	for _, op := range ops {
		acc += op.weight
		if pick < acc {
			return op.name
		}
	}
	return ops[len(ops)-1].name
}
