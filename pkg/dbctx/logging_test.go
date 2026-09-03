package dbctx

import (
	"context"
	"testing"
)

func TestSuppressQueryInfo(t *testing.T) {
	if QueryInfoSuppressed(context.Background()) {
		t.Fatal("ordinary context unexpectedly suppresses database query logs")
	}
	if !QueryInfoSuppressed(SuppressQueryInfo(context.Background())) {
		t.Fatal("polling context did not suppress database query Info logs")
	}
	if !QueryInfoSuppressed(SuppressQueryInfo(nil)) {
		t.Fatal("nil context was not converted into a polling context")
	}
}
