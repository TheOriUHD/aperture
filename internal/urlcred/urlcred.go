// Package urlcred keeps the login embedded in a camera link
// (rtsp://user:pass@host/…) on the server. Links leave the server with the login
// replaced by a fixed placeholder (Redact). A link that comes back still carrying
// the placeholder means "keep the stored login" and is filled in again (Merge), but
// only from a login stored for the same host, so a saved password can never be
// redirected to another machine by editing the address.
//
// Parsing mirrors net/url, which go2rtc uses to read these links (the authority
// ends at the first "/", "?" or "#", and the login at its last "@"), but it works on
// byte offsets instead of re-serialising: net/url would escape the "#" separators
// of go2rtc's link options (#video=copy#audio=copy) and break them.
package urlcred

import (
	"errors"
	"regexp"
	"strings"
)

// Mask stands in for a stored username or password. "•" is not a legal unescaped
// URL character, so a submitted link containing it can only be carrying the
// placeholder back, never a real credential (a real "•" arrives percent-encoded).
// Its length is fixed, so it says nothing about the real values either.
const Mask = "•••••"

// maskRune is the placeholder character; any occurrence marks placeholder text.
const maskRune = "•"

var (
	// ErrNewAddress: the link names a different host than any stored login was
	// saved for. Refilling it would send the saved password to that host.
	ErrNewAddress = errors.New("the camera's address changed, so enter its username and password again. The saved login is only ever sent to the address it was saved for")
	// ErrNothingStored: the placeholder stands in for a login that was never saved.
	ErrNothingStored = errors.New("there is no saved login for this link. Enter the username and password")
	// ErrEditedMask: the placeholder was partly overtyped, so it is neither the
	// saved value nor a complete new one.
	ErrEditedMask = errors.New("replace all of the dots with the new username or password, or leave them untouched to keep the saved one")
	// ErrMaskOutsideLogin: placeholder dots somewhere other than the login.
	ErrMaskOutsideLogin = errors.New("the link has placeholder dots outside the username and password")
)

// link is a URL split around its login, by byte offsets into the original text.
type link struct {
	prefix string // "scheme://"
	login  string // text before the authority's last "@"; "" when there is none
	hasAt  bool
	rest   string // host[:port] and everything after it, verbatim
}

func parse(raw string) (link, bool) {
	i := strings.Index(raw, "://")
	if i <= 0 {
		return link{}, false
	}
	prefix, after := raw[:i+3], raw[i+3:]
	end := len(after)
	if j := strings.IndexAny(after, "/?#"); j >= 0 {
		end = j
	}
	at := strings.LastIndex(after[:end], "@")
	if at < 0 {
		return link{prefix: prefix, rest: after}, true
	}
	return link{prefix: prefix, login: after[:at], hasAt: true, rest: after[at+1:]}, true
}

func (l link) String() string {
	if !l.hasAt {
		return l.prefix + l.rest
	}
	return l.prefix + l.login + "@" + l.rest
}

// host is the link's host name, lower-cased, without port or IPv6 brackets.
func (l link) host() string {
	hp := l.rest
	if j := strings.IndexAny(hp, "/?#"); j >= 0 {
		hp = hp[:j]
	}
	if strings.HasPrefix(hp, "[") {
		if k := strings.Index(hp, "]"); k > 0 {
			return strings.ToLower(hp[1:k])
		}
	}
	if k := strings.LastIndex(hp, ":"); k >= 0 {
		hp = hp[:k]
	}
	return strings.ToLower(hp)
}

func maskLogin(login string) string {
	if strings.Contains(login, ":") {
		return Mask + ":" + Mask
	}
	return Mask
}

// Redact returns raw with its login replaced by the placeholder: "•••••" for a
// bare username, "•••••:•••••" when a password is present. Anything that is not a
// link with a login is returned unchanged.
func Redact(raw string) string {
	l, ok := parse(raw)
	if !ok || l.login == "" {
		return raw
	}
	l.login = maskLogin(l.login)
	return l.String()
}

// HasMask reports whether s carries the placeholder anywhere.
func HasMask(s string) bool { return strings.Contains(s, maskRune) }

// Merge resolves a submitted link against the stored ones. A link without the
// placeholder is new and taken exactly as typed. Otherwise each half of its login
// that is still exactly Mask is refilled from the first stored link that has a
// login for the same host: normally the field's own saved value, else its sibling
// (a sub link pasted from the main one). The saved login is never refilled for a
// host it was not saved for.
func Merge(submitted string, stored ...string) (string, error) {
	if !HasMask(submitted) {
		return submitted, nil
	}
	in, ok := parse(submitted)
	if !ok || HasMask(in.prefix+in.rest) {
		return "", ErrMaskOutsideLogin
	}
	var src *link
	sawLogin := false
	for _, s := range stored {
		st, ok := parse(s)
		if !ok || st.login == "" {
			continue
		}
		sawLogin = true
		if st.host() == in.host() {
			src = &st
			break
		}
	}
	if src == nil {
		if sawLogin {
			return "", ErrNewAddress
		}
		return "", ErrNothingStored
	}
	user, pass, hasPass := strings.Cut(in.login, ":")
	oldUser, oldPass, oldHasPass := strings.Cut(src.login, ":")
	switch {
	case user == Mask:
		user = oldUser
	case HasMask(user):
		return "", ErrEditedMask
	}
	in.login = user
	if hasPass {
		switch {
		case pass == Mask && !oldHasPass:
			return "", ErrNothingStored
		case pass == Mask:
			pass = oldPass
		case HasMask(pass):
			return "", ErrEditedMask
		}
		in.login += ":" + pass
	}
	return in.String(), nil
}

// Follow carries a login change from the main link over to the sub link when the
// two shared the old login, so changing a camera's password in the main link does
// not silently break its sub stream. A sub link with its own, different login is
// left alone, and a login is only carried to a sub link on the new main link's host.
func Follow(sub, oldMain, newMain string) string {
	s, ok1 := parse(sub)
	om, ok2 := parse(oldMain)
	nm, ok3 := parse(newMain)
	if !ok1 || !ok2 || !ok3 || om.login == nm.login || s.login != om.login || s.host() != nm.host() {
		return sub
	}
	s.login, s.hasAt = nm.login, nm.hasAt
	return s.String()
}

// SameLogin reports whether two links carry the same login.
func SameLogin(a, b string) bool {
	la, _ := parse(a)
	lb, _ := parse(b)
	return la.login == lb.login
}

// loginInText finds "scheme://login@" anywhere in free text. The login may itself
// hold an unescaped "@" (net/url splits at the LAST one), so it matches greedily up
// to the last "@" before the host; it cannot cross whitespace or a path.
var loginInText = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)([^\s/?#"'<>]+)@`)

// Scrub masks the login of every link inside free text, such as an error message
// or a log line, so a failure report can never echo a password back.
func Scrub(text string) string {
	return loginInText.ReplaceAllStringFunc(text, func(m string) string {
		g := loginInText.FindStringSubmatch(m)
		return g[1] + maskLogin(g[2]) + "@"
	})
}
