package main

import (
	"reflect"
	"testing"
)

func TestArmedWavePassMemoSharesIndexAndAuthorization(t *testing.T) {
	vault, idx, _ := armedWaveTestFixture(t)
	var loads, auths int
	previousLoad, previousAuth := loadV7IndexObserver, armedWaveAuthorizationObserver
	loadV7IndexObserver = func() { loads++ }
	armedWaveAuthorizationObserver = func() { auths++ }
	t.Cleanup(func() { loadV7IndexObserver, armedWaveAuthorizationObserver = previousLoad, previousAuth })

	members := []string{"APP-T-0001", "APP-T-0006"}
	fresh := map[string]Note{}
	for _, id := range members {
		projected, _, ok, err := armedWaveDispatchTaskProjection(vault, idx.Tasks[id])
		if err != nil || !ok {
			t.Fatalf("fresh projection %s: ok=%v err=%v", id, ok, err)
		}
		fresh[id] = projected
	}
	if loads != 2 || auths != 2 {
		t.Fatalf("unmemoized baseline loads=%d auths=%d, want 2/2", loads, auths)
	}

	loads, auths = 0, 0
	memo := newArmedWavePassMemo(vault)
	for _, id := range members {
		projected, projectedIdx, ok, err := armedWaveDispatchTaskProjectionWithMemo(memo, vault, idx.Tasks[id])
		if err != nil || !ok {
			t.Fatalf("memo projection %s: ok=%v err=%v", id, ok, err)
		}
		if !reflect.DeepEqual(projected.Data, fresh[id].Data) {
			t.Fatalf("memo projection %s diverged:\nmemo=%#v\nfresh=%#v", id, projected.Data, fresh[id].Data)
		}
		// A caller overlaying its own view must not leak into the next lookup.
		projectedIdx.Tasks[id] = Note{}
	}
	if loads != 1 || auths != 1 {
		t.Fatalf("one pass over two members of one wave: loads=%d auths=%d, want 1/1", loads, auths)
	}

	// A canonical document write inside the pass drops the memo.
	v7DocumentWriteGeneration.Add(1)
	if _, _, ok, err := armedWaveDispatchTaskProjectionWithMemo(memo, vault, idx.Tasks["APP-T-0001"]); err != nil || !ok {
		t.Fatalf("post-write projection: ok=%v err=%v", ok, err)
	}
	if loads != 2 || auths != 2 {
		t.Fatalf("write did not invalidate memo: loads=%d auths=%d, want 2/2", loads, auths)
	}

	// Another vault never reads this memo.
	other, otherIdx, _ := armedWaveTestFixture(t)
	loads = 0
	if _, _, ok, err := armedWaveDispatchTaskProjectionWithMemo(memo, other, otherIdx.Tasks["APP-T-0001"]); err != nil || !ok {
		t.Fatalf("other vault projection: ok=%v err=%v", ok, err)
	}
	if loads != 1 {
		t.Fatalf("foreign vault was served from memo: loads=%d", loads)
	}
}
