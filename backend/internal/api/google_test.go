package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestGoogleSignInNewUserEndToEnd(t *testing.T) {
	h, _, _ := newServer(t)

	// Without the consent a new account needs: 422, nothing created.
	rec := post(h, "/v1/auth/google", `{"id_token":"token-for-abebe"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("without consent: %d %s", rec.Code, rec.Body)
	}

	rec = post(h, "/v1/auth/google", `{"id_token":"token-for-abebe","is_adult":true,"accept_terms":true,"locale":"am"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Created bool `json:"created"`
		User    struct {
			Username      string `json:"username"`
			EmailVerified bool   `json:"email_verified"`
		} `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.Created || body.User.Username != "abebe" || !body.User.EmailVerified {
		t.Errorf("body = %s", rec.Body)
	}
	if rec := me(h, tokensFrom(t, rec).AccessToken); rec.Code != http.StatusOK {
		t.Errorf("/v1/me: %d %s", rec.Code, rec.Body)
	}

	// The account has no password: password login fails like any other.
	if rec := post(h, "/v1/auth/login", `{"identifier":"abebe@gmail.com","password":"anything at all"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("password login to a Google-only account: %d %s", rec.Code, rec.Body)
	}
	// Signing in again logs in to the same account.
	rec = post(h, "/v1/auth/google", `{"id_token":"token-for-abebe"}`)
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Created {
		t.Errorf("second sign-in: %d %s", rec.Code, rec.Body)
	}
}

// Someone signed up with Kidist's Gmail address and a password of their own,
// and never verified it. Kidist's Google sign-in takes the account over: the
// password and the sessions it opened stop working.
func TestGoogleSignInExistingEmailEndToEnd(t *testing.T) {
	h, _, _ := newServer(t)
	if rec := post(h, "/v1/auth/signup", signupJSON("kidista", "kidist@gmail.com")); rec.Code != http.StatusAccepted {
		t.Fatalf("signup: %d %s", rec.Code, rec.Body)
	}
	login := post(h, "/v1/auth/login", `{"identifier":"kidista","password":"correct horse battery"}`)
	squatter := tokensFrom(t, login)

	rec := post(h, "/v1/auth/google", `{"id_token":"token-for-kidist"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("Google sign-in: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Created bool `json:"created"`
		User    struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Created || body.User.Username != "kidista" {
		t.Errorf("want the existing account: %s", rec.Body)
	}
	if rec := refresh(h, squatter.RefreshToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("the squatter's session still refreshes: %d %s", rec.Code, rec.Body)
	}
	if rec := post(h, "/v1/auth/login", `{"identifier":"kidista","password":"correct horse battery"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("the squatter's password still works: %d %s", rec.Code, rec.Body)
	}
}

func TestGoogleSignInWrongAudienceEndToEnd(t *testing.T) {
	h, pool, _ := newServer(t)
	rec := post(h, "/v1/auth/google", `{"id_token":"token-for-another-app","is_adult":true,"accept_terms":true}`)
	if rec.Code != http.StatusUnauthorized || code(t, rec) != "INVALID_GOOGLE_TOKEN" {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM users").Scan(&n); err != nil || n != 0 {
		t.Errorf("users = %d (%v), want 0", n, err)
	}
}
