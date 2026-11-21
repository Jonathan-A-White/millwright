package application

// NaturalLess reports whether a comes before b in natural order: the names are
// read in runs of digits and runs of anything else, and a run of digits is
// compared as a number, so 2026-11-21-999 comes before 2026-11-21-1000 and
// 2026-11-22-1 after both, where by text 1000 sorts below 999. It is how a
// seat's handoffs, named by date and count, are put in the order they were
// written. Two runs of digits equal as numbers are told apart by their length
// (the one with fewer leading zeros first), and a name that is a prefix of
// another comes first.
func NaturalLess(a, b string) bool {
	for a != "" && b != "" {
		if isDigit(a[0]) && isDigit(b[0]) {
			da, rest := leadingDigits(a)
			db, restB := leadingDigits(b)
			na, nb := trimZeros(da), trimZeros(db)
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			if len(da) != len(db) {
				return len(da) < len(db)
			}
			a, b = rest, restB
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return a == "" && b != ""
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// leadingDigits splits s into its first run of digits and the rest.
func leadingDigits(s string) (digits, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

func trimZeros(digits string) string {
	i := 0
	for i < len(digits)-1 && digits[i] == '0' {
		i++
	}
	return digits[i:]
}
