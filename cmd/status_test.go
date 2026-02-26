package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestParseRunningTask(t *testing.T) {
	tests := []struct {
		input     string
		wantState string
		wantTask  string
	}{
		{"review:my-task", "reviewing", "my-task"},
		{"merge:my-task", "merging", "my-task"},
		{"test:my-task", "testing", "my-task"},
		{"my-task", "running", "my-task"},
		{"review:group/task", "reviewing", "group/task"},
		{"unknown:task", "running", "unknown:task"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			state, task := parseRunningTask(tt.input)
			if state != tt.wantState {
				t.Errorf("state = %q, want %q", state, tt.wantState)
			}
			if task != tt.wantTask {
				t.Errorf("task = %q, want %q", task, tt.wantTask)
			}
		})
	}
}

func TestStatusOutputYAML(t *testing.T) {
	out := statusOutput{
		Running: map[string]statusRunning{
			"foo": {Action: "reviewing", PID: 123},
		},
		Pending: []string{"bar", "baz"},
		Review:  []string{"qux"},
	}

	var buf bytes.Buffer
	if err := yaml.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("yaml encode: %v", err)
	}

	var decoded statusOutput
	if err := yaml.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("yaml decode: %v", err)
	}

	if decoded.Running["foo"].Action != "reviewing" {
		t.Errorf("running foo action = %q, want reviewing", decoded.Running["foo"].Action)
	}
	if decoded.Running["foo"].PID != 123 {
		t.Errorf("running foo pid = %d, want 123", decoded.Running["foo"].PID)
	}
	if len(decoded.Pending) != 2 || decoded.Pending[0] != "bar" {
		t.Errorf("pending = %v, want [bar baz]", decoded.Pending)
	}
	if len(decoded.Review) != 1 || decoded.Review[0] != "qux" {
		t.Errorf("review = %v, want [qux]", decoded.Review)
	}
	if decoded.Merge != nil {
		t.Errorf("merge = %v, want nil (omitted)", decoded.Merge)
	}
}

func TestStatusOutputJSON(t *testing.T) {
	out := statusOutput{
		Running: map[string]statusRunning{
			"foo": {Action: "testing", PID: 456},
		},
		Merge: []string{"done-task"},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		t.Fatalf("json encode: %v", err)
	}

	var decoded statusOutput
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json decode: %v", err)
	}

	if decoded.Running["foo"].Action != "testing" {
		t.Errorf("running foo action = %q, want testing", decoded.Running["foo"].Action)
	}
	if decoded.Running["foo"].PID != 456 {
		t.Errorf("running foo pid = %d, want 456", decoded.Running["foo"].PID)
	}
	if len(decoded.Merge) != 1 || decoded.Merge[0] != "done-task" {
		t.Errorf("merge = %v, want [done-task]", decoded.Merge)
	}
	if decoded.Pending != nil {
		t.Errorf("pending = %v, want nil (omitted)", decoded.Pending)
	}
}

func TestStatusOutputEmptyOmitted(t *testing.T) {
	out := statusOutput{}

	buf, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}

	if string(buf) != "{}" {
		t.Errorf("empty output = %s, want {}", buf)
	}
}
