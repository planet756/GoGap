package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type NamedQuoteAdapter struct {
	Name    string
	Adapter QuoteAdapter
}

type NamedNAVAdapter struct {
	Name    string
	Adapter NAVAdapter
}

type MultiQuoteAdapter struct {
	sources []NamedQuoteAdapter
}

type MultiNAVAdapter struct {
	sources []NamedNAVAdapter
}

func NewMultiQuoteAdapter(sources []NamedQuoteAdapter) (*MultiQuoteAdapter, error) {
	if len(sources) == 0 {
		return nil, errors.New("source: at least one quote source is required")
	}
	for i, source := range sources {
		if source.Adapter == nil {
			return nil, fmt.Errorf("source: quote source %d adapter is nil", i)
		}
	}
	return &MultiQuoteAdapter{sources: append([]NamedQuoteAdapter(nil), sources...)}, nil
}

func NewMultiNAVAdapter(sources []NamedNAVAdapter) (*MultiNAVAdapter, error) {
	if len(sources) == 0 {
		return nil, errors.New("source: at least one NAV source is required")
	}
	for i, source := range sources {
		if source.Adapter == nil {
			return nil, fmt.Errorf("source: NAV source %d adapter is nil", i)
		}
	}
	return &MultiNAVAdapter{sources: append([]NamedNAVAdapter(nil), sources...)}, nil
}

func (a *MultiQuoteAdapter) FetchQuotes(ctx context.Context, secIDs []string) (map[string]QuoteResult, error) {
	var failures []string
	for _, source := range a.sources {
		results, err := source.Adapter.FetchQuotes(ctx, secIDs)
		if err == nil {
			return results, nil
		}
		failures = append(failures, sourceError(source.Name, err))
	}
	return nil, fmt.Errorf("source: all quote sources failed: %s", strings.Join(failures, "; "))
}

func (a *MultiNAVAdapter) FetchNAVs(ctx context.Context, fundCodes []string) (map[string]NAVResult, error) {
	var failures []string
	for _, source := range a.sources {
		results, err := source.Adapter.FetchNAVs(ctx, fundCodes)
		if err == nil {
			return results, nil
		}
		failures = append(failures, sourceError(source.Name, err))
	}
	return nil, fmt.Errorf("source: all NAV sources failed: %s", strings.Join(failures, "; "))
}

func (a *MultiNAVAdapter) FillMissingChangePercent(ctx context.Context, navs map[string]NAVResult, fundCodes []string) (map[string]NAVResult, error) {
	var failures []string
	for _, source := range a.sources {
		filler, ok := source.Adapter.(interface {
			FillMissingChangePercent(context.Context, map[string]NAVResult, []string) (map[string]NAVResult, error)
		})
		if !ok {
			continue
		}
		results, err := filler.FillMissingChangePercent(ctx, navs, fundCodes)
		if err == nil {
			return results, nil
		}
		failures = append(failures, sourceError(source.Name, err))
	}
	if len(failures) == 0 {
		return navs, nil
	}
	return nil, fmt.Errorf("source: all NAV change percent fillers failed: %s", strings.Join(failures, "; "))
}

func (a *MultiQuoteAdapter) SourceNames() []string {
	names := make([]string, 0, len(a.sources))
	for _, source := range a.sources {
		names = append(names, source.Name)
	}
	return names
}

func (a *MultiNAVAdapter) SourceNames() []string {
	names := make([]string, 0, len(a.sources))
	for _, source := range a.sources {
		names = append(names, source.Name)
	}
	return names
}

func sourceError(name string, err error) string {
	if name == "" {
		name = "unnamed"
	}
	return fmt.Sprintf("%s: %v", name, err)
}

var _ QuoteAdapter = (*MultiQuoteAdapter)(nil)
var _ NAVAdapter = (*MultiNAVAdapter)(nil)
