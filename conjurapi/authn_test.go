package conjurapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyberark/conjur-api-go/conjurapi/authn"
	"github.com/stretchr/testify/assert"
)

var sample_token = `{"protected":"eyJhbGciOiJjb25qdXIub3JnL3Nsb3NpbG8vdjIiLCJraWQiOiI5M2VjNTEwODRmZTM3Zjc3M2I1ODhlNTYyYWVjZGMxMSJ9","payload":"eyJzdWIiOiJhZG1pbiIsImlhdCI6MTUxMDc1MzI1OSwiZXhwIjo0MTAzMzc5MTY0fQo=","signature":"raCufKOf7sKzciZInQTphu1mBbLhAdIJM72ChLB4m5wKWxFnNz_7LawQ9iYEI_we1-tdZtTXoopn_T1qoTplR9_Bo3KkpI5Hj3DB7SmBpR3CSRTnnEwkJ0_aJ8bql5Cbst4i4rSftyEmUqX-FDOqJdAztdi9BUJyLfbeKTW9OGg-QJQzPX1ucB7IpvTFCEjMoO8KUxZpbHj-KpwqAMZRooG4ULBkxp5nSfs-LN27JupU58oRgIfaWASaDmA98O2x6o88MFpxK_M0FeFGuDKewNGrRc8lCOtTQ9cULA080M5CSnruCqu1Qd52r72KIOAfyzNIiBCLTkblz2fZyEkdSKQmZ8J3AakxQE2jyHmMT-eXjfsEIzEt-IRPJIirI3Qm"}`
var expired_token = `{"protected":"eyJhbGciOiJjb25qdXIub3JnL3Nsb3NpbG8vdjIiLCJraWQiOiI5M2VjNTEwODRmZTM3Zjc3M2I1ODhlNTYyYWVjZGMxMSJ9","payload":"eyJzdWIiOiJhZG1pbiIsImlhdCI6MTUxMDc1MzI1OX0=","signature":"raCufKOf7sKzciZInQTphu1mBbLhAdIJM72ChLB4m5wKWxFnNz_7LawQ9iYEI_we1-tdZtTXoopn_T1qoTplR9_Bo3KkpI5Hj3DB7SmBpR3CSRTnnEwkJ0_aJ8bql5Cbst4i4rSftyEmUqX-FDOqJdAztdi9BUJyLfbeKTW9OGg-QJQzPX1ucB7IpvTFCEjMoO8KUxZpbHj-KpwqAMZRooG4ULBkxp5nSfs-LN27JupU58oRgIfaWASaDmA98O2x6o88MFpxK_M0FeFGuDKewNGrRc8lCOtTQ9cULA080M5CSnruCqu1Qd52r72KIOAfyzNIiBCLTkblz2fZyEkdSKQmZ8J3AakxQE2jyHmMT-eXjfsEIzEt-IRPJIirI3Qm"}`

type rotateAPIKeyTestCase struct {
	name             string
	roleId           string
	login            string
	readResponseBody bool
}

func TestClient_RotateAPIKey(t *testing.T) {
	testCases := []rotateAPIKeyTestCase{
		{
			name:             "Rotate the API key of a foreign user role of kind user",
			roleId:           "cucumber:user:alice",
			login:            "alice",
			readResponseBody: false,
		},
		{
			name:             "Rotate the API key of a foreign role of non-user kind",
			roleId:           "cucumber:host:bob",
			login:            "host/bob",
			readResponseBody: false,
		},
		{
			name:             "Rotate the API key of a foreign role and read the data stream",
			roleId:           "cucumber:user:alice",
			login:            "alice",
			readResponseBody: true,
		},
		{
			name:             "Rotate the API key of a partially-qualified role and read the data stream",
			roleId:           "user:alice",
			login:            "alice",
			readResponseBody: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// SETUP
			conjur, err := conjurSetup(&Config{}, defaultTestPolicy)
			assert.NoError(t, err)

			// EXERCISE
			runRotateAPIKeyAssertions(t, tc, conjur)
		})
	}
}

func runRotateAPIKeyAssertions(t *testing.T, tc rotateAPIKeyTestCase, conjur *Client) {
	var userApiKey []byte
	var err error

	if tc.readResponseBody {
		rotateResponse, e := conjur.RotateAPIKeyReader("cucumber:user:alice")
		assert.NoError(t, e)
		userApiKey, err = ReadResponseBody(rotateResponse)
	} else {
		userApiKey, err = conjur.RotateAPIKey(tc.roleId)
	}

	assert.NoError(t, err)

	_, err = conjur.Authenticate(authn.LoginPair{Login: tc.login, APIKey: string(userApiKey)})
	assert.NoError(t, err)
}

func TestClient_RotateCurrentUserAPIKey(t *testing.T) {
	//TODO: This test is ugly. Refactor it into something more concise.
	t.Run("Rotate the API key of the current user", func(t *testing.T) {
		// SETUP
		// Login as admin and rotate alice's API key,
		// so we can log in as her with her new API key
		conjur, err := conjurSetup(&Config{}, defaultTestPolicy)
		assert.NoError(t, err)
		userApiKey, err := conjur.RotateAPIKey("cucumber:user:alice")
		assert.NoError(t, err)

		// Login as alice with a mock storage provider to store her API key
		config := &Config{}
		config.mergeEnv()
		conjur, err = NewClientFromKey(*config, authn.LoginPair{Login: "alice", APIKey: string(userApiKey)})
		assert.NoError(t, err)
		conjur.storage = &mockStorageProvider{}
		_, err = conjur.Login("alice", string(userApiKey))
		assert.NoError(t, err)

		// EXERCISE
		// This will use the "stored" API key to rotate alice's API key
		newAPIKey, err := conjur.RotateCurrentUserAPIKey()
		assert.NoError(t, err)

		// VERIFY
		// Ensure the new API key works
		_, err = conjur.Authenticate(authn.LoginPair{Login: "alice", APIKey: string(newAPIKey)})
		assert.NoError(t, err)
	})
}

type rotateHostAPIKeyTestCase struct {
	name       string
	hostID     string
	login      string
	assertions func(t *testing.T, tc rotateHostAPIKeyTestCase, conjur *Client)
}

func TestClient_RotateHostAPIKey(t *testing.T) {
	testCases := []rotateHostAPIKeyTestCase{
		{
			name:       "Rotate the API key of a foreign host: ID only",
			hostID:     "bob",
			login:      "host/bob",
			assertions: runRotateHostAPIKeyAssertions,
		},
		{
			name:       "Rotate the API key of a foreign host: partially qualified",
			hostID:     "host:bob",
			login:      "host/bob",
			assertions: runRotateHostAPIKeyAssertions,
		},
		{
			name:       "Rotate the API key of a foreign host: fully qualified",
			hostID:     "cucumber:host:bob",
			login:      "host/bob",
			assertions: runRotateHostAPIKeyAssertions,
		},
		{
			name:   "Rotate the API key of a foreign host: wrong role kind",
			hostID: "user:alice",
			assertions: func(t *testing.T, tc rotateHostAPIKeyTestCase, conjur *Client) {
				_, err := conjur.RotateHostAPIKey(tc.hostID)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "must represent a host")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// SETUP
			conjur, err := conjurSetup(&Config{}, defaultTestPolicy)
			assert.NoError(t, err)

			// EXERCISE
			tc.assertions(t, tc, conjur)
		})
	}
}

func runRotateHostAPIKeyAssertions(t *testing.T, tc rotateHostAPIKeyTestCase, conjur *Client) {
	var hostAPIKey []byte
	var err error

	hostAPIKey, err = conjur.RotateHostAPIKey(tc.hostID)

	assert.NoError(t, err)

	_, err = conjur.Authenticate(authn.LoginPair{Login: tc.login, APIKey: string(hostAPIKey)})
	assert.NoError(t, err)
}

// This is probably redundant with the above test case. Just going to keep them
// separate for expediency for now.
type rotateUserAPIKeyTestCase struct {
	name       string
	userID     string
	login      string
	assertions func(t *testing.T, tc rotateUserAPIKeyTestCase, conjur *Client)
}

func TestClient_RotateUserAPIKey(t *testing.T) {
	testCases := []rotateUserAPIKeyTestCase{
		{
			name:       "Rotate the API key of a user: ID only",
			userID:     "alice",
			login:      "alice",
			assertions: runRotateUserAPIKeyAssertions,
		},
		{
			name:       "Rotate the API key of a user: partially qualified",
			userID:     "user:alice",
			login:      "alice",
			assertions: runRotateUserAPIKeyAssertions,
		},
		{
			name:       "Rotate the API key of a user: fully qualified",
			userID:     "cucumber:user:alice",
			login:      "alice",
			assertions: runRotateUserAPIKeyAssertions,
		},
		{
			name:   "Rotate the API key of a user: wrong role kind",
			userID: "host:bob",
			assertions: func(t *testing.T, tc rotateUserAPIKeyTestCase, conjur *Client) {
				_, err := conjur.RotateUserAPIKey(tc.userID)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "must represent a user")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// SETUP
			conjur, err := conjurSetup(&Config{}, defaultTestPolicy)
			assert.NoError(t, err)

			// EXERCISE
			tc.assertions(t, tc, conjur)
		})
	}
}

func runRotateUserAPIKeyAssertions(t *testing.T, tc rotateUserAPIKeyTestCase, conjur *Client) {
	var userAPIKey []byte
	var err error

	userAPIKey, err = conjur.RotateUserAPIKey(tc.userID)

	assert.NoError(t, err)

	_, err = conjur.Authenticate(authn.LoginPair{Login: tc.login, APIKey: string(userAPIKey)})
	assert.NoError(t, err)
}

func TestClient_Whoami(t *testing.T) {
	t.Run("Whoami", func(t *testing.T) {
		conjur, err := conjurSetup(&Config{}, defaultTestPolicy)
		assert.NoError(t, err)

		resp, err := conjur.WhoAmI()
		assert.NoError(t, err)

		respStr := string(resp)
		assert.Contains(t, respStr, `"account":"cucumber"`)
		assert.Contains(t, respStr, `"username":"admin"`)
	})
}

func TestClient_ListOidcProviders(t *testing.T) {
	t.Run("List OIDC Providers", func(t *testing.T) {
		ts, client := setupTestClient(t)
		defer ts.Close()

		providers, err := client.ListOidcProviders()
		assert.NoError(t, err)

		assert.Equal(t, 1, len(providers))
		assert.Equal(t, "test-service-id", providers[0].ServiceID)
	})
}

func TestClient_Login(t *testing.T) {
	t.Run("Login and Authenticate", func(t *testing.T) {
		ts, client := setupTestClient(t)
		defer ts.Close()

		token, err := client.Login("alice", "password")
		assert.NoError(t, err)
		assert.Equal(t, "test-api-key", string(token))

		// Check that api key was cached to the correct location
		contents, err := os.ReadFile(client.GetConfig().NetRCPath)
		assert.NoError(t, err)
		assert.Contains(t, string(contents), client.GetConfig().ApplianceURL+"/authn")
		assert.Contains(t, string(contents), "test-api-key")

		// Check that we can authenticate with the cached api key
		token, err = client.Authenticate(authn.LoginPair{Login: "alice", APIKey: string(token)})
		assert.NoError(t, err)
		assert.Equal(t, "test-token", string(token))
	})

	t.Run("OIDC authentication", func(t *testing.T) {
		ts, client := setupTestClient(t)
		defer ts.Close()

		client.config.AuthnType = "oidc"
		client.config.ServiceID = "test-service-id"

		storage, err := createStorageProvider(client.config)
		assert.NoError(t, err)
		client.storage = storage

		token, err := client.OidcAuthenticate("code", "nonce", "code-verifier")
		assert.NoError(t, err)
		assert.Equal(t, "test-token-oidc", string(token))

		// Check that token was cached to the correct location
		contents, err := os.ReadFile(client.GetConfig().NetRCPath)
		assert.NoError(t, err)
		assert.Contains(t, string(contents), client.GetConfig().ApplianceURL+"/authn-oidc/test-service-id")
		assert.Contains(t, string(contents), "test-token-oidc")
	})
}

func TestClient_AuthenticateReader(t *testing.T) {
	t.Run("Retrieves access token reader", func(t *testing.T) {
		ts, client := setupTestClient(t)
		defer ts.Close()

		reader, err := client.AuthenticateReader(authn.LoginPair{Login: "alice", APIKey: "test-api-key"})
		assert.NoError(t, err)
		token, err := ReadResponseBody(reader)
		assert.NoError(t, err)
		assert.Equal(t, "test-token", string(token))
	})
}

type mockStorageProvider struct {
	username    string
	password    string
	injectError error
	purgeCalled bool
}

func (m *mockStorageProvider) ReadCredentials() (string, string, error) {
	return m.username, m.password, m.injectError
}

func (m *mockStorageProvider) StoreCredentials(username, password string) error {
	m.username = username
	m.password = password
	return m.injectError
}

func (m *mockStorageProvider) StoreAuthnToken(token []byte) error {
	return m.StoreCredentials("", string(token))
}

func (m *mockStorageProvider) ReadAuthnToken() ([]byte, error) {
	_, token, err := m.ReadCredentials()
	return []byte(token), err
}

func (m *mockStorageProvider) PurgeCredentials() error {
	m.purgeCalled = true
	m.username = ""
	m.password = ""
	return m.injectError
}

func TestClient_PurgeCredentials(t *testing.T) {
	client := &Client{
		config: Config{
			Account:      "cucumber",
			ApplianceURL: "https://conjur",
		},
		httpClient: &http.Client{},
		storage:    &mockStorageProvider{},
	}

	t.Run("Calls storage provider's PurgeCredentials", func(t *testing.T) {
		err := client.PurgeCredentials()
		assert.NoError(t, err)
		assert.True(t, client.storage.(*mockStorageProvider).purgeCalled)
	})

	t.Run("Returns error if storage provider returns error", func(t *testing.T) {
		client.storage.(*mockStorageProvider).injectError = errors.New("error")
		err := client.PurgeCredentials()
		assert.EqualError(t, err, "error")
	})

	t.Run("Does nothing if storage provider is nil", func(t *testing.T) {
		client.storage = nil
		err := client.PurgeCredentials()
		assert.NoError(t, err)
	})
}

func TestPurgeCredentials(t *testing.T) {
	// Test the PurgeCredentials function which doesn't require a client

	t.Run("Purges credentials from netrc", func(t *testing.T) {
		tempDir := t.TempDir()
		config := Config{
			Account:           "cucumber",
			ApplianceURL:      "https://conjur",
			NetRCPath:         filepath.Join(tempDir, ".netrc"),
			CredentialStorage: "file",
		}

		initialContent := `
machine https://conjur/authn
	login cucumber
	password test-api-key`

		err := os.WriteFile(config.NetRCPath, []byte(initialContent), 0600)
		assert.NoError(t, err)

		err = PurgeCredentials(config)
		assert.NoError(t, err)

		contents, err := os.ReadFile(config.NetRCPath)
		assert.NoError(t, err)
		assert.NotContains(t, string(contents), "https://conjur/authn")
		assert.NotContains(t, string(contents), "cucumber")
		assert.NotContains(t, string(contents), "test-api-key")
	})

	t.Run("Doesn't fail when not storing credentials", func(t *testing.T) {
		config := Config{
			Account:           "cucumber",
			ApplianceURL:      "https://conjur",
			CredentialStorage: "none",
		}
		err := PurgeCredentials(config)
		assert.NoError(t, err)
	})

	t.Run("Returns error for unrecognized storage provider", func(t *testing.T) {
		config := Config{
			Account:           "cucumber",
			ApplianceURL:      "https://conjur",
			CredentialStorage: "invalid",
		}
		err := PurgeCredentials(config)
		assert.EqualError(t, err, "Unknown credential storage type")
	})
}

func TestClient_InternalAuthenticate(t *testing.T) {
	config := Config{
		Account:      "cucumber",
		ApplianceURL: "https://conjur",
	}

	t.Run("Returns error if no authenticator", func(t *testing.T) {
		client, err := NewClient(config)
		assert.NoError(t, err)

		_, err = client.InternalAuthenticate()
		assert.EqualError(t, err, "unable to authenticate using client without authenticator")
	})

	t.Run("Returns token from authenticator", func(t *testing.T) {
		client, err := NewClient(config)
		assert.NoError(t, err)

		client.authenticator = &authn.TokenAuthenticator{Token: "test-token"}
		token, err := client.InternalAuthenticate()
		assert.NoError(t, err)
		assert.Equal(t, "test-token", string(token))
	})

	t.Run("Returns error if authenticator returns error", func(t *testing.T) {
		client, err := NewClient(config)
		assert.NoError(t, err)

		client.authenticator = &authn.OidcAuthenticator{
			Authenticate: func(code, noce, code_verifier string) ([]byte, error) {
				return nil, errors.New("error")
			},
		}
		_, err = client.InternalAuthenticate()
		assert.EqualError(t, err, "error")
	})

	t.Run("Returns token when using OIDC", func(t *testing.T) {
		token, err := runOIDCInternalAuthenticateTest(t, sample_token, nil)
		assert.NoError(t, err)
		assert.Equal(t, sample_token, string(token))
	})

	t.Run("Returns re-login message when using OIDC and token is expired", func(t *testing.T) {
		_, err := runOIDCInternalAuthenticateTest(t, expired_token, nil)
		assert.EqualError(t, err, "No valid OIDC token found. Please login again.")
	})

	t.Run("Returns error if storage returns error", func(t *testing.T) {
		_, err := runOIDCInternalAuthenticateTest(t, "", errors.New("error"))
		assert.EqualError(t, err, "No valid OIDC token found. Please login again.")
	})
}

func TestClient_RefreshToken(t *testing.T) {
	config := Config{
		Account:      "cucumber",
		ApplianceURL: "https://conjur",
	}

	t.Run("Updates token from authenticator", func(t *testing.T) {
		client, err := NewClient(config)
		assert.NoError(t, err)

		client.authenticator = &authn.TokenAuthenticator{Token: sample_token}
		err = client.RefreshToken()
		assert.NoError(t, err)
		assert.Equal(t, sample_token, string(client.authToken.Raw()))
	})

	t.Run("Doesn't update token from authenticator when not required", func(t *testing.T) {
		client, err := NewClient(config)
		assert.NoError(t, err)

		// Set token so that it doesn't need to be refreshed
		client.authToken, err = authn.NewToken([]byte(sample_token))
		assert.NoError(t, err)

		// Change authenticator token so that it doesn't match the token in the client
		client.authenticator = &authn.TokenAuthenticator{Token: "test-token"}

		// Call RefreshToken and verify that the token in the client is not updated
		err = client.RefreshToken()
		assert.NoError(t, err)
		assert.Equal(t, sample_token, string(client.authToken.Raw()))
	})

	t.Run("Returns error when authenticator returns invalid token", func(t *testing.T) {
		client, err := NewClient(config)
		assert.NoError(t, err)

		client.authenticator = &authn.TokenAuthenticator{Token: "invalid-token"}
		err = client.RefreshToken()
		assert.Error(t, err)
	})

	t.Run("Retrieves cached token when using OIDC", func(t *testing.T) {
		client, err := NewClient(Config{
			Account:      "cucumber",
			ApplianceURL: "https://conjur",
			AuthnType:    "oidc",
			ServiceID:    "test-service",
		})
		assert.NoError(t, err)

		client.storage = &mockStorageProvider{
			password: sample_token,
		}
		client.authenticator = &authn.OidcAuthenticator{}
		err = client.RefreshToken()

		assert.NoError(t, err)
		assert.Equal(t, sample_token, string(client.authToken.Raw()))
	})
}

func TestClient_ForceRefreshToken(t *testing.T) {
	config := Config{
		Account:      "cucumber",
		ApplianceURL: "https://conjur",
	}

	t.Run("Forces update of token from authenticator", func(t *testing.T) {
		client, err := NewClient(config)
		assert.NoError(t, err)

		// Set token so that it doesn't need to be refreshed
		client.authToken, err = authn.NewToken([]byte(sample_token))
		assert.NoError(t, err)

		// Change authenticator token so that it doesn't match the token in the client
		client.authenticator = &authn.TokenAuthenticator{Token: expired_token}

		// Call ForceRefreshToken and verify that the token in the client is updated
		err = client.ForceRefreshToken()
		assert.NoError(t, err)
		assert.Equal(t, expired_token, string(client.authToken.Raw()))
	})
}

func runOIDCInternalAuthenticateTest(t *testing.T, token string, injectErr error) ([]byte, error) {
	client, err := NewClient(Config{
		Account:      "cucumber",
		ApplianceURL: "https://conjur",
		AuthnType:    "oidc",
		ServiceID:    "test-service",
	})
	assert.NoError(t, err)

	client.storage = &mockStorageProvider{
		password:    token,
		injectError: injectErr,
	}
	client.authenticator = &authn.OidcAuthenticator{}
	return client.InternalAuthenticate()
}

func setupTestClient(t *testing.T) (*httptest.Server, *Client) {
	mockConjurServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Listen for the login, authenticate, and oidc endpoints and return test values
		if strings.HasSuffix(r.URL.Path, "/authn/cucumber/login") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test-api-key"))
		} else if strings.HasSuffix(r.URL.Path, "/authn/cucumber/alice/authenticate") {
			// Ensure that the api key we returned in /login is being used
			body, _ := io.ReadAll(r.Body)
			if string(body) == "test-api-key" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("test-token"))
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}
		} else if strings.HasSuffix(r.URL.Path, "/authn-oidc/test-service-id/cucumber/authenticate") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test-token-oidc"))
		} else if strings.HasSuffix(r.URL.Path, "/authn-oidc/cucumber/providers") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"service_id": "test-service-id"}]`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	tempDir := t.TempDir()
	config := Config{
		Account:           "cucumber",
		ApplianceURL:      mockConjurServer.URL,
		NetRCPath:         filepath.Join(tempDir, ".netrc"),
		CredentialStorage: "file",
	}
	storage, _ := createStorageProvider(config)
	client := &Client{
		config:     config,
		httpClient: &http.Client{},
		storage:    storage,
	}

	return mockConjurServer, client
}

type changeUserPasswordTestCase struct {
	name        string
	userID      string
	login       string
	newPassword string
}

func TestClient_ChangeUserPassword(t *testing.T) {
	testCases := []changeUserPasswordTestCase{
		{
			name:        "Change the password of a user",
			userID:      "alice",
			login:       "alice",
			newPassword: "SUp3r$3cr3t!!",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// SETUP
			config := &Config{
				CredentialStorage: "none",
			}
			conjur, err := conjurSetup(config, defaultTestPolicy)
			assert.NoError(t, err)

			// EXERCISE
			runChangeUserPasswordAssertions(t, tc, conjur)
		})
	}
}

func runChangeUserPasswordAssertions(t *testing.T, tc changeUserPasswordTestCase, conjur *Client) {
	var userAPIKey []byte
	var err error

	userAPIKey, err = conjur.RotateUserAPIKey(tc.userID)

	_, err = conjur.ChangeUserPassword(tc.login, string(userAPIKey), tc.newPassword)
	assert.NoError(t, err)

	userAPIKey, err = conjur.Login(tc.login, tc.newPassword)
	assert.NoError(t, err)

	_, err = conjur.Authenticate(authn.LoginPair{Login: tc.login, APIKey: string(userAPIKey)})
	assert.NoError(t, err)
}

type changeCurrentUserPasswordTestCase struct {
	name        string
	newPassword string
}

func TestClient_ChangeCurrentUserPassword(t *testing.T) {
	testCases := []changeCurrentUserPasswordTestCase{
		{
			name:        "Change the password of a user",
			newPassword: "SUp3r$3cr3t!!",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// SETUP
			tempDir := t.TempDir()
			config := &Config{
				NetRCPath:         filepath.Join(tempDir, ".netrc"),
				CredentialStorage: "file",
			}
			conjur, err := conjurSetup(config, defaultTestPolicy)
			assert.NoError(t, err)

			// EXERCISE
			runChangeCurrentUserPasswordAssertions(t, tc, conjur)
		})
	}
}

func runChangeCurrentUserPasswordAssertions(t *testing.T, tc changeCurrentUserPasswordTestCase, conjur *Client) {
	var userAPIKey []byte
	var err error

	userAPIKey, err = conjur.RotateUserAPIKey("alice")

	// Create empty netrc file, then login to write user credentials
	err = os.WriteFile(conjur.config.NetRCPath, []byte(""), 0600)
	assert.NoError(t, err)
	conjur.Login("alice", string(userAPIKey))

	// Change the user password, then login + authenticate to test the new password
	_, err = conjur.ChangeCurrentUserPassword(tc.newPassword)
	assert.NoError(t, err)

	userAPIKey, err = conjur.Login("alice", tc.newPassword)
	assert.NoError(t, err)

	_, err = conjur.Authenticate(authn.LoginPair{Login: "alice", APIKey: string(userAPIKey)})
	assert.NoError(t, err)
}

var publicKeysTestPolicy = `
- !user
  id: alice
  public_keys:
  - ssh-rsa test-key-1 laptop
  - ssh-rsa test-key-2 workstation
`

type publicKeysTestCase struct {
	name       string
	kind       string
	identifier string
}

func TestClient_PublicKeys(t *testing.T) {
	testCases := []publicKeysTestCase{
		{
			name:       "Display public keys",
			kind:       "user",
			identifier: "alice",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// SETUP
			config := &Config{
				CredentialStorage: "none",
			}
			conjur, err := conjurSetup(config, publicKeysTestPolicy)
			assert.NoError(t, err)

			// EXERCISE
			runPublicKeysAssertions(t, tc, conjur)
		})
	}
}

func runPublicKeysAssertions(t *testing.T, tc publicKeysTestCase, conjur *Client) {
	var publicKeys []byte
	var err error

	publicKeys, err = conjur.PublicKeys(tc.kind, tc.identifier)

	assert.NoError(t, err)

	expectedOutput := `ssh-rsa test-key-1 laptop
ssh-rsa test-key-2 workstation
`

	assert.Equal(t, expectedOutput, string(publicKeys))
}
