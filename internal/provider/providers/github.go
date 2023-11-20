package providers

import (
	"context"
	"net/http"
	"strconv"

	json "github.com/json-iterator/go"
	"github.com/synctv-org/synctv/internal/provider"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type GithubProvider struct {
	config oauth2.Config
}

func (p *GithubProvider) Init(c provider.Oauth2Option) {
	p.config.Scopes = []string{"user"}
	if c.Endpoint != nil {
		p.config.Endpoint = *c.Endpoint
	} else {
		p.config.Endpoint = github.Endpoint
	}
	p.config.ClientID = c.ClientID
	p.config.ClientSecret = c.ClientSecret
	p.config.RedirectURL = c.RedirectURL
}

func (p *GithubProvider) Provider() provider.OAuth2Provider {
	return "github"
}

func (p *GithubProvider) NewAuthURL(state string) string {
	return p.config.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

func (p *GithubProvider) GetToken(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *GithubProvider) RefreshToken(ctx context.Context, tk string) (*oauth2.Token, error) {
	return p.config.TokenSource(ctx, &oauth2.Token{RefreshToken: tk}).Token()
}

func (p *GithubProvider) GetUserInfo(ctx context.Context, tk *oauth2.Token) (*provider.UserInfo, error) {
	client := p.config.Client(ctx, tk)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	ui := githubUserInfo{}
	err = json.NewDecoder(resp.Body).Decode(&ui)
	if err != nil {
		return nil, err
	}
	return &provider.UserInfo{
		Username:       ui.Login,
		ProviderUserID: strconv.FormatUint(ui.ID, 10),
	}, nil
}

type githubUserInfo struct {
	Login string `json:"login"`
	ID    uint64 `json:"id"`
}

func init() {
	RegisterProvider(new(GithubProvider))
}
