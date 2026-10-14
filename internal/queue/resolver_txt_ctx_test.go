package queue

import (
	"context"
	"errors"
	"testing"
)

func TestRealMTASTSDNSResolverLookupTXTHonoursCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &realMTASTSDNSResolver{}
	_, err := r.LookupTXT(ctx, "_mta-sts.example.invalid.")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LookupTXT ignored cancelled ctx: err=%v", err)
	}
}
