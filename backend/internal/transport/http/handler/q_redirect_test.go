package handler_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"findus/backend/internal/config"
	"findus/backend/internal/domain"
	"findus/backend/internal/repository/sqlite"
	"findus/backend/internal/service"
	"findus/backend/internal/transport/http/handler"
)

func TestQRedirectResolvesReservedTokenAfterItemCreation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.OpenDB(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	locs := sqlite.NewLocationRepo(db)
	items := sqlite.NewItemRepo(db)
	settings := sqlite.NewSettingsRepo(db)
	templates := sqlite.NewItemTemplateRepo(db)
	reservations := sqlite.NewItemQRTokenReservationRepo(db)

	inv := &service.Inventory{
		Locations:    locs,
		Items:        items,
		ItemQRTokens: reservations,
		Templates:    templates,
		Settings:     settings,
	}
	qr := &service.QR{BaseURL: "http://localhost:8080"}
	gen := service.LabelPDFGenerator{
		QR:           qr,
		MaxBatch:     10,
		Reservations: reservations,
	}
	pol, err := inv.GetItemIDPolicy(ctx)
	require.NoError(t, err)
	_, err = gen.Generate(ctx, service.LabelPDFInput{From: 1, To: 2, Policy: pol})
	require.NoError(t, err)

	token, ok, err := reservations.GetTokenByItemID(ctx, "item_0001")
	require.NoError(t, err)
	require.True(t, ok)

	srv := &handler.Server{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:       config.Config{DataDir: dir},
		Locs:         locs,
		Items:        items,
		ItemQRTokens: reservations,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /q/{token}", srv.QRedirect)

	rrBefore := httptest.NewRecorder()
	reqBefore := httptest.NewRequest(http.MethodGet, "/q/"+token, nil)
	mux.ServeHTTP(rrBefore, reqBefore)
	require.Equal(t, http.StatusFound, rrBefore.Code)
	require.Equal(t, "/items/new?qr="+token, rrBefore.Header().Get("Location"))

	mux.HandleFunc("GET /api/qr/{token}", srv.APIQRResolve)
	rrAPI := httptest.NewRecorder()
	mux.ServeHTTP(rrAPI, httptest.NewRequest(http.MethodGet, "/api/qr/"+token, nil))
	require.Equal(t, http.StatusOK, rrAPI.Code)
	require.JSONEq(t, `{"target":"/items/new?qr=`+token+`"}`, rrAPI.Body.String())

	rrUnknown := httptest.NewRecorder()
	mux.ServeHTTP(rrUnknown, httptest.NewRequest(http.MethodGet, "/q/unknown", nil))
	require.Equal(t, http.StatusNotFound, rrUnknown.Code)

	var locID string
	err = db.QueryRowContext(ctx, `SELECT id FROM locations LIMIT 1`).Scan(&locID)
	require.NoError(t, err)
	created, err := inv.CreateItemFromQR(ctx, token, "reserved", "", locID, domain.TemplateType("standard"), []byte(`{}`), []byte(`{}`), nil)
	require.NoError(t, err)
	require.Equal(t, "item_0001", created.ID)
	require.Equal(t, token, created.QRToken)

	other, err := inv.CreateLocation(ctx, "Elsewhere", "", nil)
	require.NoError(t, err)
	require.NoError(t, inv.MoveItem(ctx, created.ID, other.ID))
	moved, err := items.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, other.ID, moved.LocationID)
	require.Equal(t, created.Name, moved.Name)
	require.Equal(t, token, moved.QRToken)
	require.Error(t, inv.MoveItem(ctx, created.ID, "missing"))

	rrAfter := httptest.NewRecorder()
	reqAfter := httptest.NewRequest(http.MethodGet, "/q/"+token, nil)
	mux.ServeHTTP(rrAfter, reqAfter)
	require.Equal(t, http.StatusFound, rrAfter.Code)
	require.Equal(t, "/items/item_0001", rrAfter.Header().Get("Location"))

	// Creation without a scan leaves reserved IDs to their labels; each label still creates its own item.
	token2, _, err := reservations.GetTokenByItemID(ctx, "item_0002")
	require.NoError(t, err)
	create := func(qrToken string) (*domain.Item, error) {
		return inv.CreateItemFromQR(ctx, qrToken, "x", "", locID, domain.TemplateType("standard"), []byte(`{}`), []byte(`{}`), nil)
	}
	unlabeled, err := create("")
	require.NoError(t, err)
	require.Equal(t, "item_0003", unlabeled.ID)
	second, err := create(token2)
	require.NoError(t, err)
	require.Equal(t, "item_0002", second.ID)
	require.Equal(t, token2, second.QRToken)
	_, err = create(token2)
	require.Error(t, err)
	_, err = create("unknown")
	require.ErrorIs(t, err, domain.ErrNotFound)

	// A label printed later for an unlabeled item resolves to that item.
	_, err = gen.Generate(ctx, service.LabelPDFInput{From: 3, To: 3, Policy: pol})
	require.NoError(t, err)
	token3, _, err := reservations.GetTokenByItemID(ctx, "item_0003")
	require.NoError(t, err)
	rrLate := httptest.NewRecorder()
	mux.ServeHTTP(rrLate, httptest.NewRequest(http.MethodGet, "/q/"+token3, nil))
	require.Equal(t, "/items/item_0003", rrLate.Header().Get("Location"))
}
