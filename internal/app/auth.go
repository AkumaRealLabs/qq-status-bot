package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func (s *Service) SetupStatus(ctx context.Context) (map[string]bool, error) {
	n, err := s.Store.UserCount(ctx)
	return map[string]bool{"initialized": n > 0}, err
}

func (s *Service) Setup(ctx context.Context, username, password string) (domain.User, error) {
	if username == "" || password == "" {
		return domain.User{}, ErrBadRequest("username and password are required")
	}
	if len(password) < minPasswordLength {
		return domain.User{}, ErrBadRequest(fmt.Sprintf("password must be at least %d characters", minPasswordLength))
	}
	n, err := s.Store.UserCount(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if n > 0 {
		return domain.User{}, ErrBadRequest("setup already completed")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, err
	}
	u, err := s.Store.CreateInitialUser(ctx, username, string(hash))
	if err != nil {
		if errors.Is(err, store.ErrInitialUserExists) {
			return domain.User{}, ErrBadRequest("setup already completed")
		}
		if n, countErr := s.Store.UserCount(ctx); countErr == nil && n > 0 {
			return domain.User{}, ErrBadRequest("setup already completed")
		}
	}
	return u, err
}

func (s *Service) Login(ctx context.Context, username, password string) (string, domain.User, error) {
	u, err := s.Store.UserByUsername(ctx, username)
	if err != nil {
		return "", u, errors.New("invalid username or password")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", u, errors.New("invalid username or password")
	}
	token, err := s.Store.CreateSession(ctx, u.ID, 30*24*time.Hour)
	return token, u, err
}

func (s *Service) Me(ctx context.Context, token string) (domain.User, error) {
	return s.Store.UserBySessionToken(ctx, token)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.Store.DeleteSession(ctx, token)
}
