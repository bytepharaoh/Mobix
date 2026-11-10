package service

import (
	"context"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/service/mocks"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

func TestCreateTrip(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := []struct {
		name    string
		req     domain.CreateTripRequest
		mock    func(repo *mocks.MockRepository)
		wantErr bool
	}{
		{
			name: "successfully creates a trip",
			req: domain.CreateTripRequest{
				RiderID: "rider123",
				Pickup:  domain.Location{Lat: 30.0, Lng: 31.0},
				Dropoff: domain.Location{Lat: 30.1, Lng: 31.1},
			},
			mock: func(repo *mocks.MockRepository) {
				repo.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, trip *domain.Trip) (*domain.Trip, error) { return trip, nil })
			},
			wantErr: false,
		},
		{
			name: "fails when rider_id is missing",
			req: domain.CreateTripRequest{
				Pickup:  domain.Location{Lat: 30.0, Lng: 31.0},
				Dropoff: domain.Location{Lat: 30.1, Lng: 31.1},
			},
			mock:    func(repo *mocks.MockRepository) {},
			wantErr: true,
		},
		{
			name: "fails when pickup is outside coordinate range",
			req: domain.CreateTripRequest{
				RiderID: "rider123",
				Pickup:  domain.Location{Lat: 100},
				Dropoff: domain.Location{Lat: 30.1, Lng: 31.1},
			},
			mock:    func(repo *mocks.MockRepository) {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := mocks.NewMockRepository(ctrl)
			tt.mock(mockRepo)

			svc := New(mockRepo)
			result, err := svc.CreateTrip(context.Background(), tt.req, "demo-key")

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Pickup != tt.req.Pickup || result.Dropoff != tt.req.Dropoff || result.FareCents < 350 || result.IdempotencyKey == "" {
				t.Fatalf("incorrect trip: %+v", result)
			}
			if result.Status != domain.StatusRequested {
				t.Errorf("got status %s, want %s", result.Status, domain.StatusRequested)
			}
		})
	}
}

func TestUpdateTripStatus(t *testing.T) {
	const driverID = "0123456789abcdef01234567"
	check := func(context.Context, string) error { return nil }
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := []struct {
		name    string
		id      string
		status  domain.TripStatus
		mock    func(repo *mocks.MockRepository)
		wantErr bool
	}{
		{
			name:   "requested to assigned — valid",
			id:     "abc123",
			status: domain.StatusAssigned,
			mock: func(repo *mocks.MockRepository) {
				repo.EXPECT().
					FindByID(gomock.Any(), "abc123").
					Return(&domain.Trip{Status: domain.StatusRequested}, nil)
				repo.EXPECT().
					Transition(gomock.Any(), "abc123", domain.StatusRequested, domain.StatusAssigned, driverID, (*messaging.TripCompleted)(nil)).
					Return(&domain.Trip{Status: domain.StatusAssigned, DriverID: driverID}, nil)
			},
			wantErr: false,
		},
		{
			name:   "completed to requested — invalid transition",
			id:     "abc123",
			status: domain.StatusRequested,
			mock: func(repo *mocks.MockRepository) {
				repo.EXPECT().
					FindByID(gomock.Any(), "abc123").
					Return(&domain.Trip{Status: domain.StatusCompleted}, nil)
			},
			wantErr: true,
		},
		{
			name:   "returns error when trip not found",
			id:     "notexist",
			status: domain.StatusAssigned,
			mock: func(repo *mocks.MockRepository) {
				repo.EXPECT().
					FindByID(gomock.Any(), "notexist").
					Return(nil, apperrors.ErrNotFound)
			},
			wantErr: true,
		},
		{
			name:    "returns error for empty id",
			id:      "",
			status:  domain.StatusAssigned,
			mock:    func(repo *mocks.MockRepository) {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := mocks.NewMockRepository(ctrl)
			tt.mock(mockRepo)

			svc := New(mockRepo)
			driver := ""
			if tt.status == domain.StatusAssigned {
				driver = driverID
			}
			_, err := svc.UpdateTripStatus(context.Background(), tt.id, tt.status, driver, check)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
