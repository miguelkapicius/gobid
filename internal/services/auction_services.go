package services

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type MessageKind int

const (
	// Requests
	PlaceBid MessageKind = iota

	// Ok/Success
	SuccessfullyPlacedBid

	// Errors
	FailedToPlaceBid
	InvalidJSON

	// Info
	NewBidPlaced
	AuctionFinished
)

type Client struct {
	Room   *AuctionRoom
	Conn   *websocket.Conn
	UserId uuid.UUID
	Send   chan Message
}

type Message struct {
	Message string      `json:"message,omitempty"`
	Kind    MessageKind `json:"kind"`
	UserId  uuid.UUID   `json:"user_id,omitempty"`
	Amount  float64     `json:"amount,omitempty"`
}

type AuctionLobby struct {
	sync.Mutex
	Rooms map[uuid.UUID]*AuctionRoom
}

type AuctionRoom struct {
	Id         uuid.UUID
	Context    context.Context
	Broadcast  chan Message
	Clients    map[uuid.UUID]*Client
	Register   chan *Client
	Unregister chan *Client

	BidsService BidsService
}

func NewAuctionRoom(ctx context.Context, BidsService BidsService, id uuid.UUID) *AuctionRoom {
	return &AuctionRoom{
		Id:          id,
		Broadcast:   make(chan Message),
		Register:    make(chan *Client),
		Unregister:  make(chan *Client),
		Clients:     make(map[uuid.UUID]*Client),
		Context:     ctx,
		BidsService: BidsService,
	}
}

func NewClient(room *AuctionRoom, conn *websocket.Conn, userId uuid.UUID) *Client {
	return &Client{
		Room:   room,
		Conn:   conn,
		Send:   make(chan Message, 512),
		UserId: userId,
	}
}

func (r *AuctionRoom) registerClient(c *Client) {
	slog.Info("New user connected", "Client", c)
	r.Clients[c.UserId] = c
}

func (r *AuctionRoom) unregisterClient(c *Client) {
	slog.Info("User disconnected", "Client", c)
	delete(r.Clients, c.UserId)
}

func (r *AuctionRoom) broadcastMessage(m Message) {
	slog.Info("New message received", "RoomID", r.Id, "message", m.Message, "UserID", m.UserId)

	switch m.Kind {
	case PlaceBid:
		bid, err := r.BidsService.PlaceBid(r.Context, r.Id, m.UserId, m.Amount)
		if err != nil {
			if errors.Is(err, ErrBidIsTooLow) {
				if client, ok := r.Clients[m.UserId]; ok {
					client.Send <- Message{Kind: FailedToPlaceBid, Message: ErrBidIsTooLow.Error()}
				}
				return
			}
		}

		if client, ok := r.Clients[m.UserId]; ok {
			client.Send <- Message{Kind: SuccessfullyPlacedBid, Message: "Your bid was successfully placed"}
		}

		for id, client := range r.Clients {
			newBidMessage := Message{
				Kind:    NewBidPlaced,
				Message: "A new bid was placed",
				Amount:  bid.BidAmount,
			}

			if id == m.UserId {
				continue
			}

			client.Send <- newBidMessage
		}

	case InvalidJSON:
		client, ok := r.Clients[m.UserId]
		if !ok {
			slog.Info("Client not found in hashmap", "user_id", m.UserId)
			return
		}

		client.Send <- m
	}
}

func (r *AuctionRoom) Run() {
	slog.Info("Room has been started", "auctionId", r.Id)
	defer func() {
		close(r.Broadcast)
		close(r.Register)
		close(r.Unregister)
	}()

	for {
		select {
		case client := <-r.Register:
			r.registerClient(client)
		case client := <-r.Unregister:
			r.unregisterClient(client)
		case message := <-r.Broadcast:
			r.broadcastMessage(message)
		case <-r.Context.Done():
			slog.Info("auction has ended.", "AuctionID", r.Id)
			for _, client := range r.Clients {
				client.Send <- Message{
					Kind:    AuctionFinished,
					Message: "auction has been finished",
				}
			}
			return
		}
	}
}

const (
	maxMessageSize = 512
	readDeadline   = 60 * time.Second
	writeDeadline  = 10 * time.Second
	pingPeriod     = (readDeadline * 9) / 10
)

func (c *Client) ReadEventLoop() {
	defer func() {
		c.Room.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(readDeadline))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(readDeadline))
		return nil
	})

	for {
		var m Message
		m.UserId = c.UserId
		err := c.Conn.ReadJSON(&m)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Error("unexpected close error", "error", err)
				return
			}

			c.Room.Broadcast <- Message{
				Kind:    InvalidJSON,
				Message: "this message should be a valid json",
				UserId:  m.UserId,
			}

			continue
		}

		c.Room.Broadcast <- m
	}
}

func (c *Client) WriteEventLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.Conn.WriteJSON(Message{
					Kind:    websocket.CloseMessage,
					Message: "closing websocket connection",
				})
				return
			}

			if message.Kind == AuctionFinished {
				close(c.Send)
				return
			}

			err := c.Conn.WriteJSON(message)

			if err != nil {
				c.Room.Unregister <- c
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeDeadline))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				slog.Error("unexpected write error", "error", err)
				return
			}
		}
	}
}
