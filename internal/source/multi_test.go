package source

import (
	"context"
	"testing"
)

func TestMultiNAVAdapterDelegatesMissingChangePercentFiller(t *testing.T) {
	nav := 1.0
	change := 1.29
	filler := &fillingNAVAdapter{results: map[string]NAVResult{"000001": {Code: "000001", NAV: &nav}}, change: change}
	adapter, err := NewMultiNAVAdapter([]NamedNAVAdapter{{Name: "test", Adapter: filler}})
	if err != nil {
		t.Fatalf("NewMultiNAVAdapter returned error: %v", err)
	}

	filled, err := adapter.FillMissingChangePercent(context.Background(), filler.results, []string{"000001"})
	if err != nil {
		t.Fatalf("FillMissingChangePercent returned error: %v", err)
	}
	if filler.fillCalls != 1 {
		t.Fatalf("expected underlying filler to be called once, got %d", filler.fillCalls)
	}
	if filled["000001"].ChangePercent == nil || *filled["000001"].ChangePercent != change {
		t.Fatalf("expected delegated filler to populate change percent, got %+v", filled["000001"])
	}
}

type fillingNAVAdapter struct {
	results   map[string]NAVResult
	change    float64
	fillCalls int
}

func (a *fillingNAVAdapter) FetchNAVs(context.Context, []string) (map[string]NAVResult, error) {
	return a.results, nil
}

func (a *fillingNAVAdapter) FillMissingChangePercent(_ context.Context, navs map[string]NAVResult, fundCodes []string) (map[string]NAVResult, error) {
	a.fillCalls++
	results := make(map[string]NAVResult, len(navs))
	for code, result := range navs {
		for _, target := range fundCodes {
			if code == target {
				result.ChangePercent = &a.change
			}
		}
		results[code] = result
	}
	return results, nil
}
