package isolation

import (
	"context"
	"testing"
)

func TestContextMarksIsolation(t *testing.T) {
	if Isolated(context.Background()) {
		t.Fatal("a plain context is isolated")
	}
	if !Isolated(Context(context.Background())) {
		t.Fatal("Context doesn't mark isolation")
	}
}
