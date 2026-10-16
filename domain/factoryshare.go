package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultInvestShareMin and DefaultInvestShareMax are the bounds on the
// investment share, as fractions, when the config's [factory] table does not
// say: the share may be asked down to a tenth and allowed up to a third.
const (
	DefaultInvestShareMin = 0.10
	DefaultInvestShareMax = 0.33
)

// Keys of the [factory] table that bound and set the investment share.
const (
	FactoryInvestShareKey    = "invest_share"
	FactoryInvestShareMinKey = "invest_share_min"
	FactoryInvestShareMaxKey = "invest_share_max"
)

// InvestShareBounds reads invest_share_min and invest_share_max off the values
// of the [factory] table (ConfigTOML.Table; nil reads as empty), the defaults
// where one is unset. Each must be a fraction above 0 and at most 1, and min
// below max; the error names the keys at fault.
func InvestShareBounds(table map[string]string) (min, max float64, err error) {
	min, max = DefaultInvestShareMin, DefaultInvestShareMax
	for key, into := range map[string]*float64{FactoryInvestShareMinKey: &min, FactoryInvestShareMaxKey: &max} {
		said := strings.Trim(strings.TrimSpace(table[key]), `"'`)
		if said == "" {
			continue
		}
		n, err := strconv.ParseFloat(said, 64)
		if err != nil || n <= 0 || n > 1 {
			return 0, 0, fmt.Errorf("%s = %q: it must be a fraction above 0 and at most 1, such as %v", key, said, DefaultInvestShareMax)
		}
		*into = n
	}
	if min >= max {
		return 0, 0, fmt.Errorf("%s = %v must be below %s = %v", FactoryInvestShareMinKey, min, FactoryInvestShareMaxKey, max)
	}
	return min, max, nil
}

// CheckFactoryChange judges the config after table.key was set: the bounds of
// the [factory] table must stay a fraction each with min below max, and
// invest_share must lie from invest_share_min to invest_share_max. Any other
// key is not its business.
func CheckFactoryChange(after *ConfigTOML, table, key string) error {
	if table != "factory" {
		return nil
	}
	switch key {
	case FactoryInvestShareMinKey, FactoryInvestShareMaxKey:
		_, _, err := InvestShareBounds(after.Table(table))
		return err
	case FactoryInvestShareKey:
		values := after.Table(table)
		min, max, err := InvestShareBounds(values)
		if err != nil {
			return err
		}
		said := strings.Trim(strings.TrimSpace(values[key]), `"'`)
		share, err := strconv.ParseFloat(said, 64)
		if err != nil {
			return fmt.Errorf("%s.%s = %q: it must be a fraction", table, key, said)
		}
		if share < min || share > max {
			return fmt.Errorf("%s.%s = %v is outside the bounds: it must be from %s %v to %s %v",
				table, key, share, FactoryInvestShareMinKey, min, FactoryInvestShareMaxKey, max)
		}
	}
	return nil
}
