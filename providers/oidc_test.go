package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/coreos/go-oidc"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/sessions"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/encryption"
	"github.com/stretchr/testify/assert"
)

type redeemTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token,omitempty"`
}

func newOIDCProvider(serverURL *url.URL) *OIDCProvider {
	providerData := &ProviderData{
		ProviderName: "oidc",
		ClientID:     oidcClientID,
		ClientSecret: oidcSecret,
		LoginURL: &url.URL{
			Scheme: serverURL.Scheme,
			Host:   serverURL.Host,
			Path:   "/login/oauth/authorize"},
		RedeemURL: &url.URL{
			Scheme: serverURL.Scheme,
			Host:   serverURL.Host,
			Path:   "/login/oauth/access_token"},
		ProfileURL: &url.URL{
			Scheme: serverURL.Scheme,
			Host:   serverURL.Host,
			Path:   "/profile"},
		IntrospectURL: &url.URL{
			Scheme: serverURL.Scheme,
			Host:   serverURL.Host,
			Path:   "/introspect"},
		ValidateURL: &url.URL{
			Scheme: serverURL.Scheme,
			Host:   serverURL.Host,
			Path:   "/api"},
		Scope:       "openid profile offline_access",
		EmailClaim:  "email",
		GroupsClaim: "groups",
		Verifier: oidc.NewVerifier(
			oidcIssuer,
			mockJWKS{},
			&oidc.Config{ClientID: oidcClientID},
		),
	}

	p := NewOIDCProvider(providerData)

	return p
}

func newOIDCServer(redeemJSON []byte, profileJSON []byte, introspectJSON []byte) *httptest.Server {
	mux := http.NewServeMux()
	if len(redeemJSON) > 0 {
		mux.HandleFunc("/login/oauth/access_token", func(rw http.ResponseWriter, req *http.Request) {
			rw.Header().Add("content-type", "application/json")
			_, _ = rw.Write(redeemJSON)
		})
	}
	if len(profileJSON) > 0 {
		mux.HandleFunc("/profile", func(rw http.ResponseWriter, req *http.Request) {
			rw.Header().Add("content-type", "application/json")
			_, _ = rw.Write(profileJSON)
		})
	}
	if len(introspectJSON) > 0 {
		mux.HandleFunc("/introspect", func(rw http.ResponseWriter, req *http.Request) {
			rw.Header().Add("content-type", "application/json")
			_, _ = rw.Write(introspectJSON)
		})
	}
	testserver := httptest.NewServer(mux)
	return testserver
}

func newTestOIDCSetup(redeemJSON []byte, profileJSON []byte, introspectJSON []byte) (*httptest.Server, *OIDCProvider) {
	server := newOIDCServer(redeemJSON, profileJSON, introspectJSON)
	serverURL, _ := url.Parse(server.URL)
	provider := newOIDCProvider(serverURL)
	return server, provider
}

func TestOIDCProviderGetLoginURL(t *testing.T) {
	serverURL := &url.URL{
		Scheme: "https",
		Host:   "oauth2proxy.oidctest",
	}
	provider := newOIDCProvider(serverURL)

	n, err := encryption.Nonce()
	assert.NoError(t, err)
	nonce := base64.RawURLEncoding.EncodeToString(n)

	// SkipNonce defaults to true
	skipNonce := provider.GetLoginURL("http://redirect/", "", nonce)
	assert.NotContains(t, skipNonce, "nonce")

	provider.SkipNonce = false
	withNonce := provider.GetLoginURL("http://redirect/", "", nonce)
	assert.Contains(t, withNonce, fmt.Sprintf("nonce=%s", nonce))
}

func TestOIDCProviderRedeem(t *testing.T) {
	idToken, _ := newSignedTestIDToken(defaultIDToken)
	body, _ := json.Marshal(redeemTokenResponse{
		AccessToken:  accessToken,
		ExpiresIn:    10,
		TokenType:    "Bearer",
		RefreshToken: refreshToken,
		IDToken:      idToken,
	})
	server, provider := newTestOIDCSetup(body, []byte(`{}`), []byte(`{}`))
	defer server.Close()

	session, err := provider.Redeem(context.Background(), provider.RedeemURL.String(), "code1234")
	assert.Equal(t, nil, err)
	assert.Equal(t, defaultIDToken.Email, session.Email)
	assert.Equal(t, accessToken, session.AccessToken)
	assert.Equal(t, idToken, session.IDToken)
	assert.Equal(t, refreshToken, session.RefreshToken)
	assert.Equal(t, "123456789", session.User)
}

func TestOIDCProviderRedeem_custom_userid(t *testing.T) {
	idToken, _ := newSignedTestIDToken(defaultIDToken)
	body, _ := json.Marshal(redeemTokenResponse{
		AccessToken:  accessToken,
		ExpiresIn:    10,
		TokenType:    "Bearer",
		RefreshToken: refreshToken,
		IDToken:      idToken,
	})

	server, provider := newTestOIDCSetup(body, []byte(`{}`), []byte(`{}`))
	provider.EmailClaim = "phone_number"
	defer server.Close()

	session, err := provider.Redeem(context.Background(), provider.RedeemURL.String(), "code1234")
	assert.Equal(t, nil, err)
	assert.Equal(t, defaultIDToken.Phone, session.Email)
}

func TestOIDCProvider_EnrichSession(t *testing.T) {
	testCases := map[string]struct {
		ExistingSession *sessions.SessionState
		EmailClaim      string
		GroupsClaim     string
		ProfileJSON     map[string]interface{}
		IntrospectJSON  map[string]interface{}
		ExpectedError   error
		ExpectedSession *sessions.SessionState
	}{
		"Already Populated": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				Groups:       []string{"already", "populated"},
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email":  "new@thing.com",
				"groups": []string{"new", "thing"},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				Groups:           []string{"already", "populated"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Email": {
			ExistingSession: &sessions.SessionState{
				User:         "missing.email",
				Groups:       []string{"already", "populated"},
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email":  "found@email.com",
				"groups": []string{"new", "thing"},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "missing.email",
				Email:            "found@email.com",
				Groups:           []string{"already", "populated"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},

		"Missing Email Only in Profile URL": {
			ExistingSession: &sessions.SessionState{
				User:         "missing.email",
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email": "found@email.com",
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "missing.email",
				Email:            "found@email.com",
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Email with Custom Claim": {
			ExistingSession: &sessions.SessionState{
				User:         "missing.email",
				Groups:       []string{"already", "populated"},
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "weird",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"weird":  "weird@claim.com",
				"groups": []string{"new", "thing"},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "missing.email",
				Email:            "weird@claim.com",
				Groups:           []string{"already", "populated"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Email not in Profile URL": {
			ExistingSession: &sessions.SessionState{
				User:         "missing.email",
				Groups:       []string{"already", "populated"},
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"groups": []string{"new", "thing"},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: errors.New("neither the id_token nor the profileURL set an email"),
			ExpectedSession: &sessions.SessionState{
				User:             "missing.email",
				Groups:           []string{"already", "populated"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Groups": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				Groups:       nil,
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email":  "new@thing.com",
				"groups": []string{"new", "thing"},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				Groups:           []string{"new", "thing"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Groups with Complex Groups in Profile URL": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				Groups:       nil,
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email": "new@thing.com",
				"groups": []map[string]interface{}{
					{
						"groupId": "Admin Group Id",
						"roles":   []string{"Admin"},
					},
				},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				Groups:           []string{"{\"groupId\":\"Admin Group Id\",\"roles\":[\"Admin\"]}"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Groups with Singleton Complex Group in Profile URL": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				Groups:       nil,
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email": "new@thing.com",
				"groups": map[string]interface{}{
					"groupId": "Admin Group Id",
					"roles":   []string{"Admin"},
				},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				Groups:           []string{"{\"groupId\":\"Admin Group Id\",\"roles\":[\"Admin\"]}"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Empty Groups Claims": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				Groups:       []string{},
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email":  "new@thing.com",
				"groups": []string{"new", "thing"},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				Groups:           []string{},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Groups with Custom Claim": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				Groups:       nil,
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "roles",
			ProfileJSON: map[string]interface{}{
				"email": "new@thing.com",
				"roles": []string{"new", "thing", "roles"},
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				Groups:           []string{"new", "thing", "roles"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Groups String Profile URL Response": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				Groups:       nil,
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email":  "new@thing.com",
				"groups": "singleton",
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				Groups:           []string{"singleton"},
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Missing Groups in both Claims and Profile URL": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email": "new@thing.com",
			},
			IntrospectJSON: map[string]interface{}{
				"active": true,
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWV9",
			},
		},
		"Introspection Response added in session": {
			ExistingSession: &sessions.SessionState{
				User:         "already",
				Email:        "already@populated.com",
				IDToken:      idToken,
				AccessToken:  accessToken,
				RefreshToken: refreshToken,
			},
			EmailClaim:  "email",
			GroupsClaim: "groups",
			ProfileJSON: map[string]interface{}{
				"email": "new@thing.com",
			},
			IntrospectJSON: map[string]interface{}{
				"active":   true,
				"scope":    "openid profile email",
				"username": "red.rush@invincible.com",
				"exp":      1620821880,
				"sub":      "a9dcb37f-a5b5-4304-b334-aa72865ebacf",
				"iss":      "https://dev.authprovider.com/oauth2/default",
				"organizations": map[string]interface{}{
					"managingOrganization": "9e99787d-ee6a-4ece-83e7-065da45c77eb",
					"organizationList": [](map[string]interface{}){
						{
							"organizationId": "a7f6fa08-4aba-4b19-844f-3d3bb9eb721d",
							"permissions": []string{
								"CLIENT.READ",
								"CP-CONFIG.BUNDLE_READ",
								"CP-CONFIG.READ",
							},
							"organizationName": "org1",
							"groups": []string{
								"admin",
							},
							"roles": []string{
								"adminaccess",
							},
						},
						{
							"organizationId": "c7c7970d-21a7-450b-9557-5fa2080f3ff4",
							"permissions": []string{
								"CLIENT.WRITE",
								"EMAILTEMPLATE.DELETE",
							},
							"organizationName": "org2",
						},
					}},
				"client_id":     "oidcclient1",
				"token_type":    "Bearer",
				"identity_type": "user",
			},
			ExpectedError: nil,
			ExpectedSession: &sessions.SessionState{
				User:             "already",
				Email:            "already@populated.com",
				IDToken:          idToken,
				AccessToken:      accessToken,
				RefreshToken:     refreshToken,
				IntrospectClaims: "eyJhY3RpdmUiOnRydWUsImNsaWVudF9pZCI6Im9pZGNjbGllbnQxIiwiZXhwIjoxNjIwODIxODgwLCJpZGVudGl0eV90eXBlIjoidXNlciIsImlzcyI6Imh0dHBzOi8vZGV2LmF1dGhwcm92aWRlci5jb20vb2F1dGgyL2RlZmF1bHQiLCJvcmdhbml6YXRpb25zIjp7Im1hbmFnaW5nT3JnYW5pemF0aW9uIjoiOWU5OTc4N2QtZWU2YS00ZWNlLTgzZTctMDY1ZGE0NWM3N2ViIiwib3JnYW5pemF0aW9uTGlzdCI6W3siZ3JvdXBzIjpbImFkbWluIl0sIm9yZ2FuaXphdGlvbklkIjoiYTdmNmZhMDgtNGFiYS00YjE5LTg0NGYtM2QzYmI5ZWI3MjFkIiwib3JnYW5pemF0aW9uTmFtZSI6Im9yZzEiLCJwZXJtaXNzaW9ucyI6WyJDTElFTlQuUkVBRCIsIkNQLUNPTkZJRy5CVU5ETEVfUkVBRCIsIkNQLUNPTkZJRy5SRUFEIl0sInJvbGVzIjpbImFkbWluYWNjZXNzIl19LHsib3JnYW5pemF0aW9uSWQiOiJjN2M3OTcwZC0yMWE3LTQ1MGItOTU1Ny01ZmEyMDgwZjNmZjQiLCJvcmdhbml6YXRpb25OYW1lIjoib3JnMiIsInBlcm1pc3Npb25zIjpbIkNMSUVOVC5XUklURSIsIkVNQUlMVEVNUExBVEUuREVMRVRFIl19XX0sInNjb3BlIjoib3BlbmlkIHByb2ZpbGUgZW1haWwiLCJzdWIiOiJhOWRjYjM3Zi1hNWI1LTQzMDQtYjMzNC1hYTcyODY1ZWJhY2YiLCJ0b2tlbl90eXBlIjoiQmVhcmVyIiwidXNlcm5hbWUiOiJyZWQucnVzaEBpbnZpbmNpYmxlLmNvbSJ9",
			},
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			profileJSON, err := json.Marshal(tc.ProfileJSON)
			assert.NoError(t, err)

			introspectJSON, err := json.Marshal(tc.IntrospectJSON)
			assert.NoError(t, err)

			server, provider := newTestOIDCSetup([]byte(`{}`), profileJSON, introspectJSON)
			assert.NoError(t, err)

			provider.EmailClaim = tc.EmailClaim
			provider.GroupsClaim = tc.GroupsClaim
			defer server.Close()

			err = provider.EnrichSession(context.Background(), tc.ExistingSession)
			assert.Equal(t, tc.ExpectedError, err)
			assert.Equal(t, *tc.ExpectedSession, *tc.ExistingSession)
		})
	}
}

func TestOIDCProviderRefreshSessionIfNeededWithoutIdToken(t *testing.T) {

	idToken, _ := newSignedTestIDToken(defaultIDToken)
	body, _ := json.Marshal(redeemTokenResponse{
		AccessToken:  accessToken,
		ExpiresIn:    10,
		TokenType:    "Bearer",
		RefreshToken: refreshToken,
	})

	server, provider := newTestOIDCSetup(body, []byte(`{}`), []byte(`{}`))
	defer server.Close()

	existingSession := &sessions.SessionState{
		AccessToken:  "changeit",
		IDToken:      idToken,
		CreatedAt:    nil,
		ExpiresOn:    nil,
		RefreshToken: refreshToken,
		Email:        "janedoe@example.com",
		User:         "11223344",
	}

	refreshed, err := provider.RefreshSessionIfNeeded(context.Background(), existingSession)
	assert.Equal(t, nil, err)
	assert.Equal(t, refreshed, true)
	assert.Equal(t, "janedoe@example.com", existingSession.Email)
	assert.Equal(t, accessToken, existingSession.AccessToken)
	assert.Equal(t, idToken, existingSession.IDToken)
	assert.Equal(t, refreshToken, existingSession.RefreshToken)
	assert.Equal(t, "11223344", existingSession.User)
}

func TestOIDCProviderRefreshSessionIfNeededWithIdToken(t *testing.T) {

	idToken, _ := newSignedTestIDToken(defaultIDToken)
	body, _ := json.Marshal(redeemTokenResponse{
		AccessToken:  accessToken,
		ExpiresIn:    10,
		TokenType:    "Bearer",
		RefreshToken: refreshToken,
		IDToken:      idToken,
	})

	server, provider := newTestOIDCSetup(body, []byte(`{}`), []byte(`{}`))
	defer server.Close()

	existingSession := &sessions.SessionState{
		AccessToken:  "changeit",
		IDToken:      "changeit",
		CreatedAt:    nil,
		ExpiresOn:    nil,
		RefreshToken: refreshToken,
		Email:        "changeit",
		User:         "changeit",
	}
	refreshed, err := provider.RefreshSessionIfNeeded(context.Background(), existingSession)
	assert.Equal(t, nil, err)
	assert.Equal(t, refreshed, true)
	assert.Equal(t, defaultIDToken.Email, existingSession.Email)
	assert.Equal(t, defaultIDToken.Subject, existingSession.User)
	assert.Equal(t, accessToken, existingSession.AccessToken)
	assert.Equal(t, idToken, existingSession.IDToken)
	assert.Equal(t, refreshToken, existingSession.RefreshToken)
}

func TestOIDCProviderCreateSessionFromToken(t *testing.T) {
	testCases := map[string]struct {
		IDToken        idTokenClaims
		GroupsClaim    string
		ExpectedUser   string
		ExpectedEmail  string
		ExpectedGroups []string
	}{
		"Default IDToken": {
			IDToken:        defaultIDToken,
			GroupsClaim:    "groups",
			ExpectedUser:   "123456789",
			ExpectedEmail:  "janed@me.com",
			ExpectedGroups: []string{"test:a", "test:b"},
		},
		"Minimal IDToken with no email claim": {
			IDToken:        minimalIDToken,
			GroupsClaim:    "groups",
			ExpectedUser:   "123456789",
			ExpectedEmail:  "123456789",
			ExpectedGroups: nil,
		},
		"Custom Groups Claim": {
			IDToken:        defaultIDToken,
			GroupsClaim:    "roles",
			ExpectedUser:   "123456789",
			ExpectedEmail:  "janed@me.com",
			ExpectedGroups: []string{"test:c", "test:d"},
		},
		"Complex Groups Claim": {
			IDToken:        complexGroupsIDToken,
			GroupsClaim:    "groups",
			ExpectedUser:   "123456789",
			ExpectedEmail:  "complex@claims.com",
			ExpectedGroups: []string{"{\"groupId\":\"Admin Group Id\",\"roles\":[\"Admin\"]}"},
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			server, provider := newTestOIDCSetup([]byte(`{}`), []byte(`{}`), []byte(`{}`))
			provider.GroupsClaim = tc.GroupsClaim
			defer server.Close()

			rawIDToken, err := newSignedTestIDToken(tc.IDToken)
			assert.NoError(t, err)

			ss, err := provider.CreateSessionFromToken(context.Background(), rawIDToken)
			assert.NoError(t, err)

			assert.Equal(t, tc.ExpectedUser, ss.User)
			assert.Equal(t, tc.ExpectedEmail, ss.Email)
			assert.Equal(t, tc.ExpectedGroups, ss.Groups)
			assert.Equal(t, rawIDToken, ss.IDToken)
			assert.Equal(t, rawIDToken, ss.AccessToken)
			assert.Equal(t, "", ss.RefreshToken)
		})
	}
}
