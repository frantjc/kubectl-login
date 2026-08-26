package kubectllogin

import (
	"encoding/json"

	"github.com/golang-jwt/jwt/v5"
)

type Token jwt.Token

var (
	_ json.Unmarshaler = new(Token)
)

// UnmarshalJSON implements [json.Unmarshaler].
func (t *Token) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	token, _, err := jwt.NewParser().ParseUnverified(s, jwt.MapClaims{})
	if err != nil {
		return err
	}

	*t = Token(*token)

	return nil
}

var (
	_ json.Marshaler = Token{}
)

// MarshalJSON implements [json.Marshaler].
func (t Token) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Raw)
}
