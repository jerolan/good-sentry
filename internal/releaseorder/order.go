// Package releaseorder implements GoodSentry's native semantic release order.
package releaseorder

import "strings"

type version struct {
	packageName string
	core        [4]string
	prerelease  []string
}

// Compare reports -1, 0 or 1 only for valid versions in the same package family.
func Compare(a, b string) (int, bool) {
	left, validLeft := parse(a)
	right, validRight := parse(b)
	if !validLeft || !validRight || left.packageName != right.packageName {
		return 0, false
	}
	for index := range left.core {
		if order := compareDecimal(left.core[index], right.core[index]); order != 0 {
			return order, true
		}
	}
	if len(left.prerelease) == 0 && len(right.prerelease) > 0 {
		return 1, true
	}
	if len(right.prerelease) == 0 && len(left.prerelease) > 0 {
		return -1, true
	}
	for index := 0; index < min(len(left.prerelease), len(right.prerelease)); index++ {
		l, r := left.prerelease[index], right.prerelease[index]
		ln, rn := decimal(l), decimal(r)
		var order int
		switch {
		case ln && rn:
			order = compareDecimal(l, r)
		case ln:
			order = -1
		case rn:
			order = 1
		default:
			order = strings.Compare(l, r)
		}
		if order != 0 {
			return order, true
		}
	}
	if len(left.prerelease) < len(right.prerelease) {
		return -1, true
	}
	if len(left.prerelease) > len(right.prerelease) {
		return 1, true
	}
	return 0, true
}

// ProjectUsesSemantic classifies at most ten associated versions, newest first.
func ProjectUsesSemantic(recent []string) bool {
	if len(recent) > 10 {
		recent = recent[:10]
	}
	if len(recent) == 0 {
		return false
	}
	if len(recent) <= 2 {
		_, valid := parse(recent[0])
		return valid
	}
	var total, newestThree int
	for index, item := range recent {
		if _, valid := parse(item); valid {
			total++
			if index < 3 {
				newestThree++
			}
		}
	}
	return newestThree > 0 && (len(recent) < 10 || total >= 3)
}

func parse(raw string) (version, bool) {
	v := version{core: [4]string{"0", "0", "0", "0"}}
	if at := strings.LastIndexByte(raw, '@'); at >= 0 {
		if at == 0 {
			return version{}, false
		}
		v.packageName, raw = raw[:at], raw[at+1:]
	}
	if plus := strings.IndexByte(raw, '+'); plus >= 0 {
		if _, valid := identifiers(raw[plus+1:], false); !valid {
			return version{}, false
		}
		raw = raw[:plus]
	}
	if dash := strings.IndexByte(raw, '-'); dash >= 0 {
		var valid bool
		v.prerelease, valid = identifiers(raw[dash+1:], true)
		if !valid {
			return version{}, false
		}
		raw = raw[:dash]
	}
	core := strings.Split(raw, ".")
	if len(core) > len(v.core) {
		return version{}, false
	}
	for index, component := range core {
		if !decimal(component) || len(component) > 1 && component[0] == '0' {
			return version{}, false
		}
		v.core[index] = component
	}
	return v, true
}

func identifiers(raw string, rejectNumericZeroes bool) ([]string, bool) {
	parts := strings.Split(raw, ".")
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		for index := range part {
			c := part[index]
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-') {
				return nil, false
			}
		}
		if rejectNumericZeroes && len(part) > 1 && part[0] == '0' && decimal(part) {
			return nil, false
		}
	}
	return parts, true
}

func decimal(raw string) bool {
	if raw == "" {
		return false
	}
	for index := range raw {
		if raw[index] < '0' || raw[index] > '9' {
			return false
		}
	}
	return true
}

// Validated decimal components have no leading zeroes, so length then digits
// preserves numeric order without an integer bound or floating point rounding.
func compareDecimal(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
