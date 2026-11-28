package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"findus/backend/internal/domain"
	"findus/backend/internal/repository"
)

type Auth struct {
	Users    repository.UserRepository
	Settings repository.SettingsRepository
	Invites  repository.InviteRepository
	// SetupToken, if set, is required to create the first (admin) account.
	SetupToken string

	// registerMu makes "check, then create" in Register atomic, so that one invite or one empty
	// database yields one account.
	// shortcut: only holds within one process, which SQLite implies; use a transaction otherwise.
	registerMu sync.Mutex
}

// dummyPasswordHash is compared for unknown users, so that response times do not reveal which names exist.
var dummyPasswordHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("no such user"), bcrypt.DefaultCost)
	return h
})

func (a *Auth) Login(ctx context.Context, username, password string) (*domain.User, error) {
	u, err := a.Users.GetByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(password))
			return nil, domain.ErrUnauthorized
		}
		return nil, err
	}
	if !u.IsActive {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(password))
		return nil, domain.ErrUnauthorized
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, domain.ErrUnauthorized
	}
	return u, nil
}

func (a *Auth) Register(ctx context.Context, username, email, password, inviteToken string) (*domain.User, error) {
	username = strings.TrimSpace(username)
	emailNorm, err := normalizeRegistrationEmail(email)
	if err != nil {
		return nil, err
	}
	if err := validateUserCreds(username, emailNorm, password); err != nil {
		return nil, err
	}
	email = emailNorm
	a.registerMu.Lock()
	defer a.registerMu.Unlock()
	n, err := a.Users.Count(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var role domain.Role
	var consumedInvite *domain.Invite

	if n == 0 {
		if a.SetupToken != "" && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(inviteToken)), []byte(a.SetupToken)) != 1 {
			return nil, domain.ErrInvalidSetupToken
		}
		role = domain.RoleAdmin
	} else {
		mode, ok, err := a.registrationMode(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			mode = domain.RegistrationAdminOnly
		}
		switch mode {
		case domain.RegistrationAdminOnly:
			return nil, domain.ErrRegistrationClosed
		case domain.RegistrationOpen:
			role = domain.RoleUser
		case domain.RegistrationInvite:
			inv, err := a.Invites.GetByToken(ctx, strings.TrimSpace(inviteToken))
			if err != nil {
				return nil, domain.ErrInvalidInvite
			}
			if inv.UsedAt != nil || time.Now().After(inv.ExpiresAt) {
				return nil, domain.ErrInvalidInvite
			}
			consumedInvite = inv
			role = inv.Role
		default:
			return nil, domain.ErrRegistrationClosed
		}
	}

	if _, err := a.Users.GetByUsername(ctx, username); err == nil {
		return nil, fmt.Errorf("username already taken: %w", domain.ErrConflict)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	if _, err := a.Users.GetByEmail(ctx, email); err == nil {
		return nil, fmt.Errorf("email already registered: %w", domain.ErrConflict)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &domain.User{
		ID:           newID(),
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		Role:         role,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
		UITheme:      domain.DefaultUITheme,
	}
	if err := a.Users.Create(ctx, u); err != nil {
		return nil, err
	}
	if consumedInvite != nil {
		if err := a.Invites.MarkUsed(ctx, consumedInvite.ID, now); err != nil {
			return nil, err
		}
	}
	if n == 0 {
		_ = a.Settings.Set(ctx, string(domain.SettingRegistrationMode), string(domain.RegistrationAdminOnly))
	}
	return u, nil
}

// UpdateProfile verifies currentPassword, then updates username, email, and optionally password.
// newPassword empty means leave password unchanged.
func (a *Auth) UpdateProfile(ctx context.Context, userID, username, email, currentPassword, newPassword string) (*domain.User, error) {
	u, err := a.Users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(currentPassword)); err != nil {
		return nil, domain.ErrInvalidCurrentPassword
	}
	username = strings.TrimSpace(username)
	emailNorm, err := normalizeRegistrationEmail(email)
	if err != nil {
		return nil, err
	}
	if err := validateProfileUsernameEmail(username); err != nil {
		return nil, err
	}
	newPassword = strings.TrimSpace(newPassword)
	if newPassword != "" && len(newPassword) < 10 {
		return nil, fmt.Errorf("%w: password min 10 chars", domain.ErrValidation)
	}
	if other, err := a.Users.GetByUsername(ctx, username); err == nil {
		if other.ID != userID {
			return nil, fmt.Errorf("username taken: %w", domain.ErrConflict)
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	if other, err := a.Users.GetByEmail(ctx, emailNorm); err == nil {
		if other.ID != userID {
			return nil, fmt.Errorf("email taken: %w", domain.ErrConflict)
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	u.Username = username
	u.Email = emailNorm
	u.UpdatedAt = time.Now().UTC()
	if newPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = string(hash)
	}
	if err := a.Users.Update(ctx, u); err != nil {
		return nil, err
	}
	if newPassword != "" {
		if u.SessionVersion, err = a.Users.BumpSessionVersion(ctx, u.ID); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func validateProfileUsernameEmail(username string) error {
	if utf8.RuneCountInString(username) < 2 || utf8.RuneCountInString(username) > 64 {
		return fmt.Errorf("%w: username length", domain.ErrValidation)
	}
	return nil
}

func normalizeRegistrationEmail(email string) (string, error) {
	e := strings.TrimSpace(strings.ToLower(email))
	if utf8.RuneCountInString(e) < 3 {
		return "", fmt.Errorf("%w: email too short", domain.ErrValidation)
	}
	if utf8.RuneCountInString(e) > 254 {
		return "", fmt.Errorf("%w: email length", domain.ErrValidation)
	}
	addr, err := mail.ParseAddress(e)
	if err != nil {
		return "", fmt.Errorf("%w: invalid email address", domain.ErrValidation)
	}
	out := strings.TrimSpace(addr.Address)
	if out == "" || !strings.Contains(out, "@") {
		return "", fmt.Errorf("%w: invalid email address", domain.ErrValidation)
	}
	return out, nil
}

// UsernameAvailable reports whether a username is free for a new account (length must be valid).
func (a *Auth) UsernameAvailable(ctx context.Context, username string) (available bool, reason string, err error) {
	username = strings.TrimSpace(username)
	if utf8.RuneCountInString(username) < 2 || utf8.RuneCountInString(username) > 64 {
		return false, "invalid_length", nil
	}
	_, e := a.Users.GetByUsername(ctx, username)
	if errors.Is(e, domain.ErrNotFound) {
		return true, "", nil
	}
	if e != nil {
		return false, "", e
	}
	return false, "taken", nil
}

func (a *Auth) registrationMode(ctx context.Context) (domain.RegistrationMode, bool, error) {
	v, ok, err := a.Settings.Get(ctx, domain.SettingRegistrationMode)
	if err != nil || !ok {
		return "", ok, err
	}
	m, valid := domain.ParseRegistrationMode(v)
	if !valid {
		return "", false, nil
	}
	return m, true, nil
}

func validateUserCreds(username, emailNorm, password string) error {
	if utf8.RuneCountInString(username) < 2 || utf8.RuneCountInString(username) > 64 {
		return fmt.Errorf("%w: username length", domain.ErrValidation)
	}
	if utf8.RuneCountInString(emailNorm) < 3 || utf8.RuneCountInString(emailNorm) > 254 {
		return fmt.Errorf("%w: email length", domain.ErrValidation)
	}
	if len(password) < 10 {
		return fmt.Errorf("%w: password min 10 chars", domain.ErrValidation)
	}
	return nil
}
