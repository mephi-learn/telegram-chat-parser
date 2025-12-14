package term

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
	"golang.org/x/xerrors"
)

// Terminal обеспечивает интерактивную аутентификацию через терминал.
// Он реализует интерфейс auth.UserAuthenticator.
type Terminal struct {
	phone   string
	in      *bufio.Reader
	out     io.Writer
	stdinfd int
}

var _ auth.UserAuthenticator = (*Terminal)(nil)

// NewTerminal создает новый экземпляр Terminal.
func NewTerminal(phone string) *Terminal {
	return &Terminal{
		phone:   phone,
		in:      bufio.NewReader(os.Stdin),
		out:     os.Stdout,
		stdinfd: int(os.Stdin.Fd()),
	}
}

// Phone возвращает номер телефона.
func (t *Terminal) Phone(_ context.Context) (string, error) {
	return t.phone, nil
}

// Password запрашивает пароль 2FA.
func (t *Terminal) Password(_ context.Context) (string, error) {
	fmt.Fprint(t.out, "Enter 2FA password: ")
	bytePwd, err := term.ReadPassword(t.stdinfd)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(t.out) // Новая строка после ввода
	return string(bytePwd), nil
}

// AcceptTermsOfService принимает Условия обслуживания.
func (t *Terminal) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	fmt.Fprintf(t.out, "Accepting Terms of Service: %s\n", tos.Text)
	return nil
}

// Code запрашивает код подтверждения.
func (t *Terminal) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Fprint(t.out, "Enter code: ")
	code, err := t.in.ReadString('\n')
	if err != nil {
		return "", xerrors.Errorf("failed to read code: %w", err)
	}
	return strings.TrimSpace(code), nil
}

// SignUp не реализован, так как мы не поддерживаем регистрацию новых пользователей.
func (t *Terminal) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, xerrors.New("signup not implemented")
}
