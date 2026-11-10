// Package targets holds certharbor-delivery's compiled-in Target
// implementations (plans/distribution.md Section 7). This file implements
// the MVP's only target, aws-acm (Section 8).
package targets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"certharbor/delivery"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/acm/types"
)

// acmFingerprintTag is the tag key the push side writes and the check side
// reads back, since ACM's own GetCertificate response carries the
// certificate body (comparable by recomputing the SHA-256) but
// ListTagsForCertificate is the cheaper of the two "what did we last push"
// checks (Section 8: "GetCertificate (or ListTagsForCertificate, whichever
// carries a usable fingerprint/tag)"). ACM already recomputes equality
// server-side on ImportCertificate, so this tag is only ever used by
// Check to decide whether a push is worth attempting at all.
const acmFingerprintTag = "certharbor-delivery:fingerprint"

// acmParams is the Params shape for an aws-acm delivery_target row
// (region, static tags). No secrets: credentials come from the standard
// AWS SDK chain, never from this JSON (Section 8).
type acmParams struct {
	Region string            `json:"region"`
	Tags   map[string]string `json:"tags,omitempty"`
}

// acmBindingParams is the Params shape for a binding's target_params
// override: holds the stable ARN once known (Section 8).
type acmBindingParams struct {
	CertificateArn string `json:"arn,omitempty"`
}

// API is the subset of the ACM SDK client this target calls, narrowed so
// tests can supply a fake instead of talking to real AWS (Section 8's
// Push/Check surface: ImportCertificate, GetCertificate,
// ListTagsForCertificate, AddTagsToCertificate).
type API interface {
	ImportCertificate(ctx context.Context, in *acm.ImportCertificateInput, optFns ...func(*acm.Options)) (*acm.ImportCertificateOutput, error)
	GetCertificate(ctx context.Context, in *acm.GetCertificateInput, optFns ...func(*acm.Options)) (*acm.GetCertificateOutput, error)
	ListTagsForCertificate(ctx context.Context, in *acm.ListTagsForCertificateInput, optFns ...func(*acm.Options)) (*acm.ListTagsForCertificateOutput, error)
	AddTagsToCertificate(ctx context.Context, in *acm.AddTagsToCertificateInput, optFns ...func(*acm.Options)) (*acm.AddTagsToCertificateOutput, error)
}

// ACMTarget implements delivery.Target for AWS Certificate Manager.
type ACMTarget struct {
	// newClient builds (or returns a cached) API for a given region.
	// Credentials are resolved by the standard AWS SDK credential chain
	// (env/role/profile) inside config.LoadDefaultConfig; this plane never
	// hardcodes or prompts for them (Section 8).
	newClient func(ctx context.Context, region string) (API, error)
}

// NewACMTarget constructs the aws-acm target against the real AWS SDK.
func NewACMTarget() *ACMTarget {
	return &ACMTarget{newClient: newLiveACMClient}
}

// NewACMTargetWithClient is the test seam: callers supply a fixed API
// (region argument ignored), so unit tests never dial real AWS.
func NewACMTargetWithClient(c API) *ACMTarget {
	return &ACMTarget{newClient: func(context.Context, string) (API, error) { return c, nil }}
}

func newLiveACMClient(ctx context.Context, region string) (API, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return acm.NewFromConfig(cfg), nil
}

// Name implements delivery.Target.
func (t *ACMTarget) Name() string { return "aws-acm" }

// Check implements delivery.Target. It reports current=true only when the
// target's last-known fingerprint tag matches. Any error reaching ACM, or
// the absence of a prior push, means a push is required (Section 7: "a
// target that cannot check the current state always returns
// pushRequired=true").
func (t *ACMTarget) Check(ctx context.Context, fingerprint string, rawParams json.RawMessage) (bool, error) {
	var tp acmBindingParams
	if err := json.Unmarshal(rawParams, &tp); err != nil || tp.CertificateArn == "" {
		return false, nil // nothing imported yet
	}
	var p acmParams
	_ = json.Unmarshal(rawParams, &p) // best-effort; region may come from the target row instead, see Push

	client, err := t.newClient(ctx, p.Region)
	if err != nil {
		return false, nil //nolint:nilerr // Check degrades to pushRequired=true per the interface contract
	}

	out, err := client.ListTagsForCertificate(ctx, &acm.ListTagsForCertificateInput{CertificateArn: aws.String(tp.CertificateArn)})
	if err != nil {
		return false, nil //nolint:nilerr // Check degrades to pushRequired=true per the interface contract
	}
	for _, tag := range out.Tags {
		if tag.Key != nil && *tag.Key == acmFingerprintTag && tag.Value != nil {
			return *tag.Value == fingerprint, nil
		}
	}
	return false, nil
}

// Push implements delivery.Target. On first push it calls ImportCertificate
// with no CertificateArn (ACM allocates one); on subsequent pushes it
// passes the previously recorded ARN so ACM reuses the same ARN (Section
// 8: "every subsequent push for that binding re-imports to the same ARN").
// The private key necessarily crosses the wire to AWS here: ACM's Import
// API requires it as a parameter (Section 8, documented not hidden).
func (t *ACMTarget) Push(ctx context.Context, material *delivery.CertMaterial, rawParams json.RawMessage) (delivery.PushResult, error) {
	var p acmParams
	if err := json.Unmarshal(rawParams, &p); err != nil {
		return delivery.PushResult{}, fmt.Errorf("aws-acm: invalid params: %w", err)
	}
	var bp acmBindingParams
	_ = json.Unmarshal(rawParams, &bp)

	if len(material.KeyPEM) == 0 {
		return delivery.PushResult{}, fmt.Errorf("aws-acm: binding has no private key material to import")
	}

	client, err := t.newClient(ctx, p.Region)
	if err != nil {
		return delivery.PushResult{}, fmt.Errorf("aws-acm: building client: %w", err)
	}

	in := &acm.ImportCertificateInput{
		Certificate:      material.CertPEM,
		PrivateKey:       material.KeyPEM,
		CertificateChain: material.ChainPEM,
	}
	if bp.CertificateArn != "" {
		in.CertificateArn = aws.String(bp.CertificateArn)
	}

	out, err := client.ImportCertificate(ctx, in)
	if err != nil {
		return delivery.PushResult{}, fmt.Errorf("aws-acm: ImportCertificate: %w", err)
	}
	if out.CertificateArn == nil {
		return delivery.PushResult{}, fmt.Errorf("aws-acm: ImportCertificate returned no ARN")
	}
	arn := *out.CertificateArn

	tags := []types.Tag{{Key: aws.String(acmFingerprintTag), Value: aws.String(material.Fingerprint)}}
	for k, v := range p.Tags {
		tags = append(tags, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	if _, err := client.AddTagsToCertificate(ctx, &acm.AddTagsToCertificateInput{CertificateArn: aws.String(arn), Tags: tags}); err != nil {
		// Import already succeeded; a tagging failure shouldn't fail the
		// push, only degrade the next Check to pushRequired=true.
		return delivery.PushResult{UpdatedParams: mergeArn(rawParams, arn)}, nil
	}

	return delivery.PushResult{UpdatedParams: mergeArn(rawParams, arn)}, nil
}

// mergeArn returns rawParams with "arn" set/overwritten, preserving every
// other key the binding's target_params already carried.
func mergeArn(rawParams json.RawMessage, arn string) json.RawMessage {
	m := map[string]any{}
	if len(rawParams) > 0 {
		_ = json.Unmarshal(rawParams, &m)
	}
	m["arn"] = arn
	out, err := json.Marshal(m)
	if err != nil {
		return rawParams
	}
	return out
}

// fingerprintOf computes the SHA-256 of PEM bytes; exported for tests that
// construct expected tag values without importing the delivery package's
// unexported helper.
func fingerprintOf(pem []byte) string {
	sum := sha256.Sum256(pem)
	return hex.EncodeToString(sum[:])
}
