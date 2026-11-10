package targets

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"certharbor/delivery"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/acm/types"
)

// fakeACMAPI implements API without ever dialing AWS.
type fakeACMAPI struct {
	importCalls int
	lastImport  *acm.ImportCertificateInput
	importArn   string
	importErr   error

	tags map[string]string

	listTagsErr error
	addTagsErr  error
}

func (f *fakeACMAPI) ImportCertificate(_ context.Context, in *acm.ImportCertificateInput, _ ...func(*acm.Options)) (*acm.ImportCertificateOutput, error) {
	f.importCalls++
	f.lastImport = in
	if f.importErr != nil {
		return nil, f.importErr
	}
	arn := f.importArn
	if in.CertificateArn != nil {
		arn = *in.CertificateArn // reusing the existing ARN, per Section 8
	}
	return &acm.ImportCertificateOutput{CertificateArn: aws.String(arn)}, nil
}

func (f *fakeACMAPI) GetCertificate(context.Context, *acm.GetCertificateInput, ...func(*acm.Options)) (*acm.GetCertificateOutput, error) {
	return &acm.GetCertificateOutput{}, nil
}

func (f *fakeACMAPI) ListTagsForCertificate(_ context.Context, _ *acm.ListTagsForCertificateInput, _ ...func(*acm.Options)) (*acm.ListTagsForCertificateOutput, error) {
	if f.listTagsErr != nil {
		return nil, f.listTagsErr
	}
	var tags []types.Tag
	for k, v := range f.tags {
		tags = append(tags, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return &acm.ListTagsForCertificateOutput{Tags: tags}, nil
}

func (f *fakeACMAPI) AddTagsToCertificate(_ context.Context, in *acm.AddTagsToCertificateInput, _ ...func(*acm.Options)) (*acm.AddTagsToCertificateOutput, error) {
	if f.addTagsErr != nil {
		return nil, f.addTagsErr
	}
	if f.tags == nil {
		f.tags = map[string]string{}
	}
	for _, tag := range in.Tags {
		f.tags[*tag.Key] = *tag.Value
	}
	return &acm.AddTagsToCertificateOutput{}, nil
}

func TestACMTarget_Name(t *testing.T) {
	if got := NewACMTarget().Name(); got != "aws-acm" {
		t.Errorf("Name() = %q, want aws-acm", got)
	}
}

func TestACMTarget_Push_FirstImport_AllocatesNewARN(t *testing.T) {
	fake := &fakeACMAPI{importArn: "arn:aws:acm:us-east-1:111111111111:certificate/new"}
	target := NewACMTargetWithClient(fake)

	material := delivery.CertMaterial{
		CertPEM:     []byte("cert-pem"),
		ChainPEM:    []byte("chain-pem"),
		KeyPEM:      []byte("key-pem"),
		Fingerprint: fingerprintOf([]byte("cert-pem")),
	}
	params := json.RawMessage(`{"region":"us-east-1"}`)

	result, err := target.Push(context.Background(), &material, params)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if fake.importCalls != 1 {
		t.Fatalf("expected 1 ImportCertificate call, got %d", fake.importCalls)
	}
	if fake.lastImport.CertificateArn != nil {
		t.Error("expected no CertificateArn on first import (ACM allocates one)")
	}

	var updated acmBindingParams
	if err := json.Unmarshal(result.UpdatedParams, &updated); err != nil {
		t.Fatalf("unmarshaling UpdatedParams: %v", err)
	}
	if updated.CertificateArn != "arn:aws:acm:us-east-1:111111111111:certificate/new" {
		t.Errorf("UpdatedParams.arn = %q, want the newly allocated ARN", updated.CertificateArn)
	}
	if fake.tags[acmFingerprintTag] != material.Fingerprint {
		t.Error("expected the fingerprint tag to be written after a successful import")
	}
}

func TestACMTarget_Push_SubsequentPush_ReusesSameARN(t *testing.T) {
	const existingArn = "arn:aws:acm:us-east-1:111111111111:certificate/existing"
	fake := &fakeACMAPI{}
	target := NewACMTargetWithClient(fake)

	material := delivery.CertMaterial{CertPEM: []byte("renewed-cert"), KeyPEM: []byte("key"), Fingerprint: "fp2"}
	params := json.RawMessage(`{"region":"us-east-1","arn":"` + existingArn + `"}`)

	result, err := target.Push(context.Background(), &material, params)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if fake.lastImport.CertificateArn == nil || *fake.lastImport.CertificateArn != existingArn {
		t.Errorf("expected ImportCertificate to be called with the existing ARN %q", existingArn)
	}
	var updated acmBindingParams
	_ = json.Unmarshal(result.UpdatedParams, &updated)
	if updated.CertificateArn != existingArn {
		t.Errorf("expected the ARN to stay stable across a renewal push, got %q", updated.CertificateArn)
	}
}

func TestACMTarget_Push_NoPrivateKey_Errors(t *testing.T) {
	target := NewACMTargetWithClient(&fakeACMAPI{})
	_, err := target.Push(context.Background(), &delivery.CertMaterial{CertPEM: []byte("cert")}, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected an error when no private key material is supplied")
	}
}

func TestACMTarget_Push_ImportError_Propagates(t *testing.T) {
	fake := &fakeACMAPI{importErr: errors.New("throttled")}
	target := NewACMTargetWithClient(fake)
	_, err := target.Push(context.Background(), &delivery.CertMaterial{CertPEM: []byte("c"), KeyPEM: []byte("k")}, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected ImportCertificate's error to propagate")
	}
}

func TestACMTarget_Check_MatchingFingerprint(t *testing.T) {
	fake := &fakeACMAPI{tags: map[string]string{acmFingerprintTag: "fp-match"}}
	target := NewACMTargetWithClient(fake)
	params := json.RawMessage(`{"region":"us-east-1","arn":"arn:aws:acm:us-east-1:111111111111:certificate/x"}`)

	current, err := target.Check(context.Background(), "fp-match", params)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !current {
		t.Error("expected Check to report current=true for a matching fingerprint tag")
	}
}

func TestACMTarget_Check_MismatchedFingerprint(t *testing.T) {
	fake := &fakeACMAPI{tags: map[string]string{acmFingerprintTag: "fp-old"}}
	target := NewACMTargetWithClient(fake)
	params := json.RawMessage(`{"arn":"arn:aws:acm:us-east-1:111111111111:certificate/x"}`)

	current, err := target.Check(context.Background(), "fp-new", params)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if current {
		t.Error("expected Check to report current=false for a mismatched fingerprint tag")
	}
}

func TestACMTarget_Check_NoPriorPush_RequiresPush(t *testing.T) {
	target := NewACMTargetWithClient(&fakeACMAPI{})
	current, err := target.Check(context.Background(), "fp-anything", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if current {
		t.Error("expected Check to report current=false when no ARN has been recorded yet")
	}
}

func TestACMTarget_Check_APIError_DegradesToPushRequired(t *testing.T) {
	fake := &fakeACMAPI{listTagsErr: errors.New("ACM unavailable")}
	target := NewACMTargetWithClient(fake)
	params := json.RawMessage(`{"arn":"arn:aws:acm:us-east-1:111111111111:certificate/x"}`)

	current, err := target.Check(context.Background(), "fp", params)
	if err != nil {
		t.Fatalf("Check must not surface the underlying API error, got: %v", err)
	}
	if current {
		t.Error("expected Check to degrade to pushRequired=true (current=false) on an API error")
	}
}
