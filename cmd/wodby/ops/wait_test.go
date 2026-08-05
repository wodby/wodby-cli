package ops

import "testing"

func TestTaskLogStepsFromValueOrdersByWorkflowPosition(t *testing.T) {
	steps := taskLogStepsFromValue([]interface{}{
		map[string]interface{}{"id": "step-3", "name": "Third", "position": float64(3)},
		map[string]interface{}{"id": "step-1", "name": "First", "position": float64(1)},
		map[string]interface{}{"id": "step-2", "name": "Run once", "position": float64(2)},
	})

	want := []string{"step-1", "step-2", "step-3"}
	if len(steps) != len(want) {
		t.Fatalf("len(steps) = %d, want %d", len(steps), len(want))
	}
	for i := range want {
		if steps[i].id != want[i] {
			t.Fatalf("steps[%d].id = %q, want %q", i, steps[i].id, want[i])
		}
	}
}

func TestTaskLogStepsFromValuePreservesLegacyAPIOrder(t *testing.T) {
	steps := taskLogStepsFromValue([]interface{}{
		map[string]interface{}{"id": "step-2", "name": "Second"},
		map[string]interface{}{"id": "step-1", "name": "First"},
	})

	want := []string{"step-2", "step-1"}
	for i := range want {
		if steps[i].id != want[i] {
			t.Fatalf("steps[%d].id = %q, want %q", i, steps[i].id, want[i])
		}
	}
}
