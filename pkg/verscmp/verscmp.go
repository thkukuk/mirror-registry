package verscmp

import (
	"os"
	"fmt"
	"strings"
	"strconv"
	"regexp"
)

var (
	versionRegexp *regexp.Regexp
)

const (
	// The raw regular expression string used for testing the validity
	// of a version.
	versionRegexpStr string = `v?([0-9]+(\.[0-9]+)*?)`
)

func init() {
	versionRegexp = regexp.MustCompile("^" + versionRegexpStr + "$")
}

func IsVersion(ver string) bool {
	if !versionRegexp.MatchString(ver) {
		// fmt.Fprintf(os.Stderr, "No number: %s\n", ver)
		return false
	}
	return true
}

func Compare(a, b string) int {
	var length, r, l int = 0,0,0

	// Change l.m.n-x.y to l.m.n.x.z.y
	a1 := strings.Replace(a, "-", ".", -1)
	b1 := strings.Replace(b, "-", ".", -1)

	// if something is no version number, compare them as string
	if !IsVersion(a1) || !IsVersion(b1) {
		return strings.Compare(a, b)
	}

	// If the string starts with 'v', remove that
	// We know the rest is a version number
	if a1[0] == 'v' {
		a1 = a1[1:]
	}
	if b1[0] == 'v' {
		b1 = b1[1:]
	}

	v1 := strings.Split(a1, ".")
	v2 := strings.Split(b1, ".")

	len1, len2 := len(v1), len(v2)

	length = len2
	if len1 > len2 {
		length = len1
	}

	for i:= 0;i<length;i++ {
		if i < len1 && i < len2 {
			if v1[i] == v2[i] {
				continue
			}
		}
		r = 0
		if i < len1 {
			if number, err := strconv.Atoi(v1[i]); err == nil {
				r = number
			} else {
				fmt.Fprintf(os.Stderr, "Atoi(%s) failed: %s\n",
					v1[i], err.Error());
			}
		}

		l = 0
		if i < len2 {
			if number, err := strconv.Atoi(v2[i]); err == nil {
				l = number
			} else {
				fmt.Fprintf(os.Stderr, "Atoi(%s) failed: %s\n",
					v2[i], err.Error());
			}
		}

		if r < l {
			return -1
		}else if r> l {
			return 1
		}
	}

	return 0
}
