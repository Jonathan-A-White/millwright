package domain

import (
	"strings"
	"testing"
)

func factoryTable(t *testing.T, text string) *ConfigTOML {
	t.Helper()
	file, err := ParseConfigTOML(text)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestTheInvestShareBoundsAreTenAndThirtyThreePercentUnlessTheTableSaysOtherwise(t *testing.T) {
	min, max, err := InvestShareBounds(nil)
	if err != nil || min != 0.10 || max != 0.33 {
		t.Fatalf("unset bounds read %v and %v: %v, want 0.10 and 0.33", min, max, err)
	}
	table := factoryTable(t, "[factory]\ninvest_share_min = 0.05\ninvest_share_max = 0.5\n").Table("factory")
	if min, max, err = InvestShareBounds(table); err != nil || min != 0.05 || max != 0.5 {
		t.Fatalf("set bounds read %v and %v: %v", min, max, err)
	}
}

func TestInvestShareBoundsWithMinNotBelowMaxAreRefusedNamingBothKeys(t *testing.T) {
	table := factoryTable(t, "[factory]\ninvest_share_min = 0.4\ninvest_share_max = 0.3\n").Table("factory")
	_, _, err := InvestShareBounds(table)
	if err == nil || !strings.Contains(err.Error(), "invest_share_min") || !strings.Contains(err.Error(), "invest_share_max") {
		t.Fatalf("min 0.4 over max 0.3 was not refused naming both keys: %v", err)
	}
	for _, bad := range []string{"invest_share_min = 0", "invest_share_max = 1.5", "invest_share_min = \"low\""} {
		table := factoryTable(t, "[factory]\n"+bad+"\n").Table("factory")
		if _, _, err := InvestShareBounds(table); err == nil || !strings.Contains(err.Error(), strings.Split(bad, " = ")[0]) {
			t.Errorf("%q was not refused naming its key: %v", bad, err)
		}
	}
}

func TestAnInvestShareOutsideTheBoundsIsRefusedNamingBoth(t *testing.T) {
	for _, c := range []struct {
		text string
		ok   bool
	}{
		{"[factory]\ninvest_share = 0.5\n", false},
		{"[factory]\ninvest_share = 0.05\n", false},
		{"[factory]\ninvest_share = 0.2\n", true},
		{"[factory]\ninvest_share = 0.33\n", true},
		{"[factory]\ninvest_share = 0.1\n", true},
		{"[factory]\ninvest_share = 0.5\ninvest_share_max = 0.6\n", true},
	} {
		err := CheckFactoryChange(factoryTable(t, c.text), "factory", "invest_share")
		if c.ok && err != nil {
			t.Errorf("%q was refused: %v", c.text, err)
		}
		if !c.ok && (err == nil || !strings.Contains(err.Error(), "invest_share_min 0.1") || !strings.Contains(err.Error(), "invest_share_max 0.33")) {
			t.Errorf("%q was not refused naming both bounds: %v", c.text, err)
		}
	}
	if err := CheckFactoryChange(factoryTable(t, "[other]\ninvest_share = 0.9\n"), "other", "invest_share"); err != nil {
		t.Errorf("a key of another table was checked: %v", err)
	}
}
