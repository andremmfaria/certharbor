package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	appdb "certharbor/db"
)

// CAInfo is the data GET /api/v1/ca composes (plans/frontend-backend-split.md
// Section 6: "subject, fingerprint, validity, reachability"). It is read off
// the root CA certificate the same way health.go's own chain check already
// parses it (readPEMCert), plus a live reachability probe through the same
// CA client Home's caOnline flag uses, so this and the dashboard's CA status
// (D10 rule 1) can never disagree about what "reachable" means.
type CAInfo struct {
	Subject           string
	FingerprintSHA256 string
	NotBefore         time.Time
	NotAfter          time.Time
	Reachable         bool
}

// FetchCAInfo implements the data half of GET /api/v1/ca. A root cert that
// fails to parse is a genuine error (the installation is broken, not merely
// unreachable), so it is returned rather than folded into Reachable=false.
func (h *Handler) FetchCAInfo(ctx context.Context) (*CAInfo, error) {
	root, err := readPEMCert(h.cfg.RootCert)
	if err != nil {
		return nil, err
	}
	_, sha256fp := certFingerprints(root)

	reachable := true
	caClient, caErr := h.caClient()
	if caErr != nil || caClient.Health(ctx) != nil {
		reachable = false
	}

	return &CAInfo{
		Subject:           root.Subject.String(),
		FingerprintSHA256: sha256fp,
		NotBefore:         root.NotBefore,
		NotAfter:          root.NotAfter,
		Reachable:         reachable,
	}, nil
}

// RootCABytes returns the root CA certificate's raw PEM bytes, for
// GET /api/v1/ca/root (replacing GET /download/ca). Mirrors
// serveCAFile's not-found semantics (a missing file is reported to the
// caller as a plain error, mapped to 404 by the api layer) without writing
// directly to an http.ResponseWriter.
func (h *Handler) RootCABytes() ([]byte, error) {
	return readCAFileBytes(h.cfg.RootCert)
}

// IntermediateCABytes is RootCABytes' intermediate-certificate equivalent,
// for GET /api/v1/ca/intermediate (replacing GET /download/intermediate-ca).
func (h *Handler) IntermediateCABytes() ([]byte, error) {
	return readCAFileBytes(h.intermediateCertPath())
}

// FullChainBytes assembles the intermediate followed by the root, exactly as
// DownloadFullChain already does, for GET /api/v1/ca/chain (replacing
// GET /download/full-chain).
func (h *Handler) FullChainBytes() ([]byte, error) {
	intermediate, err := readCAFileBytes(h.intermediateCertPath())
	if err != nil {
		return nil, err
	}
	root, err := readCAFileBytes(h.cfg.RootCert)
	if err != nil {
		return nil, err
	}

	var chain bytes.Buffer
	chain.Write(intermediate)
	if len(intermediate) > 0 && intermediate[len(intermediate)-1] != '\n' {
		chain.WriteByte('\n')
	}
	chain.Write(root)
	return chain.Bytes(), nil
}

// readCAFileBytes is serveCAFile's data-only half: stat then read, so the
// api layer can distinguish "file missing" (404) from any other read
// failure without parsing error strings.
func readCAFileBytes(path string) ([]byte, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil //nolint:nilnil // a missing CA file is "not found", not an error the caller should wrap
	}
	//nolint:gosec // G304: path is always a config-derived CA certificate path (RootCert, intermediateCertPath())
	return os.ReadFile(path)
}

// CertificateCertFile returns the filename and raw bytes of a managed
// certificate's certificate file, for GET /api/v1/certificates/{id}/certificate
// (replacing GET /download/cert/{id}). A nil summary with no error signals
// "no such certificate" to the caller, the same convention
// FetchCertificateDetail already uses.
func (h *Handler) CertificateCertFile(id int) (filename string, data []byte, err error) {
	c, err := appdb.GetCert(h.db, id)
	if err != nil {
		return "", nil, err
	}
	if c == nil || c.CertPath == "" {
		return "", nil, nil
	}
	//nolint:gosec // G304: CertPath is a DB-stored managed certificate path, not user input
	data, err = os.ReadFile(c.CertPath)
	if err != nil {
		return "", nil, err
	}
	fn := "certificate.crt"
	if safe, nameErr := safeName(c.Name); nameErr == nil {
		fn = safe + ".crt"
	}
	return fn, data, nil
}

// CertificateKeyFile is CertificateCertFile's private-key equivalent, for
// GET /api/v1/certificates/{id}/key (replacing GET /download/key/{id}).
// DownloadKey's audit side effect (a key download is always logged) is the
// caller's responsibility here: this method only reads data, since logging
// needs the *http.Request the api layer, not this one, has access to.
func (h *Handler) CertificateKeyFile(id int) (filename string, data []byte, err error) {
	c, err := appdb.GetCert(h.db, id)
	if err != nil {
		return "", nil, err
	}
	if c == nil || c.KeyPath == "" {
		return "", nil, nil
	}
	//nolint:gosec // G304: KeyPath is a DB-stored managed certificate path, not user input
	data, err = os.ReadFile(c.KeyPath)
	if err != nil {
		return "", nil, err
	}
	fn := "private.key"
	if safe, nameErr := safeName(c.Name); nameErr == nil {
		fn = safe + ".key"
	}
	return fn, data, nil
}

// CertificateSummaryForAudit returns just enough of a certificate's
// identity (id, name, domain) for the api layer's key-download audit log
// line. It is FetchCertificateDetail without the x509 parse and validation
// work the audit line does not need. Returns nil, nil when the certificate
// does not exist.
func (h *Handler) CertificateSummaryForAudit(id int) (*CertificateSummary, error) {
	c, err := appdb.GetCert(h.db, id)
	if err != nil || c == nil {
		return nil, err
	}
	summary := summarizeCert(c)
	return &summary, nil
}

// AuditKeyDownload is DownloadKey's h.auditSecurity call site, adapted so
// the api layer's getCertificateKey can log the same line a key download
// through the retired template route would. It cannot reuse h.auditSecurity
// directly: that reads the username off the raw gorilla session
// (h.sessionInfo(r)), which the huma session middleware never populates
// with "username" (only "user_id"), unlike the template auth flow. The
// api layer already has the authenticated username from context via
// userIdentity/appmw.UserFrom, so it is passed in explicitly instead.
func (h *Handler) AuditKeyDownload(r *http.Request, username string, id int, name, domain string) {
	if r == nil || username == "" {
		return
	}
	_ = appdb.LogAuth(h.db, username, clientIP(r), true,
		auditPrefix+fmt.Sprintf("certificate.key_download id=%d name=%s domain=%s", id, name, domain))
}
