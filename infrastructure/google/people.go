package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// PeopleEndpoint is where Google's People API lives.
const PeopleEndpoint = "https://people.googleapis.com"

var (
	_ application.ContactBook  = People{}
	_ application.GoogleAccess = OAuth{}
)

// AccessToken implements application.GoogleAccess: one access token for the
// refresh token, good for the call that asked. Neither token is put in an error.
func (o OAuth) AccessToken(ctx context.Context, refresh string) (string, error) {
	client, err := o.client()
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {client.ID},
	}
	if client.Secret != "" {
		form.Set("client_secret", client.Secret)
	}
	var answer tokenAnswer
	status, err := o.post(ctx, o.Endpoints.Token, form, &answer)
	if err != nil {
		return "", fmt.Errorf("calling Google's token endpoint: %w", err)
	}
	if answer.Error == "invalid_grant" {
		return "", fmt.Errorf("%w (invalid_grant): run mw google link again", application.ErrGoogleTokenDead)
	}
	if status != http.StatusOK || answer.Error != "" || answer.Access == "" {
		return "", fmt.Errorf("Google's token endpoint refused the link (HTTP %d): %s", status, refusal(answer.Error, answer.Description))
	}
	return answer.Access, nil
}

// People is application.ContactBook against Google's People API: the
// contacts.readonly link kept in Tokens, searched with people.searchContacts.
// It decodes the names and the phone numbers of a match and nothing else, and
// asks for no more (readMask phoneNumbers,names).
type People struct {
	Tokens application.GoogleTokenFile
	Access application.GoogleAccess
	// Endpoint is the People API's address; PeopleEndpoint when empty.
	Endpoint string
	// HTTP calls the API; a client bounded by callTimeout when nil.
	HTTP *http.Client
}

// searchPageSize is the most matches asked for: two already say no guess.
const searchPageSize = "30"

// Find implements application.ContactBook.
func (p People) Find(ctx context.Context, name string) ([]application.Contact, error) {
	grant, err := p.Tokens.Load()
	if err != nil {
		return nil, err
	}
	access, err := p.Access.AccessToken(ctx, grant.Refresh)
	if err != nil {
		return nil, scrubbedError(err, grant.Refresh)
	}
	// Google asks for an empty search first, to warm the cache the real one reads.
	if _, err := p.search(ctx, access, "", "names"); err != nil {
		return nil, scrubbedError(err, grant.Refresh, access)
	}
	found, err := p.search(ctx, access, name, "phoneNumbers,names")
	if err != nil {
		return nil, scrubbedError(err, grant.Refresh, access)
	}
	var contacts []application.Contact
	for _, result := range found {
		contacts = append(contacts, application.Contact{Name: result.Person.name(), Mobiles: result.Person.mobiles()})
	}
	return contacts, nil
}

// scrubbedError is err with every secret in its text replaced.
func scrubbedError(err error, secrets ...string) error {
	text := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[token]")
		}
	}
	if text == err.Error() {
		return err
	}
	return errors.New(text)
}

// person is the two fields of a People API person that are read.
type person struct {
	Names []struct {
		DisplayName string `json:"displayName"`
		Metadata    struct {
			Primary bool `json:"primary"`
		} `json:"metadata"`
	} `json:"names"`
	PhoneNumbers []struct {
		Value         string `json:"value"`
		CanonicalForm string `json:"canonicalForm"`
		Type          string `json:"type"`
		FormattedType string `json:"formattedType"`
	} `json:"phoneNumbers"`
}

// name is the primary display name, else the first.
func (p person) name() string {
	for _, n := range p.Names {
		if n.Metadata.Primary && n.DisplayName != "" {
			return n.DisplayName
		}
	}
	for _, n := range p.Names {
		if n.DisplayName != "" {
			return n.DisplayName
		}
	}
	return ""
}

// mobiles are the numbers of type mobile, each once, as +<digits>.
func (p person) mobiles() []string {
	var numbers []string
	seen := map[string]bool{}
	for _, phone := range p.PhoneNumbers {
		if !strings.EqualFold(phone.Type, "mobile") && !strings.EqualFold(phone.FormattedType, "mobile") {
			continue
		}
		number := e164(phone.CanonicalForm, phone.Value)
		if number == "" || seen[number] {
			continue
		}
		seen[number] = true
		numbers = append(numbers, number)
	}
	return numbers
}

var plainE164 = regexp.MustCompile(`^\+[0-9]{7,15}$`)

// e164 is the number as +<digits>: Google's canonical form when it is one,
// else the value as written when it is plainly one (a +, or ten digits, or
// eleven starting 1, which are North American); "" when it is not.
func e164(canonical, value string) string {
	if plainE164.MatchString(canonical) {
		return canonical
	}
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	var number string
	switch {
	case strings.HasPrefix(strings.TrimSpace(value), "+"):
		number = "+" + d
	case len(d) == 10:
		number = "+1" + d
	case len(d) == 11 && d[0] == '1':
		number = "+" + d
	}
	if plainE164.MatchString(number) {
		return number
	}
	return ""
}

// searchResult is one result of people.searchContacts.
type searchResult struct {
	Person person `json:"person"`
}

// search is one people.searchContacts call.
func (p People) search(ctx context.Context, access, query, readMask string) ([]searchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = PeopleEndpoint
	}
	params := url.Values{"query": {query}, "readMask": {readMask}, "pageSize": {searchPageSize}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/v1/people:searchContacts?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: callTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Google's People API: %w", transportOnly(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading Google's People API answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var refused struct {
			Error struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &refused)
		return nil, fmt.Errorf("Google's People API refused the lookup (HTTP %d): %s", resp.StatusCode, refusal(refused.Error.Status, refused.Error.Message))
	}
	var answer struct {
		Results []searchResult `json:"results"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, errors.New("Google's People API answered something that is not its JSON")
	}
	return answer.Results, nil
}

// transportOnly is a transport error without the request's address, which
// carries the search name in its query.
func transportOnly(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
