package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"github.com/coreos/go-oidc"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"gopkg.in/square/go-jose.v2"
)

var _ = Describe("Verify", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("Succeeds with default aud behavior", func() {
		result, err := verify(ctx, &IDTokenVerificationOptions{
			AudienceClaim:  "aud",
			ClientID:       "1226737",
			ExtraAudiences: []string{},
		}, payload{
			Iss: "https://foo",
			Aud: "1226737",
		})

		Expect(err).To(Succeed())
		Expect(result).ToNot(BeNil())
	})

	It("Fails with default aud behavior", func() {
		result, err := verify(ctx, &IDTokenVerificationOptions{
			AudienceClaim:  "aud",
			ClientID:       "7817818",
			ExtraAudiences: []string{},
		}, payload{
			Iss: "https://foo",
			Aud: "1226737",
		})
		Expect(err).To(MatchError("audience from claim aud with value 1226737 does not match with " +
			"any of allowed audiences [7817818]"))
		Expect(result).To(BeNil())
	})

	It("Succeeds with extra audiences", func() {
		result, err := verify(ctx, &IDTokenVerificationOptions{
			AudienceClaim:  "aud",
			ClientID:       "7817818",
			ExtraAudiences: []string{"xyz", "1226737"},
		}, payload{
			Iss: "https://foo",
			Aud: "1226737",
		})

		Expect(err).To(Succeed())
		Expect(result).ToNot(BeNil())
	})

	It("Fails with extra audiences", func() {
		result, err := verify(ctx, &IDTokenVerificationOptions{
			AudienceClaim:  "aud",
			ClientID:       "7817818",
			ExtraAudiences: []string{"xyz", "abc"},
		}, payload{
			Iss: "https://foo",
			Aud: "1226737",
		})

		Expect(err).To(MatchError("audience from claim aud with value 1226737 does not match with any " +
			"of allowed audiences [7817818 xyz abc]"))
		Expect(result).To(BeNil())
	})

	It("Succeeds with non default aud behavior", func() {
		result, err := verify(ctx, &IDTokenVerificationOptions{
			AudienceClaim:  "client_id",
			ClientID:       "1226737",
			ExtraAudiences: []string{},
		}, payload{
			Iss:      "https://foo",
			ClientId: "1226737",
		})

		Expect(err).To(Succeed())
		Expect(result).ToNot(BeNil())
	})

	It("Fails with non default aud behavior", func() {
		result, err := verify(ctx, &IDTokenVerificationOptions{
			AudienceClaim:  "client_id",
			ClientID:       "7817818",
			ExtraAudiences: []string{},
		}, payload{
			Iss:      "https://foo",
			ClientId: "1226737",
		})
		Expect(err).To(MatchError("audience from claim client_id with value 1226737 does not match with " +
			"any of allowed audiences [7817818]"))
		Expect(result).To(BeNil())
	})

	It("Succeeds with non default aud behavior and extra audiences", func() {
		result, err := verify(ctx, &IDTokenVerificationOptions{
			AudienceClaim:  "client_id",
			ClientID:       "7817818",
			ExtraAudiences: []string{"xyz", "1226737"},
		}, payload{
			Iss:      "https://foo",
			ClientId: "1226737",
		})

		Expect(err).To(Succeed())
		Expect(result).ToNot(BeNil())
	})

	It("Fails with non default aud behavior and extra audiences", func() {
		result, err := verify(ctx, &IDTokenVerificationOptions{
			AudienceClaim:  "client_id",
			ClientID:       "7817818",
			ExtraAudiences: []string{"xyz", "abc"},
		}, payload{
			Iss:      "https://foo",
			ClientId: "1226737",
		})

		Expect(err).To(MatchError("audience from claim client_id with value 1226737 does not match with any " +
			"of allowed audiences [7817818 xyz abc]"))
		Expect(result).To(BeNil())
	})
})

type payload struct {
	Iss      string `json:"iss,omitempty"`
	Aud      string `json:"aud,omitempty"`
	ClientId string `json:"client_id,omitempty"`
}

type jwtToken struct {
	PrivateKey jose.JSONWebKey
	PublicKey  jose.JSONWebKey
	Token      string
}

type testVerifier struct {
	jwk jose.JSONWebKey
}

func (t *testVerifier) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	jws, err := jose.ParseSigned(jwt)
	if err != nil {
		return nil, fmt.Errorf("oidc: malformed jwt: %v", err)
	}
	return jws.Verify(&t.jwk)
}

func verify(ctx context.Context, verificationOptions *IDTokenVerificationOptions, payload payload) (*oidc.IDToken, error) {
	config := &oidc.Config{
		ClientID:          "1226737",
		SkipClientIDCheck: true,
		SkipExpiryCheck:   true, // required to not run in expired Token error during testing
	}
	rawToken, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	token, _ := createToken(rawToken)
	verifier := NewVerifier(oidc.NewVerifier("https://foo", &testVerifier{jwk: token.PublicKey}, config), verificationOptions)
	return verifier.Verify(ctx, token.Token)
}

func createToken(payload []byte) (*jwtToken, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1028)
	if err != nil {
		return nil, err
	}
	privateWebKey := jose.JSONWebKey{Key: privateKey, Algorithm: string(jose.RS256), KeyID: ""}
	publicWebKey := jose.JSONWebKey{Key: privateKey.Public(), Use: "sig", Algorithm: string(jose.RS256), KeyID: ""}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: privateWebKey}, nil)
	if err != nil {
		return nil, err
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		return nil, err
	}
	data, err := jws.CompactSerialize()
	if err != nil {
		return nil, err
	}
	return &jwtToken{
		PrivateKey: privateWebKey,
		PublicKey:  publicWebKey,
		Token:      data,
	}, nil
}
