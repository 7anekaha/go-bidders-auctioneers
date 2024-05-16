package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	pb "github.com/7anekaha/go-bidder-auctioner/proto"
	"github.com/google/uuid"
)

type Client struct {
	UserID         string
	streamAuctions pb.AdService_ListenRequestsServer
	errorChan      chan error
	adsSent        map[string]bool
}

type status int

const (
	CREATED int = iota
	OPEN
	CLOSED
)

type Ad struct {
	ID          string
	Duration    int64
	CreatedAt   time.Time
	CurrentBid  int64
	CurrentUser string
	BidID       string
	MinBid      float64
	Status      status
	NumBids     int
}

type AuctionServer struct {
	pb.UnimplementedAdServiceServer
	Done chan bool

	muClients sync.Mutex
	Clients   map[string]*Client

	muAds sync.RWMutex
	Ads   map[string]*Ad

	muAdsClosed sync.RWMutex
	AdsClosed   map[string]*Ad
}

func (s *AuctionServer) Connect(ctx context.Context, user *pb.UserRequest) (*pb.UserResponse, error) {
	defer s.muClients.Unlock()

	userId := hex.EncodeToString(sha256.New().Sum([]byte(user.Name + "-" + time.Now().String()))) // use normally this, but for testing purposes we use user.Name
	userId = user.Name

	client := &Client{
		UserID:         userId,
		streamAuctions: nil,
		errorChan:      make(chan error),
		adsSent:        make(map[string]bool),
	}

	s.muClients.Lock()
	s.Clients[userId] = client
	log.Println("Client connected: ", userId)

	return &pb.UserResponse{
		UserID: userId,
	}, nil
}

func (s *AuctionServer) ListenRequests(user *pb.UserResponse, stream pb.AdService_ListenRequestsServer) error {
	for {
		select {
		case <-s.Done:
			log.Println("Server is shutting down")
			return nil
		default:
			// send ads
			s.muAds.RLock()
			for _, ad := range s.Ads {
				client, ok := s.Clients[user.UserID]

				if !ok {
					log.Printf("Client (%v) not found\n", user.UserID)
					return fmt.Errorf("Client not found")
				}
				if ad.Status == status(CLOSED) {
					continue
				}

				if ad.Status == status(CREATED) {
					ad.Status = status(OPEN)
				}

				// ad already sent
				if _, ok := client.adsSent[ad.ID]; ok {
					continue
				}

				stream.Send(&pb.AdRequest{
					AdID:           ad.ID,
					StartTimestamp: ad.CreatedAt.Unix(),
					Duration:       ad.Duration,
					Amount:         ad.CurrentBid,
				})

				client.adsSent[ad.ID] = true
			}
			s.muAds.RUnlock()

			time.Sleep(time.Millisecond * 500)
		}
	}
}

func (s *AuctionServer) Bid(ctx context.Context, req *pb.AdResponse) (*pb.AdStatus, error) {
	defer s.muAds.Unlock()
	// get client
	client, ok := s.Clients[req.UserID]
	if !ok {
		log.Println("Client not found")
		return &pb.AdStatus{
			Error: pb.Error_CLIENT_NOT_FOUND,
		}, nil
	}

	// get ad
	s.muAds.Lock()
	ad, ok := s.Ads[req.AdID]
	if !ok {
		// check if ad is closed
		s.muAdsClosed.RLock()
		_, found := s.AdsClosed[req.AdID]
		s.muAdsClosed.RUnlock()

		if found {
			return &pb.AdStatus{
				Error: pb.Error_AD_CLOSED,
			}, nil
		}

		log.Printf("Ad (%v) not found", req.AdID)
		return &pb.AdStatus{
			Error: pb.Error_AD_NOT_FOUND,
		}, nil
	}

	timeLeft := int64(ad.CreatedAt.Add(time.Duration(ad.Duration) * time.Second).Sub(time.Now()).Seconds())
	// check auction is CLOSED
	if ad.Status == status(CLOSED) || timeLeft <= 0 {
		log.Println("[Auctioneer] Auction is CLOSED or Time is up")
		ad.Status = status(CLOSED)

		// send final status
		return &pb.AdStatus{
			AdID:     req.AdID,
			BidID:    "",
			Status:   pb.Status_CLOSED,
			Amount:   ad.CurrentBid,
			TimeLeft: 0,
			UserID:   ad.CurrentUser,
			Error:    pb.Error_AD_CLOSED,
		}, nil
	}

	// update current bid
	bidID := uuid.New().String() // generate new bid ID
	if ad.CurrentBid < req.Amount {
		ad.CurrentBid = req.Amount
		ad.CurrentUser = client.UserID
		ad.BidID = bidID
	}

	// update number of bids
	ad.NumBids++

	// send bid response
	return &pb.AdStatus{
		AdID:     req.AdID,
		Status:   pb.Status_OPEN,
		BidID:    bidID,
		Amount:   ad.CurrentBid,
		TimeLeft: timeLeft,
		UserID:   ad.CurrentUser,
		Error:    pb.Error_NO_ERROR,
	}, nil
}

func CleanClosedAds(done chan bool, s *AuctionServer) {
	for {
		select {
		case <-done:
			return
		default:
			s.muAdsClosed.RLock()

			for _, ad := range s.Ads {
				// Check if the ad has expired
				if ad.CreatedAt.Add(time.Duration(ad.Duration)*time.Second).Sub(time.Now()) <= 0 {

					// Upgrade the lock to write lock
					s.muAdsClosed.RUnlock()
					s.muAdsClosed.Lock()

					// Recheck the ad status under write lock
					if ad.Status == status(CLOSED) {

						// Log and move the ad to closed ads
						log.Printf("[Auctionner] Ad %s is closed. Ad info: %+v\n", ad.ID, ad)
						s.AdsClosed[ad.ID] = ad

						// Remove the ad from the active ads
						delete(s.Ads, ad.ID)
					}

					// Release the write lock
					s.muAdsClosed.Unlock()

					// Reacquire read lock
					s.muAdsClosed.RLock()
				}
			}

			s.muAdsClosed.RUnlock()

			time.Sleep(time.Second * 2)
		}
	}
}

func StartNewAuctions(server *AuctionServer) {

	server.Ads["ad-1"] = &Ad{
		ID:          "ad-1",
		Duration:    50, //seconds
		CreatedAt:   time.Now(),
		CurrentBid:  50,
		CurrentUser: "",
		MinBid:      50,
		Status:      status(CREATED),
	}
	server.Ads["ad-2"] = &Ad{
		ID:          "ad-2",
		Duration:    20,
		CreatedAt:   time.Now(),
		CurrentBid:  100,
		CurrentUser: "",
		MinBid:      100,
		Status:      status(CREATED),
	}
	server.Ads["ad-3"] = &Ad{
		ID:          "ad-3",
		Duration:    10,
		CreatedAt:   time.Now(),
		CurrentBid:  200,
		CurrentUser: "",
		MinBid:      200,
		Status:      status(CREATED),
	}
}
