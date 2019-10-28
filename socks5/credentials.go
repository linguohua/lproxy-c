package socks5

// credit: https://github.com/armon/go-socks5

// CredentialStore is used to support user/pass authentication
type CredentialStore interface {
	valid(user, password string) bool
}

// StaticCredentials enables using a map directly as a credential store
type StaticCredentials map[string]string

func (s StaticCredentials) valid(user, password string) bool {
	pass, ok := s[user]
	if !ok {
		return false
	}
	return password == pass
}
