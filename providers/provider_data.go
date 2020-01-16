package providers

import (
	"errors"
	"github.com/pusher/oauth2_proxy/pkg/logger"
	"io/ioutil"
	"net/url"
)

// ProviderData contains information required to configure all implementations
// of OAuth2 providers
type ProviderData struct {
	ProviderName      string
	ClientID          string
	ClientSecret_     string
	ClientSecretFile  string
	LoginURL          *url.URL
	RedeemURL         *url.URL
	ProfileURL        *url.URL
	ProtectedResource *url.URL
	ValidateURL       *url.URL
	Scope             string
	ApprovalPrompt    string
}

// Data returns the ProviderData
func (p *ProviderData) Data() *ProviderData { return p }

func (p *ProviderData) ClientSecret() (ClientSecret string, err error) {
	if p.ClientSecret_ != "" || p.ClientSecretFile == "" {
		return p.ClientSecret_, nil
	}

	// Getting ClientSecret can fail in runtime so we need to report it without returning the file name to the user
	ClientSecret_, err := ioutil.ReadFile(p.ClientSecretFile)
	if err != nil {
		logger.Printf("error reading client secret file %s: %s", p.ClientSecretFile, err)
		return "", errors.New("could not read client secret file")
	}
	return string(ClientSecret_), nil
}
