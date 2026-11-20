package config

import "fmt"

// The [grist] settings of a person marked API only when the table says
// nothing; application's own defaults are the same (mw-gq6.1518).
const (
	DefaultGristAPIDailyLimit   = 30
	DefaultGristCreditsRenewDay = 17
)

// gristBillingSettings reads into settings the [grist] table's
// `api_daily_limit` (how many grist a day a person marked API only may send,
// default 30) and `credits_renew_day` (the day of the month, 1 to 31, the API
// credit renews, default 17, which an out-of-credits refusal names).
func gristBillingSettings(table map[string]string, path string, settings *GristSettings) error {
	limit, err := countIn(table, "api_daily_limit", DefaultGristAPIDailyLimit, path)
	if err != nil {
		return err
	}
	day, err := countIn(table, "credits_renew_day", DefaultGristCreditsRenewDay, path)
	if err != nil {
		return err
	}
	if day > 31 {
		return fmt.Errorf("the [%s] table of %s says credits_renew_day = %d: it must be a day of the month, 1 to 31", GristTable, path, day)
	}
	settings.APIDailyLimit, settings.CreditsRenewDay = int(limit), int(day)
	return nil
}
