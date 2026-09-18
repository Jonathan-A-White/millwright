// Command mw is the millwright factory's command line: it files stories,
// dispatches sessions to work them, and reports what the factory is doing.
package main

import "os"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// cobra has already printed the error.
		os.Exit(1)
	}
}
