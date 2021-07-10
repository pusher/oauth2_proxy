package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"net/url"

	"github.com/coreos/go-oidc"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/sessions"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	accessTokenHeader    = "ewogICJhbGciOiAiUlMyNTYiLAogICJ0eXAiOiAiSldUIgp9"
	accessTokenPayload   = "eyJyZWFsbV9hY2Nlc3MiOiB7InJvbGVzIjogWyJ3cml0ZSJdfSwgInJlc291cmNlX2FjY2VzcyI6IHsiZGVmYXVsdCI6IHsicm9sZXMiOiBbInJlYWQiXX19fQ"
	accessTokenSignature = "dyt0CoTl4WoVjAHI9Q_CwSKhl6d_9rhM3NrXuJttkao"
)

type DummyKeySet struct{}

func (DummyKeySet) VerifySignature(_ context.Context, _ string) (payload []byte, err error) {
	p, _ := base64.RawURLEncoding.DecodeString(accessTokenPayload)
	return p, nil
}

func getAccessToken() string {
	return fmt.Sprintf("%s.%s.%s", accessTokenHeader, accessTokenPayload, accessTokenSignature)
}

func newTestKeycloakOIDCSetup() (*httptest.Server, *KeycloakOIDCProvider) {
	redeemURL, server := newOIDCServer([]byte(`{"email": "new@thing.com"`))
	provider := newKeycloakOIDCProvider(redeemURL)
	return server, provider
}

func newKeycloakOIDCProvider(serverURL *url.URL) *KeycloakOIDCProvider {
	p := NewKeycloakOIDCProvider(
		&ProviderData{
			LoginURL: &url.URL{
				Scheme: "https",
				Host:   "keycloak-oidc.com",
				Path:   "/oauth/auth"},
			RedeemURL: &url.URL{
				Scheme: "https",
				Host:   "keycloak-oidc.com",
				Path:   "/oauth/token"},
			ProfileURL: &url.URL{
				Scheme: "https",
				Host:   "keycloak-oidc.com",
				Path:   "/api/v3/user"},
			ValidateURL: &url.URL{
				Scheme: "https",
				Host:   "keycloak-oidc.com",
				Path:   "/api/v3/user"},
			Scope: "openid email profile"})

	if serverURL != nil {
		p.RedeemURL.Scheme = serverURL.Scheme
		p.RedeemURL.Host = serverURL.Host
	}

	keyset := DummyKeySet{}
	p.Verifier = oidc.NewVerifier("", keyset, &oidc.Config{
		ClientID:          "client",
		SkipIssuerCheck:   true,
		SkipClientIDCheck: true,
		SkipExpiryCheck:   true,
	})
	p.EmailClaim = "email"
	p.GroupsClaim = "groups"
	return p
}

var _ = Describe("Keycloak OIDC Provider Tests", func() {
	Context("New Provider Init", func() {
		It("creates new keycloak oidc provider with expected defaults", func() {
			p := newKeycloakOIDCProvider(nil)
			providerData := p.Data()
			Expect(providerData.ProviderName).To(Equal(keycloakOIDCProviderName))
			Expect(providerData.LoginURL.String()).To(Equal("https://keycloak-oidc.com/oauth/auth"))
			Expect(providerData.RedeemURL.String()).To(Equal("https://keycloak-oidc.com/oauth/token"))
			Expect(providerData.ProfileURL.String()).To(Equal("https://keycloak-oidc.com/api/v3/user"))
			Expect(providerData.ValidateURL.String()).To(Equal("https://keycloak-oidc.com/api/v3/user"))
			Expect(providerData.Scope).To(Equal("openid email profile"))
		})
	})

	Context("Allowed Roles", func() {
		It("should prefix allowed roles and add them to groups", func() {
			p := newKeycloakOIDCProvider(nil)
			p.AddAllowedRoles([]string{"admin", "editor"})
			Expect(p.AllowedGroups).To(HaveKey("role:admin"))
			Expect(p.AllowedGroups).To(HaveKey("role:editor"))
		})
	})

	Context("Enrich Session", func() {
		It("should not fail when groups are not assigned", func() {
			server, provider := newTestKeycloakOIDCSetup()
			url, err := url.Parse(server.URL)
			Expect(err).To(BeNil())
			defer server.Close()

			provider.ProfileURL = url

			existingSession := &sessions.SessionState{
				User:         "already",
				Email:        "a@b.com",
				Groups:       nil,
				IDToken:      idToken,
				AccessToken:  getAccessToken(),
				RefreshToken: refreshToken,
			}
			expectedSession := &sessions.SessionState{
				User:         "already",
				Email:        "a@b.com",
				Groups:       []string{"role:write", "role:default:read"},
				IDToken:      idToken,
				AccessToken:  getAccessToken(),
				RefreshToken: refreshToken,
			}

			err = provider.EnrichSession(context.Background(), existingSession)
			Expect(err).To(BeNil())
			Expect(existingSession).To(Equal(expectedSession))
		})

		It("should add roles to existing groups", func() {
			server, provider := newTestKeycloakOIDCSetup()
			url, err := url.Parse(server.URL)
			Expect(err).To(BeNil())
			defer server.Close()

			provider.ProfileURL = url

			existingSession := &sessions.SessionState{
				User:         "already",
				Email:        "a@b.com",
				Groups:       []string{"existing", "group"},
				IDToken:      idToken,
				AccessToken:  getAccessToken(),
				RefreshToken: refreshToken,
			}
			expectedSession := &sessions.SessionState{
				User:         "already",
				Email:        "a@b.com",
				Groups:       []string{"existing", "group", "role:write", "role:default:read"},
				IDToken:      idToken,
				AccessToken:  getAccessToken(),
				RefreshToken: refreshToken,
			}

			err = provider.EnrichSession(context.Background(), existingSession)
			Expect(err).To(BeNil())
			Expect(existingSession).To(Equal(expectedSession))
		})
	})
})
