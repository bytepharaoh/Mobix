package service

import (
	"context"
	"strings"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

type Repository interface {
	Create(context.Context, *domain.Driver) (*domain.Driver, error)
	FindById(context.Context, string) (*domain.Driver, error)
	UpdateStatus(context.Context, string, domain.DriverStatus) error
	UpdateLocation(context.Context, string, domain.Location) error
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Register(ctx context.Context, req domain.CreateDriverRequest) (*domain.Driver, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Phone = strings.TrimSpace(req.Phone)
	req.CarNumber = strings.ToUpper(strings.TrimSpace(req.CarNumber))
	req.CarModel = strings.TrimSpace(req.CarModel)
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, &domain.Driver{
		Name: req.Name, Email: req.Email, Phone: req.Phone, CarNumber: req.CarNumber,
		CarModel: req.CarModel, Location: req.Location, Status: domain.StatusOffline,
	})
}

func (s *Service) Get(ctx context.Context, id string) (*domain.Driver, error) {
	return s.repo.FindById(ctx, id)
}

func (s *Service) SetAvailability(ctx context.Context, id string, status domain.DriverStatus) error {
	if status != domain.StatusAvailable && status != domain.StatusOffline {
		return apperrors.ErrInvalidInput
	}
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *Service) SetLocation(ctx context.Context, id string, location domain.Location) error {
	if !location.Valid() {
		return domain.ErrInvalidLocation
	}
	return s.repo.UpdateLocation(ctx, id, location)
}
