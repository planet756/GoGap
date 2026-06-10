package domain

import "testing"

func TestRequiredCategoriesIncludeHongKongInRequestedOrder(t *testing.T) {
	want := []FundCategory{CategoryQDII, CategoryCommodity, CategoryHongKong, CategoryActiveLOF, CategoryIndexLOF, CategoryETF, CategoryBondMoney}
	if len(RequiredCategories) != len(want) {
		t.Fatalf("RequiredCategories length = %d, want %d: %+v", len(RequiredCategories), len(want), RequiredCategories)
	}
	for index := range want {
		if RequiredCategories[index] != want[index] {
			t.Fatalf("RequiredCategories[%d] = %q, want %q; full order: %+v", index, RequiredCategories[index], want[index], RequiredCategories)
		}
	}
}
