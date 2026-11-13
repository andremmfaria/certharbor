package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "certharbor/db"
	"certharbor/handlers"
	appmw "certharbor/middleware"
	"certharbor/security"

	"github.com/gorilla/sessions"
)

// SessionState is GET /api/v1/session's required discriminator (5.3): the
// endpoint answers 200 in all three cases and never 401.
type SessionState string

// The three values of SessionState.
const (
	SessionAnonymous     SessionState = "anonymous"
	SessionPendingMFA    SessionState = "pendingMfa"
	SessionAuthenticated SessionState = "authenticated"
)

type sessionUserBody struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type sessionBody struct {
	State SessionState     `json:"state" enum:"anonymous,pendingMfa,authenticated"`
	User  *sessionUserBody `json:"user,omitempty"`
}

type sessionOutput struct {
	Body sessionBody
}

// getSession implements GET /api/v1/session. It is the one auth: optional
// operation (5.5): the chain validates a present session and never answers
// 401, so this handler only reports the state and keeps the CSRF pair fresh.
func getSession(h *handlers.Handler) func(context.Context, *struct{}) (*sessionOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*sessionOutput, error) {
		r, w := httpFrom(ctx)

		s, err := h.Store().Get(r, h.SessionCookieName())
		if err != nil {
			s, _ = h.Store().New(r, h.SessionCookieName()) //nolint:errcheck // New only errors on codec setup, never on a bad cookie
		}

		out := &sessionOutput{}
		switch {
		case isPending2FA(s):
			out.Body.State = SessionPendingMFA
		default:
			// sessionMiddleware ran with auth: optional, so it validated a
			// session if one was present and put the user on the context
			// without ever answering 401 (5.5). Nothing is re-checked here.
			if user, ok := appmw.UserFrom(ctx); ok {
				out.Body.State = SessionAuthenticated
				out.Body.User = &sessionUserBody{ID: user.ID, Username: user.Username, Role: user.Role}
			} else {
				out.Body.State = SessionAnonymous
			}
		}

		token, _ := s.Values["csrf_token"].(string)
		if token == "" {
			token = security.GenerateToken()
			s.Values["csrf_token"] = token
		}
		// The write this comment is about: persisting the session from inside
		// a huma operation handler, and the cookie carrying it back to the
		// client on this same response (D2 cost 2's go/no-go proof).
		if err := s.Save(r, w); err != nil {
			return nil, err
		}
		h.SetCSRFCookie(w, token)

		return out, nil
	}
}

// isPending2FA mirrors handlers.Handler.pending2FAUserID's expiry check
// without exporting that unexported method.
func isPending2FA(s *sessions.Session) bool {
	uid, ok := s.Values["pending_2fa_user_id"].(int)
	if !ok || uid <= 0 {
		return false
	}
	exp, _ := s.Values["pending_2fa_expires"].(int64)
	return exp == 0 || time.Now().Unix() <= exp
}

// deleteSessionOutput carries no body: the response is a 204.
type deleteSessionOutput struct {
	Status int
}

// deleteSession implements DELETE /api/v1/session. It expires both halves of
// the pair (5.3): leaving the readable CSRF cookie behind would have the SPA
// echo a token for a session that no longer exists, which reads as a CSRF
// failure rather than as a logged-out state.
//
// It is auth: public because logging out must work from a session the server
// has already rejected. There is nothing to authorise: the only effect is
// expiring the caller's own cookies.
func deleteSession(h *handlers.Handler) func(context.Context, *struct{}) (*deleteSessionOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*deleteSessionOutput, error) {
		r, w := httpFrom(ctx)

		if s, err := h.Store().Get(r, h.SessionCookieName()); err == nil {
			// Clearing the cookie here only stops *this* client from
			// presenting it again; a copy captured before logout (proxy
			// log, XSS, shared terminal) is a separate string the server
			// never sees, so it keeps validating under ValidateSession
			// (middleware/session.go) until SessionMaxLifetime/
			// SessionTimeout elapse on their own. session_epoch is "the
			// only real revocation available to a client-side cookie
			// store" (plans/frontend-backend-split.md 5.3) and until now
			// logout never bumped it — a security regression this fixes.
			// Bumping revokes every session for this user, not just this
			// one; that coarseness is inherent to a pure cookie store with
			// no server-side session table. h.DB() is nil in some chain
			// tests that stand up the middleware without a database
			// (api/mount_test.go), the same condition handlers.go's own
			// db-optional call sites (acme_api.go, notifications.go,
			// health.go) already guard against.
			if uid, ok := s.Values["user_id"].(int); ok && uid > 0 && h.DB() != nil {
				if err := appdb.BumpSessionEpoch(h.DB(), uid); err != nil {
					return nil, err
				}
			}
			s.Options.MaxAge = -1
			s.Values = map[interface{}]interface{}{}
			if err := s.Save(r, w); err != nil {
				return nil, err
			}
		} else {
			// An undecodable cookie still has to be cleared, or the caller is
			// stuck sending a cookie the server rejects on every request.
			http.SetCookie(w, expiredCookie(h.SessionCookieName(), h.Store().Options.Secure, true))
		}
		http.SetCookie(w, expiredCookie(h.CSRFCookieName(), h.Store().Options.Secure, false))

		return &deleteSessionOutput{Status: http.StatusNoContent}, nil
	}
}

// expiredCookie builds the deletion form of a cookie. The attributes must
// match the ones it was set with or the browser keeps the original, and with
// the __Host- prefix Path=/ and Secure are required for the delete to be
// accepted at all.
func expiredCookie(name string, secure, httpOnly bool) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // G124: Secure mirrors the store (SESSION_SECURE), a runtime value
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		HttpOnly: httpOnly,
	}
}

type createSessionBody struct {
	Username string `json:"username" minLength:"1" maxLength:"255" doc:"Account username."`
	Password string `json:"password" minLength:"1" maxLength:"1024" writeOnly:"true" doc:"Account password."`
}

type createSessionInput struct {
	Body createSessionBody
}

// createSession implements POST /api/v1/session: the credential half of
// today's POST /login. It answers with the same discriminated Session
// getSession does, so the SPA has one shape to branch on, and returns
// pendingMfa rather than an error when a second factor is owed.
func createSession(h *handlers.Handler) func(context.Context, *createSessionInput) (*sessionOutput, error) {
	return func(ctx context.Context, in *createSessionInput) (*sessionOutput, error) {
		r, w := httpFrom(ctx)
		res := h.APILogin(w, r, strings.TrimSpace(in.Body.Username), in.Body.Password)
		return loginResponse(w, r, res)
	}
}

type submitMFABody struct {
	Code     string `json:"code" minLength:"1" maxLength:"64" doc:"The authenticator code, or a recovery code."`
	CodeType string `json:"codeType" enum:"totp,recovery" doc:"Whether code is an authenticator code or a recovery code."`
}

type submitMFAInput struct {
	Body submitMFABody
}

// submitMfa implements POST /api/v1/session/mfa. It verifies against the user
// parked in the session by createSession and never against one named in the
// request (5.3).
func submitMfa(h *handlers.Handler) func(context.Context, *submitMFAInput) (*sessionOutput, error) {
	return func(ctx context.Context, in *submitMFAInput) (*sessionOutput, error) {
		r, w := httpFrom(ctx)
		res := h.APIVerifyMFA(w, r, in.Body.Code, in.Body.CodeType == "recovery")
		return loginResponse(w, r, res)
	}
}

// loginResponse maps a LoginResult onto the problem contract (5.2). Every
// credential refusal is 401 and deliberately non-specific, matching today's
// refusal to tell an unknown user from a wrong password.
func loginResponse(w http.ResponseWriter, r *http.Request, res handlers.LoginResult) (*sessionOutput, error) {
	switch res.Outcome {
	case handlers.LoginAuthenticated:
		return &sessionOutput{Body: sessionBody{
			State: SessionAuthenticated,
			User:  &sessionUserBody{ID: res.User.ID, Username: res.User.Username, Role: res.User.Role},
		}}, nil
	case handlers.LoginMFARequired:
		return &sessionOutput{Body: sessionBody{State: SessionPendingMFA}}, nil
	case handlers.LoginBlocked:
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
		return nil, &rateLimitError{
			ErrorModel:   newProblem(http.StatusTooManyRequests, "Too Many Requests", problemTypeBase+"rate-limit", security.LockoutMessage(), r.URL.Path),
			AttemptsLeft: 0,
		}
	case handlers.LoginDisabled:
		return nil, newProblem(http.StatusUnauthorized, "Unauthorized", "", "Account is disabled. Please contact your administrator.", r.URL.Path)
	case handlers.LoginLocalDisabled:
		return nil, newProblem(http.StatusForbidden, "Forbidden", problemTypeBase+"local-login-disabled", "Local login is disabled.", r.URL.Path)
	case handlers.LoginNoPending:
		return nil, newProblem(http.StatusUnauthorized, "Unauthorized", problemTypeBase+"mfa-expired", "No sign-in is waiting for a code. Start again.", r.URL.Path)
	default:
		return nil, newProblem(http.StatusUnauthorized, "Unauthorized", "",
			fmt.Sprintf("Invalid credentials. Attempts remaining: %d", res.AttemptsLeft), r.URL.Path)
	}
}
