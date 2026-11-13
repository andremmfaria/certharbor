package handlers

import (
	"net/http"
	"strconv"
	"time"

	"certharbor/middleware"
	"certharbor/models"
	"certharbor/security"
)

// clientIP returns the host portion of r.RemoteAddr, stripping the ephemeral
// port so that all connections from the same client count under one rate-limit
// key regardless of TCP connection cycling.
// When TrustProxy=true the middleware.RealIP middleware may have normalised
// RemoteAddr to a bare IP, so SplitHostPort returns an error and we fall back
// to the raw value, both cases produce the correct host-only string.
func clientIP(r *http.Request) string {
	return middleware.ClientIP(r)
}

func (h *Handler) pending2FAUserID(r *http.Request) int {
	s := h.sess(r)
	rawID, ok := s.Values["pending_2fa_user_id"].(int)
	if !ok || rawID <= 0 {
		return 0
	}
	exp, _ := s.Values["pending_2fa_expires"].(int64)
	if exp == 0 {
		if expStr, ok := s.Values["pending_2fa_expires"].(string); ok {
			exp, _ = strconv.ParseInt(expStr, 10, 64)
		}
	}
	if exp > 0 && time.Now().Unix() > exp {
		return 0
	}
	return rawID
}

func (h *Handler) clearPending2FA(w http.ResponseWriter, r *http.Request) {
	s := h.sess(r)
	delete(s.Values, "pending_2fa_user_id")
	delete(s.Values, "pending_2fa_expires")
	_ = s.Save(r, w)
}

func (h *Handler) completeLogin(w http.ResponseWriter, r *http.Request, user *models.User, reason string) {
	security.RL.Clear(clientIP(r))
	s := h.sess(r)
	s.Values = map[interface{}]interface{}{}
	s.Values["user_id"] = user.ID
	s.Values["username"] = user.Username
	s.Values["role"] = user.Role
	s.Values["session_epoch"] = user.SessionEpoch
	s.Values["session_created_at"] = time.Now().Unix()
	s.Values["last_activity"] = time.Now().Unix()
	// One value, both halves of the pair, in one response (5.4). Wiping
	// s.Values above and minting a fresh token here is what closes post-auth
	// session fixation, because with a cookie store the cookie is the session.
	csrfToken := security.GenerateToken()
	s.Values["csrf_token"] = csrfToken
	_ = s.Save(r, w)
	h.SetCSRFCookie(w, csrfToken)
	_ = h.authStore().LogAuth(user.Username, clientIP(r), true, reason)
}
