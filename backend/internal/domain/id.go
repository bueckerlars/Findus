package domain

import "regexp"

var validIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// ValidID reports whether id is safe to use in file names and URLs. IDs become both,
// so anything that comes from outside (import files) has to pass this.
func ValidID(id string) bool { return validIDRE.MatchString(id) }
