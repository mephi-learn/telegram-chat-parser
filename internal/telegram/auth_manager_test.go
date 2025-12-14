package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Моки
type mockAuthenticator struct {
	mock.Mock
}

func (m *mockAuthenticator) Phone(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockAuthenticator) Password(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockAuthenticator) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	args := m.Called(ctx, tos)
	return args.Error(0)
}
func (m *mockAuthenticator) SignUp(ctx context.Context) (auth.UserInfo, error) {
	args := m.Called(ctx)
	return auth.UserInfo{}, args.Error(1)
}
func (m *mockAuthenticator) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	args := m.Called(ctx, sentCode)
	return args.String(0), args.Error(1)
}

var _ auth.UserAuthenticator = (*mockAuthenticator)(nil)

type mockAPIClient struct {
	mock.Mock
}

func (m *mockAPIClient) UsersGetUsers(ctx context.Context, request []tg.InputUserClass) ([]tg.UserClass, error) {
	args := m.Called(ctx, request)
	if res := args.Get(0); res != nil {
		return res.([]tg.UserClass), args.Error(1)
	}
	return nil, args.Error(1)
}

type mockAuthClient struct {
	mock.Mock
}

func (m *mockAuthClient) IfNecessary(ctx context.Context, flow auth.Flow) error {
	args := m.Called(ctx, flow)
	return args.Error(0)
}

type mockTgClient struct {
	mock.Mock
	api  APIClient
	auth AuthClient
}

func (m *mockTgClient) Run(ctx context.Context, f func(ctx context.Context) error) error {
	err := f(ctx)
	args := m.Called(ctx, f)
	if runErr := args.Error(0); runErr != nil {
		return runErr
	}
	return err
}
func (m *mockTgClient) API() APIClient   { return m.api }
func (m *mockTgClient) Auth() AuthClient { return m.auth }

func TestAuthManager_RunAuthenticatedClient(t *testing.T) {
	ctx := context.Background()

	setup := func() (*AuthManager, *mockTgClient, *mockAPIClient, *mockAuthClient) {
		apiMock := new(mockAPIClient)
		authMock := new(mockAuthClient)
		clientMock := &mockTgClient{
			api:  apiMock,
			auth: authMock,
		}

		am := NewAuthManager(1, "hash", "phone", "session.json")
		am.newClient = func(appID int, appHash string, opts telegram.Options) tgClient {
			return clientMock
		}
		am.isTerminal = func(fd int) bool { return true }
		am.newAuthFlow = func(a auth.UserAuthenticator, opts auth.SendCodeOptions) auth.Flow {
			return auth.Flow{}
		}
		am.authenticator = new(mockAuthenticator)

		return am, clientMock, apiMock, authMock
	}

	t.Run("Сессия действительна", func(t *testing.T) {
		am, clientMock, apiMock, _ := setup()

		apiMock.On("UsersGetUsers", mock.Anything, mock.Anything).Return([]tg.UserClass{&tg.User{}}, nil)
		clientMock.On("Run", mock.Anything, mock.Anything).Return(nil)

		err := am.RunAuthenticatedClient(ctx, nil) // Передаем nil обратный вызов

		assert.NoError(t, err)
		apiMock.AssertExpectations(t)
	})

	t.Run("Интерактивная аутентификация успешна", func(t *testing.T) {
		am, clientMock, apiMock, authMock := setup()

		apiMock.On("UsersGetUsers", mock.Anything, mock.Anything).Return(nil, errors.New("auth error")).Once()
		authMock.On("IfNecessary", mock.Anything, mock.Anything).Return(nil)
		clientMock.On("Run", mock.Anything, mock.Anything).Return(nil)

		err := am.RunAuthenticatedClient(ctx, nil)

		assert.NoError(t, err)
		apiMock.AssertExpectations(t)
		authMock.AssertExpectations(t)
	})

	t.Run("Интерактивная аутентификация не удалась", func(t *testing.T) {
		am, clientMock, apiMock, authMock := setup()
		authErr := errors.New("interactive auth failed")

		apiMock.On("UsersGetUsers", mock.Anything, mock.Anything).Return(nil, errors.New("auth error")).Once()
		authMock.On("IfNecessary", mock.Anything, mock.Anything).Return(authErr)
		clientMock.On("Run", mock.Anything, mock.Anything).Return(authErr)

		err := am.RunAuthenticatedClient(ctx, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), authErr.Error())
		authMock.AssertExpectations(t)
	})

	t.Run("Не терминал", func(t *testing.T) {
		am, clientMock, apiMock, _ := setup()
		am.isTerminal = func(fd int) bool { return false }

		apiMock.On("UsersGetUsers", mock.Anything, mock.Anything).Return(nil, errors.New("auth error"))
		clientMock.On("Run", mock.Anything, mock.Anything).Return(nil)

		err := am.RunAuthenticatedClient(ctx, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "невозможно запустить интерактивную аутентификацию в неинтерактивном терминале")
	})
}
