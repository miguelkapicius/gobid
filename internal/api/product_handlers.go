package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/miguelkapicius/gobid/internal/jsonutils"
	"github.com/miguelkapicius/gobid/internal/services"
	"github.com/miguelkapicius/gobid/internal/usecase/product"
)

func (api *Api) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	data, problems, err := jsonutils.DecodeValidJson[product.CreateProductReq](r)

	if err != nil {
		jsonutils.EncodeJson(w, r, http.StatusUnprocessableEntity, problems)
		return
	}

	userId, ok := api.Sessions.Get(r.Context(), "AuthenticatedUserId").(uuid.UUID)

	if !ok {
		jsonutils.EncodeJson(w, r, http.StatusInternalServerError, map[string]any{
			"error": "unexpected error, try again later",
		})
		return
	}

	productId, err := api.ProductService.CreateProduct(
		r.Context(),
		userId,
		data.ProductName,
		data.Description,
		data.BasePrice,
		data.AuctionEnd,
	)

	if err != nil {
		jsonutils.EncodeJson(w, r, http.StatusInternalServerError, map[string]any{
			"message": "failed to create product auction try again later",
		})
		return
	}

	ctx, _ := context.WithDeadline(context.Background(), data.AuctionEnd)

	auctionRoom := services.NewAuctionRoom(ctx, api.BidsService, productId)

	go auctionRoom.Run()

	api.AuctionLobby.Lock()
	api.AuctionLobby.Rooms[productId] = auctionRoom
	api.AuctionLobby.Unlock()

	jsonutils.EncodeJson(w, r, http.StatusCreated, map[string]any{
		"message": "auction has started with success",
		"id":      productId,
	})
}
