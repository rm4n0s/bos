package service

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/errors"
)

// Length limits of SPEC §3.
const (
	maxServerName = 100
	maxHost       = 100
	maxCodeName   = 100
	maxName       = 300
	maxTag        = 50
)

// checkCodeName verifies that a code name can be referenced from a command
// (SPEC §6.1): a WORD, not a reserved word, at most 100 characters.
func checkCodeName(entity, code string) error {
	switch {
	case utf8.RuneCountInString(code) > maxCodeName:
		return errors.New("CodeNameInvalid", "a code name has at most 100 characters", "entity", entity, "code_name", code)
	case code == "" || !command.IsWord(code):
		return errors.New("CodeNameInvalid", "a code name may contain only letters, digits and underscore", "entity", entity, "code_name", code)
	case command.IsReserved(code):
		return errors.New("CodeNameReserved", "this code name is a reserved word of the command language", "entity", entity, "code_name", code)
	}
	return nil
}

// checkName verifies a display name: not blank and not longer than max.
func checkName(entity, name string, limit int) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("NameInvalid", "the name must not be empty", "entity", entity)
	}
	if utf8.RuneCountInString(name) > limit {
		return errors.New("NameInvalid", "the name is too long", "entity", entity, "limit", limit)
	}
	return nil
}

// checkHost verifies that a server address is an http or https URL with a host.
func checkHost(host string) error {
	if utf8.RuneCountInString(host) > maxHost {
		return errors.New("ServerHostInvalid", "the host has at most 100 characters", "host", host)
	}
	u, err := url.Parse(host)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("ServerHostInvalid", "the host must be an http or https address", "host", host)
	}
	return nil
}

// cleanTags lower-cases tags, checks each one (SPEC §3.4) and drops repeats.
func cleanTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.ToLower(t)
		if err := checkTag(t); err != nil {
			return nil, err
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

// checkTag verifies one lower-case tag: [a-z0-9_]+, not reserved, at most 50
// characters.
func checkTag(tag string) error {
	if tag == "" || utf8.RuneCountInString(tag) > maxTag || !isTagWord(tag) || command.IsReserved(tag) {
		return errors.New("TagInvalid", "a tag is a lower-case word of letters, digits and underscore, not a reserved word, at most 50 characters", "tag", tag)
	}
	return nil
}

func isTagWord(s string) bool {
	for _, r := range s {
		if r != '_' && (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}
