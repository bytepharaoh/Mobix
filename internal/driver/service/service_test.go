package service

import (
	"context"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/internal/driver/service/mocks"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

func TestCreate(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	driver := &domain.Driver{
		Name:  "Pavel Khramov",
		Email: "pavel@example.com",
	}

	tests := []struct {
		name    string
		driver  *domain.Driver
		mock    func(*mocks.MockRepository)
		wantErr bool
	}{
		{
			name:   "success",
			driver: driver,
			mock: func(repo *mocks.MockRepository) {
				repo.EXPECT().
					Create(gomock.Any(), driver).
					Return(driver, nil)
			},
		},
		{
			name:   "repository error",
			driver: driver,
			mock: func(repo *mocks.MockRepository) {
				repo.EXPECT().
					Create(gomock.Any(), driver).
					Return(nil, apperrors.ErrInternal)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := mocks.NewMockRepository(ctrl)
			tt.mock(mockRepo)

			svc := New(mockRepo)

			_, err := svc.Create(context.Background(), tt.driver)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetByID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	driver := &domain.Driver{Name: "Pavel"}

	tests := []struct {
		name    string
		id      string
		mock    func(*mocks.MockRepository)
		wantErr bool
	}{
		{
			name: "success",
			id:   "driver123",
			mock: func(repo *mocks.MockRepository) {
				repo.EXPECT().
					FindById(gomock.Any(), "driver123").
					Return(driver, nil)
			},
		},
		{
			name: "not found",
			id:   "driver123",
			mock: func(repo *mocks.MockRepository) {
				repo.EXPECT().
					FindById(gomock.Any(), "driver123").
					Return(nil, domain.ErrDriverNotFound)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := mocks.NewMockRepository(ctrl)
			tt.mock(mockRepo)

			svc := New(mockRepo)

			_, err := svc.GetByID(context.Background(), tt.id)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestIncreaseTotalTrips(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepository(ctrl)

	mockRepo.EXPECT().
		IncreaseTotalTrips(gomock.Any(), "driver123", 5).
		Return(nil)

	svc := New(mockRepo)

	if err := svc.IncreaseTotalTrips(context.Background(), "driver123", 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepository(ctrl)

	mockRepo.EXPECT().
		UpdateStatus(gomock.Any(), "driver123", domain.StatusAvailable).
		Return(nil)

	svc := New(mockRepo)

	if err := svc.UpdateStatus(context.Background(), "driver123", domain.StatusAvailable); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateLastLogin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	now := time.Now()

	mockRepo := mocks.NewMockRepository(ctrl)

	mockRepo.EXPECT().
		UpdateLastLogin(gomock.Any(), "driver123", now).
		Return(nil)

	svc := New(mockRepo)

	if err := svc.UpdateLastLogin(context.Background(), "driver123", now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateRating(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepository(ctrl)

	mockRepo.EXPECT().
		UpdateRating(gomock.Any(), "driver123", 4.9).
		Return(nil)

	svc := New(mockRepo)

	if err := svc.UpdateRating(context.Background(), "driver123", 4.9); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
