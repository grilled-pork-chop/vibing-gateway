package model

import "testing"

func TestObjectIDSetID(t *testing.T) {
	o := Object{"id": "facebook/opt-125m", "object": "model"}
	if o.ID() != "facebook/opt-125m" {
		t.Fatalf("ID() = %q", o.ID())
	}
	o.SetID("x")
	if o.ID() != "x" {
		t.Fatalf("SetID not applied: %q", o.ID())
	}
	if (Object{}).ID() != "" {
		t.Fatal("missing id should be empty")
	}
}

func TestPrefixRewriterExpandsAndPreservesFields(t *testing.T) {
	in := []Object{
		{"id": "facebook/opt-125m", "object": "model", "max_model_len": 2048.0},
		{"id": "opt-lora", "object": "model"},
		{"object": "model"}, // no id → dropped
	}
	out := PrefixRewriter("publishers/llm-demo/models/")(in)
	if len(out) != 2 {
		t.Fatalf("want 2 objects, got %d", len(out))
	}
	if out[0].ID() != "publishers/llm-demo/models/facebook/opt-125m" {
		t.Errorf("base id = %q", out[0].ID())
	}
	if out[1].ID() != "publishers/llm-demo/models/opt-lora" {
		t.Errorf("adapter id = %q", out[1].ID())
	}
	if out[0]["max_model_len"] != 2048.0 {
		t.Errorf("non-id field not preserved: %v", out[0]["max_model_len"])
	}
}

func TestFixedRewriterPicksBaseAndDropsRest(t *testing.T) {
	in := []Object{
		{"id": "some-adapter"},
		{"id": "meta-llama/Llama-3-70B", "max_model_len": 8192.0},
	}
	out := FixedRewriter("publishers/slurm/models/llama3-70b", "meta-llama/Llama-3-70B")(in)
	if len(out) != 1 {
		t.Fatalf("slurm advertises exactly one id, got %d", len(out))
	}
	if out[0].ID() != "publishers/slurm/models/llama3-70b" {
		t.Errorf("id = %q", out[0].ID())
	}
	if out[0]["max_model_len"] != 8192.0 {
		t.Errorf("field not preserved: %v", out[0]["max_model_len"])
	}
}

func TestFixedRewriterEmptyInput(t *testing.T) {
	if out := FixedRewriter("x", "y")(nil); out != nil {
		t.Fatalf("want nil for empty input, got %v", out)
	}
}

func TestFallback(t *testing.T) {
	tg := Target{BaseID: "publishers/llm-demo/models/facebook/opt-125m", Root: "facebook/opt-125m", OwnedBy: "kserve", Created: 42}
	fb := tg.Fallback()
	if fb.ID() != tg.BaseID || fb["object"] != "model" || fb["owned_by"] != "kserve" || fb["root"] != "facebook/opt-125m" || fb["created"] != int64(42) {
		t.Fatalf("unexpected fallback: %v", fb)
	}
}
