package application

import "context"

// GivenBackAs lets the tests of package application_test judge a give-back by
// the story, as the stop give-backs of mw next do.
func GivenBackAs(ctx context.Context, tracker WorkTracker, id, status string, err error) error {
	return givenBackAs(ctx, tracker, id, status, err)
}
