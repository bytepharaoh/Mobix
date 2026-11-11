package service

import (
	"context"
	"strings"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

type Repository interface {
	Create(context.Context, *domain.Driver) (*domain.Driver, error)
	FindById(context.Context, string) (*domain.Driver, error)
	FindByAccountID(context.Context, string) (*domain.Driver, error)
	UpdateStatus(context.Context, string, domain.DriverStatus) error
	UpdateLocation(context.Context, string, domain.Location) error
	Candidates(context.Context, domain.CandidateSearch) ([]domain.Candidate, error)
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Register(ctx context.Context, req domain.CreateDriverRequest) (*domain.Driver, error) {
	actor := authn.From(ctx)
	if actor.Role != authn.Driver || actor.ID == "" {
		return nil, apperrors.ErrForbidden
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Phone = strings.TrimSpace(req.Phone)
	req.CarNumber = strings.ToUpper(strings.TrimSpace(req.CarNumber))
	req.CarModel = strings.TrimSpace(req.CarModel)
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, &domain.Driver{
		AccountID: actor.ID, Name: req.Name, Email: req.Email, Phone: req.Phone, CarNumber: req.CarNumber,
		CarModel: req.CarModel, Location: req.Location, Status: domain.StatusOffline,
	})
}

func (s *Service) Get(ctx context.Context, id string) (*domain.Driver, error) {
	actor := authn.From(ctx)
	if actor.Role != authn.Driver && actor.Role != authn.Operator {
		return nil, apperrors.ErrForbidden
	}
	profile, err := s.repo.FindById(ctx, id)
	if err != nil {
		return nil, err
	}
	if actor.Role == authn.Driver && (actor.ID == "" || profile.AccountID != actor.ID) {
		return nil, domain.ErrDriverNotFound
	}
	return profile, nil
}

func (s *Service) Me(ctx context.Context) (*domain.Driver, error) {
	actor := authn.From(ctx)
	if actor.Role != authn.Driver || actor.ID == "" {
		return nil, apperrors.ErrForbidden
	}
	return s.repo.FindByAccountID(ctx, actor.ID)
}

func (s *Service) DispatchProfile(ctx context.Context, id string) (domain.DispatchProfile, error) {
	if authn.From(ctx).Role != authn.Service {
		return domain.DispatchProfile{}, apperrors.ErrForbidden
	}
	profile, err := s.repo.FindById(ctx, id)
	if err != nil {
		return domain.DispatchProfile{}, err
	}
	return domain.DispatchProfile{AccountID: profile.AccountID, Status: profile.Status, LocationUpdatedAt: profile.LocationUpdatedAt}, nil
}

func (s *Service) SetAvailability(ctx context.Context, id string, status domain.DriverStatus) error {
	if status != domain.StatusAvailable && status != domain.StatusOffline {
		return apperrors.ErrInvalidInput
	}
	if err := s.requireDriverOwner(ctx, id); err != nil {
		return err
	}
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *Service) SetLocation(ctx context.Context, id string, location domain.Location) error {
	if !location.Valid() {
		return domain.ErrInvalidLocation
	}
	if err := s.requireDriverOwner(ctx, id); err != nil {
		return err
	}
	return s.repo.UpdateLocation(ctx, id, location)
}

func (s *Service) requireDriverOwner(ctx context.Context, id string) error {
	if authn.From(ctx).Role != authn.Driver {
		return apperrors.ErrForbidden
	}
	_, err := s.Get(ctx, id)
	return err
}

func (s *Service) Candidates(ctx context.Context, search domain.CandidateSearch) ([]domain.Candidate, error) {
	if authn.From(ctx).Role != authn.Service {
		return nil, apperrors.ErrForbidden
	}
	if !search.Pickup.Valid() || search.FreshAfter.IsZero() ||
		search.RadiusMeters < 1 || search.RadiusMeters > domain.MaxDispatchRadiusMeters ||
		(search.After != nil && (search.After.ID.IsZero() || search.After.DistanceMeters < 0 || search.After.DistanceMeters > float64(search.RadiusMeters))) {
		return nil, apperrors.ErrInvalidInput
	}
	return s.repo.Candidates(ctx, search)
}
