package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"step-webui/models"
	"step-webui/security"
)

// LoginOutcome is the result of one credential or MFA submission. The API
// layer maps it onto a status code, so this package never decides what HTTP
// shape a refusal takes.
type LoginOutcome int

// The values of LoginOutcome.
const (
	// LoginAuthenticated: a session was established.
	LoginAuthenticated LoginOutcome = iota
	// LoginMFARequired: credentials were right and a second factor is owed.
	LoginMFARequired
	// LoginInvalid: wrong credentials, or a wrong second factor.
	LoginInvalid
	// LoginBlocked: this attempt consumed the last of the address's allowance.
	LoginBlocked
	// LoginDisabled: right credentials on a deactivated account.
	LoginDisabled
	// LoginLocalDisabled: password login is switched off for the installation.
	LoginLocalDisabled
	// LoginNoPending: an MFA code arrived with no sign-in in progress.
	LoginNoPending
)

// LoginUser is the identity a successful login reports. It is deliberately not
// models.User, which carries the password hash and TOTP secret.
type LoginUser struct {
	ID       int
	Username string
	Role     string
}

// LoginResult carries an outcome plus the few facts a caller renders from it.
type LoginResult struct {
	Outcome      LoginOutcome
	User         *LoginUser
	AttemptsLeft int
	RetryAfter   time.Duration
}

func loginUser(u *models.User) *LoginUser {
	return &LoginUser{ID: u.ID, Username: u.Username, Role: u.Role}
}

// APILogin is the JSON-API form of LoginPost's credential half: same checks,
// same audit rows, no flash and no redirect.
//
// A parked second-factor state is dropped before the credentials are looked
// at, because LoginPost checks it first and a stale pending user would
// otherwise shadow a fresh submission (5.3).
func (h *Handler) APILogin(w http.ResponseWriter, r *http.Request, username, password string) LoginResult {
	ip := clientIP(r)
	if !h.cfg.LocalLoginEnabled {
		return LoginResult{Outcome: LoginLocalDisabled}
	}

	s := h.sess(r)
	delete(s.Values, "pending_2fa_user_id")
	delete(s.Values, "pending_2fa_expires")

	store := h.authStore()
	user, _ := store.UserByUsername(username)
	if user == nil || !security.VerifyPassword(password, user.PasswordHash) {
		security.RL.Register(ip)
		left := security.RL.Left(ip)
		_ = store.LogAuth(username, ip, false, fmt.Sprintf("Wrong password (%d attempts remaining)", left))
		_ = s.Save(r, w)
		if left == 0 {
			h.notifyAsync("auth-burst:"+ip+":"+time.Now().Format("2006-01-02T15:04"), "auth.failed_burst", "warn",
				"Failed login burst",
				fmt.Sprintf("IP %s blocked after repeated failed login attempts", ip),
				map[string]string{"username": username, "ip": ip})
			return LoginResult{Outcome: LoginBlocked, RetryAfter: security.RL.RetryAfter(ip)}
		}
		return LoginResult{Outcome: LoginInvalid, AttemptsLeft: left}
	}

	if !user.IsActive {
		_ = store.LogAuth(username, ip, false, "Account is disabled")
		_ = s.Save(r, w)
		return LoginResult{Outcome: LoginDisabled}
	}

	if security.NeedsPasswordRehash(user.PasswordHash) {
		_ = store.SetPasswordHash(user.ID, security.HashPassword(password))
	}

	if user.TOTPEnabled {
		s.Values["pending_2fa_user_id"] = user.ID
		s.Values["pending_2fa_expires"] = time.Now().Add(totpPendingTTL).Unix()
		if err := s.Save(r, w); err != nil {
			return LoginResult{Outcome: LoginInvalid}
		}
		return LoginResult{Outcome: LoginMFARequired}
	}

	h.completeLogin(w, r, user, "")
	return LoginResult{Outcome: LoginAuthenticated, User: loginUser(user)}
}

// APIVerifyMFA is the second half of APILogin. It verifies against the user
// parked in the session and never against one named in the request (5.3).
// recovery selects the recovery-code path instead of TOTP.
func (h *Handler) APIVerifyMFA(w http.ResponseWriter, r *http.Request, code string, recovery bool) LoginResult {
	ip := clientIP(r)
	uid := h.pending2FAUserID(r)
	if uid <= 0 {
		return LoginResult{Outcome: LoginNoPending}
	}

	store := h.authStore()
	user, _ := store.UserByID(uid)
	if user == nil || !user.IsActive || !user.TOTPEnabled {
		h.clearPending2FA(w, r)
		return LoginResult{Outcome: LoginNoPending}
	}

	code = strings.TrimSpace(code)
	var ok bool
	if recovery {
		ok = h.verifyRecoveryCode(user.ID, code)
	} else {
		ok = h.validateTOTPWithReplayCtx(r.Context(), user.ID, user.TOTPSecret, code)
	}
	if !ok {
		security.RL.Register(ip)
		left := security.RL.Left(ip)
		_ = store.LogAuth(user.Username, ip, false, fmt.Sprintf("Wrong 2FA code (%d attempts remaining)", left))
		if left == 0 {
			return LoginResult{Outcome: LoginBlocked, RetryAfter: security.RL.RetryAfter(ip)}
		}
		return LoginResult{Outcome: LoginInvalid, AttemptsLeft: left}
	}

	reason := ""
	if recovery {
		reason = "Login with recovery code"
	}
	h.completeLogin(w, r, user, reason)
	return LoginResult{Outcome: LoginAuthenticated, User: loginUser(user)}
}
