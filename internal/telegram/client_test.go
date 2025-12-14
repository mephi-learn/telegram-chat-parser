package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

type mockTelegramAPI struct {
	mock.Mock
}

func (m *mockTelegramAPI) UsersGetUsers(ctx context.Context, req []tg.InputUserClass) ([]tg.UserClass, error) {
	args := m.Called(ctx, req)
	res, _ := args.Get(0).([]tg.UserClass)
	return res, args.Error(1)
}

func (m *mockTelegramAPI) ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	args := m.Called(ctx, req)
	res, _ := args.Get(0).(*tg.ContactsResolvedPeer)
	return res, args.Error(1)
}

func (m *mockTelegramAPI) UsersGetFullUser(ctx context.Context, req tg.InputUserClass) (*tg.UsersUserFull, error) {
	args := m.Called(ctx, req)
	res, _ := args.Get(0).(*tg.UsersUserFull)
	return res, args.Error(1)
}

func (m *mockTelegramAPI) HelpGetConfig(ctx context.Context) (*tg.Config, error) {
	args := m.Called(ctx)
	res, _ := args.Get(0).(*tg.Config)
	return res, args.Error(1)
}

type mockTelegramRunner struct {
	mock.Mock
	api *mockTelegramAPI
}

func newMockTelegramRunner() *mockTelegramRunner {
	return &mockTelegramRunner{
		api: new(mockTelegramAPI),
	}
}

func (m *mockTelegramRunner) Run(ctx context.Context, f func(ctx context.Context) error) error {
	// This implementation manually handles the case of a function as a return value.
	// This is a workaround for a subtle issue where the mock framework doesn't seem
	// to evaluate the return function automatically in this specific test setup.
	args := m.Called(ctx, f)

	retVal := args.Get(0)
	if retFunc, ok := retVal.(func(context.Context, func(context.Context) error) error); ok {
		// If the return argument is a function with the correct signature, execute it.
		return retFunc(ctx, f)
	}

	// Otherwise, treat it as a regular error value.
	return args.Error(0)
}

func (m *mockTelegramRunner) API() telegramAPI {
	return m.api
}

func (m *mockTelegramRunner) Auth() telegramAuth {
	return nil
}

type mockAuthFlow struct {
	mock.Mock
}

func (m *mockAuthFlow) Run(ctx context.Context, client auth.FlowClient) error {
	args := m.Called(ctx, client)
	return args.Error(0)
}

// --- Test Clock ---

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func newManualClock(t time.Time) *manualClock {
	return &manualClock{now: t}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// --- Helper to create a test client ---

func newTestClient(t *testing.T) (*Client, *mockTelegramRunner, *mockAuthFlow, *manualClock) {
	runner := newMockTelegramRunner()
	authFlow := new(mockAuthFlow)
	clock := newManualClock(time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	client := &Client{
		id:             "test-client",
		tgRunner:       runner,
		authFlow:       authFlow,
		isTerminal:     func(fd int) bool { return true }, // Assume interactive for tests
		clock:          clock.Now,
		log:            logger,
		mu:             sync.RWMutex{},
		unhealthyUntil: time.Time{},
		runErr:         make(chan error, 1),
	}

	// No default auth check mock anymore. It's context-dependent.

	return client, runner, authFlow, clock
}

// --- Tests ---

func TestClient_HappyPath(t *testing.T) {
	client, runner, _, _ := newTestClient(t)
	ctx := context.Background()

	// For a simple health check, we don't need the runner to do anything complex.
	// The `do` method will just execute the function.
	runner.api.On("HelpGetConfig", ctx).Return(&tg.Config{}, nil).Once()

	err := client.Health(ctx)
	require.NoError(t, err)

	runner.api.AssertExpectations(t)
}

func TestClient_FloodWait_BlocksRequests(t *testing.T) {
	client, runner, _, clock := newTestClient(t)
	ctx := context.Background()

	// 1. First call gets a FLOOD_WAIT error
	floodWaitErr := errors.New("RPC_ERROR_420: FLOOD_WAIT (60)")
	runner.api.On("HelpGetConfig", ctx).Return(nil, floodWaitErr).Once()

	err := client.Health(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FLOOD_WAIT (60)")

	// 2. Check internal state
	require.True(t, client.unhealthyUntil.After(clock.Now()))

	// 3. Second call should be blocked immediately
	err = client.Health(ctx)
	require.ErrorIs(t, err, ErrFloodWaitActive)

	// 4. Advance time, but not enough
	clock.Advance(30 * time.Second)
	err = client.Health(ctx)
	require.ErrorIs(t, err, ErrFloodWaitActive)

	// 5. Advance time past the flood wait period
	clock.Advance(31 * time.Second)

	// Now the call should go through again. No new auth check is expected here.
	runner.api.On("HelpGetConfig", ctx).Return(&tg.Config{}, nil).Once()

	err = client.Health(ctx)
	require.NoError(t, err)

	runner.api.AssertExpectations(t)
}

func TestClient_HealthCheckItselfGetsFloodWait(t *testing.T) {
	client, runner, _, clock := newTestClient(t)
	ctx := context.Background()

	// 1. First health check gets flood wait
	floodWaitErr60 := errors.New("RPC_ERROR_420: FLOOD_WAIT (60)")
	runner.api.On("HelpGetConfig", ctx).Return(nil, floodWaitErr60).Once()

	err := client.Health(ctx)
	require.Error(t, err)
	require.True(t, client.unhealthyUntil.Equal(clock.Now().Add(60*time.Second)))

	// 2. Advance time past the first wait
	clock.Advance(61 * time.Second)

	// 3. Second health check also gets a flood wait, for a different duration
	floodWaitErr30 := errors.New("RPC_ERROR_420: FLOOD_WAIT (30)")
	runner.api.On("HelpGetConfig", ctx).Return(nil, floodWaitErr30).Once()

	err = client.Health(ctx)
	require.Error(t, err)

	// 4. Verify that the unhealthy time has been *updated* based on the new error
	require.True(t, client.unhealthyUntil.Equal(clock.Now().Add(30*time.Second)))

	runner.api.AssertExpectations(t)
}

func TestClient_Authentication_Required(t *testing.T) {
	client, runner, authFlow, _ := newTestClient(t)
	ctx := context.Background()

	// 1. Initial session check fails
	runner.api.On("UsersGetUsers", mock.Anything, mock.Anything).Return(nil, errors.New("auth session invalid")).Once()
	// 2. Interactive auth flow is triggered and succeeds
	authFlow.On("Run", mock.Anything, mock.Anything).Return(nil).Once()
	// 3. The runner executes the function passed to it (via .Run hook) and returns no error.
	runner.On("Run", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			f := args.Get(1).(func(context.Context) error)
			// We don't care about the error here, just that it runs.
			_ = f(args.Get(0).(context.Context))
		}).
		Return(nil).
		Once()

	// We call Start, which contains the auth logic.
	client.Start(ctx)

	// Wait for the startup logic to complete.
	// In a real scenario, we might have a better synchronization mechanism.
	time.Sleep(50 * time.Millisecond)

	// Verify mocks
	runner.api.AssertExpectations(t)
	authFlow.AssertExpectations(t)
	runner.AssertExpectations(t)
}

func TestClient_Authentication_Fails(t *testing.T) {
	client, runner, authFlow, _ := newTestClient(t)
	ctx := context.Background()

	// 1. Initial session check fails
	runner.api.On("UsersGetUsers", mock.Anything, mock.Anything).Return(nil, errors.New("auth session invalid")).Once()
	// 2. Interactive auth flow also fails
	authErr := errors.New("user entered wrong code")
	authFlow.On("Run", mock.Anything, mock.Anything).Return(authErr).Once()

	// The runner's Run method will now return an error because the setup function failed
	var runErr error
	runner.On("Run", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			f := args.Get(1).(func(ctx context.Context) error)
			runErr = f(ctx) // Execute f and capture its error
		}).
		Return(func(context.Context, func(context.Context) error) error {
			return runErr // Return the captured error
		}).
		Once()

	client.Start(ctx)
	err := <-client.runErr // Get the error from the client's error channel

	require.Error(t, err)
	require.ErrorContains(t, err, "interactive auth failed: user entered wrong code")

	authFlow.AssertExpectations(t)
}

func TestClient_NonInteractiveAuthFails(t *testing.T) {
	client, runner, authFlow, _ := newTestClient(t)
	ctx := context.Background()
	client.isTerminal = func(fd int) bool { return false } // Set to non-interactive

	// 1. Initial session check fails
	runner.api.On("UsersGetUsers", mock.Anything, mock.Anything).Return(nil, errors.New("auth session invalid")).Once()

	// Auth flow should NOT be called
	authFlow.AssertNotCalled(t, "Run", mock.Anything, mock.Anything)

	// The runner's Run method will return an error because the setup function failed
	var runErr error
	runner.On("Run", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			f := args.Get(1).(func(ctx context.Context) error)
			runErr = f(ctx)
		}).
		Return(func(context.Context, func(context.Context) error) error {
			return runErr
		}).
		Once()

	client.Start(ctx)
	err := <-client.runErr

	require.Error(t, err)
	require.ErrorContains(t, err, "cannot perform interactive auth in non-terminal")
}

func TestParseFloodWait(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantWait time.Duration
		wantOk   bool
	}{
		{
			name:     "Valid FLOOD_WAIT error",
			err:      errors.New("rpc error code 420: FLOOD_WAIT (123)"),
			wantWait: 123 * time.Second,
			wantOk:   true,
		},
		{
			name:     "Another valid FLOOD_WAIT error",
			err:      fmt.Errorf("wrapped: %w", errors.New("FLOOD_WAIT (45)")),
			wantWait: 45 * time.Second,
			wantOk:   true,
		},
		{
			name:     "No FLOOD_WAIT in string",
			err:      errors.New("some other error"),
			wantWait: 0,
			wantOk:   false,
		},
		{
			name:     "Nil error",
			err:      nil,
			wantWait: 0,
			wantOk:   false,
		},
		{
			name:     "Malformed FLOOD_WAIT",
			err:      errors.New("FLOOD_WAIT (abc)"),
			wantWait: 0,
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotWait, gotOk := parseFloodWait(tt.err)
			require.Equal(t, tt.wantOk, gotOk)
			require.Equal(t, tt.wantWait, gotWait)
		})
	}
}

func TestClient_OtherApiMethods(t *testing.T) {
	ctx := context.Background()

	t.Run("UsersGetUsers", func(t *testing.T) {
		client, runner, _, _ := newTestClient(t)
		runner.api.On("UsersGetUsers", ctx, mock.Anything).Return([]tg.UserClass{&tg.User{}}, nil).Once()

		_, err := client.UsersGetUsers(ctx, []tg.InputUserClass{})
		require.NoError(t, err)
		runner.api.AssertExpectations(t)
	})

	t.Run("ContactsResolveUsername", func(t *testing.T) {
		client, runner, _, _ := newTestClient(t)
		runner.api.On("ContactsResolveUsername", ctx, mock.Anything).Return(&tg.ContactsResolvedPeer{}, nil).Once()

		_, err := client.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{})
		require.NoError(t, err)
		runner.api.AssertExpectations(t)
	})

	t.Run("UsersGetFullUser", func(t *testing.T) {
		client, runner, _, _ := newTestClient(t)
		runner.api.On("UsersGetFullUser", ctx, mock.Anything).Return(&tg.UsersUserFull{}, nil).Once()

		_, err := client.UsersGetFullUser(ctx, &tg.InputUserSelf{})
		require.NoError(t, err)
		runner.api.AssertExpectations(t)
	})
}
