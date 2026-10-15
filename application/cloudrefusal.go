package application

import (
	"fmt"
	"strings"
)

// cloudRefusals are the words of Vultr (or the terraform that speaks for it)
// that mean a refusal no retry mends, each with what it is. The first is the
// one seen (mw-gq6.460); the others are what Vultr's API and its terraform
// provider say for a plan sold out in a region, an instance limit and a token
// or address it will not serve, matched loosely and in lower case.
var cloudRefusals = []struct {
	err   error
	words []string
}{
	{ErrCloudFeeLimit, []string{"maximum monthly fee limit", "monthly fee limit"}},
	{ErrCloudQuota, []string{"instance limit", "maximum number of instances", "maximum instance", "reached your limit", "exceeded your limit"}},
	{ErrCloudSoldOut, []string{"sold out", "out of stock", "not available in this region", "not available in the region", "plan is not available", "no capacity", "insufficient capacity"}},
	{ErrCloudBadToken, []string{"invalid api key", "invalid api token", "unauthorized ip", "unauthorized api", "401 unauthorized", "status code: 401", "authentication failed"}},
}

// ClassifyCloudError wraps the refusal said in output around err when the
// provider's words are one of the cloudRefusals, so that errors.Is names it;
// any other failure is returned as it was.
func ClassifyCloudError(err error, output string) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(output + "\n" + err.Error())
	for _, r := range cloudRefusals {
		for _, w := range r.words {
			if strings.Contains(text, w) {
				return &cloudRefusal{refusal: r.err, cause: err}
			}
		}
	}
	return err
}

// cloudRefusal is a failure that is one of the Err* refusals: its text is the
// plain sentence, the provider's own failure kept for Unwrap.
type cloudRefusal struct {
	refusal, cause error
}

func (c *cloudRefusal) Error() string { return c.refusal.Error() }

func (c *cloudRefusal) Is(target error) bool { return target == c.refusal }

func (c *cloudRefusal) Unwrap() error { return c.cause }

// Detail is the provider's own failure, for the log.
func (c *cloudRefusal) Detail() string { return fmt.Sprintf("%v", c.cause) }
