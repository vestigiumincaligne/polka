package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newService(t *testing.T) *Service {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLoginLogout(t *testing.T) {
	s := newService(t)
	ctx := context.Background()

	admin, err := s.CreateUser(ctx, "admin", "secret123", "Админ", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if !admin.IsAdmin() {
		t.Error("role lost")
	}

	if _, _, err := s.Login(ctx, "admin", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if _, _, err := s.Login(ctx, "nobody", "x"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: %v", err)
	}

	token, u, err := s.Login(ctx, "admin", "secret123")
	if err != nil || u.Login != "admin" {
		t.Fatalf("login: %v %v", err, u)
	}

	got, err := s.GetByToken(ctx, token)
	if err != nil || got.ID != admin.ID {
		t.Fatalf("GetByToken: %v %v", err, got)
	}

	if err := s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("token must be dead after logout: %v", err)
	}
}

func TestLastAdminProtection(t *testing.T) {
	s := newService(t)
	ctx := context.Background()

	admin, _ := s.CreateUser(ctx, "admin", "pass1234", "", RoleAdmin)
	user, _ := s.CreateUser(ctx, "reader", "pass1234", "", RoleUser)

	if err := s.DeleteUser(ctx, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete last admin: %v", err)
	}
	demote := RoleUser
	if err := s.UpdateUser(ctx, admin.ID, nil, nil, &demote, nil); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote last admin: %v", err)
	}
	off := true
	if err := s.UpdateUser(ctx, admin.ID, nil, nil, nil, &off); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable last admin: %v", err)
	}

	// The second admin removes the protection
	promote := RoleAdmin
	if err := s.UpdateUser(ctx, user.ID, nil, nil, &promote, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, admin.ID); err != nil {
		t.Errorf("delete admin with another present: %v", err)
	}
}

func TestDisabledUserAndPasswordChange(t *testing.T) {
	s := newService(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, "reader", "pass1234", "", RoleUser)
	token, _, err := s.Login(ctx, "reader", "pass1234")
	if err != nil {
		t.Fatal(err)
	}

	// Disabling kills the session and forbids login
	off := true
	if err := s.UpdateUser(ctx, u.ID, nil, nil, nil, &off); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Error("session must die on disable")
	}
	if _, _, err := s.Login(ctx, "reader", "pass1234"); !errors.Is(err, ErrInvalidCredentials) {
		t.Error("disabled user must not log in")
	}

	// Re-enabling + password change resets sessions
	on := false
	s.UpdateUser(ctx, u.ID, nil, nil, nil, &on)
	token, _, _ = s.Login(ctx, "reader", "pass1234")
	newPass := "newpass99"
	if err := s.UpdateUser(ctx, u.ID, &newPass, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Error("session must die on password change")
	}
	if _, _, err := s.Login(ctx, "reader", "newpass99"); err != nil {
		t.Errorf("new password must work: %v", err)
	}
}

func TestDuplicateLogin(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	s.CreateUser(ctx, "reader", "pass1234", "", RoleUser)
	if _, err := s.CreateUser(ctx, "Reader", "pass1234", "", RoleUser); !errors.Is(err, ErrLoginTaken) {
		t.Errorf("case-insensitive duplicate: %v", err)
	}
}
