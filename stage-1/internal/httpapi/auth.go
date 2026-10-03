package httpapi

import (
	"io"
	"net/http"
	"strings"

	"tablekeeper/internal/idgen"
	"tablekeeper/internal/store"
)

type signupResponse struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Token       string `json:"token"`
}

func (a *API) handleSignup(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	raw, err := decodeBody(body)
	if err != nil {
		writeError(w, err)
		return
	}
	email, _, eerr := fieldString(raw, "email")
	if eerr != nil {
		writeError(w, eerr)
		return
	}
	password, _, perr := fieldString(raw, "password")
	if perr != nil {
		writeError(w, perr)
		return
	}
	displayName, _, derr := fieldString(raw, "display_name")
	if derr != nil {
		writeError(w, derr)
		return
	}

	if email == "" || !validEmail(email) {
		writeError(w, errValidationFailed("email must be of the form local@domain"))
		return
	}
	if len(password) < 8 {
		writeError(w, errValidationFailed("password must be at least 8 characters"))
		return
	}

	a.Store.Lock()
	defer a.Store.Unlock()

	if _, exists := a.Store.UserByEmail(email); exists {
		writeError(w, errEmailTaken)
		return
	}

	hash, hashErr := store.HashPassword(password)
	if hashErr != nil {
		writeError(w, errValidationFailed("password could not be hashed"))
		return
	}
	user := &store.User{
		ID:           idgen.New("u"),
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
	}
	a.Store.AddUser(user)
	token := idgen.Token()
	a.Store.AddToken(token, user.ID)

	writeJSON(w, http.StatusCreated, signupResponse{UserID: user.ID, DisplayName: user.DisplayName, Token: token})
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	raw, err := decodeBody(body)
	if err != nil {
		writeError(w, err)
		return
	}
	email, _, eerr := fieldString(raw, "email")
	if eerr != nil {
		writeError(w, eerr)
		return
	}
	password, _, perr := fieldString(raw, "password")
	if perr != nil {
		writeError(w, perr)
		return
	}

	a.Store.Lock()
	defer a.Store.Unlock()

	user, exists := a.Store.UserByEmail(email)
	if !exists {
		writeError(w, errUnauthenticated)
		return
	}
	if !store.CheckPassword(user.PasswordHash, password) {
		writeError(w, errUnauthenticated)
		return
	}

	token := idgen.Token()
	a.Store.AddToken(token, user.ID)
	writeJSON(w, http.StatusOK, signupResponse{UserID: user.ID, DisplayName: user.DisplayName, Token: token})
}

// authenticate resolves the bearer token for the request. Caller must hold a.Store lock is
// NOT required for the lookup itself (UserByToken is cheap) but callers typically hold the
// lock for the whole handler anyway for atomicity with the rest of the operation.
func (a *API) authenticate(r *http.Request) (*store.User, *apiError) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return nil, errUnauthenticated
	}
	token := strings.TrimPrefix(h, prefix)
	if token == "" {
		return nil, errUnauthenticated
	}
	user, ok := a.Store.UserByToken(token)
	if !ok {
		return nil, errUnauthenticated
	}
	return user, nil
}
