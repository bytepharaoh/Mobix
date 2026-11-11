package service

import (
	"context"
	"errors"
	"testing"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/service/mocks"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"go.uber.org/mock/gomock"
)

func TestTripReadPermissions(t *testing.T) {
	for _, item := range []struct {
		name  string
		actor authn.Actor
		trip  *domain.Trip
		allow bool
	}{
		{"owner", authn.Actor{ID: "rider-a", Role: authn.Rider}, &domain.Trip{RiderAccountID: "rider-a"}, true},
		{"other rider", authn.Actor{ID: "rider-b", Role: authn.Rider}, &domain.Trip{RiderAccountID: "rider-a"}, false},
		{"assigned driver", authn.Actor{ID: "driver-a", Role: authn.Driver}, &domain.Trip{DriverAccountID: "driver-a"}, true},
		{"other driver", authn.Actor{ID: "driver-b", Role: authn.Driver}, &domain.Trip{DriverAccountID: "driver-a"}, false},
		{"operator sees legacy", authn.Actor{ID: "operator", Role: authn.Operator}, &domain.Trip{RiderID: "legacy"}, true},
		{"legacy rider string grants no access", authn.Actor{ID: "legacy", Role: authn.Rider}, &domain.Trip{RiderID: "legacy"}, false},
		{"anonymous", authn.Actor{}, &domain.Trip{}, false},
		{"service identity", authn.Actor{Role: authn.Service}, &domain.Trip{}, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo := mocks.NewMockRepository(ctrl)
			repo.EXPECT().FindByID(gomock.Any(), "trip").Return(item.trip, nil)
			result, err := New(repo).GetTrip(authn.WithActor(context.Background(), item.actor), "trip")
			if item.allow {
				if err != nil || result == nil {
					t.Fatalf("expected access, got %v", err)
				}
			} else if !errors.Is(err, domain.ErrTripNotFound) || result != nil {
				t.Fatalf("expected hidden trip, got %+v, %v", result, err)
			}
		})
	}
}

func TestTransitionChecksPermissionBeforeRetry(t *testing.T) {
	for _, item := range []struct {
		name   string
		actor  authn.Actor
		target domain.TripStatus
		want   error
	}{
		{"owner completion retry forbidden", authn.Actor{ID: "rider", Role: authn.Rider}, domain.StatusCompleted, apperrors.ErrForbidden},
		{"operator completion retry forbidden", authn.Actor{ID: "operator", Role: authn.Operator}, domain.StatusCompleted, apperrors.ErrForbidden},
		{"foreign completion retry hidden", authn.Actor{ID: "other", Role: authn.Rider}, domain.StatusCompleted, domain.ErrTripNotFound},
		{"unassigned driver completion retry hidden", authn.Actor{ID: "other", Role: authn.Driver}, domain.StatusCompleted, domain.ErrTripNotFound},
		{"assigned driver completion retry", authn.Actor{ID: "driver", Role: authn.Driver}, domain.StatusCompleted, nil},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo := mocks.NewMockRepository(ctrl)
			repo.EXPECT().FindByID(gomock.Any(), "trip").Return(&domain.Trip{
				RiderAccountID: "rider", DriverAccountID: "driver", Status: domain.StatusCompleted,
			}, nil)
			result, err := New(repo).UpdateTripStatus(authn.WithActor(context.Background(), item.actor), "trip", item.target, "", nil)
			if !errors.Is(err, item.want) || (item.want == nil && result == nil) {
				t.Fatalf("got result %+v error %v, want %v", result, err, item.want)
			}
		})
	}
}

func TestConcurrentTransitionRechecksOwner(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mocks.NewMockRepository(ctrl)
	before := &domain.Trip{RiderAccountID: "rider", Status: domain.StatusRequested}
	after := &domain.Trip{RiderAccountID: "other", Status: domain.StatusCancelled}
	gomock.InOrder(
		repo.EXPECT().FindByID(gomock.Any(), "trip").Return(before, nil),
		repo.EXPECT().Transition(gomock.Any(), "trip", before, domain.Transition{Status: domain.StatusCancelled}).Return(nil, apperrors.ErrConflict),
		repo.EXPECT().FindByID(gomock.Any(), "trip").Return(after, nil),
	)
	ctx := authn.WithActor(context.Background(), authn.Actor{ID: "rider", Role: authn.Rider})
	result, err := New(repo).UpdateTripStatus(ctx, "trip", domain.StatusCancelled, "", nil)
	if result != nil || !errors.Is(err, domain.ErrTripNotFound) {
		t.Fatalf("expected owner recheck after conflict, got %+v, %v", result, err)
	}
}
