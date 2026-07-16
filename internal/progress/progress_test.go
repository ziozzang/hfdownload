package progress

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	if b == nil {
		t.Fatal("New returned nil")
	}
	if b.Done() != 0 {
		t.Fatalf("expected Done()=0, got %d", b.Done())
	}
	b.Finish()
}

func TestAdd(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	b.Add(10)
	if b.Done() != 10 {
		t.Fatalf("expected Done()=10, got %d", b.Done())
	}
	b.Add(5)
	if b.Done() != 15 {
		t.Fatalf("expected Done()=15, got %d", b.Done())
	}
	b.Finish()
}

func TestAddCompleted(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	b.AddCompleted(20)
	if b.Done() != 20 {
		t.Fatalf("expected Done()=20, got %d", b.Done())
	}
	if b.rateBase.Load() != 20 {
		t.Fatalf("expected rateBase=20, got %d", b.rateBase.Load())
	}
	b.Finish()
}

func TestSetDone(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	b.SetDone(50)
	if b.Done() != 50 {
		t.Fatalf("expected Done()=50, got %d", b.Done())
	}
	if b.rateBase.Load() != 50 {
		t.Fatalf("expected rateBase=50, got %d", b.rateBase.Load())
	}
	b.Finish()
}

func TestSetLabel(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "initial")
	b.SetLabel("updated")
	b.labelMu.RLock()
	label := b.label
	b.labelMu.RUnlock()
	if label != "updated" {
		t.Fatalf("expected label 'updated', got '%s'", label)
	}
	b.Finish()
}

func TestFinishIdempotent(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	b.Finish()
	b.Finish() // should not panic or block
}

func TestFinishConcurrent(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Finish()
		}()
	}
	wg.Wait()
}

func TestRenderNonInteractive(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	b.SetDone(50)
	b.Finish()
	output := buf.String()
	if !strings.Contains(output, "50.00%") {
		t.Fatalf("expected output to contain '50.00%%', got: %s", output)
	}
}

func TestRenderZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 0, "test") // total = 0 -> indeterminate
	b.Add(42)
	b.Finish()
	output := buf.String()
	if !strings.Contains(output, "42 B") {
		t.Fatalf("expected output to contain '42 B', got: %s", output)
	}
}

func TestRenderExceedsTotal(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	b.SetDone(120)
	b.Finish()
	output := buf.String()
	// should clamp to 100% not show 120%
	if !strings.Contains(output, "100.00%") {
		t.Fatalf("expected output to contain '100.00%%', got: %s", output)
	}
}

func TestLogf(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	b.Logf("message: %d", 42)
	b.Finish()
	output := buf.String()
	if !strings.Contains(output, "message: 42") {
		t.Fatalf("expected output to contain 'message: 42', got: %s", output)
	}
}

func TestAddConcurrent(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 10000, "test")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Add(1)
		}()
	}
	wg.Wait()
	if b.Done() != 100 {
		t.Fatalf("expected Done()=100, got %d", b.Done())
	}
	b.Finish()
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TiB"},
	}
	for _, tc := range tests {
		got := formatBytes(tc.input)
		if got != tc.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestSetLabelVersionIncrement(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, 100, "test")
	v0 := b.labelVersion.Load()
	b.SetLabel("new")
	v1 := b.labelVersion.Load()
	if v1 != v0+1 {
		t.Fatalf("expected labelVersion to increment by 1 (%d -> %d)", v0, v1)
	}
	b.Finish()
}
